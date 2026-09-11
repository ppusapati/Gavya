//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// Wire types, written against the services' JSON contracts rather than imported
// from them. A test that imports the server's own structs cannot catch a field
// the server renamed — it would rename on both sides at once.

type declarePolicyReq struct {
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Dimensions    []string `json:"dimensions"`
	Resolution    string   `json:"resolution"`
	Version       int32    `json:"version"`
	EffectiveFrom string   `json:"effective_from"`
	EffectiveTo   string   `json:"effective_to,omitempty"`
	Actor         string   `json:"actor"`
}
type policyProto struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Dimensions []string `json:"dimensions"`
	Resolution string   `json:"resolution"`
	Version    int32    `json:"version"`
}
type declarePolicyResp struct {
	Policy *policyProto `json:"policy"`
}

type mapIdentityReq struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	EntityKind     string `json:"entity_kind"`
	ExternalID     string `json:"external_id"`
	EntityID       string `json:"entity_id"`
	Method         string `json:"method"`
	ValidFrom      string `json:"valid_from"`
	ValidTo        string `json:"valid_to,omitempty"`
	Actor          string `json:"actor"`
}
type identityProto struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	EntityID   string `json:"entity_id"`
	ValidFrom  string `json:"valid_from"`
	ValidTo    string `json:"valid_to"`
}
type mapIdentityResp struct {
	Identity *identityProto `json:"identity"`
}

type resolveIdentityReq struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	EntityKind     string `json:"entity_kind"`
	ExternalID     string `json:"external_id"`
	AsOf           string `json:"as_of"`
}
type resolveIdentityResp struct {
	Identity *identityProto `json:"identity"`
}

type claimSlotReq struct {
	TenantID    string            `json:"tenant_id"`
	SourceRef   string            `json:"source_ref"`
	Values      map[string]string `json:"values"`
	Origin      string            `json:"origin"`
	RecordedAt  string            `json:"recorded_at"`
	Quality     int32             `json:"quality,omitempty"`
	CollectedAt string            `json:"collected_at,omitempty"`
	Actor       string            `json:"actor"`
}
type slotProto struct {
	ID               string `json:"id"`
	SlotKey          string `json:"slot_key"`
	OriginKind       string `json:"origin_kind"`
	AuthoritativeRef string `json:"authoritative_ref"`
	Status           string `json:"status"`
}
type claimSlotResp struct {
	Outcome string     `json:"outcome"`
	Reason  string     `json:"reason"`
	Slot    *slotProto `json:"slot"`
}

type componentProto struct {
	Kind     string `json:"kind"`
	Amount   string `json:"amount"`
	Quantity string `json:"quantity,omitempty"`
	Rate     string `json:"rate,omitempty"`
}

type ingestAssertionReq struct {
	TenantID             string           `json:"tenant_id"`
	SourceSystemID       string           `json:"source_system_id"`
	ExternalSettlementID string           `json:"external_settlement_id"`
	ProducerRef          string           `json:"producer_ref"`
	PeriodStart          string           `json:"period_start"`
	PeriodEnd            string           `json:"period_end"`
	Currency             string           `json:"currency"`
	AmountScale          int32            `json:"amount_scale"`
	Total                string           `json:"total"`
	Components           []componentProto `json:"components"`
	AssertedAt           string           `json:"asserted_at"`
	ImportBatchID        string           `json:"import_batch_id"`
	SourceRecordID       string           `json:"source_record_id"`
	RawPayload           json.RawMessage  `json:"raw_payload"`
	CreatedBy            string           `json:"created_by"`
}
type assertionProto struct {
	ID                string `json:"id"`
	ProducerRef       string `json:"producer_ref"`
	Total             string `json:"total"`
	OriginKind        string `json:"origin_kind"`
	SourcePayloadHash string `json:"source_payload_hash"`
}
type ingestAssertionResp struct {
	Assertion *assertionProto `json:"assertion"`
	Created   bool            `json:"created"`
}

type recordComputationReq struct {
	TenantID      string           `json:"tenant_id"`
	AssertionID   string           `json:"assertion_id"`
	ProducerRef   string           `json:"producer_ref"`
	PeriodStart   string           `json:"period_start"`
	PeriodEnd     string           `json:"period_end"`
	Currency      string           `json:"currency"`
	AmountScale   int32            `json:"amount_scale"`
	Total         string           `json:"total"`
	Components    []componentProto `json:"components"`
	PolicyVersion string           `json:"policy_version"`
	RateCardID    string           `json:"rate_card_id"`
	InputDigest   string           `json:"input_digest"`
	AsOf          string           `json:"as_of"`
	CreatedBy     string           `json:"created_by"`
}
type computationProto struct {
	ID    string `json:"id"`
	Total string `json:"total"`
}
type recordComputationResp struct {
	Computation *computationProto `json:"computation"`
}

type adjudicateReq struct {
	TenantID      string `json:"tenant_id"`
	AssertionID   string `json:"assertion_id"`
	ComputationID string `json:"computation_id"`
	Actor         string `json:"actor"`
}
type evidenceProto struct {
	Kind            string `json:"kind"`
	DeltaMinorUnits int64  `json:"delta_minor_units"`
	QuantityDiffers bool   `json:"quantity_differs,omitempty"`
	RateDiffers     bool   `json:"rate_differs,omitempty"`
}
type divergenceProto struct {
	ID              string            `json:"id"`
	Delta           string            `json:"delta"`
	DeltaMinorUnits int64             `json:"delta_minor_units"`
	Classification  string            `json:"classification"`
	Rationale       string            `json:"rationale"`
	Evidence        []evidenceProto   `json:"evidence"`
	MLHypotheses    []json.RawMessage `json:"ml_hypotheses,omitempty"`
	Status          string            `json:"status"`
	NeedsReview     bool              `json:"needs_review"`
}
type adjudicateResp struct {
	Divergence *divergenceProto `json:"divergence"`
}

const (
	canonicalSvc = "canonical.v1.CanonicalService"
	shadowSvc    = "shadowsettlement.v1.ShadowSettlementService"
)

var (
	periodStart = "2026-02-01"
	periodEnd   = "2026-02-28"
	assertedAt  = "2026-03-01T00:00:00Z"
	collectedAt = "2026-02-14T06:00:00Z"
)

// TestShadowModePipeline runs the platform's central claim end to end: an
// incumbent's settlement is imported under a canonicalised producer identity,
// the platform's own recomputation is recorded beside it, and the difference is
// classified with a reason — across three services, over HTTP, against real
// databases.
func TestShadowModePipeline(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	sourceSystem := newID("src")
	externalProducer := "P-001"
	platformProducer := newID("prd")

	// 1. Canonicalisation: the incumbent's producer number means a platform
	//    producer, over a stated period.
	identity, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/MapIdentity", mapIdentityReq{
			TenantID:       p.tenant,
			SourceSystemID: sourceSystem,
			EntityKind:     "PRODUCER",
			ExternalID:     externalProducer,
			EntityID:       platformProducer,
			Method:         "EXACT",
			ValidFrom:      "2020-01-01T00:00:00Z",
			Actor:          "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("MapIdentity: %v", err)
	}
	if identity.Identity.EntityID != platformProducer {
		t.Fatalf("mapped to %q, want %q", identity.Identity.EntityID, platformProducer)
	}

	// 2. Resolution requires an instant, because identifiers get reused.
	resolved, err := svcclient.Call[resolveIdentityReq, resolveIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveIdentity", resolveIdentityReq{
			TenantID:       p.tenant,
			SourceSystemID: sourceSystem,
			EntityKind:     "PRODUCER",
			ExternalID:     externalProducer,
			AsOf:           assertedAt,
		}, p.opts())
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	if resolved.Identity.EntityID != platformProducer {
		t.Fatalf("resolved to %q, want %q", resolved.Identity.EntityID, platformProducer)
	}
	producerRef := "producer:" + resolved.Identity.EntityID

	// 3. The incumbent's settlement, recorded verbatim as an assertion.
	payload := json.RawMessage(`{"member":"P-001","period":"2026-02","net":"1050.00"}`)
	assertion, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](ctx, p.shadow(),
		shadowSvc+"/IngestAssertion", ingestAssertionReq{
			TenantID:             p.tenant,
			SourceSystemID:       sourceSystem,
			ExternalSettlementID: "EXT-2026-02-001",
			ProducerRef:          producerRef,
			PeriodStart:          periodStart,
			PeriodEnd:            periodEnd,
			Currency:             "INR",
			AmountScale:          2,
			Total:                "1050.00",
			Components: []componentProto{
				{Kind: "BASE_PRICE", Amount: "1050.00", Quantity: "300.000", Rate: "3.5000"},
			},
			AssertedAt:     assertedAt,
			ImportBatchID:  newID("bat"),
			SourceRecordID: "REC-001",
			RawPayload:     payload,
			CreatedBy:      "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("IngestAssertion: %v", err)
	}
	if !assertion.Created {
		t.Error("the first ingestion reported the assertion as already present")
	}
	if assertion.Assertion.OriginKind != "IMPORTED" {
		t.Errorf("origin = %q, want IMPORTED", assertion.Assertion.OriginKind)
	}
	if assertion.Assertion.SourcePayloadHash == "" {
		t.Error("no payload hash was recorded, so a replay could not be detected")
	}

	// 4. The platform's own recomputation: same milk, its own rate card.
	computation, err := svcclient.Call[recordComputationReq, recordComputationResp](ctx, p.shadow(),
		shadowSvc+"/RecordComputation", recordComputationReq{
			TenantID:    p.tenant,
			AssertionID: assertion.Assertion.ID,
			ProducerRef: producerRef,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Currency:    "INR",
			AmountScale: 2,
			Total:       "1000.00",
			Components: []componentProto{
				{Kind: "BASE_PRICE", Amount: "1000.00", Quantity: "300.000", Rate: "3.3333"},
			},
			PolicyVersion: "policy-2026.02",
			RateCardID:    newID("rct"),
			InputDigest:   "sha256:e2e-inputs",
			AsOf:          "2026-03-02T00:00:00Z",
			CreatedBy:     "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RecordComputation: %v", err)
	}

	// 5. Adjudication: same quantity at a different rate is a policy difference,
	//    and the classifier must say so without consulting any model.
	adjudicated, err := svcclient.Call[adjudicateReq, adjudicateResp](ctx, p.shadow(),
		shadowSvc+"/Adjudicate", adjudicateReq{
			TenantID:      p.tenant,
			AssertionID:   assertion.Assertion.ID,
			ComputationID: computation.Computation.ID,
			Actor:         "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("Adjudicate: %v", err)
	}

	d := adjudicated.Divergence
	if d.Classification != "POLICY_DIFFERENCE" {
		t.Fatalf("classification = %q, want POLICY_DIFFERENCE: %s", d.Classification, d.Rationale)
	}
	if d.DeltaMinorUnits != 5000 {
		t.Errorf("delta = %d minor units, want 5000", d.DeltaMinorUnits)
	}
	if d.Delta != "50.00" {
		t.Errorf("delta = %q, want 50.00", d.Delta)
	}
	if d.Rationale == "" {
		t.Error("the divergence carries no rationale a reviewer could read")
	}
	if len(d.Evidence) == 0 {
		t.Error("no per-component evidence was recorded")
	}
	if !d.NeedsReview {
		t.Error("a policy difference must reach a reviewer")
	}
	// The ML tier is not running in this harness, and the verdict is complete
	// regardless — that is the point of the deterministic classifier.
	if len(d.MLHypotheses) != 0 {
		t.Errorf("hypotheses were attached with no ML tier configured: %v", d.MLHypotheses)
	}
}

// Replaying an import must be a no-op, end to end and not merely in the unit
// tests: a source system that re-sends yesterday's file must not double-pay.
func TestReplayedImportIsIdempotentAcrossTheWire(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	sourceSystem := newID("src")
	req := ingestAssertionReq{
		TenantID:             p.tenant,
		SourceSystemID:       sourceSystem,
		ExternalSettlementID: "EXT-REPLAY-001",
		ProducerRef:          "producer:" + newID("prd"),
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		Currency:             "INR",
		AmountScale:          2,
		Total:                "900.00",
		Components:           []componentProto{{Kind: "BASE_PRICE", Amount: "900.00"}},
		AssertedAt:           assertedAt,
		ImportBatchID:        newID("bat"),
		SourceRecordID:       "REC-REPLAY",
		RawPayload:           json.RawMessage(`{"net":"900.00"}`),
		CreatedBy:            "e2e",
	}

	first, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](ctx, p.shadow(),
		shadowSvc+"/IngestAssertion", req, p.opts())
	if err != nil {
		t.Fatalf("first ingestion: %v", err)
	}
	if !first.Created {
		t.Fatal("the first ingestion did not create the assertion")
	}

	for i := 0; i < 5; i++ {
		// A replay arrives under a new batch id, as a re-run genuinely would.
		replay := req
		replay.ImportBatchID = newID("bat")

		again, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](ctx, p.shadow(),
			shadowSvc+"/IngestAssertion", replay, p.opts())
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if again.Created {
			t.Fatalf("replay %d created a second assertion", i)
		}
		if again.Assertion.ID != first.Assertion.ID {
			t.Fatalf("replay %d returned %s, want the original %s",
				i, again.Assertion.ID, first.Assertion.ID)
		}
	}
}

// An identical total is a MATCH and needs no reviewer — the calm path has to
// work too, or every period would raise noise.
func TestAgreeingSettlementsMatch(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	sourceSystem := newID("src")
	producerRef := "producer:" + newID("prd")

	assertion, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](ctx, p.shadow(),
		shadowSvc+"/IngestAssertion", ingestAssertionReq{
			TenantID: p.tenant, SourceSystemID: sourceSystem,
			ExternalSettlementID: "EXT-MATCH-001", ProducerRef: producerRef,
			PeriodStart: periodStart, PeriodEnd: periodEnd,
			Currency: "INR", AmountScale: 2, Total: "1000.00",
			Components: []componentProto{{Kind: "BASE_PRICE", Amount: "1000.00", Quantity: "300.000", Rate: "3.3333"}},
			AssertedAt: assertedAt, ImportBatchID: newID("bat"),
			SourceRecordID: "REC-MATCH", RawPayload: json.RawMessage(`{"net":"1000.00"}`),
			CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("IngestAssertion: %v", err)
	}

	computation, err := svcclient.Call[recordComputationReq, recordComputationResp](ctx, p.shadow(),
		shadowSvc+"/RecordComputation", recordComputationReq{
			TenantID: p.tenant, AssertionID: assertion.Assertion.ID, ProducerRef: producerRef,
			PeriodStart: periodStart, PeriodEnd: periodEnd,
			Currency: "INR", AmountScale: 2, Total: "1000.00",
			Components:    []componentProto{{Kind: "BASE_PRICE", Amount: "1000.00", Quantity: "300.000", Rate: "3.3333"}},
			PolicyVersion: "policy-2026.02", RateCardID: newID("rct"),
			InputDigest: "sha256:match", AsOf: "2026-03-02T00:00:00Z", CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RecordComputation: %v", err)
	}

	adjudicated, err := svcclient.Call[adjudicateReq, adjudicateResp](ctx, p.shadow(),
		shadowSvc+"/Adjudicate", adjudicateReq{
			TenantID: p.tenant, AssertionID: assertion.Assertion.ID,
			ComputationID: computation.Computation.ID, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("Adjudicate: %v", err)
	}

	d := adjudicated.Divergence
	if d.Classification != "MATCH" {
		t.Fatalf("classification = %q, want MATCH: %s", d.Classification, d.Rationale)
	}
	if d.DeltaMinorUnits != 0 {
		t.Errorf("delta = %d, want 0", d.DeltaMinorUnits)
	}
	if d.NeedsReview {
		t.Error("a match was sent to a reviewer")
	}
}

// The collection slot is where two records claiming to be the same collection
// meet. Across the wire, the policy must resolve them the way it declares.
func TestCollectionSlotResolvesAcrossTheWire(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	if _, err := svcclient.Call[declarePolicyReq, declarePolicyResp](ctx, p.canonical(),
		canonicalSvc+"/DeclarePolicy", declarePolicyReq{
			TenantID:      p.tenant,
			Name:          "e2e-default",
			Dimensions:    []string{"PRODUCER", "COLLECTION_DATE", "SHIFT"},
			Resolution:    "FIRST_WINS",
			Version:       1,
			EffectiveFrom: "2020-01-01T00:00:00Z",
			Actor:         "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("DeclarePolicy: %v", err)
	}

	values := map[string]string{
		"PRODUCER":        "P-001",
		"COLLECTION_DATE": "2026-02-14",
		"SHIFT":           "MORNING",
	}

	first, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: p.tenant, SourceRef: "obs-early", Values: values,
			Origin: "NATIVE", RecordedAt: "2026-02-14T06:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("first ClaimSlot: %v", err)
	}
	if first.Outcome != "SLOT_ESTABLISHED" {
		t.Fatalf("first claim: %s (%s)", first.Outcome, first.Reason)
	}

	// A later duplicate under FIRST_WINS must not displace the incumbent.
	second, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: p.tenant, SourceRef: "obs-late", Values: values,
			Origin: "NATIVE", RecordedAt: "2026-02-14T09:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("second ClaimSlot: %v", err)
	}
	if second.Outcome != "SLOT_RETAINED" {
		t.Fatalf("second claim: %s (%s), want SLOT_RETAINED", second.Outcome, second.Reason)
	}
	if second.Slot.AuthoritativeRef != "obs-early" {
		t.Errorf("slot holder = %q, want obs-early", second.Slot.AuthoritativeRef)
	}

	// An imported record for the same collection is not a competing claim: in
	// shadow mode both exist by design and comparing them is settlement's job.
	imported, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: p.tenant, SourceRef: "rec-imported", Values: values,
			Origin: "IMPORTED", RecordedAt: "2026-02-14T06:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("imported ClaimSlot: %v", err)
	}
	if imported.Outcome != "SLOT_ESTABLISHED" {
		t.Fatalf("imported claim: %s (%s), want its own slot", imported.Outcome, imported.Reason)
	}
	if imported.Slot.ID == second.Slot.ID {
		t.Error("the imported claim landed in the native record's slot")
	}
	if imported.Slot.SlotKey != second.Slot.SlotKey {
		t.Error("the same collection produced different slot keys across origins")
	}
}

// A tenant must not read another tenant's records, over the wire as well as in
// the repository tests.
func TestTenantIsolationOverTheWire(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	sourceSystem := newID("src")
	if _, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/MapIdentity", mapIdentityReq{
			TenantID: p.tenant, SourceSystemID: sourceSystem,
			EntityKind: "PRODUCER", ExternalID: "P-ISOLATED",
			EntityID: newID("prd"), Method: "EXACT",
			ValidFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("MapIdentity: %v", err)
	}

	other := newID("tnt")
	_, err := svcclient.Call[resolveIdentityReq, resolveIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveIdentity", resolveIdentityReq{
			TenantID: other, SourceSystemID: sourceSystem,
			EntityKind: "PRODUCER", ExternalID: "P-ISOLATED",
			AsOf: assertedAt,
		}, svcclient.CallOptions{TenantID: other, RequestID: newID("req")})
	if !svcclient.IsNotFound(err) {
		t.Fatalf("another tenant resolved the mapping: err = %v", err)
	}
}

// A validation failure must arrive as invalid_argument, not as a 500 the caller
// would retry.
func TestValidationFailureIsNotRetryable(t *testing.T) {
	p := startPlatform(t)

	_, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](context.Background(), p.shadow(),
		shadowSvc+"/IngestAssertion", ingestAssertionReq{
			TenantID: p.tenant,
			// No source system, producer, batch or payload: unusable.
			ExternalSettlementID: "EXT-BAD",
			Currency:             "INR",
			AmountScale:          2,
			Total:                "10.00",
			PeriodStart:          periodStart,
			PeriodEnd:            periodEnd,
			AssertedAt:           assertedAt,
			CreatedBy:            "e2e",
		}, p.opts())
	if err == nil {
		t.Fatal("an unusable assertion was accepted")
	}

	var svcErr *svcclient.Error
	if !asSvcError(err, &svcErr) {
		t.Fatalf("got %v, want a structured error", err)
	}
	if svcErr.Retryable() {
		t.Errorf("a validation failure was reported as retryable (http %d)", svcErr.HTTPStatus)
	}
	if svcErr.HTTPStatus != 400 {
		t.Errorf("status = %d, want 400", svcErr.HTTPStatus)
	}
}

func asSvcError(err error, target **svcclient.Error) bool {
	e, ok := err.(*svcclient.Error)
	if ok {
		*target = e
	}
	return ok
}
