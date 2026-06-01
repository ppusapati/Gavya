package pgxpostgres

import (
	"context"
	"fmt"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxOpenConns    = 30
	connMaxLifetime = 60
	maxIdleConns    = 10
	connMaxIdleTime = 10
)

// AppContext holds the context for the application, including the dynamic database pool
type DBContext struct {
	DBPool            *pgxpool.Pool
	DBPoolShared      *pgxpool.Pool
	DBPoolIndependent map[string]*pgxpool.Pool
}

// NewDBContext creates and returns a new instance of DBContext.
func NewDBContext(conn *pgxpool.Pool) *DBContext {
	return &DBContext{
		DBPoolShared:      conn,
		DBPoolIndependent: make(map[string]*pgxpool.Pool),
	}
}

// NewPgx initializes a new PostgreSQL connection pool and returns it along with a cleanup function.
// This is typically called during application startup. For context-aware initialization,
// use NewPgxWithContext instead.
func NewPgx(c *config.Data) (*pgxpool.Pool, func(), error) {
	return NewPgxWithContext(context.Background(), c)
}

// NewPgxWithContext initializes a new PostgreSQL connection pool with the provided context.
// The context is used for the initial connection and health check operations.
// This allows callers to pass in a context with timeout/cancellation for startup scenarios.
func NewPgxWithContext(ctx context.Context, c *config.Data) (*pgxpool.Pool, func(), error) {
	dataSourceName := fmt.Sprintf("user=%s password=%s host=%s port=%d dbname=%s sslmode=disable connect_timeout=5 statement_timeout=15000 idle_in_transaction_session_timeout=15000 pool_max_conns=%d",
		c.Postgres.User,
		c.Postgres.Password,
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.Dbname,
		maxOpenConns,
	)

	poolConfig, err := pgxpool.ParseConfig(dataSourceName)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing connection string: %w", err)
	}

	// Configure connection pool settings before creating the pool
	poolConfig.MaxConnLifetime = connMaxLifetime * time.Second
	poolConfig.MaxConnIdleTime = connMaxIdleTime * time.Second
	poolConfig.MaxConns = int32(maxOpenConns)
	poolConfig.MinConns = int32(maxIdleConns)

	conn, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Test the connection with a timeout
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err = conn.Ping(pingCtx); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("database connection test failed: %w", err)
	}

	// Test a simple query to verify full connectivity
	if _, err = conn.Exec(pingCtx, "SELECT 1"); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("database query test failed: %w", err)
	}

	// Cleanup function to close the connection pool
	cleanup := func() {
		conn.Close()
	}

	return conn, cleanup, nil
}
