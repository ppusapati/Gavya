package serviceclient_test

import (
	"sync"
	"testing"

	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/serviceclient"
)

func TestRegistry_DefaultIsStable(t *testing.T) {
	reg := serviceclient.NewRegistry()
	if reg.Default() == nil {
		t.Fatal("default bus must not be nil")
	}
	if reg.Default() != reg.Default() {
		t.Error("Default() must return the same instance across calls")
	}
}

func TestRegistry_WithDefaultSharesBus(t *testing.T) {
	shared := bus.New()
	reg := serviceclient.NewRegistryWithDefault(shared)
	if reg.Default() != shared {
		t.Error("NewRegistryWithDefault must reuse the given bus")
	}
}

func TestRegistry_NamedBusesAreMemoized(t *testing.T) {
	reg := serviceclient.NewRegistry()

	a := reg.Bus("bi")
	b := reg.Bus("bi")
	c := reg.Bus("sales")

	if a == nil || b == nil || c == nil {
		t.Fatal("Bus must never return nil")
	}
	if a != b {
		t.Error("same-name lookups must return the same bus")
	}
	if a == c {
		t.Error("different-name lookups must return different buses")
	}
}

func TestRegistry_ConcurrentLookup(t *testing.T) {
	reg := serviceclient.NewRegistry()
	const N = 50

	var wg sync.WaitGroup
	results := make([]*bus.EventBus, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = reg.Bus("bi")
		}(i)
	}
	wg.Wait()

	for i := 1; i < N; i++ {
		if results[i] != results[0] {
			t.Fatal("concurrent Bus() calls must return the same instance")
		}
	}
}
