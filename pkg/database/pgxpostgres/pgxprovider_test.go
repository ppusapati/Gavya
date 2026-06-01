package pgxpostgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHasTenant(t *testing.T) {
	tests := []struct {
		name     string
		hasTenant HasTenant
		expected  sql.NullString
	}{
		{
			name:      "valid tenant",
			hasTenant: HasTenant{String: "tenant-123", Valid: true},
			expected:  sql.NullString{String: "tenant-123", Valid: true},
		},
		{
			name:      "null tenant",
			hasTenant: HasTenant{String: "", Valid: false},
			expected:  sql.NullString{String: "", Valid: false},
		},
		{
			name:      "empty but valid tenant",
			hasTenant: HasTenant{String: "", Valid: true},
			expected:  sql.NullString{String: "", Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// HasTenant is a type alias for sql.NullString
			nullStr := sql.NullString(tt.hasTenant)

			if nullStr.String != tt.expected.String {
				t.Errorf("expected String=%s, got %s", tt.expected.String, nullStr.String)
			}
			if nullStr.Valid != tt.expected.Valid {
				t.Errorf("expected Valid=%v, got %v", tt.expected.Valid, nullStr.Valid)
			}
		})
	}
}

func TestMultiTenancy(t *testing.T) {
	tests := []struct {
		name     string
		tenantId HasTenant
	}{
		{
			name:     "with tenant",
			tenantId: HasTenant{String: "tenant-123", Valid: true},
		},
		{
			name:     "without tenant",
			tenantId: HasTenant{String: "", Valid: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := MultiTenancy{
				TenantId: tt.tenantId,
			}

			if mt.TenantId.String != tt.tenantId.String {
				t.Errorf("expected TenantId.String=%s, got %s", tt.tenantId.String, mt.TenantId.String)
			}
			if mt.TenantId.Valid != tt.tenantId.Valid {
				t.Errorf("expected TenantId.Valid=%v, got %v", tt.tenantId.Valid, mt.TenantId.Valid)
			}
		})
	}
}

// Mock ClientProvider for testing
type mockClientProvider struct {
	pool *pgxpool.Pool
	err  error
}

func (m *mockClientProvider) Get(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pool, nil
}

func TestClientProviderFunc(t *testing.T) {
	mockPool := &pgxpool.Pool{}
	ctx := context.Background()
	dsn := "postgres://user:pass@localhost/db"

	tests := []struct {
		name    string
		fn      ClientProviderFunc
		wantErr bool
	}{
		{
			name: "successful get",
			fn: func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
				return mockPool, nil
			},
			wantErr: false,
		},
		{
			name: "error get",
			fn: func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
				return nil, sql.ErrNoRows
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := tt.fn.Get(ctx, dsn)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if pool != nil {
					t.Error("expected nil pool on error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if pool != mockPool {
					t.Error("expected pool to match mockPool")
				}
			}
		})
	}
}

func TestNewDbWrap(t *testing.T) {
	mockPool := &pgxpool.Pool{}

	dbWrap := NewDbWrap(mockPool)

	if dbWrap == nil {
		t.Fatal("expected non-nil DbWrap")
	}

	if dbWrap.Pool != mockPool {
		t.Error("expected DbWrap.Pool to match mockPool")
	}
}

func TestDbWrap_Close(t *testing.T) {
	// Note: DbWrap.Close() calls closeDb which will panic with nil pool
	// This is expected behavior - in production, Close is only called with valid pools
	t.Run("verify close method exists", func(t *testing.T) {
		// We verify the method exists and is callable
		// Actual close testing requires integration tests with real DB
		var nilPool *pgxpool.Pool
		dbWrap := NewDbWrap(nilPool)

		if dbWrap.Pool != nilPool {
			t.Error("expected Pool to be nil")
		}

		// We can't actually call Close() without panicking
		// This test verifies structure only
	})
}

func TestCloseDb(t *testing.T) {
	// Test closeDb function
	// Note: closeDb will panic with nil pool, which is expected behavior
	// This test verifies the function signature and structure
	t.Run("verify function exists", func(t *testing.T) {
		// We can't actually test closeDb with a nil pool as it will panic
		// This test just verifies the function is accessible
		// In a real scenario, closeDb is only called with valid pools
		_ = closeDb
	})
}

func TestMultiTenancy_NullTenantOperations(t *testing.T) {
	mt := MultiTenancy{
		TenantId: HasTenant{String: "", Valid: false},
	}

	// Convert to sql.NullString to test operations
	nullStr := sql.NullString(mt.TenantId)

	if nullStr.Valid {
		t.Error("expected Valid to be false for null tenant")
	}

	// Test scanning behavior
	var scanned sql.NullString
	scanned = nullStr

	if scanned.Valid {
		t.Error("expected scanned Valid to be false")
	}
	if scanned.String != "" {
		t.Error("expected scanned String to be empty")
	}
}

func TestMultiTenancy_ValidTenantOperations(t *testing.T) {
	tenantID := "tenant-abc-123"
	mt := MultiTenancy{
		TenantId: HasTenant{String: tenantID, Valid: true},
	}

	// Convert to sql.NullString to test operations
	nullStr := sql.NullString(mt.TenantId)

	if !nullStr.Valid {
		t.Error("expected Valid to be true for valid tenant")
	}

	if nullStr.String != tenantID {
		t.Errorf("expected String to be %s, got %s", tenantID, nullStr.String)
	}

	// Test scanning behavior
	var scanned sql.NullString
	scanned = nullStr

	if !scanned.Valid {
		t.Error("expected scanned Valid to be true")
	}
	if scanned.String != tenantID {
		t.Errorf("expected scanned String to be %s, got %s", tenantID, scanned.String)
	}
}

// Mock ConnStrResolver for testing
type mockConnStrResolver struct {
	dsn string
	err error
}

func (m *mockConnStrResolver) Resolve(ctx context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.dsn, nil
}

func TestNewDbProvider(t *testing.T) {
	mockDsn := "postgres://user:pass@localhost/db"
	mockResolver := &mockConnStrResolver{dsn: mockDsn}
	mockClient := &mockClientProvider{pool: &pgxpool.Pool{}}

	provider := NewDbProvider(mockResolver, mockClient)

	if provider == nil {
		t.Fatal("expected non-nil DbProvider")
	}

	// Test Get method
	ctx := context.Background()
	pool, err := provider.Get(ctx, "test-key")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if pool == nil {
		t.Error("expected non-nil pool")
	}
}

func TestNewDbProvider_ResolverError(t *testing.T) {
	expectedErr := sql.ErrNoRows
	mockResolver := &mockConnStrResolver{err: expectedErr}
	mockClient := &mockClientProvider{pool: &pgxpool.Pool{}}

	provider := NewDbProvider(mockResolver, mockClient)

	ctx := context.Background()
	_, err := provider.Get(ctx, "test-key")

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestNewDbProvider_ClientError(t *testing.T) {
	expectedErr := sql.ErrConnDone
	mockDsn := "postgres://user:pass@localhost/db"
	mockResolver := &mockConnStrResolver{dsn: mockDsn}
	mockClient := &mockClientProvider{err: expectedErr}

	provider := NewDbProvider(mockResolver, mockClient)

	ctx := context.Background()
	_, err := provider.Get(ctx, "test-key")

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

// Benchmark tests
func BenchmarkNewDbWrap(b *testing.B) {
	mockPool := &pgxpool.Pool{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewDbWrap(mockPool)
	}
}

func BenchmarkClientProviderFunc_Get(b *testing.B) {
	mockPool := &pgxpool.Pool{}
	ctx := context.Background()
	dsn := "postgres://user:pass@localhost/db"

	fn := ClientProviderFunc(func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
		return mockPool, nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn.Get(ctx, dsn)
	}
}

func BenchmarkMultiTenancy_Creation(b *testing.B) {
	tenantID := "tenant-123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MultiTenancy{
			TenantId: HasTenant{String: tenantID, Valid: true},
		}
	}
}
