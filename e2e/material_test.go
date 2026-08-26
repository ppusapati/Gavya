//go:build e2e

// Milk moving, and what the two ends of a journey came to.
//
// balance-service already reconciles a network of flows with uncertainty
// weighting. What it had no way to say was whether a node names a cooler that
// exists, a tanker with a registration, or a typo — every node was a string.
// This is the physical layer under it.
//
// The claim these tests make is the one a plant manager already cares about:
// milk left a cooler, milk arrived at a plant, and the difference is a number
// somebody can be asked about. The figures are chosen so every one of them can
// be checked by hand.
package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const materialSvc = "material.v1.MaterialService"

type quantityProto struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

type densityProto struct {
	KgPerLitre string `json:"kg_per_litre"`
	Scale      int32  `json:"scale"`
	AtCelsius  int32  `json:"at_celsius"`
	Source     string `json:"source"`
}

type registerNodeReq struct {
	TenantID string         `json:"tenant_id"`
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Capacity *quantityProto `json:"capacity,omitempty"`
	Actor    string         `json:"actor"`
}

type nodeProto struct {
	ID       string         `json:"id"`
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Capacity *quantityProto `json:"capacity,omitempty"`
	Active   bool           `json:"active"`
}

type nodeResp struct {
	Node *nodeProto `json:"node"`
}

type dispatchReq struct {
	TenantID   string         `json:"tenant_id"`
	FromNodeID string         `json:"from_node_id"`
	ToNodeID   string         `json:"to_node_id"`
	At         string         `json:"at"`
	Quantity   quantityProto  `json:"quantity"`
	Method     string         `json:"method"`
	Holdup     *quantityProto `json:"holdup,omitempty"`
	Actor      string         `json:"actor"`
}

type receiveReq struct {
	TenantID   string        `json:"tenant_id"`
	MovementID string        `json:"movement_id"`
	At         string        `json:"at"`
	Quantity   quantityProto `json:"quantity"`
	Method     string        `json:"method"`
	Density    *densityProto `json:"density,omitempty"`
	Rounding   string        `json:"rounding,omitempty"`
	Actor      string        `json:"actor"`
}

type abandonMovementReq struct {
	TenantID   string `json:"tenant_id"`
	MovementID string `json:"movement_id"`
	Reason     string `json:"reason"`
	Actor      string `json:"actor"`
}

type movementProto struct {
	ID         string `json:"id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`

	DispatchedAt   string        `json:"dispatched_at"`
	Dispatched     quantityProto `json:"dispatched"`
	DispatchMethod string        `json:"dispatch_method"`

	ReceivedAt    string         `json:"received_at,omitempty"`
	Received      *quantityProto `json:"received,omitempty"`
	ReceiptMethod string         `json:"receipt_method,omitempty"`

	Holdup  *quantityProto `json:"holdup,omitempty"`
	Density *densityProto  `json:"density,omitempty"`

	Variance                  *quantityProto `json:"variance,omitempty"`
	VarianceUnavailableReason string         `json:"variance_unavailable_reason,omitempty"`

	Status          string `json:"status"`
	AbandonedReason string `json:"abandoned_reason,omitempty"`
}

type movementResp struct {
	Movement *movementProto `json:"movement"`
}

type listMovementsReq struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id,omitempty"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
}

type listMovementsResp struct {
	Movements    []*movementProto `json:"movements"`
	Unreconciled int32            `json:"unreconciled"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func litres(v string) quantityProto    { return quantityProto{Value: v, Unit: "LITRES"} }
func kilograms(v string) quantityProto { return quantityProto{Value: v, Unit: "KILOGRAMS"} }

// lactometerDensity is 1.0300 kg per litre read at 4 degrees, which is what a
// dock hand's lactometer gives on chilled milk.
func lactometerDensity() *densityProto {
	return &densityProto{KgPerLitre: "1.0300", Scale: 4, AtCelsius: 40, Source: "LACTOMETER"}
}

func registerNode(t *testing.T, p *platform, in registerNodeReq) (*nodeProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	resp, err := svcclient.Call[registerNodeReq, nodeResp](
		context.Background(), p.material(), materialSvc+"/RegisterNode", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Node, nil
}

func mustNode(t *testing.T, p *platform, code, name, kind string) *nodeProto {
	t.Helper()
	n, err := registerNode(t, p, registerNodeReq{Code: code, Name: name, Kind: kind})
	if err != nil {
		t.Fatalf("RegisterNode %s: %v", code, err)
	}
	return n
}

func dispatch(t *testing.T, p *platform, in dispatchReq) (*movementProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e_loader"
	}
	resp, err := svcclient.Call[dispatchReq, movementResp](
		context.Background(), p.material(), materialSvc+"/Dispatch", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Movement, nil
}

func receive(t *testing.T, p *platform, in receiveReq) (*movementProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e_dock"
	}
	resp, err := svcclient.Call[receiveReq, movementResp](
		context.Background(), p.material(), materialSvc+"/Receive", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Movement, nil
}

// ---------------------------------------------------------------------------
// The journey
// ---------------------------------------------------------------------------

// A morning's milk, cooler to tanker to plant, with the loss visible at each hop.
//
// The tanker is a node rather than a carrier attached to one long movement, and
// this test is why. Modelled as one hop, cooler to plant, a hundred missing
// litres could have gone at either end and the platform could not say which.
// Two hops put the tanker's own holdup where somebody can see it.
func TestAMorningsMilkTravelsFromCoolerToPlantAndTheLossIsVisible(t *testing.T) {
	p := startPlatform(t)

	cooler := mustNode(t, p, "BMC-04", "Kothapalli cooler", "BULK_COOLER")
	tanker := mustNode(t, p, "TS09-UA-4471", "Tanker 1", "TANKER")
	plant := mustNode(t, p, "PLANT-1", "Sangam plant", "PLANT")

	// The cooler dips 5000 and the tanker's flowmeter reads 4990: ten litres
	// left in the cooler's own pipework.
	load, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: tanker.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if load.Status != "IN_TRANSIT" {
		t.Errorf("a fresh dispatch is %s, want IN_TRANSIT", load.Status)
	}
	if load.Variance != nil {
		t.Errorf("a movement that has not arrived has a variance of %v", load.Variance)
	}

	loaded, err := receive(t, p, receiveReq{
		MovementID: load.ID, At: "2026-03-01T05:40:00Z",
		Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if loaded.Variance == nil || loaded.Variance.Value != "10.000" {
		t.Errorf("the cooler-to-tanker variance is %v, want 10.000 litres", loaded.Variance)
	}

	// The tanker leaves with 4990 and the plant's weighbridge is not in use
	// today, so the dock dips 4900. Ninety litres between the cooler yard and
	// the plant gate.
	haul, err := dispatch(t, p, dispatchReq{
		FromNodeID: tanker.ID, ToNodeID: plant.ID,
		At: "2026-03-01T06:00:00Z", Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Dispatch haul: %v", err)
	}
	delivered, err := receive(t, p, receiveReq{
		MovementID: haul.ID, At: "2026-03-01T08:30:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Receive haul: %v", err)
	}
	if delivered.Variance == nil || delivered.Variance.Value != "90.000" {
		t.Errorf("the tanker-to-plant variance is %v, want 90.000 litres", delivered.Variance)
	}
	if delivered.Status != "RECEIVED" {
		t.Errorf("a delivered movement is %s", delivered.Status)
	}

	// The whole morning, from a query about the tanker: what it loaded and what
	// it delivered, because a query returning only one half shows a vessel that
	// fills and never empties.
	list, err := svcclient.Call[listMovementsReq, listMovementsResp](
		context.Background(), p.material(), materialSvc+"/ListMovements",
		listMovementsReq{TenantID: p.tenant, NodeID: tanker.ID,
			From: "2026-03-01", To: "2026-03-02"}, p.opts())
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	if len(list.Movements) != 2 {
		t.Fatalf("the tanker's morning shows %d movements, want the load and the delivery",
			len(list.Movements))
	}
	if list.Unreconciled != 0 {
		t.Errorf("%d movements went unreconciled in a morning measured in litres throughout",
			list.Unreconciled)
	}
}

// The failure the whole design is arranged around.
//
// A cooler dips litres. A plant weighs kilograms. Subtracting one from the
// other reports about three per cent of every consignment as missing, which is
// the density of milk and looks exactly like theft.
func TestALitreDispatchAgainstAKilogramReceiptIsNotReportedAsALoss(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-07", "Chinnapuram cooler", "BULK_COOLER")
	plant := mustNode(t, p, "PLANT-2", "Sangam plant", "PLANT")

	m, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Received on a weighbridge, with nobody supplying a density.
	arrived, err := receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T08:00:00Z",
		Quantity: kilograms("5090.000"), Method: "WEIGHBRIDGE",
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if arrived.Variance != nil {
		t.Errorf("a variance of %s was reported across units; 5000 litres against 5090 "+
			"kilograms differs by the density of milk, not by a loss", arrived.Variance.Value)
	}
	if arrived.Status != "RECEIVED" {
		t.Errorf("the milk arrived and the movement is %s", arrived.Status)
	}
	// The reason is the point. An empty variance and a variance that could not
	// be computed look identical in a report, and only one is a thing somebody
	// can go and fix.
	if !strings.Contains(arrived.VarianceUnavailableReason, "density") {
		t.Errorf("the movement does not say why it could not be reconciled: %q",
			arrived.VarianceUnavailableReason)
	}
	if !strings.Contains(arrived.VarianceUnavailableReason, "5090.000") {
		t.Errorf("the reason does not name the receipt that could not be compared: %q",
			arrived.VarianceUnavailableReason)
	}

	// And a period's report counts it, so a total of transit loss says how much
	// of the period is missing from that total.
	list, err := svcclient.Call[listMovementsReq, listMovementsResp](
		context.Background(), p.material(), materialSvc+"/ListMovements",
		listMovementsReq{TenantID: p.tenant, From: "2026-03-01", To: "2026-03-02"}, p.opts())
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	if list.Unreconciled != 1 {
		t.Errorf("the period reports %d unreconciled movements, want 1", list.Unreconciled)
	}
}

// The same consignment with a density reconciles, and to a figure a hundred
// times smaller than the raw subtraction would have given.
//
//	5090 kg at 1.0300 kg/l is 4941.748 litres
//	5000 dispatched less that is 58.252 litres
//
// The raw subtraction would have said 90 of something.
func TestWithADensityTheWeighbridgeConsignmentReconciles(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-08", "Chinnapuram cooler", "BULK_COOLER")
	plant := mustNode(t, p, "PLANT-3", "Sangam plant", "PLANT")

	m, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	arrived, err := receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T08:00:00Z",
		Quantity: kilograms("5090.000"), Method: "WEIGHBRIDGE",
		Density: lactometerDensity(), Rounding: "HALF_UP",
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if arrived.Variance == nil {
		t.Fatalf("no variance: %s", arrived.VarianceUnavailableReason)
	}
	if arrived.Variance.Value != "58.252" || arrived.Variance.Unit != "LITRES" {
		t.Errorf("variance %s %s, want 58.252 LITRES — the unit the movement started in",
			arrived.Variance.Value, arrived.Variance.Unit)
	}
	// The density used is kept on the movement, because a transit-loss dispute
	// turns on which figure was applied and where it came from.
	if arrived.Density == nil {
		t.Fatal("the movement does not record the density it was reconciled with")
	}
	if arrived.Density.Source != "LACTOMETER" || arrived.Density.AtCelsius != 40 {
		t.Errorf("the density reads %s at %d tenths of a degree",
			arrived.Density.Source, arrived.Density.AtCelsius)
	}
}

// The holdup and the variance are separate figures, and neither is netted
// against the other.
//
// A tanker that left with 5000, held 40 back on its walls and delivered 4900
// has a variance of 100 and a holdup of 40. Netting them would report 60 and
// lose the fact that 40 is in a vessel somebody can look inside.
func TestTheHoldupAndTheVarianceAreSeparateFigures(t *testing.T) {
	p := startPlatform(t)
	tanker := mustNode(t, p, "TS09-UA-5512", "Tanker 2", "TANKER")
	plant := mustNode(t, p, "PLANT-4", "Sangam plant", "PLANT")

	m, err := dispatch(t, p, dispatchReq{
		FromNodeID: tanker.ID, ToNodeID: plant.ID,
		At: "2026-03-01T06:00:00Z", Quantity: litres("5000.000"), Method: "FLOWMETER",
		Holdup: &quantityProto{Value: "40.000", Unit: "LITRES"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if m.Holdup == nil || m.Holdup.Value != "40.000" {
		t.Fatalf("the holdup reads %v, want 40.000 litres", m.Holdup)
	}

	arrived, err := receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T08:00:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if arrived.Variance == nil {
		t.Fatalf("no variance: %s", arrived.VarianceUnavailableReason)
	}
	if arrived.Variance.Value != "100.000" {
		t.Errorf("variance %s, want 100.000 — a variance of 60.000 would mean the 40 litres "+
			"of holdup had been netted away and nobody would know it was accounted for",
			arrived.Variance.Value)
	}
	if arrived.Holdup == nil || arrived.Holdup.Value != "40.000" {
		t.Errorf("the holdup reads %v after the receipt, want 40.000 litres", arrived.Holdup)
	}
}

// A tanker is in one place at a time.
//
// The commonest data-entry error on this table: a load recorded against a
// tanker already out on a route, because the clerk picked the wrong
// registration from a list. Without the check the milk balances against a
// vessel that was fifty kilometres away, and the route it actually came from is
// short.
func TestATankerCannotBeLoadedWhileItIsAlreadyOut(t *testing.T) {
	p := startPlatform(t)
	first := mustNode(t, p, "BMC-11", "First cooler", "BULK_COOLER")
	second := mustNode(t, p, "BMC-12", "Second cooler", "BULK_COOLER")
	tanker := mustNode(t, p, "TS09-UA-7788", "Tanker 3", "TANKER")
	plant := mustNode(t, p, "PLANT-5", "Sangam plant", "PLANT")

	out, err := dispatch(t, p, dispatchReq{
		FromNodeID: first.ID, ToNodeID: tanker.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	_, err = dispatch(t, p, dispatchReq{
		FromNodeID: second.ID, ToNodeID: tanker.ID,
		At: "2026-03-01T05:30:00Z", Quantity: litres("3000.000"), Method: "DIP",
	})
	if err == nil {
		t.Fatal("a second load was recorded against a tanker that was already carrying one")
	}
	if !strings.Contains(err.Error(), "already carrying") {
		t.Errorf("the refusal does not say the tanker is busy: %v", err)
	}

	// A plant is not a vessel: it receives from several coolers at once, and
	// refusing that would make the platform unusable at every plant.
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: first.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:15:00Z", Quantity: litres("1000.000"), Method: "DIP",
	}); err != nil {
		t.Errorf("a plant was refused a second simultaneous delivery: %v", err)
	}
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: second.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:20:00Z", Quantity: litres("1000.000"), Method: "DIP",
	}); err != nil {
		t.Errorf("a plant was refused a third simultaneous delivery: %v", err)
	}

	// Once the tanker has arrived it is free again.
	if _, err := receive(t, p, receiveReq{
		MovementID: out.ID, At: "2026-03-01T05:45:00Z",
		Quantity: litres("5000.000"), Method: "FLOWMETER",
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: second.ID, ToNodeID: tanker.ID,
		At: "2026-03-01T06:00:00Z", Quantity: litres("3000.000"), Method: "DIP",
	}); err != nil {
		t.Errorf("the tanker was still refused after arriving: %v", err)
	}
}

// A receipt happens once, and what it recorded does not change afterwards.
//
// The variance on a received movement is a figure somebody has been shown,
// often a transporter being asked about ninety litres. A quantity edited
// afterwards is an argument nobody can reconstruct.
func TestAMovementIsReceivedOnceAndDoesNotChangeAfterwards(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-21", "A cooler", "BULK_COOLER")
	plant := mustNode(t, p, "PLANT-6", "Sangam plant", "PLANT")

	m, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T08:00:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	// A second receipt is refused.
	_, err = receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T09:00:00Z",
		Quantity: litres("4950.000"), Method: "DIP",
	})
	if err == nil {
		t.Fatal("the same movement was received twice")
	}
	if !strings.Contains(err.Error(), "RECEIVED") {
		t.Errorf("the refusal does not say the movement has already arrived: %v", err)
	}

	// And so is abandoning it after the fact.
	if _, err := svcclient.Call[abandonMovementReq, movementResp](
		context.Background(), p.material(), materialSvc+"/AbandonMovement",
		abandonMovementReq{TenantID: p.tenant, MovementID: m.ID,
			Reason: "changed my mind", Actor: "e2e"}, p.opts()); err == nil {
		t.Error("a movement that had already arrived was abandoned")
	}

	// The figure that was recorded is the figure that stands.
	after, err := svcclient.Call[getMovementReq, movementResp](
		context.Background(), p.material(), materialSvc+"/GetMovement",
		getMovementReq{TenantID: p.tenant, ID: m.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetMovement: %v", err)
	}
	if after.Movement.Received == nil || after.Movement.Received.Value != "4900.000" {
		t.Errorf("the receipt reads %v after a second attempt, want the original 4900.000",
			after.Movement.Received)
	}
}

type getMovementReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

// Milk does not arrive before it leaves, and a movement that never arrived says
// why rather than sitting in transit forever.
func TestATankerThatNeverArrivedIsAbandonedWithAReason(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-31", "A cooler", "BULK_COOLER")
	plant := mustNode(t, p, "PLANT-7", "Sangam plant", "PLANT")

	m, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// A receipt timed before the dispatch is refused.
	if _, err := receive(t, p, receiveReq{
		MovementID: m.ID, At: "2026-03-01T04:00:00Z",
		Quantity: litres("5000.000"), Method: "DIP",
	}); err == nil {
		t.Error("milk arrived an hour before it left")
	}

	// Abandoning with no reason is refused: a tanker that never arrived is
	// worth a sentence, and a status alone does not carry one.
	if _, err := svcclient.Call[abandonMovementReq, movementResp](
		context.Background(), p.material(), materialSvc+"/AbandonMovement",
		abandonMovementReq{TenantID: p.tenant, MovementID: m.ID, Actor: "e2e"}, p.opts()); err == nil {
		t.Error("a movement was abandoned with no reason recorded")
	}

	done, err := svcclient.Call[abandonMovementReq, movementResp](
		context.Background(), p.material(), materialSvc+"/AbandonMovement",
		abandonMovementReq{TenantID: p.tenant, MovementID: m.ID,
			Reason: "axle broke at Jangaon; load transferred to TS09-UA-9982", Actor: "e2e"},
		p.opts())
	if err != nil {
		t.Fatalf("AbandonMovement: %v", err)
	}
	if done.Movement.Status != "ABANDONED" {
		t.Errorf("the movement is %s", done.Movement.Status)
	}
	if !strings.Contains(done.Movement.AbandonedReason, "axle broke") {
		t.Errorf("the movement does not carry why it was abandoned: %q",
			done.Movement.AbandonedReason)
	}
}

// A movement into a vessel that cannot hold it is a mis-keyed figure roughly
// every time, and accepting it puts phantom milk into the balance.
func TestALoadLargerThanTheVesselIsRefused(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-41", "A cooler", "BULK_COOLER")
	small, err := registerNode(t, p, registerNodeReq{
		Code: "TS09-UA-1111", Name: "Small tanker", Kind: "TANKER",
		Capacity: &quantityProto{Value: "10000.000", Unit: "LITRES"},
	})
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if small.Capacity == nil || small.Capacity.Value != "10000.000" {
		t.Fatalf("the tanker's capacity reads %v", small.Capacity)
	}

	_, err = dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: small.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("12000.000"), Method: "DIP",
	})
	if err == nil {
		t.Fatal("12000 litres were loaded into a 10000 litre tanker")
	}
	if !strings.Contains(err.Error(), "10000.000") || !strings.Contains(err.Error(), "12000.000") {
		t.Errorf("the refusal does not name the capacity and the load: %v", err)
	}

	// A load it can hold goes through.
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: small.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("9000.000"), Method: "DIP",
	}); err != nil {
		t.Errorf("a load within capacity was refused: %v", err)
	}
}

// A node carries the code its society calls it by, and two nodes cannot share
// one. Two coolers both called BMC-04 is a reconciliation that balances against
// the wrong one, and the wrong one is on somebody's route.
func TestANodeCodeIdentifiesOnePlace(t *testing.T) {
	p := startPlatform(t)
	first := mustNode(t, p, "BMC-51", "Kothapalli cooler", "BULK_COOLER")
	if first.Code != "BMC-51" {
		t.Errorf("the node reads back as %q", first.Code)
	}

	if _, err := registerNode(t, p, registerNodeReq{
		Code: "BMC-51", Name: "A different cooler", Kind: "BULK_COOLER",
	}); err == nil {
		t.Error("two nodes were registered with the same code")
	}

	// A node with no code is refused: a member of staff asked about BMC-51
	// cannot look up an identifier they have never seen.
	if _, err := registerNode(t, p, registerNodeReq{
		Name: "Nameless", Kind: "BULK_COOLER",
	}); err == nil {
		t.Error("a node was registered with no code")
	}

	// And a movement naming a node that does not exist says so.
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: first.ID, ToNodeID: "NOSUCHNODE0000000000000000",
		At: "2026-03-01T05:00:00Z", Quantity: litres("100.000"), Method: "DIP",
	}); err == nil {
		t.Error("milk was dispatched to a node that does not exist")
	}
}

// A measurement says how it was taken, because balance-service weights a
// reconciliation by measurement uncertainty and a dipstick is not a weighbridge.
func TestAMeasurementWithNoMethodIsRefused(t *testing.T) {
	p := startPlatform(t)
	cooler := mustNode(t, p, "BMC-61", "A cooler", "BULK_COOLER")
	plant := mustNode(t, p, "PLANT-8", "Sangam plant", "PLANT")

	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Quantity: litres("5000.000"),
	}); err == nil {
		t.Error("a dispatch was recorded without saying how it was measured")
	}

	// And a quantity with no unit is refused rather than assumed to be litres.
	if _, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: plant.ID,
		At: "2026-03-01T05:00:00Z", Method: "DIP",
		Quantity: quantityProto{Value: "5000.000"},
	}); err == nil {
		t.Error("a quantity with no unit was accepted and would have been taken as litres")
	}
}

// ---------------------------------------------------------------------------
// The join to the reconciler
// ---------------------------------------------------------------------------

const balanceSvc = "balance.v1.BalanceService"

type registerInstrumentReq struct {
	TenantID       string         `json:"tenant_id"`
	NodeID         string         `json:"node_id"`
	Method         string         `json:"method"`
	Label          string         `json:"label"`
	RelativePPM    int64          `json:"relative_ppm,omitempty"`
	Absolute       *quantityProto `json:"absolute,omitempty"`
	CertificateRef string         `json:"certificate_ref"`
	CalibratedOn   string         `json:"calibrated_on"`
	ValidUntil     string         `json:"valid_until"`
	Actor          string         `json:"actor"`
}

type instrumentProto struct {
	ID             string `json:"id"`
	NodeID         string `json:"node_id"`
	Method         string `json:"method"`
	Label          string `json:"label"`
	RelativePPM    int64  `json:"relative_ppm,omitempty"`
	CertificateRef string `json:"certificate_ref"`
	ValidUntil     string `json:"valid_until"`
}

type instrumentResp struct {
	Instrument *instrumentProto `json:"instrument"`
}

type flowProto struct {
	FlowID           string         `json:"flow_id"`
	FromNodeID       string         `json:"from_node_id"`
	ToNodeID         string         `json:"to_node_id"`
	Measured         quantityProto  `json:"measured"`
	Uncertainty      *quantityProto `json:"standard_uncertainty,omitempty"`
	Unmeasured       bool           `json:"unmeasured"`
	UnmeasuredReason string         `json:"unmeasured_reason,omitempty"`
	MovementID       string         `json:"movement_id"`
}

type proposeFlowsReq struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Rounding string `json:"rounding"`
}

type proposeFlowsResp struct {
	Flows      []*flowProto `json:"flows"`
	Unmeasured int32        `json:"unmeasured"`
}

type createWindowReq struct {
	TenantID    string `json:"tenant_id"`
	RouteRef    string `json:"route_ref"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit"`
	Actor       string `json:"actor"`
}

type windowProto struct {
	ID     string `json:"id"`
	Unit   string `json:"unit"`
	Status string `json:"status"`
}

type createWindowResp struct {
	Window *windowProto `json:"window"`
}

type addFlowReq struct {
	TenantID            string `json:"tenant_id"`
	WindowID            string `json:"window_id"`
	FlowID              string `json:"flow_id"`
	FromNode            string `json:"from_node"`
	FromNodeKind        string `json:"from_node_kind,omitempty"`
	ToNode              string `json:"to_node"`
	ToNodeKind          string `json:"to_node_kind,omitempty"`
	Measured            string `json:"measured"`
	StandardUncertainty string `json:"standard_uncertainty,omitempty"`
	Unmeasured          bool   `json:"unmeasured,omitempty"`
	ObservationRef      string `json:"observation_ref,omitempty"`
	Actor               string `json:"actor"`
}

type addFlowResp struct {
	Flow map[string]any `json:"flow"`
}

type reconcileReq struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
	Actor    string `json:"actor"`
}

type runProto struct {
	ID             string `json:"id"`
	Converged      bool   `json:"converged"`
	ResidualBefore string `json:"residual_before"`
	Reason         string `json:"reason,omitempty"`
}

type reconcileResp struct {
	Run *runProto `json:"run"`
}

func registerInstrument(t *testing.T, p *platform, in registerInstrumentReq) (*instrumentProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e_metrology"
	}
	resp, err := svcclient.Call[registerInstrumentReq, instrumentResp](
		context.Background(), p.material(), materialSvc+"/RegisterInstrument", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Instrument, nil
}

// The join: material proposes the network, balance reconciles it, and the two
// agree about the milk.
//
// This is the only place either service means anything. balance-service had the
// mathematics and no way to know whether a node was a real cooler;
// material-service knows the coolers and does not reconcile. Until this ran,
// nothing had ever carried a reading from one to the other.
//
// The morning: a cooler loads 5000 litres into a tanker, the tanker's flowmeter
// reads 4990 on arrival, and the tanker then delivers that 4990 to the plant,
// where the dock dips 4900.
//
// Only the tanker both receives and sends, so it is the one node this window can
// test. Its balance is 5000 in less 4990 out, which is the ten litres the
// loading variance already reported — the same subtraction reached twice, by two
// services, through different code.
func TestTheNetworkMaterialProposesIsTheNetworkBalanceReconciles(t *testing.T) {
	p := startPlatform(t)

	cooler := mustNode(t, p, "BMC-71", "Kothapalli cooler", "BULK_COOLER")
	tanker := mustNode(t, p, "TS09-UA-3300", "Tanker 7", "TANKER")
	plant := mustNode(t, p, "PLANT-9", "Sangam plant", "PLANT")

	// Both sending instruments certified and current.
	for _, in := range []registerInstrumentReq{
		{NodeID: cooler.ID, Method: "DIP", Label: "Dipstick BMC-71",
			RelativePPM: 5000, CertificateRef: "CERT-DIP-71",
			CalibratedOn: "2026-01-01", ValidUntil: "2027-01-01"},
		{NodeID: tanker.ID, Method: "FLOWMETER", Label: "Flowmeter TS09-UA-3300",
			RelativePPM: 2000, CertificateRef: "CERT-FM-3300",
			CalibratedOn: "2026-01-01", ValidUntil: "2027-01-01"},
	} {
		if _, err := registerInstrument(t, p, in); err != nil {
			t.Fatalf("RegisterInstrument %s: %v", in.Label, err)
		}
	}

	load, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: tanker.ID,
		At: "2026-04-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	loaded, err := receive(t, p, receiveReq{
		MovementID: load.ID, At: "2026-04-01T05:40:00Z",
		Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if loaded.Variance == nil || loaded.Variance.Value != "10.000" {
		t.Fatalf("the loading variance is %v, want 10.000 litres", loaded.Variance)
	}

	haul, err := dispatch(t, p, dispatchReq{
		FromNodeID: tanker.ID, ToNodeID: plant.ID,
		At: "2026-04-01T06:00:00Z", Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Dispatch haul: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: haul.ID, At: "2026-04-01T08:30:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	}); err != nil {
		t.Fatalf("Receive haul: %v", err)
	}

	proposed, err := svcclient.Call[proposeFlowsReq, proposeFlowsResp](
		context.Background(), p.material(), materialSvc+"/ProposeFlows",
		proposeFlowsReq{TenantID: p.tenant, From: "2026-04-01", To: "2026-04-02",
			Rounding: "HALF_UP"}, p.opts())
	if err != nil {
		t.Fatalf("ProposeFlows: %v", err)
	}
	if len(proposed.Flows) != 2 {
		t.Fatalf("%d flows proposed, want the two legs", len(proposed.Flows))
	}
	if proposed.Unmeasured != 0 {
		t.Errorf("%d legs came back unmeasured with both instruments in calibration",
			proposed.Unmeasured)
	}

	// The cooler only sends and the plant only receives, so both are outside
	// what this window can test.
	byFlow := map[string]*flowProto{}
	for _, f := range proposed.Flows {
		byFlow[f.MovementID] = f
	}
	if got := byFlow[load.ID].FromNodeID; got != "" {
		t.Errorf("the cooler is %q; it only sends in this window", got)
	}
	if got := byFlow[load.ID].ToNodeID; got != tanker.ID {
		t.Errorf("the loading flow arrives at %q, want the tanker", got)
	}
	if got := byFlow[haul.ID].ToNodeID; got != "" {
		t.Errorf("the plant is %q; it only receives in this window", got)
	}
	// The uncertainties came off the certificates: 5000 ppm of 5000 is 25, and
	// 2000 ppm of 4990 is 9.98.
	if got := byFlow[load.ID].Uncertainty; got == nil || got.Value != "25.000" {
		t.Errorf("the loading uncertainty is %v, want 25.000 litres from the dipstick certificate", got)
	}
	if got := byFlow[haul.ID].Uncertainty; got == nil || got.Value != "9.980" {
		t.Errorf("the haul uncertainty is %v, want 9.980 litres from the flowmeter certificate", got)
	}

	// Hand the proposal to the reconciler, unchanged.
	window, err := svcclient.Call[createWindowReq, createWindowResp](
		context.Background(), p.balance(), balanceSvc+"/CreateWindow",
		createWindowReq{TenantID: p.tenant, RouteRef: "ROUTE-7",
			PeriodStart: "2026-04-01T00:00:00Z", PeriodEnd: "2026-04-02T00:00:00Z",
			Unit: "LITRES", Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("CreateWindow: %v", err)
	}
	for _, f := range proposed.Flows {
		in := addFlowReq{
			TenantID: p.tenant, WindowID: window.Window.ID, FlowID: f.FlowID,
			FromNode: f.FromNodeID, ToNode: f.ToNodeID,
			Measured: f.Measured.Value, Unmeasured: f.Unmeasured,
			ObservationRef: f.MovementID, Actor: "e2e",
		}
		if f.Uncertainty != nil {
			in.StandardUncertainty = f.Uncertainty.Value
		}
		// The node kinds balance-service wants: it refuses a non-boundary node
		// with no kind, and the boundary must carry none.
		if f.FromNodeID != "" {
			in.FromNodeKind = "TANKER"
		}
		if f.ToNodeID != "" {
			in.ToNodeKind = "TANKER"
		}
		if _, err := svcclient.Call[addFlowReq, addFlowResp](
			context.Background(), p.balance(), balanceSvc+"/AddFlow", in, p.opts()); err != nil {
			t.Fatalf("AddFlow %s: %v", f.FlowID, err)
		}
	}

	run, err := svcclient.Call[reconcileReq, reconcileResp](
		context.Background(), p.balance(), balanceSvc+"/Reconcile",
		reconcileReq{TenantID: p.tenant, WindowID: window.Window.ID, Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// The claim. The tanker's balance is 5000 in less 4990 out, and the loading
	// variance material computed movement-by-movement was 10.000. The same
	// subtraction, reached twice, by two services, through different code.
	if run.Run.ResidualBefore != "10.000" {
		t.Errorf("the reconciler puts the tanker's imbalance at %s and material put the "+
			"loading variance at %s; two services disagree about the same milk",
			run.Run.ResidualBefore, loaded.Variance.Value)
	}
	// The ML tier is not in this harness, and that is a supported deployment:
	// the imbalance is computed locally and an investigation is never blocked by
	// a model being unreachable.
	if run.Run.Converged {
		t.Error("the run reports convergence with no reconciler configured")
	}
	if !strings.Contains(run.Run.Reason, "locally") {
		t.Errorf("the run does not say the imbalance was computed without a model: %q",
			run.Run.Reason)
	}
}

// An expired calibration reaches the reconciler as a leg to solve for, not as a
// weighted reading.
//
// This is the whole point of registering instruments. Weighting a settlement by
// an instrument nobody has checked in three years is worse than solving for the
// leg: the reconciler pulls the entire solution towards a number no one can
// defend, and every other reading absorbs the difference.
func TestAnExpiredCertificateReachesTheReconcilerAsAnUnmeasuredLeg(t *testing.T) {
	p := startPlatform(t)

	cooler := mustNode(t, p, "BMC-81", "A cooler", "BULK_COOLER")
	tanker := mustNode(t, p, "TS09-UA-4400", "Tanker 8", "TANKER")
	plant := mustNode(t, p, "PLANT-10", "Sangam plant", "PLANT")

	// The cooler's dipstick certificate ran out in February; the milk moved in
	// April.
	if _, err := registerInstrument(t, p, registerInstrumentReq{
		NodeID: cooler.ID, Method: "DIP", Label: "Dipstick BMC-81",
		RelativePPM: 5000, CertificateRef: "CERT-DIP-81",
		CalibratedOn: "2025-02-01", ValidUntil: "2026-02-01",
	}); err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}
	if _, err := registerInstrument(t, p, registerInstrumentReq{
		NodeID: tanker.ID, Method: "FLOWMETER", Label: "Flowmeter TS09-UA-4400",
		RelativePPM: 2000, CertificateRef: "CERT-FM-4400",
		CalibratedOn: "2026-01-01", ValidUntil: "2027-01-01",
	}); err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}

	load, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: tanker.ID,
		At: "2026-04-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: load.ID, At: "2026-04-01T05:40:00Z",
		Quantity: litres("4990.000"), Method: "FLOWMETER",
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	haul, err := dispatch(t, p, dispatchReq{
		FromNodeID: tanker.ID, ToNodeID: plant.ID,
		At: "2026-04-01T06:00:00Z", Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Dispatch haul: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: haul.ID, At: "2026-04-01T08:30:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	}); err != nil {
		t.Fatalf("Receive haul: %v", err)
	}

	proposed, err := svcclient.Call[proposeFlowsReq, proposeFlowsResp](
		context.Background(), p.material(), materialSvc+"/ProposeFlows",
		proposeFlowsReq{TenantID: p.tenant, From: "2026-04-01", To: "2026-04-02",
			Rounding: "HALF_UP"}, p.opts())
	if err != nil {
		t.Fatalf("ProposeFlows: %v", err)
	}
	if proposed.Unmeasured != 1 {
		t.Fatalf("%d unmeasured legs, want the one whose certificate had expired",
			proposed.Unmeasured)
	}
	for _, f := range proposed.Flows {
		if f.MovementID != load.ID {
			continue
		}
		if !f.Unmeasured {
			t.Fatalf("a reading taken two months after the certificate expired was weighted at %v",
				f.Uncertainty)
		}
		// The reason has to name the instrument, the certificate and both dates,
		// because the person reading it has to go and get the thing recalibrated.
		for _, want := range []string{"Dipstick BMC-81", "CERT-DIP-81", "2026-02-01", "2026-04-01"} {
			if !strings.Contains(f.UnmeasuredReason, want) {
				t.Errorf("the reason does not carry %q: %q", want, f.UnmeasuredReason)
			}
		}
	}

	// And a listing of the society's instruments says how many are out of date,
	// so somebody can find out before a settlement rather than after.
	list, err := svcclient.Call[struct {
		TenantID string `json:"tenant_id"`
	}, struct {
		Instruments []*instrumentProto `json:"instruments"`
		Expired     int32              `json:"expired"`
		AsOf        string             `json:"as_of"`
	}](context.Background(), p.material(), materialSvc+"/ListInstruments",
		struct {
			TenantID string `json:"tenant_id"`
		}{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("ListInstruments: %v", err)
	}
	if len(list.Instruments) != 2 {
		t.Fatalf("%d instruments registered, want 2", len(list.Instruments))
	}
	if list.Expired != 1 {
		t.Errorf("%d instruments report as expired, want the dipstick", list.Expired)
	}
}

// An uncertainty with no certificate behind it is refused, and so is a
// calibration that never expires.
//
// There is no table of typical uncertainties by method in this platform and
// there will not be one. A weighbridge is around a tenth of a per cent and a
// society's weighbridge is whatever its certificate says; inventing the first
// would put a number nobody measured into the weighting of a settlement.
func TestAnInstrumentMustCarryItsCertificateAndAnExpiry(t *testing.T) {
	p := startPlatform(t)
	node := mustNode(t, p, "BMC-91", "A cooler", "BULK_COOLER")

	base := registerInstrumentReq{
		NodeID: node.ID, Method: "DIP", Label: "Dipstick",
		RelativePPM: 5000, CertificateRef: "CERT-1",
		CalibratedOn: "2026-01-01", ValidUntil: "2027-01-01",
	}

	noCert := base
	noCert.CertificateRef = ""
	if _, err := registerInstrument(t, p, noCert); err == nil {
		t.Error("an uncertainty was registered with no certificate behind it")
	}

	noExpiry := base
	noExpiry.ValidUntil = ""
	if _, err := registerInstrument(t, p, noExpiry); err == nil {
		t.Error("a calibration was registered that never expires")
	}

	noUncertainty := base
	noUncertainty.RelativePPM = 0
	if _, err := registerInstrument(t, p, noUncertainty); err == nil {
		t.Error("an instrument was registered with no uncertainty at all")
	}

	both := base
	both.Absolute = &quantityProto{Value: "5.000", Unit: "LITRES"}
	if _, err := registerInstrument(t, p, both); err == nil {
		t.Error("an instrument was registered with both a relative and an absolute uncertainty")
	}

	if _, err := registerInstrument(t, p, base); err != nil {
		t.Fatalf("a complete instrument was refused: %v", err)
	}
	// One instrument per node per method: two would mean the platform holds two
	// uncertainties for one measurement and picks one invisibly.
	if _, err := registerInstrument(t, p, base); err == nil {
		t.Error("a second dipstick was registered at the same node")
	}
}

// Proposing flows without saying how a relative uncertainty rounds is refused.
//
// It is a small effect on one flow — a millilitre either way — and it decides
// which of two nearly-equal legs the reconciler blames when a window does not
// close. A default here would make that decision invisibly, and the society
// would never know a choice had been made.
func TestProposingFlowsWithoutARoundingModeIsRefused(t *testing.T) {
	p := startPlatform(t)

	cooler := mustNode(t, p, "BMC-95", "A cooler", "BULK_COOLER")
	tanker := mustNode(t, p, "TS09-UA-5500", "Tanker 9", "TANKER")
	plant := mustNode(t, p, "PLANT-11", "Sangam plant", "PLANT")
	if _, err := registerInstrument(t, p, registerInstrumentReq{
		NodeID: cooler.ID, Method: "DIP", Label: "Dipstick BMC-95",
		RelativePPM: 5000, CertificateRef: "CERT-DIP-95",
		CalibratedOn: "2026-01-01", ValidUntil: "2027-01-01",
	}); err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}

	load, err := dispatch(t, p, dispatchReq{
		FromNodeID: cooler.ID, ToNodeID: tanker.ID,
		At: "2026-05-01T05:00:00Z", Quantity: litres("5000.000"), Method: "DIP",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: load.ID, At: "2026-05-01T05:40:00Z",
		Quantity: litres("4990.000"), Method: "FLOWMETER",
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	haul, err := dispatch(t, p, dispatchReq{
		FromNodeID: tanker.ID, ToNodeID: plant.ID,
		At: "2026-05-01T06:00:00Z", Quantity: litres("4990.000"), Method: "FLOWMETER",
	})
	if err != nil {
		t.Fatalf("Dispatch haul: %v", err)
	}
	if _, err := receive(t, p, receiveReq{
		MovementID: haul.ID, At: "2026-05-01T08:30:00Z",
		Quantity: litres("4900.000"), Method: "DIP",
	}); err != nil {
		t.Fatalf("Receive haul: %v", err)
	}

	_, err = svcclient.Call[proposeFlowsReq, proposeFlowsResp](
		context.Background(), p.material(), materialSvc+"/ProposeFlows",
		proposeFlowsReq{TenantID: p.tenant, From: "2026-05-01", To: "2026-05-02"}, p.opts())
	if err == nil {
		t.Fatal("a network was proposed without anybody saying how its uncertainties round")
	}
	if !strings.Contains(err.Error(), "rounding") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}

	// With a mode it goes through, so the test is about the mode and not about
	// the movements.
	if _, err := svcclient.Call[proposeFlowsReq, proposeFlowsResp](
		context.Background(), p.material(), materialSvc+"/ProposeFlows",
		proposeFlowsReq{TenantID: p.tenant, From: "2026-05-01", To: "2026-05-02",
			Rounding: "HALF_UP"}, p.opts()); err != nil {
		t.Errorf("a proposal with a rounding mode was refused: %v", err)
	}
}
