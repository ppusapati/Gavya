package circuitbreaker

import (
	"context"
	"sync"
	"time"
)

// MemoryStorage provides an in-memory implementation of the Storage interface.
type MemoryStorage struct {
	states   map[string]*memoryEntry
	mu       sync.RWMutex
	maxAge   time.Duration
	stopChan chan struct{}
}

type memoryEntry struct {
	state     *CircuitState
	updatedAt time.Time
}

// NewMemoryStorage creates a new in-memory storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		states:   make(map[string]*memoryEntry),
		stopChan: make(chan struct{}),
	}
}

// Get retrieves the circuit state for the given key.
func (m *MemoryStorage) Get(ctx context.Context, key string) (*CircuitState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.states[key]
	if !ok {
		return nil, nil
	}

	// Return a copy to prevent external modification
	return copyState(entry.state), nil
}

// Save persists the circuit state for the given key.
func (m *MemoryStorage) Save(ctx context.Context, key string, state *CircuitState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.states[key] = &memoryEntry{
		state:     copyState(state),
		updatedAt: time.Now(),
	}
	return nil
}

// Delete removes the circuit state for the given key.
func (m *MemoryStorage) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, key)
	return nil
}

// GetAll returns all circuit states.
func (m *MemoryStorage) GetAll(ctx context.Context) (map[string]*CircuitState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*CircuitState, len(m.states))
	for key, entry := range m.states {
		result[key] = copyState(entry.state)
	}
	return result, nil
}

// GetByState returns all circuit states with the given state.
func (m *MemoryStorage) GetByState(ctx context.Context, state State) (map[string]*CircuitState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*CircuitState)
	for key, entry := range m.states {
		if entry.state.State == state {
			result[key] = copyState(entry.state)
		}
	}
	return result, nil
}

// Cleanup removes stale entries from the storage.
func (m *MemoryStorage) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	removed := 0
	for key, entry := range m.states {
		if now.Sub(entry.updatedAt) > maxAge {
			delete(m.states, key)
			removed++
		}
	}
	return removed
}

// Count returns the number of entries in storage.
func (m *MemoryStorage) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.states)
}

// Close stops background workers and releases resources.
func (m *MemoryStorage) Close() error {
	close(m.stopChan)
	return nil
}

// copyState creates a deep copy of a circuit state.
func copyState(src *CircuitState) *CircuitState {
	if src == nil {
		return nil
	}

	dst := &CircuitState{
		Config:           src.Config,
		State:            src.State,
		FailureCount:     src.FailureCount,
		SuccessCount:     src.SuccessCount,
		HalfOpenRequests: src.HalfOpenRequests,
	}

	if src.LastFailureAt != nil {
		t := *src.LastFailureAt
		dst.LastFailureAt = &t
	}
	if src.LastSuccessAt != nil {
		t := *src.LastSuccessAt
		dst.LastSuccessAt = &t
	}
	if src.OpenedAt != nil {
		t := *src.OpenedAt
		dst.OpenedAt = &t
	}
	if src.RecoveryAt != nil {
		t := *src.RecoveryAt
		dst.RecoveryAt = &t
	}

	return dst
}
