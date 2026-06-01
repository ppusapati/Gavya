package serviceclient_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/serviceclient"
)

type FooCreated struct {
	ID string
}

func TestPublishSubscribe_FanOut(t *testing.T) {
	b := bus.New()
	var a, b2 int32

	sub := serviceclient.NewInProcSubscriber[FooCreated](b)
	if _, err := sub.Subscribe(func(ctx context.Context, ev FooCreated) error {
		atomic.AddInt32(&a, 1)
		return nil
	}); err != nil {
		t.Fatalf("sub A: %v", err)
	}
	if _, err := sub.Subscribe(func(ctx context.Context, ev FooCreated) error {
		atomic.AddInt32(&b2, 1)
		return nil
	}); err != nil {
		t.Fatalf("sub B: %v", err)
	}

	pub := serviceclient.NewInProcPublisher[FooCreated](b)
	if err := pub.Publish(context.Background(), FooCreated{ID: "1"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if atomic.LoadInt32(&a) != 1 || atomic.LoadInt32(&b2) != 1 {
		t.Errorf("fan-out failed: a=%d b=%d", a, b2)
	}
}

func TestSubscribe_DisposerUnhooks(t *testing.T) {
	b := bus.New()
	var count int32

	sub := serviceclient.NewInProcSubscriber[FooCreated](b)
	disposer, err := sub.Subscribe(func(ctx context.Context, ev FooCreated) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	pub := serviceclient.NewInProcPublisher[FooCreated](b)
	_ = pub.Publish(context.Background(), FooCreated{ID: "1"})
	if atomic.LoadInt32(&count) != 1 {
		t.Fatalf("pre-dispose delivery missed: count=%d", count)
	}

	if err := disposer.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	_ = pub.Publish(context.Background(), FooCreated{ID: "2"})
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("post-dispose delivery should be suppressed: count=%d", count)
	}
}

func TestSubscribe_HandlerErrorStopsFanOut(t *testing.T) {
	// Mirrors packages/events/bus behaviour — publish() short-circuits on
	// first handler error. Pin this so future changes to the bus surface it
	// clearly rather than silently changing behaviour.
	b := bus.New()
	var afterCount int32
	boom := errors.New("stop")

	sub := serviceclient.NewInProcSubscriber[FooCreated](b)
	_, _ = sub.Subscribe(func(ctx context.Context, ev FooCreated) error {
		return boom
	})
	_, _ = sub.Subscribe(func(ctx context.Context, ev FooCreated) error {
		atomic.AddInt32(&afterCount, 1)
		return nil
	})

	pub := serviceclient.NewInProcPublisher[FooCreated](b)
	err := pub.Publish(context.Background(), FooCreated{ID: "1"})
	if !errors.Is(err, boom) {
		t.Fatalf("Publish should propagate first handler's error, got %v", err)
	}
	if atomic.LoadInt32(&afterCount) != 0 {
		t.Errorf("later subscribers should not fire after error: afterCount=%d", afterCount)
	}
}
