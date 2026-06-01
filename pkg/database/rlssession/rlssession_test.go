package rlssession

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"p9e.in/samavaya/packages/p9context"
)

// fakeExecer captures every SQL statement passed to Exec so tests can
// assert the exact SET shape without a real Postgres.
type fakeExecer struct {
	stmts []string
	err   error // optional: returned from every Exec
}

func (f *fakeExecer) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.stmts = append(f.stmts, sql)
	return pgconn.CommandTag{}, f.err
}

func TestSetLocalEmitsAllThreeWhenAllPresent(t *testing.T) {
	f := &fakeExecer{}
	err := SetLocal(context.Background(), f, p9context.RLSScope{
		TenantID:  "t-001",
		CompanyID: "c-001",
		BranchID:  "b-001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %v", len(f.stmts), f.stmts)
	}
	expected := []string{
		"SET LOCAL app.tenant_id = 't-001'",
		"SET LOCAL app.company_id = 'c-001'",
		"SET LOCAL app.branch_id = 'b-001'",
	}
	for i, want := range expected {
		if f.stmts[i] != want {
			t.Errorf("stmt[%d] = %q, want %q", i, f.stmts[i], want)
		}
	}
}

func TestSetLocalSkipsEmptyValues(t *testing.T) {
	f := &fakeExecer{}
	err := SetLocal(context.Background(), f, p9context.RLSScope{
		TenantID: "t-001",
		// CompanyID + BranchID empty
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d: %v", len(f.stmts), f.stmts)
	}
	if f.stmts[0] != "SET LOCAL app.tenant_id = 't-001'" {
		t.Errorf("got %q", f.stmts[0])
	}
}

func TestSetSessionUsesSetVerb(t *testing.T) {
	f := &fakeExecer{}
	err := SetSession(context.Background(), f, p9context.RLSScope{TenantID: "t-001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.stmts) != 1 || !strings.HasPrefix(f.stmts[0], "SET app.tenant_id") {
		t.Errorf("expected SET (no LOCAL), got %v", f.stmts)
	}
}

// Each rejected character should fail BEFORE any Exec is called — so the
// fakeExecer should record zero statements.
func TestSetLocalRejectsInvalidChars(t *testing.T) {
	cases := []struct {
		name string
		val  string
		ch   rune
	}{
		{"single quote", "t'001", '\''},
		{"backslash", "t\\001", '\\'},
		{"semicolon", "t;001", ';'},
		{"null byte", "t\x00001", '\x00'},
		{"newline", "t\n001", '\n'},
		{"carriage return", "t\r001", '\r'},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeExecer{}
			err := SetLocal(context.Background(), f, p9context.RLSScope{TenantID: c.val})
			if err == nil {
				t.Fatalf("expected error for %q, got nil", c.val)
			}
			if !IsInvalidValue(err) {
				t.Errorf("expected ErrInvalidValue, got %T: %v", err, err)
			}
			var ev *ErrInvalidValue
			_ = errors.As(err, &ev)
			if ev.Char != c.ch {
				t.Errorf("expected char %q, got %q", c.ch, ev.Char)
			}
			if len(f.stmts) != 0 {
				t.Errorf("expected zero Exec calls (validation must fail before SET); got %d", len(f.stmts))
			}
		})
	}
}

// If the second variable is invalid, the first must NOT have been
// executed — atomicity guard. Otherwise a partial SET LOCAL would set
// tenant_id but leave company_id stale from a previous request.
func TestSetLocalIsAtomicWhenLaterValueRejected(t *testing.T) {
	f := &fakeExecer{}
	err := SetLocal(context.Background(), f, p9context.RLSScope{
		TenantID:  "t-001", // valid
		CompanyID: "c'001", // invalid
		BranchID:  "b-001", // valid
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsInvalidValue(err) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
	if len(f.stmts) != 0 {
		t.Errorf("expected zero Exec calls (must validate ALL values before any SET); got %d: %v",
			len(f.stmts), f.stmts)
	}
}

func TestExecErrorPropagates(t *testing.T) {
	wantErr := errors.New("connection lost")
	f := &fakeExecer{err: wantErr}
	err := SetLocal(context.Background(), f, p9context.RLSScope{TenantID: "t-001"})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error chain does not contain wantErr; got %v", err)
	}
}
