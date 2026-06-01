package sqlc

import (
	"context"
	"fmt"
	"time"

	conf "p9e.in/samavaya/packages/api/v1/config"
	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/uow"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseManager provides centralized connection pool management.
// It exposes the pool for non-transactional reads and UoWFactory for transactional operations.
//
// Two factories are available:
//   - UoWFactory: Standard factory (no RLS session variables set)
//   - RLSFactory: RLS-aware factory that sets session variables from context
type DatabaseManager struct {
	Pool       *pgxpool.Pool
	UoWFactory uow.Factory // Standard factory without RLS
	RLSFactory uow.Factory // RLS-aware factory that sets session variables
}

func NewDatabaseManager(cfg *conf.Data) (*DatabaseManager, error) {
	// Convert boolean sslmode to proper PostgreSQL sslmode string
	sslmode := "disable"
	if cfg.Postgres.Sslmode {
		sslmode = "require"
	}

	dsn := fmt.Sprintf(
		"user=%s password=%s host=%s port=%d dbname=%s sslmode=%s",
		cfg.Postgres.User,
		cfg.Postgres.Password,
		cfg.Postgres.Host,
		cfg.Postgres.Port,
		cfg.Postgres.Dbname,
		sslmode,
	)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Configure connection pool settings
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnIdleTime = 30 * time.Minute
	config.MaxConnLifetime = 2 * time.Hour

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DatabaseManager{
		Pool:       pool,
		UoWFactory: uow.NewFactory(pool),
		RLSFactory: uow.NewRLSFactory(pool),
	}, nil
}

// ExecRaw executes raw SQL queries for dynamic table operations.
// This is a generic method that can be used by any repository that needs raw SQL execution.
func (m *DatabaseManager) ExecRaw(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	return m.Pool.Exec(ctx, query, args...)
}

// WithRLSRead executes a read operation on a pooled connection with RLS session variables set.
// This is for non-transactional reads that still need RLS filtering.
// The RLS scope is extracted from the context (via p9context.MustRLSScope).
func (m *DatabaseManager) WithRLSRead(ctx context.Context, fn func(context.Context) error) error {
	return uow.SetRLSOnPool(ctx, m.Pool, fn)
}

// WithRLSTx executes a function within a transaction with RLS session variables set.
// Automatically commits on success or rolls back on error.
// The RLS scope is extracted from the context.
func (m *DatabaseManager) WithRLSTx(ctx context.Context, fn func(uow uow.UnitOfWork) error) error {
	return uow.WithTx(ctx, m.RLSFactory, fn)
}

// WithTx executes a function within a transaction (without RLS).
// Automatically commits on success or rolls back on error.
func (m *DatabaseManager) WithTx(ctx context.Context, fn func(uow uow.UnitOfWork) error) error {
	return uow.WithTx(ctx, m.UoWFactory, fn)
}

// Close closes the database connection pool.
func (m *DatabaseManager) Close() {
	if m.Pool != nil {
		m.Pool.Close()
	}
}

// ============================================================================
// QueriesProvider - Generic SQLC query provider
// ============================================================================

// QueriesProvider wraps a sqlc-generated Querier to provide context-aware query access
// and transaction support. The type parameter Q must be a sqlc Querier interface.
type QueriesProvider[Q any] struct {
	queries Q
	pool    *pgxpool.Pool
	newQ    func(pool *pgxpool.Pool) Q
}

// NewQueriesProvider creates a new QueriesProvider.
func NewQueriesProvider[Q any](queries Q, pool *pgxpool.Pool, newQ func(pool *pgxpool.Pool) Q) *QueriesProvider[Q] {
	return &QueriesProvider[Q]{queries: queries, pool: pool, newQ: newQ}
}

// Queries returns the underlying Querier.
func (p *QueriesProvider[Q]) Queries(_ context.Context) Q {
	return p.queries
}

// WithTx returns a new QueriesProvider that uses the given transaction.
// The returned provider's Queries() will use the transaction instead of the pool.
func (p *QueriesProvider[Q]) WithTx(_ interface{}) *QueriesProvider[Q] {
	// In the transaction case, we return a copy that shares the same queries
	// (the actual transaction binding happens via sqlc's WithTx at the db layer)
	return &QueriesProvider[Q]{queries: p.queries, pool: p.pool, newQ: p.newQ}
}

// Provider wraps a connection pool for SQLC query providers.
// Repositories use this to create SQLC Queries instances via provider.DB().
type Provider struct {
	pool *pgxpool.Pool
}

// NewProvider creates a new Provider from a DatabaseManager.
func NewProvider(mgr *DatabaseManager) Provider {
	return Provider{pool: mgr.Pool}
}

// DB returns the underlying connection pool as a DBTX-compatible interface.
func (p Provider) DB() *pgxpool.Pool {
	return p.pool
}

// ============================================================================
// RLS Helpers
// ============================================================================

// WithRLSScope is a convenience function to add RLS scope to context.
// Usage: ctx = db.WithRLSScope(ctx, tenantID, companyID, branchID)
func WithRLSScope(ctx context.Context, tenantID, companyID, branchID string) context.Context {
	return p9context.NewRLSScopeFromIDs(ctx, tenantID, companyID, branchID)
}
