//go:build e2e

package e2e

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

// pooled returns a one-connection pool as the unprivileged application role,
// against a database with the isolation policies applied.
//
// One connection on purpose. Tenant leakage between requests is a property of
// connection reuse, and a pool with room to spare hides it by handing each
// request a connection of its own — so a test on a default-sized pool can pass
// while the defect is present.
func pooled(t *testing.T) *pgxpool.Pool {
	t.Helper()
	owner, _ := isolated(t)
	_ = owner

	cfg, err := pgxpool.ParseConfig(asRole(dsn(t, "e2e_isolation"), "gavya_app"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 1
	tenantdb.Configure(cfg)

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// The defect this exists for: one connection, used by one tenant and then handed
// to another, still carrying the first tenant's identity. Every request would
// succeed and the second tenant would read the first one's milk.
func TestOnePooledConnectionServingTwoTenantsInTurnDoesNotLeak(t *testing.T) {
	pool := pooled(t)

	for _, tc := range []struct{ tenant, want string }{
		{alpha, "ALPHA-1"},
		{beta, "BETA-1"},
		{alpha, "ALPHA-1"}, // back again, on the connection beta just used
		{beta, "BETA-1"},
	} {
		ctx := tenantdb.WithTenant(context.Background(), tc.tenant)
		var n int
		var tags *string
		if err := pool.QueryRow(ctx,
			"SELECT count(*), string_agg(tag_number, ',') FROM cattle").Scan(&n, &tags); err != nil {
			t.Fatalf("%s: %v", tc.tenant, err)
		}
		if n != 1 || tags == nil || *tags != tc.want {
			t.Fatalf("%s saw %d rows (%s), want only %q", tc.tenant, n, deref(tags), tc.want)
		}
	}
}

// A request with no tenant, arriving on a connection the previous request left
// scoped, must be refused rather than answered as the previous tenant. This is
// the case that decides whether the tenant is set on the way out of the pool or
// merely cleared on the way back in.
func TestARequestWithNoTenantIsRefusedEvenOnAWarmConnection(t *testing.T) {
	pool := pooled(t)
	ctx := context.Background()

	// Warm the connection with a real tenant.
	var n int
	if err := pool.QueryRow(tenantdb.WithTenant(ctx, alpha),
		"SELECT count(*) FROM cattle").Scan(&n); err != nil {
		t.Fatalf("the scoped query failed: %v", err)
	}

	err := pool.QueryRow(ctx, "SELECT count(*) FROM cattle").Scan(&n)
	if err == nil {
		t.Fatalf("a request with no tenant read %d rows on a connection alpha had just used", n)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("failed with %v, want a refusal naming the missing tenant", err)
	}
}

// A transaction acquires its connection once and holds it, so the tenant has to
// be on it before BEGIN. Setting it inside the transaction would be rolled back
// with everything else on failure, leaving the connection unscoped for whoever
// gets it next.
func TestATransactionCarriesTheTenantOfTheContextThatOpenedIt(t *testing.T) {
	pool := pooled(t)
	ctx := tenantdb.WithTenant(context.Background(), beta)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var tag string
	if err := tx.QueryRow(ctx, "SELECT tag_number FROM cattle").Scan(&tag); err != nil {
		t.Fatalf("read inside the transaction: %v", err)
	}
	if tag != "BETA-1" {
		t.Errorf("the transaction saw %q, want beta's own animal", tag)
	}
}

// The tenant must survive a rolled-back transaction. If it were set with SET
// LOCAL inside the transaction, the rollback would take it with it and the next
// request on this connection would be unscoped.
func TestARolledBackTransactionDoesNotStripTheTenant(t *testing.T) {
	pool := pooled(t)
	ctx := tenantdb.WithTenant(context.Background(), alpha)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Fail inside the transaction, the way a constraint violation would.
	if _, err := tx.Exec(ctx, "SELECT 1/0"); err == nil {
		t.Fatal("the deliberate failure did not fail")
	}
	_ = tx.Rollback(ctx)

	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM cattle").Scan(&n); err != nil {
		t.Fatalf("the connection was unusable after a rollback: %v", err)
	}
	if n != 1 {
		t.Errorf("saw %d rows after a rollback, want alpha's 1", n)
	}
}

// Requests do not arrive one at a time. With a single connection they queue, and
// each has to be re-scoped as it reaches the front.
func TestConcurrentRequestsForDifferentTenantsStayApart(t *testing.T) {
	pool := pooled(t)

	const rounds = 25
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)

	check := func(tenant, want string) {
		defer wg.Done()
		ctx := tenantdb.WithTenant(context.Background(), tenant)
		var tag *string
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*), string_agg(tag_number, ',') FROM cattle").Scan(&n, &tag); err != nil {
			errs <- err
			return
		}
		if n != 1 || tag == nil || *tag != want {
			errs <- errors.New(tenant + " saw " + deref(tag) + ", want " + want)
		}
	}

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go check(alpha, "ALPHA-1")
		go check(beta, "BETA-1")
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}
