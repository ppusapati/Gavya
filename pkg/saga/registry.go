// Registry: process-wide saga-type → SagaHandler registration.
package saga

import (
	"fmt"
	"sync"
)

// sagaHandlerRegistry is a thread-safe map from sagaType → SagaHandler.
// The registration pattern: each domain package's fx.go calls
// `saga.GlobalSagaRegistry.Register(handler.SagaType(), handler)` in its
// module-init function so the orchestrator can resolve a handler by type
// at runtime. Added 2026-04-19 (B.8) to satisfy the 19 sagas/<domain>/fx.go
// files that reference it.
type sagaHandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]SagaHandler
}

// Register binds a handler to its saga type. Re-registration overwrites.
func (r *sagaHandlerRegistry) Register(sagaType string, handler SagaHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]SagaHandler)
	}
	r.handlers[sagaType] = handler
}

// Get resolves the handler for a saga type. Second return indicates presence.
func (r *sagaHandlerRegistry) Get(sagaType string) (SagaHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[sagaType]
	return h, ok
}

// MustGet returns the handler or panics — used by orchestrator code paths
// where a missing handler indicates a misconfiguration, not a runtime
// condition.
func (r *sagaHandlerRegistry) MustGet(sagaType string) SagaHandler {
	if h, ok := r.Get(sagaType); ok {
		return h
	}
	panic(fmt.Sprintf("saga: no handler registered for type %q", sagaType))
}

// Types returns the set of registered saga types. Order is not guaranteed.
func (r *sagaHandlerRegistry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	return out
}

// GlobalSagaRegistry is the process-wide singleton. Domain packages
// register against it in their fx init hooks.
var GlobalSagaRegistry = &sagaHandlerRegistry{}
