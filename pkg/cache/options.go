package cache

import (
	"time"
)

// CacheOptions defines configuration for the cache
type CacheOptions struct {
	Enabled             bool
	EnableExpiry        bool
	DefaultTTL          time.Duration
	ExpiryCheckInterval time.Duration
	MaxEntries          *int
}

// defaultOptions provides sensible default cache configuration
func defaultOptions() CacheOptions {
	return CacheOptions{
		Enabled:             true,
		EnableExpiry:        true,
		DefaultTTL:          1 * time.Hour,
		ExpiryCheckInterval: 5 * time.Minute,
		MaxEntries:          new(int),
	}
}
