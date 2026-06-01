package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/p9log"
)

// CircuitBreaker manages circuit breaker state for multiple circuits.
type CircuitBreaker struct {
	storage Storage
	logger  *p9log.Helper
	opts    Options

	// In-memory cache for hot circuit breakers
	cache   map[string]*cachedState
	cacheMu sync.RWMutex

	// Background worker control
	stopChan chan struct{}
	wg       sync.WaitGroup
}

type cachedState struct {
	state     *CircuitState
	mu        sync.Mutex
	lastCheck time.Time
}

// New creates a new circuit breaker manager.
func New(storage Storage, logger p9log.Logger, opts ...Option) *CircuitBreaker {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &CircuitBreaker{
		storage:  storage,
		logger:   p9log.NewHelper(p9log.With(logger, "component", "circuitbreaker")),
		opts:     options,
		cache:    make(map[string]*cachedState),
		stopChan: make(chan struct{}),
	}
}

// Check checks if a request is allowed through the circuit breaker.
// The key uniquely identifies the circuit (e.g., "service:operation", "integration:123").
func (cb *CircuitBreaker) Check(ctx context.Context, key string) (*CheckResult, error) {
	return cb.CheckWithConfig(ctx, key, cb.opts.DefaultConfig)
}

// CheckWithConfig checks if a request is allowed using a custom configuration.
func (cb *CircuitBreaker) CheckWithConfig(ctx context.Context, key string, cfg Config) (*CheckResult, error) {
	state, err := cb.getOrCreateState(ctx, key, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get circuit state: %w", err)
	}

	cached := cb.getOrCreateCache(key, state)
	cached.mu.Lock()
	defer cached.mu.Unlock()

	now := time.Now()

	switch state.State {
	case StateClosed:
		return &CheckResult{
			Allowed:      true,
			State:        StateClosed,
			FailureCount: state.FailureCount,
			SuccessCount: state.SuccessCount,
		}, nil

	case StateOpen:
		// Check if recovery time has passed
		if state.ShouldTransitionToHalfOpen() {
			// Transition to half-open
			oldState := state.State
			state.State = StateHalfOpen
			state.SuccessCount = 0
			state.HalfOpenRequests = 0

			if err := cb.storage.Save(ctx, key, state); err != nil {
				cb.logger.Warn("failed to save state transition",
					"key", key,
					"error", err,
				)
			}
			cached.state = state

			cb.notifyStateChange(key, oldState, StateHalfOpen, "recovery timeout elapsed")

			return &CheckResult{
				Allowed:      true,
				State:        StateHalfOpen,
				FailureCount: state.FailureCount,
				SuccessCount: state.SuccessCount,
			}, nil
		}

		// Still in recovery period
		waitTime := state.GetWaitTime()
		return &CheckResult{
			Allowed:      false,
			State:        StateOpen,
			FailureCount: state.FailureCount,
			RecoveryAt:   state.RecoveryAt,
			WaitTime:     waitTime,
		}, nil

	case StateHalfOpen:
		// Check if we've hit the max requests limit
		if state.HalfOpenRequests >= state.Config.HalfOpenMaxRequests {
			return &CheckResult{
				Allowed:      false,
				State:        StateHalfOpen,
				FailureCount: state.FailureCount,
				SuccessCount: state.SuccessCount,
				WaitTime:     time.Second, // Short wait before retry
			}, nil
		}

		// Increment half-open request counter
		state.HalfOpenRequests++
		cached.state = state
		cached.lastCheck = now

		return &CheckResult{
			Allowed:      true,
			State:        StateHalfOpen,
			FailureCount: state.FailureCount,
			SuccessCount: state.SuccessCount,
		}, nil

	default:
		// Unknown state - allow but log warning
		cb.logger.Warn("unknown circuit breaker state",
			"key", key,
			"state", state.State,
		)
		return &CheckResult{
			Allowed: true,
			State:   StateClosed,
		}, nil
	}
}

// RecordSuccess records a successful operation.
func (cb *CircuitBreaker) RecordSuccess(ctx context.Context, key string) error {
	state, err := cb.getState(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get circuit state: %w", err)
	}

	if state == nil {
		return nil // No circuit breaker configured
	}

	cached := cb.getOrCreateCache(key, state)
	cached.mu.Lock()
	defer cached.mu.Unlock()

	now := time.Now()
	state.LastSuccessAt = &now

	switch state.State {
	case StateClosed:
		// Reset failure count on success
		state.FailureCount = 0

	case StateHalfOpen:
		state.SuccessCount++

		// Check if we should close the circuit
		if state.SuccessCount >= state.Config.SuccessThreshold {
			oldState := state.State
			state.State = StateClosed
			state.FailureCount = 0
			state.SuccessCount = 0
			state.HalfOpenRequests = 0
			state.OpenedAt = nil
			state.RecoveryAt = nil

			cb.notifyStateChange(key, oldState, StateClosed, "success threshold reached")
		}
	}

	if err := cb.storage.Save(ctx, key, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	cached.state = state
	cached.lastCheck = now

	return nil
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure(ctx context.Context, key string) error {
	state, err := cb.getState(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get circuit state: %w", err)
	}

	if state == nil {
		return nil // No circuit breaker configured
	}

	cached := cb.getOrCreateCache(key, state)
	cached.mu.Lock()
	defer cached.mu.Unlock()

	now := time.Now()
	state.LastFailureAt = &now
	state.FailureCount++

	switch state.State {
	case StateClosed:
		// Check if we should open the circuit
		if state.FailureCount >= state.Config.FailureThreshold {
			oldState := state.State
			state.State = StateOpen
			state.OpenedAt = &now
			recoveryAt := now.Add(state.Config.RecoveryTimeout)
			state.RecoveryAt = &recoveryAt

			cb.notifyStateChange(key, oldState, StateOpen, "failure threshold reached")
		}

	case StateHalfOpen:
		// Single failure in half-open reopens the circuit
		oldState := state.State
		state.State = StateOpen
		state.OpenedAt = &now
		recoveryAt := now.Add(state.Config.RecoveryTimeout)
		state.RecoveryAt = &recoveryAt
		state.SuccessCount = 0
		state.HalfOpenRequests = 0

		cb.notifyStateChange(key, oldState, StateOpen, "failure in half-open state")
	}

	if err := cb.storage.Save(ctx, key, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	cached.state = state
	cached.lastCheck = now

	return nil
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset(ctx context.Context, key string) error {
	state, err := cb.getState(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get circuit state: %w", err)
	}

	if state == nil {
		return nil
	}

	oldState := state.State
	state.State = StateClosed
	state.FailureCount = 0
	state.SuccessCount = 0
	state.HalfOpenRequests = 0
	state.OpenedAt = nil
	state.RecoveryAt = nil

	if err := cb.storage.Save(ctx, key, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	cb.cacheMu.Lock()
	delete(cb.cache, key)
	cb.cacheMu.Unlock()

	if oldState != StateClosed {
		cb.notifyStateChange(key, oldState, StateClosed, "manual reset")
	}

	return nil
}

// Remove removes the circuit breaker for the given key.
func (cb *CircuitBreaker) Remove(ctx context.Context, key string) error {
	if err := cb.storage.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to delete circuit state: %w", err)
	}

	cb.cacheMu.Lock()
	delete(cb.cache, key)
	cb.cacheMu.Unlock()

	return nil
}

// GetStatus returns the current status of a circuit breaker.
func (cb *CircuitBreaker) GetStatus(ctx context.Context, key string) (*CircuitState, error) {
	return cb.getState(ctx, key)
}

// ProcessRecoveries checks for open circuit breakers that can transition to half-open.
func (cb *CircuitBreaker) ProcessRecoveries(ctx context.Context) error {
	states, err := cb.storage.GetByState(ctx, StateOpen)
	if err != nil {
		return fmt.Errorf("failed to get open circuits: %w", err)
	}

	for key, state := range states {
		if state.ShouldTransitionToHalfOpen() {
			oldState := state.State
			state.State = StateHalfOpen
			state.SuccessCount = 0
			state.HalfOpenRequests = 0

			if err := cb.storage.Save(ctx, key, state); err != nil {
				cb.logger.Warn("failed to transition to half-open",
					"key", key,
					"error", err,
				)
				continue
			}

			cb.updateCache(key, state)
			cb.notifyStateChange(key, oldState, StateHalfOpen, "recovery worker")

			cb.logger.Info("circuit breaker transitioned to half-open",
				"key", key,
			)
		}
	}

	return nil
}

// Cleanup removes stale entries from the cache.
func (cb *CircuitBreaker) Cleanup(maxAge time.Duration) {
	cb.cacheMu.Lock()
	defer cb.cacheMu.Unlock()

	now := time.Now()
	for key, cached := range cb.cache {
		if now.Sub(cached.lastCheck) > maxAge {
			delete(cb.cache, key)
		}
	}
}

// StartBackgroundWorkers starts background workers for recovery and cleanup.
func (cb *CircuitBreaker) StartBackgroundWorkers(ctx context.Context) {
	// Recovery worker
	cb.wg.Add(1)
	go func() {
		defer cb.wg.Done()
		ticker := time.NewTicker(cb.opts.CleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-cb.stopChan:
				return
			case <-ticker.C:
				if err := cb.ProcessRecoveries(ctx); err != nil {
					cb.logger.Error("failed to process recoveries", "error", err)
				}
				cb.Cleanup(cb.opts.MaxAge)
			}
		}
	}()
}

// Stop stops background workers.
func (cb *CircuitBreaker) Stop() {
	close(cb.stopChan)
	cb.wg.Wait()
}

// getState retrieves state from cache or storage.
func (cb *CircuitBreaker) getState(ctx context.Context, key string) (*CircuitState, error) {
	// Check cache first
	cb.cacheMu.RLock()
	cached, ok := cb.cache[key]
	cb.cacheMu.RUnlock()

	if ok {
		return cached.state, nil
	}

	// Load from storage
	return cb.storage.Get(ctx, key)
}

// getOrCreateState gets or creates a circuit state with the given configuration.
func (cb *CircuitBreaker) getOrCreateState(ctx context.Context, key string, cfg Config) (*CircuitState, error) {
	state, err := cb.getState(ctx, key)
	if err != nil {
		return nil, err
	}

	if state != nil {
		return state, nil
	}

	// Create new state with configuration
	state = NewCircuitState(cfg)
	state.Config.Name = key

	if err := cb.storage.Save(ctx, key, state); err != nil {
		return nil, fmt.Errorf("failed to create circuit state: %w", err)
	}

	return state, nil
}

// getOrCreateCache gets or creates a cache entry.
func (cb *CircuitBreaker) getOrCreateCache(key string, state *CircuitState) *cachedState {
	cb.cacheMu.RLock()
	cached, ok := cb.cache[key]
	cb.cacheMu.RUnlock()

	if ok {
		return cached
	}

	cb.cacheMu.Lock()
	defer cb.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := cb.cache[key]; ok {
		return cached
	}

	cached = &cachedState{
		state:     state,
		lastCheck: time.Now(),
	}
	cb.cache[key] = cached

	return cached
}

// updateCache updates the cache with the given state.
func (cb *CircuitBreaker) updateCache(key string, state *CircuitState) {
	cb.cacheMu.Lock()
	defer cb.cacheMu.Unlock()

	if cached, ok := cb.cache[key]; ok {
		cached.state = state
		cached.lastCheck = time.Now()
	} else {
		cb.cache[key] = &cachedState{
			state:     state,
			lastCheck: time.Now(),
		}
	}
}

// notifyStateChange notifies the event handler of a state change.
func (cb *CircuitBreaker) notifyStateChange(key string, oldState, newState State, reason string) {
	if cb.opts.OnStateChange != nil {
		cb.opts.OnStateChange(StateChangeEvent{
			Key:       key,
			OldState:  oldState,
			NewState:  newState,
			Timestamp: time.Now(),
			Reason:    reason,
		})
	}
}

// Execute runs a function with circuit breaker protection.
// If the circuit is open, it returns ErrCircuitOpen.
// On success, it records a success. On failure (returned error), it records a failure.
func (cb *CircuitBreaker) Execute(ctx context.Context, key string, fn func() error) error {
	result, err := cb.Check(ctx, key)
	if err != nil {
		return fmt.Errorf("circuit breaker check failed: %w", err)
	}

	if !result.Allowed {
		return &CircuitOpenError{
			Key:        key,
			State:      result.State,
			RecoveryAt: result.RecoveryAt,
			WaitTime:   result.WaitTime,
		}
	}

	// Execute the function
	fnErr := fn()

	// Record result
	if fnErr != nil {
		if recordErr := cb.RecordFailure(ctx, key); recordErr != nil {
			cb.logger.Warn("failed to record failure",
				"key", key,
				"error", recordErr,
			)
		}
		return fnErr
	}

	if recordErr := cb.RecordSuccess(ctx, key); recordErr != nil {
		cb.logger.Warn("failed to record success",
			"key", key,
			"error", recordErr,
		)
	}

	return nil
}

// ExecuteWithConfig runs a function with custom circuit breaker configuration.
func (cb *CircuitBreaker) ExecuteWithConfig(ctx context.Context, key string, cfg Config, fn func() error) error {
	result, err := cb.CheckWithConfig(ctx, key, cfg)
	if err != nil {
		return fmt.Errorf("circuit breaker check failed: %w", err)
	}

	if !result.Allowed {
		return &CircuitOpenError{
			Key:        key,
			State:      result.State,
			RecoveryAt: result.RecoveryAt,
			WaitTime:   result.WaitTime,
		}
	}

	// Execute the function
	fnErr := fn()

	// Record result
	if fnErr != nil {
		if recordErr := cb.RecordFailure(ctx, key); recordErr != nil {
			cb.logger.Warn("failed to record failure",
				"key", key,
				"error", recordErr,
			)
		}
		return fnErr
	}

	if recordErr := cb.RecordSuccess(ctx, key); recordErr != nil {
		cb.logger.Warn("failed to record success",
			"key", key,
			"error", recordErr,
		)
	}

	return nil
}
