// Package uow is the Unit-of-Work abstraction — repositories' escape hatch
// for multi-table transactional writes.
//
// The three entry points:
//
//	type Factory interface {
//	    Begin(ctx context.Context) (UnitOfWork, error)
//	}
//	type UnitOfWork interface {
//	    Tx() pgx.Tx                          // hand to sqlc: queries.WithTx(tx.Tx())
//	    Commit(ctx context.Context) error
//	    Rollback(ctx context.Context) error
//	}
//	type UnitOfWorkManager interface {
//	    WithTx(ctx, fn func(UnitOfWork) error) error   // begin+commit/rollback wrapper
//	    WithRead(ctx, fn func(UnitOfWork) error) error // read-only variant
//	}
//
// Typical service usage:
//
//	err := s.uowFactory.BeginFn(ctx, func(tx uow.UnitOfWork) error {
//	    txRepos := s.repos.WithTx(tx)         // each repo exposes WithTx
//	    if _, err := txRepos.Parent.Save(ctx, p); err != nil { return err }
//	    return txRepos.Child.Save(ctx, c)
//	})
//
// Repositories implement WithTx(tx uow.UnitOfWork) ThisRepo so services
// can bind a whole set of repos to the same transaction — the canonical
// pattern used by every core/bi/* service.
//
// NewManager(factory) wraps a Factory with the higher-level WithTx /
// WithRead ergonomics; the helpers in helpers/service use it to centralise
// begin/commit/rollback plumbing so call sites stay at one level of
// abstraction.
package uow
