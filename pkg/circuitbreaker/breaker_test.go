package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"p9e.in/samavaya/packages/p9log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_Basic(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    3,
		SuccessThreshold:    2,
		RecoveryTimeout:     100 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Initially closed
	result, err := cb.Check(ctx, key)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, StateClosed, result.State)
}

func TestCircuitBreaker_OpensOnFailures(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    3,
		SuccessThreshold:    2,
		RecoveryTimeout:     time.Hour, // Long timeout for test
		HalfOpenMaxRequests: 2,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Record failures to open the circuit
	for i := 0; i < 3; i++ {
		_, err := cb.Check(ctx, key)
		require.NoError(t, err)
		err = cb.RecordFailure(ctx, key)
		require.NoError(t, err)
	}

	// Should be open now
	result, err := cb.Check(ctx, key)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, StateOpen, result.State)
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    2,
		SuccessThreshold:    2,
		RecoveryTimeout:     50 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Check(ctx, key)
		cb.RecordFailure(ctx, key)
	}

	// Wait for recovery timeout
	time.Sleep(100 * time.Millisecond)

	// Should transition to half-open
	result, err := cb.Check(ctx, key)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, StateHalfOpen, result.State)
}

func TestCircuitBreaker_ClosesOnSuccesses(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    2,
		SuccessThreshold:    2,
		RecoveryTimeout:     10 * time.Millisecond,
		HalfOpenMaxRequests: 5,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Open the circuit
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)

	// Wait for recovery
	time.Sleep(50 * time.Millisecond)

	// Transition to half-open
	result, err := cb.Check(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, StateHalfOpen, result.State)

	// Record successes to close
	cb.RecordSuccess(ctx, key)
	cb.RecordSuccess(ctx, key)

	// Should be closed now
	result, err = cb.Check(ctx, key)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, StateClosed, result.State)
}

func TestCircuitBreaker_ReopensOnFailureInHalfOpen(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    2,
		SuccessThreshold:    3,
		RecoveryTimeout:     10 * time.Millisecond,
		HalfOpenMaxRequests: 5,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Open the circuit
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)

	// Wait for recovery
	time.Sleep(50 * time.Millisecond)

	// Transition to half-open
	result, _ := cb.Check(ctx, key)
	assert.Equal(t, StateHalfOpen, result.State)

	// Fail in half-open
	cb.RecordFailure(ctx, key)

	// Should be open again
	result, _ = cb.Check(ctx, key)
	assert.False(t, result.Allowed)
	assert.Equal(t, StateOpen, result.State)
}

func TestCircuitBreaker_Execute(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    2,
		SuccessThreshold:    2,
		RecoveryTimeout:     time.Hour,
		HalfOpenMaxRequests: 2,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Success case
	err := cb.Execute(ctx, key, func() error {
		return nil
	})
	assert.NoError(t, err)

	// Failure cases to open circuit
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		err = cb.Execute(ctx, key, func() error {
			return testErr
		})
		assert.Equal(t, testErr, err)
	}

	// Circuit should be open
	err = cb.Execute(ctx, key, func() error {
		return nil
	})
	assert.True(t, IsCircuitOpenError(err))
}

func TestCircuitBreaker_Reset(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    2,
		SuccessThreshold:    2,
		RecoveryTimeout:     time.Hour,
		HalfOpenMaxRequests: 2,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Open the circuit
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)

	result, _ := cb.Check(ctx, key)
	assert.Equal(t, StateOpen, result.State)

	// Reset
	err := cb.Reset(ctx, key)
	require.NoError(t, err)

	// Should be closed
	result, _ = cb.Check(ctx, key)
	assert.True(t, result.Allowed)
	assert.Equal(t, StateClosed, result.State)
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()
	cb := New(storage, logger, WithDefaultConfig(Config{
		Name:                "test",
		FailureThreshold:    100,
		SuccessThreshold:    10,
		RecoveryTimeout:     time.Second,
		HalfOpenMaxRequests: 10,
	}))
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.Check(ctx, key)
			if i%2 == 0 {
				cb.RecordSuccess(ctx, key)
			} else {
				cb.RecordFailure(ctx, key)
			}
		}()
	}
	wg.Wait()

	// Should not panic and state should be consistent
	result, err := cb.Check(ctx, key)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	logger := p9log.NewNopLogger()
	storage := NewMemoryStorage()

	var events []StateChangeEvent
	var mu sync.Mutex

	cb := New(storage, logger,
		WithDefaultConfig(Config{
			Name:                "test",
			FailureThreshold:    2,
			SuccessThreshold:    2,
			RecoveryTimeout:     time.Hour,
			HalfOpenMaxRequests: 2,
		}),
		WithOnStateChange(func(event StateChangeEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}),
	)
	defer cb.Stop()

	ctx := context.Background()
	key := "test-service"

	// Open the circuit
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)
	cb.Check(ctx, key)
	cb.RecordFailure(ctx, key)

	mu.Lock()
	assert.Len(t, events, 1)
	assert.Equal(t, StateClosed, events[0].OldState)
	assert.Equal(t, StateOpen, events[0].NewState)
	mu.Unlock()
}

func TestMemoryStorage_Cleanup(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	// Add some states
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		storage.Save(ctx, key, NewCircuitState(DefaultConfig()))
	}

	assert.Equal(t, 5, storage.Count())

	// Wait and cleanup
	time.Sleep(10 * time.Millisecond)
	removed := storage.Cleanup(5 * time.Millisecond)
	assert.Equal(t, 5, removed)
	assert.Equal(t, 0, storage.Count())
}
