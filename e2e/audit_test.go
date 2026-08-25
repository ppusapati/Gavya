//go:build e2e

// The audit trail, against a real database.
//
// Two claims are being tested and they are separate. That a change and its
// record are written together, so neither can exist without the other. And that
// the record cannot be altered afterwards without it showing.
package e2e

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

// testIDs counts across the whole run, not per instance. A per-instance counter
// meant two separately-constructed generators both produced the first id, which
// collided on the primary key — and would have gone unnoticed in any test that
// wrote only one record.
type testIDs struct{}

var auditIDCounter atomic.Int64

func (testIDs) New() string { return "AA" + pad(int(auditIDCounter.Add(1))) }

func pad(n int) string {
	s := ""
	for i := 0; i < 24; i++ {
		s += string(rune('0' + (n % 10)))
		n /= 10
	}
	return s
}

// auditing returns the owner connection, a tenant-scoped pool as the application
// role, and applies the tamper-evidence schema on top of the isolated database.
func auditing(t *testing.T) (owner *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	owner, _ = isolated(t)
	seedIdentity(t, owner)

	root := workspaceRoot(t)
	applySQL(t, owner, root, "services/audit-service/internal/db/tamper_evidence.sql")

	cfg, err := pgxpool.ParseConfig(asRole(dsn(t, "e2e_isolation"), "gavya_app"))
	if err != nil {
		t.Fatal(err)
	}
	// Small, so a test that leaks a transaction is caught rather than absorbed —
	// but not so small that the first leak hangs the whole run waiting for a
	// connection that is never coming back.
	cfg.MaxConns = 4
	tenantdb.Configure(cfg)
	pool, err = pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return owner, pool
}

func acting(tenant, user string) context.Context {
	return tenantctx.WithActor(
		tenantdb.WithTenant(context.Background(), tenant),
		tenantctx.Actor{ID: user})
}

// The claim the whole design rests on. If the audit write fails, the change it
// describes must fail with it.
func TestAChangeAndItsRecordAreWrittenTogetherOrNotAtAll(t *testing.T) {
	owner, pool := auditing(t)
	ctx := acting(alpha, "US_ALPHA_ONLY_000000000000")

	// A transaction that writes a cattle row and then an audit record that
	// cannot be written, because its resource id is missing.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cattle (id,tenant_id,tag_number,gender,created_by,updated_by)
		VALUES ('C_TOGETHER_00000000000000',$1,'TOGETHER','F','US_1','US_1')`, alpha); err != nil {
		t.Fatal(err)
	}
	err = audit.Write(ctx, tx, testIDs{}, audit.Entry{
		Action: "register_cattle", ResourceType: "cattle", // no ResourceID
		ServiceName: "cattle-service",
	})
	if err == nil {
		t.Fatal("an unattributable record was accepted")
	}
	_ = tx.Rollback(ctx)

	// The animal is not there, because the transaction that would have created
	// it is the one that failed.
	var n int
	if err := owner.QueryRow(context.Background(),
		"SELECT count(*) FROM cattle WHERE id='C_TOGETHER_00000000000000'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the change committed even though its record could not be written")
	}
}

func TestAChangeAndItsRecordBothLandOnSuccess(t *testing.T) {
	owner, pool := auditing(t)
	ctx := acting(alpha, "US_ALPHA_ONLY_000000000000")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cattle (id,tenant_id,tag_number,gender,created_by,updated_by)
		VALUES ('C_BOTH_000000000000000000',$1,'BOTH','F','US_1','US_1')`, alpha); err != nil {
		t.Fatal(err)
	}
	if err := audit.Write(ctx, tx, testIDs{}, audit.Entry{
		Action: "register_cattle", ResourceType: "cattle", ResourceID: "C_BOTH_000000000000000000",
		After: map[string]any{"tag_number": "BOTH"}, ServiceName: "cattle-service",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var actor, action, resource string
	if err := owner.QueryRow(context.Background(),
		`SELECT actor_id, action, resource_id FROM audit_logs
		 WHERE resource_id = 'C_BOTH_000000000000000000'`).Scan(&actor, &action, &resource); err != nil {
		t.Fatalf("no record for a change that committed: %v", err)
	}
	if actor != "US_ALPHA_ONLY_000000000000" || action != "register_cattle" {
		t.Errorf("recorded %s doing %s", actor, action)
	}
}

// Audit rows belong to a tenant like everything else.
func TestOneTenantCannotReadAnothersAuditTrail(t *testing.T) {
	owner, pool := auditing(t)

	for _, tc := range []struct{ tenant, user, resource string }{
		{alpha, "US_ALPHA_ONLY_000000000000", "R_ALPHA_00000000000000000"},
		{beta, "US_BETA_ONLY_0000000000000", "R_BETA_000000000000000000"},
	} {
		ctx := acting(tc.tenant, tc.user)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if err := audit.Write(ctx, tx, testIDs{}, audit.Entry{
			Action: "record_milk", ResourceType: "milk_record", ResourceID: tc.resource,
			ServiceName: "milk-service",
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	ctx := acting(alpha, "US_ALPHA_ONLY_000000000000")
	rows, err := pool.Query(ctx, "SELECT resource_id FROM audit_logs ORDER BY resource_id")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "R_ALPHA_00000000000000000" {
		t.Errorf("alpha sees %v, want only its own", seen)
	}

	var total int
	if err := owner.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("%d rows written in total, want 2 — otherwise this proves nothing", total)
	}
}

// The application may add to the trail and may not edit it. A grant rather than
// a convention, because convention is what "we never update audit rows" is until
// somebody writes a cleanup script.
func TestTheApplicationCannotChangeOrRemoveAnAuditRow(t *testing.T) {
	_, pool := auditing(t)
	ctx := acting(alpha, "US_ALPHA_ONLY_000000000000")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := audit.Write(ctx, tx, testIDs{}, audit.Entry{
		Action: "record_milk", ResourceType: "milk_record", ResourceID: "R_FIXED_00000000000000000",
		ServiceName: "milk-service",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, sql string }{
		{"update", "UPDATE audit_logs SET action = 'something_else'"},
		{"delete", "DELETE FROM audit_logs"},
	} {
		_, err := pool.Exec(ctx, tc.sql)
		if err == nil {
			t.Errorf("the application could %s an audit row", tc.name)
			continue
		}
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Errorf("%s refused with %v, want a privilege error", tc.name, err)
		}
	}
}

// And the owner cannot either, which is the case a grant does not cover: the
// owner is who runs the migration that was going to tidy up the old rows.
func TestEvenTheOwnerCannotEditASealedAuditRow(t *testing.T) {
	owner, pool := auditing(t)
	ctx := acting(alpha, "US_ALPHA_ONLY_000000000000")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := audit.Write(ctx, tx, testIDs{}, audit.Entry{
		Action: "record_milk", ResourceType: "milk_record", ResourceID: "R_OWNER_00000000000000000",
		ServiceName: "milk-service",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := owner.Exec(context.Background(), "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(context.Background(),
		"UPDATE audit_logs SET action = 'something_else'"); err == nil {
		t.Error("the owner edited a sealed audit row")
	}
	if _, err := owner.Exec(context.Background(), "DELETE FROM audit_logs"); err == nil {
		t.Error("the owner deleted an audit row")
	}
}

// -------------------------------------------------------------------------
// The chain
// -------------------------------------------------------------------------

func writeEntries(t *testing.T, pool *pgxpool.Pool, tenant, user string, n int) {
	t.Helper()
	ids := testIDs{}
	ctx := acting(tenant, user)
	for i := 1; i <= n; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if err := audit.Write(ctx, tx, ids, audit.Entry{
			Action: "record_milk", ResourceType: "milk_record",
			// 26 characters exactly, which is what an id column holds.
			ResourceID:  "R" + pad(i) + "R",
			After:       map[string]any{"litres": i},
			ServiceName: "milk-service",
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func verify(t *testing.T, owner *pgx.Conn, tenant string) (bool, string) {
	t.Helper()
	var intact bool
	var checked int64
	var brokenAt *int64
	var brokenID, detail *string
	if err := owner.QueryRow(context.Background(),
		"SELECT intact, rows_checked, broken_at, broken_id, detail FROM gavya_verify_audit_chain($1)",
		tenant).Scan(&intact, &checked, &brokenAt, &brokenID, &detail); err != nil {
		t.Fatal(err)
	}
	return intact, deref(detail)
}

func TestASealedChainVerifies(t *testing.T) {
	owner, pool := auditing(t)
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 5)

	if _, err := owner.Exec(context.Background(), "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}
	if intact, detail := verify(t, owner, alpha); !intact {
		t.Errorf("a freshly sealed chain does not verify: %s", detail)
	}
}

// Sealing twice over the same rows must produce the same chain, or two operators
// running the job would disagree about what the trail says.
func TestSealingIsRepeatableAndDoesNotDuplicate(t *testing.T) {
	owner, pool := auditing(t)
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 4)
	ctx := context.Background()

	var sealed, lastSeq int64
	var lastHash string
	if err := owner.QueryRow(ctx, "SELECT sealed, last_seq, last_hash FROM gavya_seal_audit_log($1)", alpha).
		Scan(&sealed, &lastSeq, &lastHash); err != nil {
		t.Fatal(err)
	}
	if sealed != 4 || lastSeq != 4 {
		t.Fatalf("sealed %d to seq %d, want 4 and 4", sealed, lastSeq)
	}

	var again, seq2 int64
	var hash2 string
	if err := owner.QueryRow(ctx, "SELECT sealed, last_seq, last_hash FROM gavya_seal_audit_log($1)", alpha).
		Scan(&again, &seq2, &hash2); err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("a second seal claimed %d more rows", again)
	}
	if hash2 != lastHash {
		t.Error("sealing twice produced a different head")
	}
}

// New rows continue the chain rather than starting a new one.
func TestRowsWrittenAfterASealExtendTheSameChain(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 3)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 3)

	var sealed, lastSeq int64
	var lastHash string
	if err := owner.QueryRow(ctx, "SELECT sealed, last_seq, last_hash FROM gavya_seal_audit_log($1)", alpha).
		Scan(&sealed, &lastSeq, &lastHash); err != nil {
		t.Fatal(err)
	}
	if sealed != 3 || lastSeq != 6 {
		t.Errorf("sealed %d rows to seq %d, want 3 and 6", sealed, lastSeq)
	}
	if intact, detail := verify(t, owner, alpha); !intact {
		t.Errorf("the extended chain does not verify: %s", detail)
	}
}

// Changing a field breaks the row's own hash. Done with the trigger disabled,
// which is what somebody with database access would do.
func TestChangingASealedRowIsDetected(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 5)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}

	withTriggerOff(t, owner, func() {
		if _, err := owner.Exec(ctx,
			`UPDATE audit_logs SET new_value = '{"litres": 999}' WHERE seq = 3 AND tenant_id = $1`,
			alpha); err != nil {
			t.Fatal(err)
		}
	})

	intact, detail := verify(t, owner, alpha)
	if intact {
		t.Fatal("an edited row verified")
	}
	if detail == "" {
		t.Error("the break was not described")
	}
}

// Removing a row from the middle breaks the links after it.
func TestRemovingARowFromTheMiddleIsDetected(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 5)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}

	withTriggerOff(t, owner, func() {
		if _, err := owner.Exec(ctx, "DELETE FROM audit_logs WHERE seq = 3 AND tenant_id = $1", alpha); err != nil {
			t.Fatal(err)
		}
	})

	if intact, _ := verify(t, owner, alpha); intact {
		t.Fatal("a chain with a row removed from the middle verified")
	}
}

// The case a hash chain alone cannot see: cutting off the end leaves a shorter
// chain that verifies perfectly. This is what the anchor is for, and this test
// is the reason the anchor exists rather than a nice-to-have.
func TestCuttingOffTheEndIsDetectedOnlyBecauseOfTheAnchor(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 5)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}

	// Without an anchor, truncation is invisible. Shown rather than asserted
	// from theory, because it is the limitation the design is built around.
	withTriggerOff(t, owner, func() {
		if _, err := owner.Exec(ctx, "DELETE FROM audit_logs WHERE seq >= 4 AND tenant_id = $1", alpha); err != nil {
			t.Fatal(err)
		}
	})
	if intact, _ := verify(t, owner, alpha); !intact {
		t.Fatal("this test's premise is wrong: truncation was caught with no anchor, so the " +
			"anchor may not be doing what the comments say")
	}

	// Now with one. Rebuild, anchor, then truncate.
	withTriggerOff(t, owner, func() {
		if _, err := owner.Exec(ctx, "DELETE FROM audit_logs WHERE tenant_id = $1", alpha); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := owner.Exec(ctx, "DELETE FROM audit_chain_checkpoints WHERE tenant_id = $1", alpha); err != nil {
		t.Fatal(err)
	}
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 5)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, "SELECT gavya_checkpoint_audit_chain($1)", alpha); err != nil {
		t.Fatal(err)
	}

	withTriggerOff(t, owner, func() {
		if _, err := owner.Exec(ctx, "DELETE FROM audit_logs WHERE seq >= 4 AND tenant_id = $1", alpha); err != nil {
			t.Fatal(err)
		}
	})
	intact, detail := verify(t, owner, alpha)
	if intact {
		t.Fatal("truncation was not detected even with an anchor")
	}
	if !contains(detail, "removed from the end") {
		t.Errorf("the finding does not name what happened: %s", detail)
	}
}

// The status has to say how much of the trail is not yet covered, because
// sealing happens after the fact and "the last few seconds are not tamper-
// evident" should be visible rather than assumed.
func TestTheStatusSaysHowMuchIsNotYetSealed(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 3)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 2)

	var total, sealed, unsealed int64
	var unsealedFor *time.Duration
	var oldest *time.Time
	if err := owner.QueryRow(ctx, `
		SELECT total_rows, sealed_rows, unsealed_rows, oldest_unsealed_at
		FROM gavya_audit_chain_status WHERE tenant_id = $1`, alpha).
		Scan(&total, &sealed, &unsealed, &oldest); err != nil {
		t.Fatal(err)
	}
	_ = unsealedFor
	if total != 5 || sealed != 3 || unsealed != 2 {
		t.Errorf("status says %d total, %d sealed, %d unsealed; want 5, 3, 2", total, sealed, unsealed)
	}
	if oldest == nil {
		t.Error("the status does not say how old the oldest unsealed row is")
	}
}

// Two tenants have independent chains: one tenant's rows must not affect
// another's sequence or hashes.
func TestEachTenantHasItsOwnChain(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 3)
	writeEntries(t, pool, beta, "US_BETA_ONLY_0000000000000", 2)

	for _, tenant := range []string{alpha, beta} {
		if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", tenant); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		tenant string
		want   int64
	}{{alpha, 3}, {beta, 2}} {
		var last int64
		if err := owner.QueryRow(ctx,
			"SELECT max(seq) FROM audit_logs WHERE tenant_id = $1", tc.tenant).Scan(&last); err != nil {
			t.Fatal(err)
		}
		if last != tc.want {
			t.Errorf("%s's chain ends at %d, want %d", tc.tenant, last, tc.want)
		}
		if intact, detail := verify(t, owner, tc.tenant); !intact {
			t.Errorf("%s's chain does not verify: %s", tc.tenant, detail)
		}
	}
}

func withTriggerOff(t *testing.T, owner *pgx.Conn, f func()) {
	t.Helper()
	ctx := context.Background()
	if _, err := owner.Exec(ctx, "ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_append_only"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := owner.Exec(ctx, "ALTER TABLE audit_logs ENABLE TRIGGER audit_logs_append_only"); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The guard that stops a sealed row being re-sealed.
//
// Rewriting the chain columns of a sealed row changes no content, so the
// content guard does not fire and verification still passes afterwards — the
// chain is simply a different, self-consistent chain. That is how somebody
// removes a row and re-links what remains, and it is a separate hole from
// editing a field.
//
// This exists because mutating the "already sealed" check away left every other
// audit test passing.
func TestASealedRowCannotBeResealed(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 3)
	if _, err := owner.Exec(ctx, "SELECT gavya_seal_audit_log($1)", alpha); err != nil {
		t.Fatal(err)
	}

	// No content changes: only the chain columns, which is what re-linking a
	// shortened chain would do.
	_, err := owner.Exec(ctx, `
		UPDATE audit_logs SET seq = seq + 10, previous_hash = 'deadbeef', row_hash = 'deadbeef'
		WHERE tenant_id = $1 AND seq = 2`, alpha)
	if err == nil {
		t.Fatal("a sealed row was re-sealed, so the chain can be rewritten in place")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("refused with %v, want a privilege error naming the sealed row", err)
	}
}

// A row that has not been sealed may be sealed — that is the one update the
// table permits, and without it nothing could ever be chained.
func TestAnUnsealedRowCanStillBeSealed(t *testing.T) {
	owner, pool := auditing(t)
	ctx := context.Background()
	writeEntries(t, pool, alpha, "US_ALPHA_ONLY_000000000000", 2)

	var sealed int64
	if err := owner.QueryRow(ctx, "SELECT sealed FROM gavya_seal_audit_log($1)", alpha).Scan(&sealed); err != nil {
		t.Fatalf("sealing was refused by the append-only trigger: %v", err)
	}
	if sealed != 2 {
		t.Errorf("sealed %d rows, want 2", sealed)
	}
}
