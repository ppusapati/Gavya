// Tests for packages/uow. Covers the Factory/UnitOfWork interface contract
// via fakes (no real Postgres): closed-flag idempotency on Commit/Rollback,
// the WithTx / WithRead helpers' begin → fn → commit/rollback flow, and the
// Manager thin-wrapper. Integration tests against a real pool live under
// `//go:build integration`.
package uow_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"p9e.in/samavaya/packages/uow"
)

// ────────────────────────────────────────────────────────────────────────────
// Fakes
// ────────────────────────────────────────────────────────────────────────────

// fakeTx is a pgx.Tx that records Commit/Rollback calls and can be
// configured to fail either. Every other pgx.Tx method is a no-op
// (returning zero values) rather than panicking — UnitOfWork doesn't
// call them, but if it did we'd rather surface the assertion in the
// specific test than fault out with a "not used" panic mid-chain. The
// embedded pgx.Tx is nil; we shadow the two methods UoW actually uses.
type fakeTx struct {
	pgx.Tx // embedded interface, all methods return zero values unless shadowed

	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
}

func (f *fakeTx) Commit(ctx context.Context) error {
	f.commits++
	return f.commitErr
}

func (f *fakeTx) Rollback(ctx context.Context) error {
	f.rollbacks++
	return f.rollbackErr
}

// fakeFactory hands out a fresh UnitOfWork wrapping a configurable fakeTx.
type fakeFactory struct {
	beginErr  error
	tx        *fakeTx
	beginCalls int
}

func (f *fakeFactory) Begin(ctx context.Context) (uow.UnitOfWork, error) {
	f.beginCalls++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return uow.NewUnitOfWork(f.tx), nil
}

func (f *fakeFactory) New(ctx context.Context) (uow.UnitOfWork, error) {
	return f.Begin(ctx)
}

// ────────────────────────────────────────────────────────────────────────────
// UnitOfWork — Commit / Rollback / idempotency
// ────────────────────────────────────────────────────────────────────────────

func TestUnitOfWork_CommitCallsTxCommit(t *testing.T) {
	tx := &fakeTx{}
	u := uow.NewUnitOfWork(tx)

	if err := u.Commit(context.Background()); err != nil {
		t.Fatalf("Commit returned err: %v", err)
	}
	if tx.commits != 1 {
		t.Fatalf("tx.Commit calls = %d, want 1", tx.commits)
	}
}

func TestUnitOfWork_CommitTwice_ReturnsAlreadyClosed(t *testing.T) {
	tx := &fakeTx{}
	u := uow.NewUnitOfWork(tx)

	if err := u.Commit(context.Background()); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	err := u.Commit(context.Background())
	if err == nil {
		t.Fatal("second Commit returned nil; want already-closed error")
	}
	// The implementation wraps as packages/errors.BadRequest with reason
	// TRANSACTION_ALREADY_CLOSED — surface-level check only.
	if tx.commits != 1 {
		t.Fatalf("tx.Commit called %d times after second Commit — expected 1 (closed state gate)", tx.commits)
	}
}

func TestUnitOfWork_RollbackIsIdempotent(t *testing.T) {
	// Rollback after Commit must NOT call tx.Rollback (idempotent, no-op).
	tx := &fakeTx{}
	u := uow.NewUnitOfWork(tx)

	if err := u.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback after commit should be no-op, got: %v", err)
	}
	if tx.rollbacks != 0 {
		t.Fatalf("tx.Rollback called %d times after commit; want 0", tx.rollbacks)
	}
}

func TestUnitOfWork_TxClosedErrorIsSwallowed(t *testing.T) {
	// When the underlying tx reports pgx.ErrTxClosed on Rollback, the
	// UoW treats it as already-closed (no bubble-up).
	tx := &fakeTx{rollbackErr: pgx.ErrTxClosed}
	u := uow.NewUnitOfWork(tx)

	if err := u.Rollback(context.Background()); err != nil {
		t.Fatalf("pgx.ErrTxClosed should be swallowed, got: %v", err)
	}
}

func TestUnitOfWork_RollbackOtherErrorBubbles(t *testing.T) {
	other := stderrors.New("disk full")
	tx := &fakeTx{rollbackErr: other}
	u := uow.NewUnitOfWork(tx)

	err := u.Rollback(context.Background())
	if err == nil {
		t.Fatal("Rollback returned nil; want wrapped disk-full")
	}
}

func TestUnitOfWork_TxAccessor(t *testing.T) {
	tx := &fakeTx{}
	u := uow.NewUnitOfWork(tx)

	if got := u.Tx(); got != pgx.Tx(tx) {
		t.Fatalf("Tx() returned different object than NewUnitOfWork's input")
	}
	if got := u.GetTx(); got != pgx.Tx(tx) {
		t.Fatalf("GetTx() returned different object than NewUnitOfWork's input")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// WithTx / WithRead helpers
// ────────────────────────────────────────────────────────────────────────────

func TestWithTx_HappyPath_Commits(t *testing.T) {
	tx := &fakeTx{}
	f := &fakeFactory{tx: tx}

	called := false
	err := uow.WithTx(context.Background(), f, func(u uow.UnitOfWork) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx returned err: %v", err)
	}
	if !called {
		t.Fatal("fn not invoked")
	}
	if tx.commits != 1 {
		t.Fatalf("tx.Commit calls = %d, want 1", tx.commits)
	}
	// deferred Rollback after successful commit is a no-op on the UoW
	// (closed flag prevents bubble into tx).
	if tx.rollbacks != 0 {
		t.Fatalf("tx.Rollback calls = %d after commit; want 0", tx.rollbacks)
	}
}

func TestWithTx_FnError_Rollbacks(t *testing.T) {
	tx := &fakeTx{}
	f := &fakeFactory{tx: tx}

	wanted := stderrors.New("fn failed")
	err := uow.WithTx(context.Background(), f, func(u uow.UnitOfWork) error {
		return wanted
	})
	if err != wanted {
		t.Fatalf("WithTx returned %v, want %v", err, wanted)
	}
	if tx.commits != 0 {
		t.Fatalf("tx.Commit calls = %d, want 0 on fn error", tx.commits)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("tx.Rollback calls = %d, want 1 on fn error", tx.rollbacks)
	}
}

func TestWithTx_BeginError_Propagates(t *testing.T) {
	beginErr := stderrors.New("pool exhausted")
	f := &fakeFactory{beginErr: beginErr}

	fnCalled := false
	err := uow.WithTx(context.Background(), f, func(u uow.UnitOfWork) error {
		fnCalled = true
		return nil
	})
	if err != beginErr {
		t.Fatalf("WithTx err = %v, want %v", err, beginErr)
	}
	if fnCalled {
		t.Fatal("fn should not be invoked when Begin fails")
	}
}

func TestWithRead_AlwaysRollbacks(t *testing.T) {
	// Read-only path must NOT commit even on success — it rolls back so
	// no write accidentally persists. The read itself returns the fn's
	// result.
	tx := &fakeTx{}
	f := &fakeFactory{tx: tx}

	err := uow.WithRead(context.Background(), f, func(u uow.UnitOfWork) error {
		return nil
	})
	if err != nil {
		t.Fatalf("WithRead: %v", err)
	}
	if tx.commits != 0 {
		t.Fatalf("WithRead committed %d times; want 0", tx.commits)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("WithRead rollbacks = %d; want 1", tx.rollbacks)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Manager thin-wrapper
// ────────────────────────────────────────────────────────────────────────────

func TestManager_WithTxDelegatesToFactory(t *testing.T) {
	tx := &fakeTx{}
	f := &fakeFactory{tx: tx}
	m := uow.NewManager(f)

	if err := m.WithTx(context.Background(), func(u uow.UnitOfWork) error { return nil }); err != nil {
		t.Fatalf("m.WithTx: %v", err)
	}
	if f.beginCalls != 1 {
		t.Fatalf("Factory.Begin calls = %d, want 1", f.beginCalls)
	}
	if tx.commits != 1 {
		t.Fatalf("tx.Commit calls = %d, want 1", tx.commits)
	}
}

func TestManager_WithReadDelegatesToFactory(t *testing.T) {
	tx := &fakeTx{}
	f := &fakeFactory{tx: tx}
	m := uow.NewManager(f)

	if err := m.WithRead(context.Background(), func(u uow.UnitOfWork) error { return nil }); err != nil {
		t.Fatalf("m.WithRead: %v", err)
	}
	if f.beginCalls != 1 {
		t.Fatalf("Factory.Begin calls = %d, want 1", f.beginCalls)
	}
	if tx.commits != 0 {
		t.Fatal("WithRead must not commit")
	}
	if tx.rollbacks != 1 {
		t.Fatalf("WithRead rollbacks = %d; want 1", tx.rollbacks)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Type-alias compatibility
// ────────────────────────────────────────────────────────────────────────────

func TestUnitOfWorkFactory_IsAliasOfFactory(t *testing.T) {
	// uow.UnitOfWorkFactory is a type alias for uow.Factory — assigning a
	// fakeFactory (which implements Factory) to UnitOfWorkFactory must
	// compile. This catches accidental removal of the alias.
	var _ uow.UnitOfWorkFactory = (*fakeFactory)(nil)
}
