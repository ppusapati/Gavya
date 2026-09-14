//go:build e2e

// What actually happens when the ML tier is connected.
//
// Everything else in this suite runs with no ML tier, and proves the platform is
// complete without one. These tests run the other half: the Rust services are
// started, the Go services are told where they are, and the question is whether
// consulting them changes anything and whether the answer survives the wire.
//
// The two things a unit test on either side cannot catch, and both of which have
// bitten this boundary before:
//
//   - A field renamed on one side. Both sides' tests pass; the value arrives as
//     the zero value of its type and nothing says so.
//   - A value that does not survive JSON. serde_json writes a non-finite float
//     as null and Go decodes null into float64 as 0.0 — so an unbounded anomaly
//     band arrives as [0, 0], an infinitely *tight* one, and infinite effective
//     degrees of freedom arrive as zero. Both inversions are silent.
package e2e

import (
	"context"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// mlObservationReq carries the uncertainty fields the plain observation tests
// leave out, because without a tier there is nothing to send them to.
type mlObservationReq struct {
	TenantID string       `json:"tenant_id"`
	Subject  subjectProto `json:"subject"`
	Quantity string       `json:"quantity_kind"`
	Value    float64      `json:"value"`
	Unit     string       `json:"unit,omitempty"`

	InstrumentID string `json:"instrument_id,omitempty"`

	Origin    originProto `json:"origin"`
	ValidFrom string      `json:"valid_from"`

	UncertaintyModelID  string             `json:"uncertainty_model_id,omitempty"`
	UncertaintyInputs   map[string]float64 `json:"uncertainty_inputs,omitempty"`
	CoverageProbability float64            `json:"coverage_probability,omitempty"`

	CreatedBy string `json:"created_by"`
}

type uncertaintyProto struct {
	ModelID             string  `json:"uncertainty_model_id"`
	ModelVersion        string  `json:"model_version,omitempty"`
	StandardUncertainty float64 `json:"standard_uncertainty"`
	ExpandedUncertainty float64 `json:"expanded_uncertainty"`
	CoverageFactor      float64 `json:"coverage_factor"`
	CoverageProbability float64 `json:"coverage_probability"`
}

type mlObservationProto struct {
	ID                 string            `json:"id"`
	Value              exact.Fixed       `json:"value"`
	Uncertainty        *uncertaintyProto `json:"uncertainty,omitempty"`
	UncertaintyMissing bool              `json:"uncertainty_missing"`
	EligibilityVerdict string            `json:"eligibility_verdict"`
}

type mlObservationResp struct {
	Observation *mlObservationProto `json:"observation"`
}

func recordWithML(t *testing.T, p *mlPlatform, in mlObservationReq) *mlObservationProto {
	t.Helper()
	in.TenantID = p.tenant
	if in.CreatedBy == "" {
		in.CreatedBy = "e2e"
	}
	if in.Origin.Kind == "" {
		in.Origin = originProto{Kind: "NATIVE"}
	}
	if in.ValidFrom == "" {
		in.ValidFrom = "2026-03-01T06:00:00Z"
	}
	resp, err := svcclient.Call[mlObservationReq, mlObservationResp](
		context.Background(), p.observation(), observationSvc+"/RecordObservation", in, p.opts())
	if err != nil {
		t.Fatalf("record observation with the ML tier connected: %v", err)
	}
	if resp.Observation == nil {
		t.Fatal("no observation came back")
	}
	return resp.Observation
}

// The same reading, recorded against the tier and without it.
//
// This is the test the whole file exists for. Everything else in the suite shows
// the platform is complete with no tier; this shows that connecting one does
// something, which is the claim that had never been checked.
func TestConnectingTheUncertaintyTierPutsAnEstimateOnTheReading(t *testing.T) {
	withML := startMLPlatform(t)

	got := recordWithML(t, withML, mlObservationReq{
		Subject:  subjectProto{Kind: "CATTLE", ID: newID("cat")},
		Quantity: "FAT_PERCENT", Value: 4.1, Unit: "PERCENT",
		UncertaintyModelID: "milk.fat.gerber.v1",
		UncertaintyInputs: map[string]float64{
			"repeatability_sd":        0.03,
			"calibration_uncertainty": 0.02,
			"resolution":              0.05,
		},
	})

	if got.UncertaintyMissing {
		t.Fatal("the tier was connected, answered, and the observation still says its " +
			"uncertainty is missing")
	}
	if got.Uncertainty == nil {
		t.Fatal("uncertainty_missing is false and there is no estimate; a consumer reading " +
			"this would find nothing where it was told to expect something")
	}
	u := got.Uncertainty
	if u.StandardUncertainty <= 0 {
		t.Errorf("standard uncertainty came back as %v; a reading whose stated uncertainty is "+
			"zero claims to be exact, which no Gerber test is", u.StandardUncertainty)
	}
	if u.ExpandedUncertainty <= u.StandardUncertainty {
		t.Errorf("expanded %v is not larger than standard %v; the coverage factor did not "+
			"survive the wire", u.ExpandedUncertainty, u.StandardUncertainty)
	}
	if u.CoverageFactor <= 1 {
		t.Errorf("coverage factor %v; a k of 0 or 1 is what arrives when the field was "+
			"renamed on one side and Go decoded a missing number as zero", u.CoverageFactor)
	}
	if u.CoverageProbability <= 0 || u.CoverageProbability >= 1 {
		t.Errorf("coverage probability %v is not a probability", u.CoverageProbability)
	}
	if u.ModelVersion == "" {
		t.Error("no model version on the estimate; a figure that priced milk has to say " +
			"which model produced it or a replay cannot be pinned to the same one")
	}

	// And the same reading with no model named gets no estimate, and says so.
	// Without this the test above would pass against a service that fabricates
	// an estimate from nothing.
	plain := recordWithML(t, withML, mlObservationReq{
		Subject:  subjectProto{Kind: "CATTLE", ID: newID("cat")},
		Quantity: "FAT_PERCENT", Value: 4.1, Unit: "PERCENT",
	})
	if !plain.UncertaintyMissing || plain.Uncertainty != nil {
		t.Errorf("a reading that named no uncertainty model came back with an estimate: %+v",
			plain.Uncertainty)
	}
}

// The disabled path still holds, in the same suite run.
//
// The main platform has no ML tier configured. If connecting one were to become
// required — a nil dereference on the estimate, an error where absence was
// meant — this is where it would show, and it must not.
func TestTheSameReadingIsStillRecordedWithNoTierAtAll(t *testing.T) {
	// Confirms the ML tier is genuinely available before asserting anything
	// about its absence elsewhere; otherwise this passes for the wrong reason
	// on a machine with no cargo.
	_ = startMLPlatform(t)
	p := startPlatform(t)

	resp, err := svcclient.Call[mlObservationReq, mlObservationResp](
		context.Background(), p.observation(), observationSvc+"/RecordObservation",
		mlObservationReq{
			TenantID: p.tenant,
			Subject:  subjectProto{Kind: "CATTLE", ID: newID("cat")},
			Quantity: "FAT_PERCENT", Value: 4.1, Unit: "PERCENT",
			Origin: originProto{Kind: "NATIVE"}, ValidFrom: "2026-03-01T06:00:00Z",
			UncertaintyModelID: "milk.fat.gerber.v1",
			UncertaintyInputs:  map[string]float64{"repeatability_sd": 0.03},
			CreatedBy:          "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("recording against a platform with no ML tier failed: %v", err)
	}
	got := resp.Observation
	if !got.UncertaintyMissing {
		t.Error("no tier is configured and the observation does not report its uncertainty " +
			"as missing; absence of an estimate must never read as a perfect one")
	}
	if got.Uncertainty != nil {
		t.Errorf("an estimate appeared with no tier to produce it: %+v", got.Uncertainty)
	}
	if got.EligibilityVerdict == "" {
		t.Error("the deterministic eligibility verdict is missing; it does not depend on the " +
			"ML tier and must be there either way")
	}
}

// ---------------------------------------------------------------------------
// The tier, spoken to directly
// ---------------------------------------------------------------------------
//
// Only what this platform can prove and the contract test cannot. The encoding
// traps — a non-finite float arriving as zero, insufficient evidence arriving as
// a retryable 5xx, a model pin being ignored — are covered by
// libs/integrity/mlclient/contract_integration_test.go, which drives the real
// binaries through the real typed client. Repeating them here with a hand-rolled
// request would be a second, worse copy of a test that already exists, and the
// hand-rolled version would not even use the client the platform ships.
//
// scripts/check-all.sh runs that contract suite. It is behind a build tag
// because it needs `cargo build` first, not because it is optional.

func mlClient(t *testing.T, p *mlPlatform, bin string) *svcclient.Client {
	t.Helper()
	url, ok := p.mlURLs[bin]
	if !ok {
		t.Fatalf("%s is not running in the ML platform", bin)
	}
	return svcclient.New(svcclient.Config{BaseURL: url})
}

type estimateReq struct {
	TenantID           string             `json:"tenant_id"`
	UncertaintyModelID string             `json:"uncertainty_model_id"`
	MeasuredValue      float64            `json:"measured_value"`
	Unit               string             `json:"unit,omitempty"`
	Inputs             map[string]float64 `json:"inputs,omitempty"`
}

type estimateResp struct {
	ModelVersion        string  `json:"model_version"`
	StandardUncertainty float64 `json:"standard_uncertainty"`
}

// Every Rust service in the tier answers a health check, which is what a
// deployment's readiness probe rests on.
func TestEveryMLServiceServesHealth(t *testing.T) {
	p := startMLPlatform(t)
	for _, svc := range mlServices {
		if _, ok := p.mlURLs[svc.bin]; !ok {
			t.Errorf("%s is not running", svc.bin)
			continue
		}
		c := mlClient(t, p, svc.bin)
		if err := c.Health(context.Background()); err != nil {
			t.Errorf("%s does not answer a health check: %v", svc.bin, err)
		}
	}
}

// The tier is tenant-scoped like everything else: a request whose body names a
// different tenant from the one on the transport is refused rather than
// answered for whichever of the two the service happened to read.
func TestTheMLTierRefusesAMismatchedTenant(t *testing.T) {
	p := startMLPlatform(t)
	c := mlClient(t, p, "uncertainty-service")

	_, err := svcclient.Call[estimateReq, estimateResp](
		context.Background(), c,
		"gavya.ml.v1.UncertaintyService/EstimateUncertainty",
		estimateReq{
			TenantID:           newID("tnt"), // not the tenant on the transport
			UncertaintyModelID: "milk.fat.gerber.v1",
			MeasuredValue:      4.0, Unit: "PERCENT",
			Inputs: map[string]float64{"resolution": 0.02},
		}, p.opts())
	if err == nil {
		t.Error("a request whose body named one tenant and whose transport named another was " +
			"answered; one of the two was silently ignored and nothing says which")
	}
}
