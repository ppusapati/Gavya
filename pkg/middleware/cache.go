package middleware

import (
	"context"
	"crypto/md5"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

// CacheMiddleware implements caching for RPC calls
type CacheMiddleware struct {
	client *redis.Client
	ttl    time.Duration
}

// NewCacheMiddleware creates a new cache middleware
func NewCacheMiddleware(redisURL string, ttl time.Duration) (*CacheMiddleware, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &CacheMiddleware{
		client: client,
		ttl:    ttl,
	}, nil
}

// UnaryInterceptor returns a unary RPC interceptor
func (c *CacheMiddleware) UnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		// Only cache GET-like operations
		if !isReadOperation(method) {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		// Generate cache key
		cacheKey := generateCacheKey(method, req)

		// Try to get from cache
		_, err := c.client.Get(ctx, cacheKey).Result()
		if err == nil {
			// Found in cache - deserialize and return
			// TODO: Implement deserialization based on method
			return nil
		}

		// Not in cache - call the actual RPC
		err = invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			return err
		}

		// Cache the result
		// TODO: Implement serialization based on reply type
		_ = c.client.Set(ctx, cacheKey, "", c.ttl).Err()

		return nil
	}
}

// generateCacheKey creates a cache key from method and request
func generateCacheKey(method string, req interface{}) string {
	// Hash the request to create a stable key
	hash := md5.Sum([]byte(fmt.Sprintf("%v", req)))
	return fmt.Sprintf("rpc:%s:%x", method, hash)
}

// isReadOperation determines if a method is read-only
func isReadOperation(method string) bool {
	readMethods := map[string]bool{
		"Get":         true,
		"List":        true,
		"Describe":    true,
		"Search":      true,
		"GetAnalytics": true,
	}

	// Check if method contains any read operation keywords
	for op := range readMethods {
		if len(method) > len(op) && method[:len(op)] == op {
			return true
		}
	}
	return false
}

// InvalidateCache clears cache for a service
func (c *CacheMiddleware) InvalidateCache(ctx context.Context, servicePattern string) error {
	iter := c.client.Scan(ctx, 0, fmt.Sprintf("rpc:%s:*", servicePattern), 0).Iterator()
	for iter.Next(ctx) {
		if err := c.client.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}

// Close closes the cache connection
func (c *CacheMiddleware) Close() error {
	return c.client.Close()
}

// CacheStats returns cache statistics
func (c *CacheMiddleware) CacheStats(ctx context.Context) (map[string]string, error) {
	info := c.client.Info(ctx, "stats")
	return map[string]string{"info": info.Val()}, nil
}
