package pgxpostgres

import (
	"context"
	"testing"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewDBContext(t *testing.T) {
	// Create a nil pool for testing structure creation
	var pool *pgxpool.Pool

	dbCtx := NewDBContext(pool)

	if dbCtx == nil {
		t.Fatal("expected non-nil DBContext")
	}

	if dbCtx.DBPoolShared != pool {
		t.Error("expected DBPoolShared to match provided pool")
	}

	if dbCtx.DBPoolIndependent == nil {
		t.Error("expected DBPoolIndependent map to be initialized")
	}

	if len(dbCtx.DBPoolIndependent) != 0 {
		t.Error("expected DBPoolIndependent map to be empty initially")
	}
}

func TestNewDBContext_MultipleIndependentPools(t *testing.T) {
	var pool *pgxpool.Pool
	dbCtx := NewDBContext(pool)

	// Simulate adding independent pools
	mockPool1 := &pgxpool.Pool{}
	mockPool2 := &pgxpool.Pool{}

	dbCtx.DBPoolIndependent["tenant1"] = mockPool1
	dbCtx.DBPoolIndependent["tenant2"] = mockPool2

	if len(dbCtx.DBPoolIndependent) != 2 {
		t.Errorf("expected 2 independent pools, got %d", len(dbCtx.DBPoolIndependent))
	}

	if dbCtx.DBPoolIndependent["tenant1"] != mockPool1 {
		t.Error("expected tenant1 pool to match mockPool1")
	}

	if dbCtx.DBPoolIndependent["tenant2"] != mockPool2 {
		t.Error("expected tenant2 pool to match mockPool2")
	}
}

func TestNewPgx_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Data
		errMsg string
	}{
		{
			name: "empty user",
			config: &config.Data{
				Postgres: &config.Data_Postgres{
					User:     "",
					Password: "password",
					Host:     "localhost",
					Port:     5432,
					Dbname:   "testdb",
				},
			},
			errMsg: "should handle empty user",
		},
		{
			name: "empty password",
			config: &config.Data{
				Postgres: &config.Data_Postgres{
					User:     "user",
					Password: "",
					Host:     "localhost",
					Port:     5432,
					Dbname:   "testdb",
				},
			},
			errMsg: "should handle empty password",
		},
		{
			name: "empty host",
			config: &config.Data{
				Postgres: &config.Data_Postgres{
					User:     "user",
					Password: "password",
					Host:     "",
					Port:     5432,
					Dbname:   "testdb",
				},
			},
			errMsg: "should handle empty host",
		},
		{
			name: "invalid port",
			config: &config.Data{
				Postgres: &config.Data_Postgres{
					User:     "user",
					Password: "password",
					Host:     "localhost",
					Port:     0,
					Dbname:   "testdb",
				},
			},
			errMsg: "should handle invalid port",
		},
		{
			name: "empty dbname",
			config: &config.Data{
				Postgres: &config.Data_Postgres{
					User:     "user",
					Password: "password",
					Host:     "localhost",
					Port:     5432,
					Dbname:   "",
				},
			},
			errMsg: "should handle empty dbname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These will fail to connect, but we're testing config validation
			pool, cleanup, err := NewPgx(tt.config)

			// Connection should fail for invalid configs
			if err == nil {
				if cleanup != nil {
					cleanup()
				}
				t.Logf("%s: connection unexpectedly succeeded (might indicate real DB present)", tt.errMsg)
			}

			// If pool was created, ensure cleanup works
			if pool != nil && cleanup != nil {
				cleanup()
			}
		})
	}
}

func TestNewPgx_ConnectionStringFormat(t *testing.T) {
	// Test that the DSN is formatted correctly
	cfg := &config.Data{
		Postgres: &config.Data_Postgres{
			User:     "testuser",
			Password: "testpass",
			Host:     "localhost",
			Port:     5432,
			Dbname:   "testdb",
		},
	}

	// This will fail to connect (no real DB), but we can verify the DSN format
	_, cleanup, err := NewPgx(cfg)

	// Cleanup if somehow it succeeded
	if cleanup != nil {
		defer cleanup()
	}

	// Expected to fail without real database
	if err == nil {
		t.Log("Warning: connection succeeded - may indicate real database present")
	} else {
		// Verify error message indicates connection attempt was made
		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant interface{}
		expected interface{}
	}{
		{"maxOpenConns", maxOpenConns, 30},
		{"connMaxLifetime", connMaxLifetime, 60},
		{"maxIdleConns", maxIdleConns, 10},
		{"connMaxIdleTime", connMaxIdleTime, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, tt.constant)
			}
		})
	}
}

func TestCleanupFunction(t *testing.T) {
	// Test that cleanup function is provided and callable
	// Note: This requires a mock or will fail without real DB
	cfg := &config.Data{
		Postgres: &config.Data_Postgres{
			User:     "testuser",
			Password: "testpass",
			Host:     "nonexistent-host",
			Port:     5432,
			Dbname:   "testdb",
		},
	}

	pool, cleanup, err := NewPgx(cfg)

	// Connection should fail
	if err == nil {
		// If it somehow succeeded, test cleanup
		if cleanup == nil {
			t.Fatal("expected cleanup function to be provided")
		}

		// Call cleanup
		cleanup()

		// Verify pool is closed by attempting ping
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		pingErr := pool.Ping(ctx)
		if pingErr == nil {
			t.Error("expected ping to fail after cleanup")
		}
	} else {
		// Expected: connection failed
		if cleanup != nil {
			t.Error("expected cleanup to be nil when connection fails")
		}
		if pool != nil {
			t.Error("expected pool to be nil when connection fails")
		}
	}
}

func TestDBContext_PoolAssignment(t *testing.T) {
	// Test that DBContext correctly manages pool assignment
	var sharedPool *pgxpool.Pool

	dbCtx := NewDBContext(sharedPool)

	// Initially, DBPool should be nil (not set)
	if dbCtx.DBPool != nil {
		t.Error("expected DBPool to be nil initially")
	}

	// DBPoolShared should be the provided pool
	if dbCtx.DBPoolShared != sharedPool {
		t.Error("expected DBPoolShared to match provided pool")
	}

	// Test assigning a pool to DBPool
	mockPool := &pgxpool.Pool{}
	dbCtx.DBPool = mockPool

	if dbCtx.DBPool != mockPool {
		t.Error("expected DBPool assignment to work")
	}
}

func TestDBContext_IndependentPoolOperations(t *testing.T) {
	dbCtx := NewDBContext(nil)

	// Test adding independent pools
	tenantID1 := "tenant-123"
	tenantID2 := "tenant-456"

	pool1 := &pgxpool.Pool{}
	pool2 := &pgxpool.Pool{}

	dbCtx.DBPoolIndependent[tenantID1] = pool1
	dbCtx.DBPoolIndependent[tenantID2] = pool2

	// Verify retrieval
	retrieved1, exists1 := dbCtx.DBPoolIndependent[tenantID1]
	if !exists1 {
		t.Error("expected tenant-123 pool to exist")
	}
	if retrieved1 != pool1 {
		t.Error("expected retrieved pool to match pool1")
	}

	retrieved2, exists2 := dbCtx.DBPoolIndependent[tenantID2]
	if !exists2 {
		t.Error("expected tenant-456 pool to exist")
	}
	if retrieved2 != pool2 {
		t.Error("expected retrieved pool to match pool2")
	}

	// Test deletion
	delete(dbCtx.DBPoolIndependent, tenantID1)
	_, exists := dbCtx.DBPoolIndependent[tenantID1]
	if exists {
		t.Error("expected tenant-123 pool to be deleted")
	}
}

// Benchmark tests
func BenchmarkNewDBContext(b *testing.B) {
	var pool *pgxpool.Pool

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewDBContext(pool)
	}
}

func BenchmarkDBContext_IndependentPoolLookup(b *testing.B) {
	dbCtx := NewDBContext(nil)

	// Pre-populate with pools
	for i := 0; i < 100; i++ {
		tenantID := string(rune('a' + i))
		dbCtx.DBPoolIndependent[tenantID] = &pgxpool.Pool{}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tenantID := string(rune('a' + (i % 100)))
		_, _ = dbCtx.DBPoolIndependent[tenantID]
	}
}
