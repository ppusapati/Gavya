package serviceclient

import (
	"sync"

	"p9e.in/samavaya/packages/events/bus"
)

// Registry holds named bus instances. Most services use the default bus
// (via Default()), but large deployments sometimes want traffic isolation —
// e.g. a dedicated bus for BI events so slow BI subscribers don't block
// sales events.
//
// Registry is safe for concurrent use. Instances are created on first
// lookup; there's no separate "register" step. This keeps usage simple:
//
//	reg := serviceclient.NewRegistry()
//	b := reg.Bus("bi")  // creates if missing, returns existing otherwise
type Registry struct {
	mu     sync.RWMutex
	buses  map[string]*bus.EventBus
	defaultBus *bus.EventBus
}

// NewRegistry builds a registry backed by a fresh default bus. Callers that
// want to share the package-level bus.Default can pass NewRegistryWithDefault(bus.Default).
func NewRegistry() *Registry {
	return &Registry{
		buses:      map[string]*bus.EventBus{},
		defaultBus: bus.New(),
	}
}

// NewRegistryWithDefault builds a registry that uses the supplied bus as its
// default. Useful when the composition root wants to share a single bus
// across services without per-service buses.
func NewRegistryWithDefault(b *bus.EventBus) *Registry {
	return &Registry{
		buses:      map[string]*bus.EventBus{},
		defaultBus: b,
	}
}

// Default returns the registry's default bus. This is the bus most services
// use unless they have a specific reason to isolate traffic.
func (r *Registry) Default() *bus.EventBus {
	return r.defaultBus
}

// Bus returns the named bus, creating it on first access. Name is an
// opaque tag — we don't enforce a format because it's only a map key.
// Services typically use their top-level namespace ("bi", "sales",
// "inventory").
func (r *Registry) Bus(name string) *bus.EventBus {
	r.mu.RLock()
	b, ok := r.buses[name]
	r.mu.RUnlock()
	if ok {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under write lock.
	if b, ok := r.buses[name]; ok {
		return b
	}
	b = bus.New()
	r.buses[name] = b
	return b
}
