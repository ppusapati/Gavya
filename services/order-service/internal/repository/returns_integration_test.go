package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Two approvals racing each other cannot both find room on one order.
//
// The trigger sums what is already committed against the order and compares it
// with what the order charged. Written with that sum taken before the order row
// is locked, two concurrent approvals of 60 both passed against an order of 100
// — each reading the other's uncommitted row as absent. Measured, not reasoned
// about.
//
// This is a transaction-level test rather than an end-to-end one, and that is
// the point. The e2e version fires four approvals through HTTP and passes
// whichever way round the trigger's two statements are written: the window is
// too narrow to hit from out there, so it is a guard that reports success
// without exercising anything. Holding one transaction open and stepping the
// other into it is the only way to make the interleaving happen every time
// rather than sometimes.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...
func TestTwoApprovalsRacingCannotBothFindRoom(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tenant, order := newTestID("tnt"), newTestID("ord")
	seedOrderWorth(t, pool, tenant, order, "100.00")

	// A holds a transaction open with an approved return of 60 in it, uncommitted.
	a, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Rollback(context.Background())
	insertApproved(t, a, tenant, order, newTestID("ret"), "60.00")

	// B tries the same. It must wait for A — the trigger locks the order — and
	// once A commits it must see A's 60 and refuse.
	done := make(chan error, 1)
	go func() {
		b, err := pool.Begin(context.Background())
		if err != nil {
			done <- err
			return
		}
		defer b.Rollback(context.Background())
		_, err = b.Exec(context.Background(),
			`INSERT INTO returns (id,tenant_id,order_id,status,refund_amount,currency,created_by,updated_by)
			 VALUES ($1,$2,$3,'approved',$4::numeric,'INR','test','test')`,
			newTestID("ret"), tenant, order, "60.00")
		if err != nil {
			done <- err
			return
		}
		done <- b.Commit(context.Background())
	}()

	// B should still be waiting. If it has already answered, the trigger did not
	// take the lock and the rest of this test is theatre.
	select {
	case err := <-done:
		t.Fatalf("the second approval answered (%v) while the first was still open; "+
			"the trigger did not lock the order, so nothing here is serialised", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := a.Commit(ctx); err != nil {
		t.Fatalf("commit the first approval: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("both approvals of 60 succeeded against an order of 100; the second " +
				"summed the first's uncommitted row as absent, which is what happens when " +
				"the sum is taken before the order is locked")
		} else if !strings.Contains(err.Error(), "would take the total refunded") {
			t.Errorf("the second approval failed for the wrong reason: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second approval never answered after the first committed")
	}

	// Exactly 60 is committed.
	var total string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(refund_amount),0)::text FROM returns
		  WHERE order_id=$1 AND tenant_id=$2 AND status IN ('approved','completed')`,
		order, tenant).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(total, "60") {
		t.Errorf("%s is committed against an order of 100, want 60", total)
	}
}

// The exact remainder is allowed. Without this the refusal above is satisfied by
// a trigger that refuses everything.
func TestTheExactRemainderMayBeRefunded(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tenant, order := newTestID("tnt"), newTestID("ord")
	seedOrderWorth(t, pool, tenant, order, "100.00")

	for _, amount := range []string{"60.00", "40.00"} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		insertApproved(t, tx, tenant, order, newTestID("ret"), amount)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("approve %s: %v", amount, err)
		}
	}

	// And one more penny is not.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx,
		`INSERT INTO returns (id,tenant_id,order_id,status,refund_amount,currency,created_by,updated_by)
		 VALUES ($1,$2,$3,'approved',$4::numeric,'INR','test','test')`,
		newTestID("ret"), tenant, order, "0.01"); err == nil {
		t.Error("a further penny was refunded on an order already refunded in full")
	}
}

// A request may ask for anything; only committing it is bounded.
func TestARequestedReturnIsNotBoundedByTheOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tenant, order := newTestID("tnt"), newTestID("ord")
	seedOrderWorth(t, pool, tenant, order, "100.00")

	if _, err := pool.Exec(ctx,
		`INSERT INTO returns (id,tenant_id,order_id,status,refund_amount,currency,created_by,updated_by)
		 VALUES ($1,$2,$3,'requested',$4::numeric,'INR','test','test')`,
		newTestID("ret"), tenant, order, "500.00"); err != nil {
		t.Errorf("a request for more than the order charged was refused: %v\n"+
			"What somebody asked for is part of the record of what was decided, and "+
			"refusing to write it down loses the fact that it was asked", err)
	}
}

// seedOrderWorth writes one confirmed order with the given total, which is what
// a refund is measured against.
func seedOrderWorth(t *testing.T, pool *pgxpool.Pool, tenant, order, total string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO orders (id,tenant_id,customer_id,order_number,status,total_amount,currency,ordered_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,'confirmed',$5::numeric,'INR',NOW(),'test','test')`,
		order, tenant, newTestID("cst"), newTestID("num"), total); err != nil {
		t.Fatalf("seed an order worth %s: %v", total, err)
	}
}

func insertApproved(t *testing.T, tx pgx.Tx, tenant, order, id, amount string) {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		`INSERT INTO returns (id,tenant_id,order_id,status,refund_amount,currency,created_by,updated_by)
		 VALUES ($1,$2,$3,'approved',$4::numeric,'INR','test','test')`,
		id, tenant, order, amount); err != nil {
		t.Fatalf("approve %s: %v", amount, err)
	}
}
