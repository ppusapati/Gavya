//go:build e2e

// Authorisation, against services that are really running.
//
// libs/integrity/authz proves the model: which permission each procedure needs,
// and which roles hold it. Its middleware test proves the
// guard consults that model. Neither proves the guard is actually in front of a
// service — a platform could have a correct model, a correct middleware, and
// nothing wired between them, and every one of those tests would pass.
//
// So these call real services over the wire, holding one role at a time.
//
// The rest of this suite acts as an administrator, because it is testing what
// the procedures do rather than who may call them. This file is the other half.
package e2e

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// as builds call options for one tenant holding one role's permissions.
func as(tenant, role string) svcclient.CallOptions {
	return svcclient.CallOptions{
		TenantID: tenant, RequestID: newID("req"),
		Tenant: tenant, Actor: "US_E2E_" + strings.ToUpper(role),
		Permissions: authz.Roles()[role].Permissions.String(),
	}
}

// A collector records milk and cannot approve a payment for it.
//
// The separation the whole model exists for, through a running service rather
// than through a table lookup.
func TestACollectorRecordsMilkAndCannotApproveThePaymentForIt(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	session, err := svcclient.Call[createMilkSessionReq, milkSessionResp](ctx, p.milk(),
		milkSvc+"/CreateSession", createMilkSessionReq{
			TenantID: p.tenant, CattleID: newID("cow"), ShiftType: "morning",
			Timezone: tenantTimezone, CreatedBy: "collector",
		}, as(p.tenant, "collector"))
	if err != nil {
		t.Fatalf("a collector could not open a collection session: %v", err)
	}
	if session.Session == nil {
		t.Fatal("no session came back")
	}

	// And the same collector cannot approve a cycle.
	_, err = svcclient.Call[cycleActionReq, cycleResp](ctx, p.settlement(),
		settlementSvc+"/ApproveCycle", cycleActionReq{
			TenantID: p.tenant, CycleID: newID("cyc"), Actor: "collector",
		}, as(p.tenant, "collector"))
	if err == nil {
		t.Fatal("a collector approved a settlement cycle")
	}
	assertRefusedFor(t, err, "settlement.approve")
}

// An auditor reads a payment cycle and changes nothing about it.
func TestAnAuditorReadsAndChangesNothing(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Reading something that does not exist is a not-found, which is the point:
	// the request got past authorisation and failed on its merits.
	_, err := svcclient.Call[getCycleReq, cycleResp](ctx, p.settlement(),
		settlementSvc+"/GetCycle", getCycleReq{TenantID: p.tenant, ID: newID("cyc")},
		as(p.tenant, "auditor"))
	if err != nil && !svcclient.IsNotFound(err) {
		assertNotAPermissionRefusal(t, err, "an auditor reading a cycle")
	}

	// And every kind of change is refused.
	_, err = svcclient.Call[openCycleReq, cycleResp](ctx, p.settlement(),
		settlementSvc+"/OpenCycle", openCycleReq{
			TenantID: p.tenant, SocietyCode: newID("soc"), Name: "audit attempt",
			PeriodStart: "2026-05-01", PeriodEnd: "2026-05-15",
			Currency: "INR", AmountScale: 2,
			DeductionPolicy: "CAP_AT_EARNINGS", Actor: "auditor",
		}, as(p.tenant, "auditor"))
	if err == nil {
		t.Error("an auditor opened a payment cycle")
	}
	assertRefusedFor(t, err, "settlement.write")
}

// A caller presenting no permissions at all is refused everything.
//
// This is what an unauthenticated request reaching a service directly looks
// like, and before this work it was every request: the tenant was checked and
// nothing else was.
func TestACallerWithNoPermissionsIsRefused(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	_, err := svcclient.Call[createMilkSessionReq, milkSessionResp](ctx, p.milk(),
		milkSvc+"/CreateSession", createMilkSessionReq{
			TenantID: p.tenant, CattleID: newID("cow"), ShiftType: "morning",
			Timezone: tenantTimezone, CreatedBy: "nobody",
		}, svcclient.CallOptions{
			TenantID: p.tenant, Tenant: p.tenant,
			Actor: "US_NOBODY", RequestID: newID("req"),
			// No permissions.
		})
	if err == nil {
		t.Fatal("a caller holding no permissions opened a collection session")
	}
	assertRefusedFor(t, err, "milk.write")
}

// The refusal names the permission, over the wire, not just in the Go error.
//
// Somebody reading this in a log needs to know which role to widen. A bare
// "forbidden" turns into a support ticket; this turns into a decision.
func TestTheRefusalThatCrossesTheWireNamesThePermission(t *testing.T) {
	p := startPlatform(t)

	_, err := svcclient.Call[cycleActionReq, cycleResp](context.Background(), p.settlement(),
		settlementSvc+"/ApproveCycle", cycleActionReq{
			TenantID: p.tenant, CycleID: newID("cyc"), Actor: "collector",
		}, as(p.tenant, "collector"))
	if err == nil {
		t.Fatal("the call was allowed")
	}
	if !strings.Contains(err.Error(), "settlement.approve") {
		t.Errorf("the refusal that reached the client is %q and does not name the "+
			"permission", err.Error())
	}
	if !strings.Contains(err.Error(), "ApproveCycle") {
		t.Errorf("the refusal is %q and does not name the procedure", err.Error())
	}
}

// A service identity records and does not approve.
//
// The role a device or a recomputation acts under. It has to be able to record
// collections — that is its job — and must not be able to sign off the payments
// that follow, because an approval nobody can be asked about is not an approval.
func TestAServiceRecordsAndDoesNotApprove(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](ctx, p.milk(),
		milkSvc+"/CreateSession", createMilkSessionReq{
			TenantID: p.tenant, CattleID: newID("cow"), ShiftType: "evening",
			Timezone: tenantTimezone, CreatedBy: "device",
		}, as(p.tenant, "service")); err != nil {
		t.Fatalf("a device could not open a collection session: %v", err)
	}

	_, err := svcclient.Call[cycleActionReq, cycleResp](ctx, p.settlement(),
		settlementSvc+"/ApproveCycle", cycleActionReq{
			TenantID: p.tenant, CycleID: newID("cyc"), Actor: "device",
		}, as(p.tenant, "service"))
	if err == nil {
		t.Fatal("a service identity approved a settlement cycle")
	}
	assertRefusedFor(t, err, "settlement.approve")
}

func assertRefusedFor(t *testing.T, err error, permission string) {
	t.Helper()
	if err == nil {
		t.Fatal("the call was allowed")
	}
	var ce *connect.Error
	if errors.As(err, &ce) && ce.Code() != connect.CodePermissionDenied {
		t.Errorf("refused with %s, want permission_denied: %v", ce.Code(), err)
	}
	if !strings.Contains(err.Error(), permission) {
		t.Errorf("the refusal is %q and does not name %s", err.Error(), permission)
	}
}

func assertNotAPermissionRefusal(t *testing.T, err error, what string) {
	t.Helper()
	if strings.Contains(err.Error(), "may not call") {
		t.Errorf("%s was refused on permissions: %v", what, err)
	}
}
