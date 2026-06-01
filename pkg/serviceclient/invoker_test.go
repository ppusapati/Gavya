package serviceclient_test

import (
	"context"
	"errors"
	"testing"

	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/serviceclient"
)

type GetFooReq struct {
	ID string
}

type GetFooResp struct {
	Name string
}

func TestInvoker_RoundTrip(t *testing.T) {
	b := bus.New()

	_, err := serviceclient.RegisterHandler[GetFooReq, GetFooResp](b,
		func(ctx context.Context, req GetFooReq) (GetFooResp, error) {
			return GetFooResp{Name: "foo_" + req.ID}, nil
		},
	)
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	inv := serviceclient.NewInProcInvoker[GetFooReq, GetFooResp](b)
	got, err := inv.Invoke(context.Background(), GetFooReq{ID: "42"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got.Name != "foo_42" {
		t.Errorf("unexpected resp: %+v", got)
	}
}

func TestInvoker_NoHandlerReturnsSentinel(t *testing.T) {
	b := bus.New()
	inv := serviceclient.NewInProcInvoker[GetFooReq, GetFooResp](b)

	_, err := inv.Invoke(context.Background(), GetFooReq{ID: "x"})
	if err == nil {
		t.Fatal("expected error when no handler registered")
	}
	if !errors.Is(err, serviceclient.ErrNoHandler) {
		t.Errorf("expected ErrNoHandler, got %v", err)
	}
}

func TestInvoker_HandlerErrorPropagates(t *testing.T) {
	b := bus.New()
	boom := errors.New("boom")

	_, err := serviceclient.RegisterHandler[GetFooReq, GetFooResp](b,
		func(ctx context.Context, req GetFooReq) (GetFooResp, error) {
			return GetFooResp{}, boom
		},
	)
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	inv := serviceclient.NewInProcInvoker[GetFooReq, GetFooResp](b)
	_, err = inv.Invoke(context.Background(), GetFooReq{ID: "x"})
	if !errors.Is(err, boom) {
		t.Errorf("handler error must propagate verbatim, got %v", err)
	}
}

func TestInvoker_DisposerRemovesHandler(t *testing.T) {
	b := bus.New()

	disposer, err := serviceclient.RegisterHandler[GetFooReq, GetFooResp](b,
		func(ctx context.Context, req GetFooReq) (GetFooResp, error) {
			return GetFooResp{Name: req.ID}, nil
		},
	)
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	inv := serviceclient.NewInProcInvoker[GetFooReq, GetFooResp](b)
	if _, err := inv.Invoke(context.Background(), GetFooReq{ID: "x"}); err != nil {
		t.Fatalf("pre-dispose Invoke: %v", err)
	}

	if err := disposer.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}

	_, err = inv.Invoke(context.Background(), GetFooReq{ID: "x"})
	if !errors.Is(err, serviceclient.ErrNoHandler) {
		t.Errorf("after Dispose, invoke must fail with ErrNoHandler, got %v", err)
	}
}
