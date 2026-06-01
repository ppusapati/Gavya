package classregistry

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// PreWriteHook runs before a class_entity write commits. Returning a
// non-nil error aborts the write; the error surfaces to the caller
// as-is so domain-specific error codes propagate (e.g. a PHI
// enforcement hook returns its own typed error code, not a generic
// "hook failed" wrapper).
//
// The `old` argument is nil on inserts, populated with the previous
// row on updates. `new` is always populated (it's the about-to-be-
// committed row). Hooks that care about state transitions — "was
// this batch already released?" — inspect `old`; hooks that enforce
// invariants on the new row ("harvest_date must be after
// spray_date + PHI") inspect `new`.
type PreWriteHook func(ctx context.Context, tenantID string, old, new *ClassEntity) error

// PostWriteHook runs after commit. Errors are logged (by the caller)
// but do not abort — post-write hooks are observational (emit events,
// kick off notifications, warm caches). If a post-write hook needs
// to fail the whole flow, it should have been a pre-write hook.
type PostWriteHook func(ctx context.Context, tenantID string, entity *ClassEntity) error

// HookRegistry manages per-class pre/post-write hooks. Matches the
// SAP BAdI / Oracle business event / D365 plugin pattern — the
// generic EntityStore.Upsert fires registered hooks without knowing
// anything about the industry-specific logic they carry.
//
// Multiple hooks per (domain, class) are supported and run in
// registration order for PreWrite, reverse registration order for
// PostWrite (so cleanup hooks registered last run first). PreWrite
// chain short-circuits on the first error; PostWrite chain runs every
// hook regardless of intermediate errors and returns the first error
// (if any).
//
// Registration is thread-safe. Firing is thread-safe with the caveat
// that individual hooks are responsible for their own concurrency if
// they touch shared state. The registry does not serialize hook
// invocation.
type HookRegistry interface {
	// RegisterPreWrite adds a pre-write hook for (domain, class).
	RegisterPreWrite(domain, class string, hook PreWriteHook)

	// RegisterPostWrite adds a post-write hook for (domain, class).
	RegisterPostWrite(domain, class string, hook PostWriteHook)

	// FirePreWrite runs every registered pre-write hook for the
	// entity's class in order. Returns the first error or nil. Called
	// by EntityStore.Upsert before the SQL write commits.
	FirePreWrite(ctx context.Context, tenantID string, old, new *ClassEntity) error

	// FirePostWrite runs every registered post-write hook for the
	// entity's class. Does not short-circuit on errors — every hook
	// fires. Returns the first error encountered (if any) for caller
	// logging; the write has already committed so the error is
	// informational.
	FirePostWrite(ctx context.Context, tenantID string, entity *ClassEntity) error

	// ListRegistrations returns diagnostic info about what's registered.
	// Sorted by (domain, class, kind). Used by admin UIs and tests.
	ListRegistrations() []HookRegistration
}

// HookRegistration is a diagnostic description of one registered hook.
type HookRegistration struct {
	Domain string
	Class  string
	Kind   string // "pre_write" or "post_write"
	Index  int    // 0-based position within the (domain, class, kind) slot
}

// NewHookRegistry constructs the default in-memory registry.
func NewHookRegistry() HookRegistry {
	return &memHookRegistry{
		pre:  map[string][]PreWriteHook{},
		post: map[string][]PostWriteHook{},
	}
}

type memHookRegistry struct {
	mu   sync.RWMutex
	pre  map[string][]PreWriteHook  // key = domain|class
	post map[string][]PostWriteHook // key = domain|class
}

func hookKey(domain, class string) string { return domain + "|" + class }

func (r *memHookRegistry) RegisterPreWrite(domain, class string, hook PreWriteHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := hookKey(domain, class)
	r.pre[k] = append(r.pre[k], hook)
}

func (r *memHookRegistry) RegisterPostWrite(domain, class string, hook PostWriteHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := hookKey(domain, class)
	r.post[k] = append(r.post[k], hook)
}

func (r *memHookRegistry) FirePreWrite(ctx context.Context, tenantID string, old, new *ClassEntity) error {
	if new == nil {
		return fmt.Errorf("FirePreWrite: new entity is nil")
	}
	r.mu.RLock()
	hooks := append([]PreWriteHook(nil), r.pre[hookKey(new.Domain, new.Class)]...)
	r.mu.RUnlock()
	for i, h := range hooks {
		if err := h(ctx, tenantID, old, new); err != nil {
			return fmt.Errorf("pre-write hook #%d for %s/%s failed: %w",
				i, new.Domain, new.Class, err)
		}
	}
	return nil
}

func (r *memHookRegistry) FirePostWrite(ctx context.Context, tenantID string, entity *ClassEntity) error {
	if entity == nil {
		return fmt.Errorf("FirePostWrite: entity is nil")
	}
	r.mu.RLock()
	hooks := append([]PostWriteHook(nil), r.post[hookKey(entity.Domain, entity.Class)]...)
	r.mu.RUnlock()

	// Reverse order so cleanup hooks registered last run first
	// (mirrors defer semantics).
	var firstErr error
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](ctx, tenantID, entity); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("post-write hook #%d for %s/%s failed: %w",
				i, entity.Domain, entity.Class, err)
		}
	}
	return firstErr
}

func (r *memHookRegistry) ListRegistrations() []HookRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []HookRegistration
	for k, hooks := range r.pre {
		domain, class := splitHookKey(k)
		for i := range hooks {
			out = append(out, HookRegistration{Domain: domain, Class: class, Kind: "pre_write", Index: i})
		}
	}
	for k, hooks := range r.post {
		domain, class := splitHookKey(k)
		for i := range hooks {
			out = append(out, HookRegistration{Domain: domain, Class: class, Kind: "post_write", Index: i})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func splitHookKey(k string) (string, string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}
