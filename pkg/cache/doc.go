// Package cache is the in-memory cache abstraction used via ServiceDeps.
//
// Exposes a Cache interface with Get / Set / Delete + TTL semantics, plus
// a CacheProvider that implementations register themselves with. The
// default implementation is an expiring sync.Map; NoopCache is a drop-in
// for environments where caching is disabled at configuration time.
//
// The contract is deliberately minimal — Get/Set/Delete keyed by string —
// so the backing store can be swapped (sync.Map, Redis, memcached) without
// touching callers. Values are stored as interface{}; callers are
// responsible for type-asserting on retrieval.
//
// Typical usage from ServiceDeps:
//
//	if err := deps.Cache.Set(ctx, key, value, 5*time.Minute); err != nil {
//	    return nil, err
//	}
//	var result User
//	if err := deps.Cache.Get(ctx, key, &result); err != nil {
//	    // cache miss — fall through to DB
//	}
package cache
