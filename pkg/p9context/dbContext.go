package p9context

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBContext holds the context for the application, including the dynamic database pool.
// This is the legacy shared struct approach - prefer using context-based pool storage.
type DBContext struct {
	DBPool            *pgxpool.Pool
	DBPoolShared      *pgxpool.Pool
	DBPoolIndependent map[string]*pgxpool.Pool
}

type dbPoolContextKey struct{}

// NewDBPoolContext creates a new context with the database pool.
// This is the preferred approach over modifying shared DBContext.
func NewDBPoolContext(ctx context.Context, pool *pgxpool.Pool) context.Context {
	return context.WithValue(ctx, dbPoolContextKey{}, pool)
}

// FromDBPoolContext retrieves the database pool from context.
// Returns nil and false if not present.
func FromDBPoolContext(ctx context.Context) (*pgxpool.Pool, bool) {
	v, ok := ctx.Value(dbPoolContextKey{}).(*pgxpool.Pool)
	if ok && v != nil {
		return v, true
	}
	return nil, false
}

// MustDBPoolContext retrieves the database pool from context.
// Panics if not present.
func MustDBPoolContext(ctx context.Context) *pgxpool.Pool {
	pool, ok := FromDBPoolContext(ctx)
	if !ok || pool == nil {
		panic("database pool not found in context")
	}
	return pool
}

// DBPool retrieves the database pool from context.
// Returns nil if not present. Use MustDBPoolContext if pool is required.
func DBPool(ctx context.Context) *pgxpool.Pool {
	pool, _ := FromDBPoolContext(ctx)
	return pool
}

// HasDBPoolContext returns true if the context has a database pool set.
func HasDBPoolContext(ctx context.Context) bool {
	_, ok := FromDBPoolContext(ctx)
	return ok
}
