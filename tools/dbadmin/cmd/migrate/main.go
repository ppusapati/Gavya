// Command migrate applies the platform's schema to a database that already
// exists.
//
// deploy/postgres-init does this once, when a PostgreSQL data directory is first
// created, and never again. Everything after that had no procedure: a schema
// change reached a running deployment by somebody opening psql and applying
// files in an order recorded only in that script's comments. Two people doing it
// at once, or one person doing it in the wrong order, produces a database that
// looks right and is not.
//
// This is the other half. It applies the same files in the same order — the
// order lives in internal/schema now, and a test compares it against the init
// script so the two cannot drift — with three things the manual procedure did
// not have, and the refusals it had no way to make:
//
//   - An advisory lock, so two rollouts cannot apply at once. The second waits
//     rather than interleaving with the first.
//   - A record of what was applied, when, by whom, and the SHA-256 of each file
//     as it was at the time. The schemas in this platform are re-runnable and
//     are edited in place, so the hash is not a version to check against; it is
//     the answer to "what was actually applied on the ninth", which nothing
//     could answer before.
//   - The four refusals the init script ends with. The one that matters most on
//     a migration rather than on a fresh database is the unisolated table: a new
//     table arrives with a schema change, and the isolation sweep is what puts
//     row-level security on it. If the sweep did not reach it, the service works
//     and that one table serves every tenant.
//
// It is safe to run when nothing has changed. Every file is written to be
// re-runnable, and that property is checked from the other direction by
// e2e/upgrade_test.go, which builds one database from the current schema and
// another by applying every committed version in order and compares them.
//
// Run it as a Kubernetes Job before a rollout, or by hand:
//
//	migrate -dsn "postgres://postgres@host:5432/dairy?sslmode=verify-full" -root .
//
// The DSN must be a superuser: the isolation sweep alters every table and
// grants to the application role, which the application role may not do itself.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/tools/dbadmin/internal/schema"
)

// lockKey is the advisory lock every run of this command takes.
//
// An arbitrary constant, and it only has to be the same in every copy of this
// binary. PostgreSQL advisory locks are per-database, so two deployments sharing
// a server do not block each other.
const lockKey int64 = 0x6761767961 // "gavya"

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"),
		"a superuser connection to the database to migrate")
	root := flag.String("root", ".", "repository root")
	dryRun := flag.Bool("dry-run", false, "say what would be applied and change nothing")
	timeout := flag.Duration("lock-timeout", 5*time.Minute,
		"how long to wait for another migration to finish")
	flag.Parse()

	if err := run(*dsn, *root, *dryRun, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(dsn, root string, dryRun bool, timeout time.Duration) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("no database given: pass -dsn or set DATABASE_URL")
	}
	plan := schema.Plan()

	if dryRun {
		return describe(plan, root)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())

	// One migration at a time. Without this, two rollouts of the same release
	// interleave: one adds a column while the other sweeps isolation, and the
	// sweep misses the table the first is still creating.
	//
	// The lock is taken outside a transaction and held until this connection
	// closes, because the sweep functions below cannot all run inside one.
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := conn.Exec(lockCtx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("another migration is running and did not finish within %s: %w", timeout, err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	if err := ensureHistory(ctx, conn); err != nil {
		return err
	}
	first, err := isFirstRun(ctx, conn)
	if err != nil {
		return err
	}
	if first {
		fmt.Println("migrate: this database has no migration history; recording this run as its first")
	}

	by := whoami()
	started := time.Now()
	for _, step := range plan {
		files, err := step.Files(root)
		if err != nil {
			return err
		}
		if len(files) == 0 && step.Command == "" {
			continue
		}
		fmt.Printf("migrate: %s\n", step.Describe)

		for _, f := range files {
			rel, _ := filepath.Rel(root, f)
			sql, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			if _, err := conn.Exec(ctx, step.Prelude(f)); err != nil {
				return fmt.Errorf("prepare the schema for %s: %w", rel, err)
			}
			at := time.Now()
			if _, err := conn.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("apply %s: %w", rel, err)
			}
			if err := record(ctx, conn, rel, sum(sql), by, time.Since(at)); err != nil {
				return err
			}
			fmt.Printf("migrate:   %s\n", rel)
		}
		if step.Command != "" {
			// Back to public first. The commands are sweeps across every
			// schema and read nothing from the search path, but one that later
			// creates a table would create it wherever the last file left us.
			if _, err := conn.Exec(ctx, "SET search_path = public"); err != nil {
				return err
			}
			if _, err := conn.Exec(ctx, step.Command); err != nil {
				return fmt.Errorf("%s: %w", step.Describe, err)
			}
		}
	}

	// The same refusals the init script ends with. Coming up half-isolated is
	// worse than not coming up: the services work, and one table quietly serves
	// every tenant.
	for _, check := range []func(context.Context, *pgx.Conn) error{
		refuseUndecidedReferences,
		refuseAnUnisolatedTable,
		refuseACrossableForeignKey,
		refuseAWritableAuditTrail,
	} {
		if err := check(ctx, conn); err != nil {
			return err
		}
	}
	reportRefusedReferences(ctx, conn)

	fmt.Printf("migrate: done in %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

func describe(plan []schema.Step, root string) error {
	for _, step := range plan {
		files, err := step.Files(root)
		if err != nil {
			return err
		}
		fmt.Printf("would apply: %s\n", step.Describe)
		for _, f := range files {
			rel, _ := filepath.Rel(root, f)
			fmt.Printf("              %s\n", rel)
		}
		if step.Command != "" {
			fmt.Printf("              then: %s\n", step.Command)
		}
	}
	return nil
}

// ensureHistory creates the record this command keeps.
//
// Deliberately not a table any service's schema owns: it describes the act of
// migrating rather than anything the platform models, and a service schema that
// created it would be applied by the very step it is meant to record.
func ensureHistory(ctx context.Context, conn *pgx.Conn) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS gavya_schema_history (
    id          BIGSERIAL   PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_by  TEXT        NOT NULL,
    file        TEXT        NOT NULL,
    sha256      TEXT        NOT NULL,
    duration_ms INTEGER     NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gavya_schema_history_file
    ON gavya_schema_history (file, applied_at DESC);`
	if _, err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create the migration history: %w", err)
	}
	return nil
}

func isFirstRun(ctx context.Context, conn *pgx.Conn) (bool, error) {
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM gavya_schema_history").Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

func record(ctx context.Context, conn *pgx.Conn, file, hash, by string, took time.Duration) error {
	_, err := conn.Exec(ctx,
		`INSERT INTO gavya_schema_history (applied_by, file, sha256, duration_ms)
		 VALUES ($1, $2, $3, $4)`,
		by, file, hash, took.Milliseconds())
	if err != nil {
		return fmt.Errorf("record %s: %w", file, err)
	}
	return nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func whoami() string {
	if v := os.Getenv("GAVYA_MIGRATED_BY"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		if host, err := os.Hostname(); err == nil {
			return u.Username + "@" + host
		}
		return u.Username
	}
	return "unknown"
}

// refuseUndecidedReferences is the init script's check, and the reason for it is
// the same: a reference-shaped column nobody has decided about is a foreign key
// somebody forgot, or a column that only looks like one, and which of those it
// is cannot be guessed.
func refuseUndecidedReferences(ctx context.Context, conn *pgx.Conn) error {
	var n int
	err := conn.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM gavya_undecided_references)
		     + (SELECT count(*) FROM gavya_unguessable_references)`).Scan(&n)
	if err != nil {
		return fmt.Errorf("count undecided references: %w", err)
	}
	if n != 0 {
		return fmt.Errorf("%d reference-shaped column(s) have no decision recorded; add each to "+
			"gavya_reference_decisions in libs/integrity/isolation/references.sql, saying whether "+
			"it is a reference and why", n)
	}
	return nil
}

// refuseAWritableAuditTrail is the other one. A trail the application can edit
// is not evidence, and finishing without saying so is how nobody finds out until
// an auditor asks.
func refuseAWritableAuditTrail(ctx context.Context, conn *pgx.Conn) error {
	var n int
	err := conn.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.role_table_grants
		WHERE grantee = 'gavya_app' AND table_name = 'audit_logs'
		  AND privilege_type IN ('UPDATE', 'DELETE')`).Scan(&n)
	if err != nil {
		return fmt.Errorf("check the audit trail's grants: %w", err)
	}
	if n != 0 {
		return errors.New("the application role can still modify audit_logs")
	}
	return nil
}

// refuseAnUnisolatedTable is the check that matters most on a migration rather
// than on a fresh database.
//
// A new table arrives with a schema change, and the isolation sweep is what puts
// row-level security on it. If the sweep did not reach it — because the file that
// created it ran after the sweep, which is exactly what an out-of-order apply
// does — the service works and that one table serves every tenant.
func refuseAnUnisolatedTable(ctx context.Context, conn *pgx.Conn) error {
	rows, err := conn.Query(ctx, `
		SELECT schema_name, table_name FROM gavya_isolation_report
		WHERE (has_tenant_column OR table_name = 'tenants')
		  AND NOT (rls_enabled AND rls_forced)`)
	if err != nil {
		return fmt.Errorf("read the isolation report: %w", err)
	}
	defer rows.Close()
	var loose []string
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return err
		}
		loose = append(loose, schema+"."+table)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(loose) > 0 {
		return fmt.Errorf("%d tenant-owned table(s) are not isolated and would serve every "+
			"tenant: %s", len(loose), strings.Join(loose, ", "))
	}
	return nil
}

// refuseACrossableForeignKey is the same refusal for keys. One left crossable is
// one probe away from telling a tenant what exists in another.
func refuseACrossableForeignKey(ctx context.Context, conn *pgx.Conn) error {
	var n int
	err := conn.QueryRow(ctx, `
		SELECT count(*) FROM gavya_foreign_key_report
		WHERE both_sides_have_a_tenant AND NOT carries_the_tenant`).Scan(&n)
	if err != nil {
		return fmt.Errorf("read the foreign key report: %w", err)
	}
	if n != 0 {
		return fmt.Errorf("%d foreign key(s) can still cross a tenant boundary", n)
	}
	return nil
}

// reportRefusedReferences says what could not be enforced because the data
// already violates it.
//
// Printed rather than refused, and the init script does the same. A declared
// reference that existing rows break is a data problem somebody has to look at;
// refusing here would leave the database half-migrated over it, which is worse
// than finishing and saying so.
func reportRefusedReferences(ctx context.Context, conn *pgx.Conn) {
	rows, err := conn.Query(ctx,
		`SELECT constraint_name, outcome FROM gavya_enforce_references() WHERE outcome LIKE 'REFUSED%'`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: could not read which references were refused: %v\n", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, outcome string
		if err := rows.Scan(&name, &outcome); err != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "migrate: reference %s could not be enforced: %s\n", name, outcome)
	}
}
