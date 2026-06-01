package classregistry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHookRegistry_PreWrite_Blocks(t *testing.T) {
	r := NewHookRegistry()
	r.RegisterPreWrite("lotserial", "pharma_batch", func(_ context.Context, _ string, _, _ *ClassEntity) error {
		return errors.New("policy violation: spray_date too recent")
	})
	err := r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
		TenantID: "t1", Domain: "lotserial", Class: "pharma_batch",
	})
	if err == nil {
		t.Fatalf("expected blocking error from pre-write hook")
	}
}

func TestHookRegistry_PreWrite_ShortCircuits(t *testing.T) {
	r := NewHookRegistry()
	var second atomic.Bool
	r.RegisterPreWrite("x", "y", func(_ context.Context, _ string, _, _ *ClassEntity) error {
		return errors.New("first fails")
	})
	r.RegisterPreWrite("x", "y", func(_ context.Context, _ string, _, _ *ClassEntity) error {
		second.Store(true)
		return nil
	})
	_ = r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
		TenantID: "t1", Domain: "x", Class: "y",
	})
	if second.Load() {
		t.Fatalf("second hook must not run after first aborts")
	}
}

func TestHookRegistry_PostWrite_FiresAfterCommit(t *testing.T) {
	r := NewHookRegistry()
	var fired atomic.Int32
	r.RegisterPostWrite("x", "y", func(_ context.Context, _ string, _ *ClassEntity) error {
		fired.Add(1)
		return nil
	})
	if err := r.FirePostWrite(context.Background(), "t1", &ClassEntity{
		TenantID: "t1", Domain: "x", Class: "y",
	}); err != nil {
		t.Fatalf("post-write: %v", err)
	}
	if fired.Load() != 1 {
		t.Fatalf("post-write hook must run, fired=%d", fired.Load())
	}
}

func TestHookRegistry_PostWrite_AllFireOnError(t *testing.T) {
	r := NewHookRegistry()
	var ran atomic.Int32
	// Registered in order A, B, C. Post-write fires in reverse: C, B, A.
	// Hook B returns an error; A and C should still fire.
	r.RegisterPostWrite("x", "y", func(_ context.Context, _ string, _ *ClassEntity) error {
		ran.Add(1)
		return nil // A
	})
	r.RegisterPostWrite("x", "y", func(_ context.Context, _ string, _ *ClassEntity) error {
		ran.Add(1)
		return errors.New("B failed")
	})
	r.RegisterPostWrite("x", "y", func(_ context.Context, _ string, _ *ClassEntity) error {
		ran.Add(1)
		return nil // C
	})
	err := r.FirePostWrite(context.Background(), "t1", &ClassEntity{
		TenantID: "t1", Domain: "x", Class: "y",
	})
	if ran.Load() != 3 {
		t.Fatalf("all 3 post-write hooks must fire despite B's error, ran=%d", ran.Load())
	}
	if err == nil {
		t.Fatalf("expected B's error to surface")
	}
}

func TestHookRegistry_MissingClassPassesThrough(t *testing.T) {
	r := NewHookRegistry()
	// No hooks registered for x/y.
	err := r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
		TenantID: "t1", Domain: "x", Class: "y",
	})
	if err != nil {
		t.Fatalf("class with no hooks must pass through, got %v", err)
	}
}

func TestHookRegistry_ConcurrentRegistrationAndFire(t *testing.T) {
	r := NewHookRegistry()
	// 100 goroutines registering pre-write hooks concurrently while
	// another 100 fire. The registry's RWMutex must keep this safe.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.RegisterPreWrite("x", "y", func(_ context.Context, _ string, _, _ *ClassEntity) error {
				return nil
			})
		}()
	}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
				TenantID: "t1", Domain: "x", Class: "y",
			})
		}()
	}
	wg.Wait()
}

func TestHookRegistry_ListRegistrationsSorted(t *testing.T) {
	r := NewHookRegistry()
	r.RegisterPreWrite("b_dom", "b_cls", func(_ context.Context, _ string, _, _ *ClassEntity) error { return nil })
	r.RegisterPreWrite("a_dom", "a_cls", func(_ context.Context, _ string, _, _ *ClassEntity) error { return nil })
	r.RegisterPostWrite("a_dom", "a_cls", func(_ context.Context, _ string, _ *ClassEntity) error { return nil })
	regs := r.ListRegistrations()
	if len(regs) != 3 {
		t.Fatalf("expected 3 registrations, got %d", len(regs))
	}
	if regs[0].Domain != "a_dom" || regs[0].Kind != "post_write" {
		t.Fatalf("expected first = a_dom post_write, got %+v", regs[0])
	}
}

// ---------- PHI demo hook ----------
// Exemplifies a BAdI-equivalent hook. Not registered in production code
// — each deployment decides which hooks to wire. Lives here as a
// canonical example showing what industry-specific logic looks like
// when it attaches to the generic entity plane.

// phiEnforcementHook rejects pharma_batch writes where
// `harvest_date` is earlier than `spray_date + phi_days`.
//
// The fields are pulled from the new entity's attribute map; the
// hook is declarative in the sense that adding or removing it does
// not touch generic EntityStore code.
func phiEnforcementHook(_ context.Context, _ string, _, new *ClassEntity) error {
	if new == nil {
		return nil
	}
	spray, hasSpray := new.Attributes["spray_date"]
	harvest, hasHarvest := new.Attributes["harvest_date"]
	phiAttr, hasPhi := new.Attributes["phi_days"]
	if !hasSpray || !hasHarvest || !hasPhi {
		// Incomplete data — shape validator handles required checks;
		// the hook only kicks in when all three are present.
		return nil
	}
	phiDays := int(phiAttr.Int)
	threshold := spray.Date.AddDate(0, 0, phiDays)
	if harvest.Date.Before(threshold) {
		return fmt.Errorf("PHI_VIOLATION: harvest_date %s is before spray_date %s + %d days (earliest allowed: %s)",
			harvest.Date.Format("2006-01-02"),
			spray.Date.Format("2006-01-02"),
			phiDays,
			threshold.Format("2006-01-02"))
	}
	return nil
}

func TestHookRegistry_PHIDemoRejectsShortInterval(t *testing.T) {
	r := NewHookRegistry()
	r.RegisterPreWrite("lotserial", "pharma_batch", phiEnforcementHook)
	spray := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	err := r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
		TenantID: "t1", Domain: "lotserial", Class: "pharma_batch",
		Attributes: map[string]AttributeValue{
			"spray_date":   {Kind: KindDate, Date: spray},
			"harvest_date": {Kind: KindDate, Date: spray.AddDate(0, 0, 7)}, // only 7 days later
			"phi_days":     {Kind: KindInt, Int: 14},
		},
	})
	if err == nil {
		t.Fatalf("7-day interval must fail 14-day PHI")
	}
}

func TestHookRegistry_PHIDemoAcceptsLongInterval(t *testing.T) {
	r := NewHookRegistry()
	r.RegisterPreWrite("lotserial", "pharma_batch", phiEnforcementHook)
	spray := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	err := r.FirePreWrite(context.Background(), "t1", nil, &ClassEntity{
		TenantID: "t1", Domain: "lotserial", Class: "pharma_batch",
		Attributes: map[string]AttributeValue{
			"spray_date":   {Kind: KindDate, Date: spray},
			"harvest_date": {Kind: KindDate, Date: spray.AddDate(0, 0, 21)},
			"phi_days":     {Kind: KindInt, Int: 14},
		},
	})
	if err != nil {
		t.Fatalf("21-day interval must clear 14-day PHI, got %v", err)
	}
}
