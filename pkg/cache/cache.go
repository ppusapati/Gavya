package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"
	"p9e.in/samavaya/packages/metrics"
	"p9e.in/samavaya/packages/p9log"
)

// Cache interface for standard cache operations
type Cache interface {
	Set(key interface{}, value interface{})
	SetWithTTL(key interface{}, value interface{}, ttl time.Duration)
	Get(key interface{}) (interface{}, bool)
	GetJSON(key interface{}, dest interface{}) error
	SetJSON(key interface{}, value interface{}) error
	Delete(key interface{})
	Exists(key interface{}) bool
	Clear()
	Close()
}

// CacheProvider implements Cache using sync.Map
type CacheProvider struct {
	data         sync.Map
	log          p9log.Helper
	metrics      metrics.MetricsProvider
	expiryTicker *time.Ticker
	stopChan     chan struct{}
	cfg          *config.Data
}

// NoopCache is a fallback cache provider (for disabled caching)
type NoopCache struct{}

func (n *NoopCache) Set(key, value interface{})                           {}
func (n *NoopCache) SetWithTTL(key, value interface{}, ttl time.Duration) {}
func (n *NoopCache) Get(key interface{}) (interface{}, bool)              { return nil, false }
func (n *NoopCache) GetJSON(key interface{}, dest interface{}) error {
	return fmt.Errorf("cache disabled")
}
func (n *NoopCache) SetJSON(key interface{}, value interface{}) error {
	return fmt.Errorf("cache disabled")
}
func (n *NoopCache) Exists(key interface{}) bool { return false }
func (n *NoopCache) Delete(key interface{})      {}
func (n *NoopCache) Clear()                      {}
func (n *NoopCache) Close()                      {}

// NewCacheProvider initializes the cache provider
func NewCacheProvider(
	log p9log.Logger,
	metrics metrics.MetricsProvider,
	cfg *config.Data,
) *CacheProvider {
	options := defaultOptions()

	cache := &CacheProvider{
		log:      *p9log.NewHelper(p9log.With(log, "module", "cache")),
		metrics:  metrics,
		stopChan: make(chan struct{}),
		cfg:      cfg,
	}

	// Start expiry monitoring if enabled
	if options.EnableExpiry {
		cache.startExpiryMonitoring()
	}

	return cache
}

// NewCache creates the appropriate cache provider
func NewCache(
	ctx context.Context,
	cfg *config.Data,
	log p9log.Logger,
	metrics metrics.MetricsProvider,
) Cache {
	if cfg == nil || (cfg.Cache != nil && !cfg.Cache.Enabled.GetValue()) {
		return &NoopCache{}
	}

	cache := NewCacheProvider(log, metrics, cfg)

	go func() {
		<-ctx.Done()
		cache.Close()
	}()

	return cache
}

// Set stores a key-value pair in cache
func (c *CacheProvider) Set(key interface{}, value interface{}) {
	c.SetWithTTL(key, value, c.cfg.Cache.DefaultTtl.AsDuration())
}

// SetWithTTL stores a key-value pair with a custom TTL
func (c *CacheProvider) SetWithTTL(key interface{}, value interface{}, ttl time.Duration) {
	if !c.cfg.Cache.Enabled.GetValue() {
		return
	}

	if c.cfg.Cache.MaxEntries != nil && c.cfg.Cache.MaxEntries.GetValue() > int32(0) {
		if c.countEntries() >= c.cfg.Cache.MaxEntries.GetValue() {
			c.evictLeastRecentlyUsed()
		}
	}

	entry := cacheEntry{
		value:      value,
		expiration: time.Now().Add(ttl),
		timestamp:  time.Now(),
	}

	c.data.Store(key, entry)
}

// Get retrieves a value from cache
func (c *CacheProvider) Get(key interface{}) (interface{}, bool) {
	if !c.cfg.Cache.Enabled.GetValue() {
		return nil, false
	}

	value, ok := c.data.Load(key)
	if !ok {
		return nil, false
	}

	entry, valid := value.(cacheEntry)
	if !valid {
		return nil, false
	}

	if c.cfg.Cache.EnableExpiry.GetValue() && time.Now().After(entry.expiration) {
		c.data.Delete(key)
		return nil, false
	}

	return entry.value, true
}

// GetJSON retrieves a JSON object from cache
func (c *CacheProvider) GetJSON(key interface{}, dest interface{}) error {
	if !c.cfg.Cache.Enabled.GetValue() {
		return fmt.Errorf("cache disabled")
	}

	value, ok := c.Get(key)
	if !ok {
		return fmt.Errorf("cache miss")
	}

	data, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid cache entry format")
	}

	return json.Unmarshal(data, dest)
}

// SetJSON stores an object in cache as JSON
func (c *CacheProvider) SetJSON(key interface{}, value interface{}) error {
	if !c.cfg.Cache.Enabled.GetValue() {
		return fmt.Errorf("cache disabled")
	}

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	c.Set(key, data)
	return nil
}

// Exists checks if a key exists in the cache
func (c *CacheProvider) Exists(key interface{}) bool {
	_, exists := c.data.Load(key)
	return exists
}

// Delete removes a key from the cache
func (c *CacheProvider) Delete(key interface{}) {
	if !c.cfg.Cache.Enabled.GetValue() {
		return
	}
	c.data.Delete(key)
}

// Clear removes all items from the cache
func (c *CacheProvider) Clear() {
	if !c.cfg.Cache.Enabled.GetValue() {
		return
	}
	c.data.Range(func(key, _ interface{}) bool {
		c.data.Delete(key)
		return true
	})
}

// Close shuts down background processes
func (c *CacheProvider) Close() {
	if c.expiryTicker != nil {
		c.expiryTicker.Stop()
	}
	close(c.stopChan)
}

// cleanupExpiredEntries removes expired items
func (c *CacheProvider) cleanupExpiredEntries() {
	if !c.cfg.Cache.EnableExpiry.GetValue() {
		return
	}

	now := time.Now()
	c.data.Range(func(key, value interface{}) bool {
		entry, ok := value.(cacheEntry)
		if ok && now.After(entry.expiration) {
			c.data.Delete(key)
		}
		return true
	})
}

// evictLeastRecentlyUsed removes the least recently used (LRU) entry
func (c *CacheProvider) evictLeastRecentlyUsed() {
	var lruKey interface{}
	var oldestTimestamp time.Time

	c.data.Range(func(key, value interface{}) bool {
		entry := value.(cacheEntry)
		if lruKey == nil || entry.timestamp.Before(oldestTimestamp) {
			lruKey = key
			oldestTimestamp = entry.timestamp
		}
		return true
	})

	if lruKey != nil {
		c.data.Delete(lruKey)
		c.log.Infof("Evicted LRU cache entry: %v", lruKey)
	}
}

// startExpiryMonitoring runs a background process to remove expired entries
func (c *CacheProvider) startExpiryMonitoring() {
	interval := c.cfg.Cache.ExpiryCheckInterval.AsDuration()
	if interval <= 0 {
		interval = defaultExpiryCheckInterval
	}

	c.expiryTicker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-c.stopChan:
				return
			case <-c.expiryTicker.C:
				c.cleanupExpiredEntries()
			}
		}
	}()
}

// countEntries returns the number of entries in the cache
func (c *CacheProvider) countEntries() int32 {
	var count int32
	c.data.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// cacheEntry represents a cache entry
type cacheEntry struct {
	value      interface{}
	expiration time.Time
	timestamp  time.Time
}

// Default constants
var defaultExpiryCheckInterval = 5 * time.Minute
