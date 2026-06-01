package saas

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"p9e.in/samavaya/packages/p9log"
)

// ConnectionInfo contains the resolved connection and metadata
type ConnectionInfo struct {
	Pool           *pgxpool.Pool
	TenantID       string
	TenantType     TenantType
	RequiresFilter bool // true if queries need tenant_id WHERE clause
}

// PoolConfig holds configuration for connection pools
type PoolConfig struct {
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// DefaultPoolConfig returns sensible default pool configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: 2 * time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}
}

// Resolver resolves tenant connections based on tenant type
type Resolver struct {
	sharedPool     *pgxpool.Pool
	dedicatedPools map[string]*pgxpool.Pool
	poolsMu        sync.RWMutex
	store          TenantStore
	configCache    map[string]*cachedConfig
	cacheMu        sync.RWMutex
	cacheTTL       time.Duration
	poolConfig     PoolConfig
	logger         *p9log.Helper
}

type cachedConfig struct {
	config   *TenantConfig
	cachedAt time.Time
}

// ResolverOption is a functional option for configuring Resolver
type ResolverOption func(*Resolver)

// WithCacheTTL sets the cache TTL for tenant configs
func WithCacheTTL(ttl time.Duration) ResolverOption {
	return func(r *Resolver) {
		r.cacheTTL = ttl
	}
}

// WithPoolConfig sets the pool configuration
func WithPoolConfig(cfg PoolConfig) ResolverOption {
	return func(r *Resolver) {
		r.poolConfig = cfg
	}
}

// WithLogger sets the logger
func WithLogger(logger *p9log.Helper) ResolverOption {
	return func(r *Resolver) {
		r.logger = logger
	}
}

// NewResolver creates a new tenant connection resolver
func NewResolver(sharedPool *pgxpool.Pool, store TenantStore, opts ...ResolverOption) *Resolver {
	r := &Resolver{
		sharedPool:     sharedPool,
		dedicatedPools: make(map[string]*pgxpool.Pool),
		store:          store,
		configCache:    make(map[string]*cachedConfig),
		cacheTTL:       5 * time.Minute,
		poolConfig:     DefaultPoolConfig(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Resolve returns the appropriate connection pool for a tenant
func (r *Resolver) Resolve(ctx context.Context, tenantID string) (*ConnectionInfo, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required")
	}

	// Get tenant config (from cache or store)
	config, err := r.getTenantConfig(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant config: %w", err)
	}

	if !config.IsActive {
		return nil, fmt.Errorf("tenant %s is not active", tenantID)
	}

	switch config.Type {
	case TenantTypeFree, "":
		return &ConnectionInfo{
			Pool:           r.sharedPool,
			TenantID:       tenantID,
			TenantType:     TenantTypeFree,
			RequiresFilter: true,
		}, nil

	case TenantTypePaid:
		pool, err := r.getDedicatedPool(ctx, tenantID, config.Conn.Default())
		if err != nil {
			return nil, fmt.Errorf("failed to get dedicated pool: %w", err)
		}
		return &ConnectionInfo{
			Pool:           pool,
			TenantID:       tenantID,
			TenantType:     TenantTypePaid,
			RequiresFilter: true, // defense in depth
		}, nil

	default:
		return nil, fmt.Errorf("unknown tenant type: %s", config.Type)
	}
}

// getTenantConfig retrieves tenant config from cache or store
func (r *Resolver) getTenantConfig(ctx context.Context, tenantID string) (*TenantConfig, error) {
	// Check cache first
	r.cacheMu.RLock()
	cached, ok := r.configCache[tenantID]
	r.cacheMu.RUnlock()

	if ok && time.Since(cached.cachedAt) < r.cacheTTL {
		return cached.config, nil
	}

	// Fetch from store
	config, err := r.store.GetByNameOrId(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Update cache
	r.cacheMu.Lock()
	r.configCache[tenantID] = &cachedConfig{
		config:   config,
		cachedAt: time.Now(),
	}
	r.cacheMu.Unlock()

	return config, nil
}

// getDedicatedPool returns or creates a dedicated pool for a paid tenant
func (r *Resolver) getDedicatedPool(ctx context.Context, tenantID, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, fmt.Errorf("no connection string for paid tenant %s", tenantID)
	}

	// Check if pool already exists
	r.poolsMu.RLock()
	pool, ok := r.dedicatedPools[tenantID]
	r.poolsMu.RUnlock()

	if ok {
		return pool, nil
	}

	// Create new pool
	r.poolsMu.Lock()
	defer r.poolsMu.Unlock()

	// Double-check after acquiring write lock
	if pool, ok = r.dedicatedPools[tenantID]; ok {
		return pool, nil
	}

	pool, err := createPool(dsn, r.poolConfig)
	if err != nil {
		return nil, err
	}

	r.dedicatedPools[tenantID] = pool

	if r.logger != nil {
		r.logger.Infow("msg", "created dedicated pool for tenant", "tenant_id", tenantID)
	}

	return pool, nil
}

// createPool creates a new pgxpool with the given configuration
func createPool(dsn string, cfg PoolConfig) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	config.MaxConns = cfg.MaxConns
	config.MinConns = cfg.MinConns
	config.MaxConnLifetime = cfg.MaxConnLifetime
	config.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// InvalidateCache removes a tenant from the config cache
func (r *Resolver) InvalidateCache(tenantID string) {
	r.cacheMu.Lock()
	delete(r.configCache, tenantID)
	r.cacheMu.Unlock()
}

// InvalidateAllCache clears the entire config cache
func (r *Resolver) InvalidateAllCache() {
	r.cacheMu.Lock()
	r.configCache = make(map[string]*cachedConfig)
	r.cacheMu.Unlock()
}

// RemoveDedicatedPool closes and removes a dedicated pool for a tenant
func (r *Resolver) RemoveDedicatedPool(tenantID string) {
	r.poolsMu.Lock()
	defer r.poolsMu.Unlock()

	if pool, ok := r.dedicatedPools[tenantID]; ok {
		pool.Close()
		delete(r.dedicatedPools, tenantID)
		if r.logger != nil {
			r.logger.Infow("msg", "removed dedicated pool for tenant", "tenant_id", tenantID)
		}
	}
}

// Close closes all dedicated connection pools (shared pool is managed externally)
func (r *Resolver) Close() {
	r.poolsMu.Lock()
	defer r.poolsMu.Unlock()

	for tenantID, pool := range r.dedicatedPools {
		pool.Close()
		if r.logger != nil {
			r.logger.Infow("msg", "closed dedicated pool for tenant", "tenant_id", tenantID)
		}
	}
	r.dedicatedPools = make(map[string]*pgxpool.Pool)
}

// GetSharedPool returns the shared pool
func (r *Resolver) GetSharedPool() *pgxpool.Pool {
	return r.sharedPool
}

// Stats returns statistics about the resolver
type ResolverStats struct {
	SharedPoolStats    *pgxpool.Stat
	DedicatedPoolCount int
	CachedConfigCount  int
}

// Stats returns resolver statistics
func (r *Resolver) Stats() ResolverStats {
	r.poolsMu.RLock()
	dedicatedCount := len(r.dedicatedPools)
	r.poolsMu.RUnlock()

	r.cacheMu.RLock()
	cacheCount := len(r.configCache)
	r.cacheMu.RUnlock()

	return ResolverStats{
		SharedPoolStats:    r.sharedPool.Stat(),
		DedicatedPoolCount: dedicatedCount,
		CachedConfigCount:  cacheCount,
	}
}
