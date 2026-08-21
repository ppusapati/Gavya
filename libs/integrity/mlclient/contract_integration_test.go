//go:build mlintegration

// Cross-language contract tests.
//
// These start the real Rust ML binaries and drive them through the real Go
// client, which is the only way to prove the two sides' wire types agree.
// Unit tests on either side cannot catch a renamed JSON field or a value that
// does not survive the encoding.
//
// Run with:
//
//	cd ml && cargo build --workspace
//	cd libs/integrity && go test -tags mlintegration ./mlclient/...
//
// Set ML_BIN_DIR to override the default target/debug location.
package mlclient

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startService boots one Rust ML binary on a free port and returns its base URL.
func startService(t *testing.T, name string) string {
	t.Helper()

	binDir := os.Getenv("ML_BIN_DIR")
	if binDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		binDir = filepath.Join(wd, "..", "..", "..", "ml", "target", "debug")
	}
	bin := filepath.Join(binDir, name)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("%s not built (%v); run `cargo build --workspace` in ml/ first", name, err)
	}

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "LISTEN_ADDR="+addr, "LOG_LEVEL=warn")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	baseURL := "http://" + addr
	waitReady(t, baseURL, name)
	return baseURL
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitReady(t *testing.T, baseURL, name string) {
	t.Helper()
	c := New(Config{BaseURL: baseURL, Timeout: time.Second})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := c.Health(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not become ready", name)
}

func opts() CallOptions { return CallOptions{TenantID: "01HTENANT0000000000000000", RequestID: "req-1"} }

func TestAnomalyContract(t *testing.T) {
	client := NewAnomalyClient(Config{BaseURL: startService(t, "anomaly-service"), Timeout: 5 * time.Second})
	ctx := context.Background()

	points := []SeriesPoint{
		{ObservationID: "a", ValidAt: "2026-02-01T06:00:00Z", Value: 12.0},
		{ObservationID: "b", ValidAt: "2026-02-02T06:00:00Z", Value: 12.4},
		{ObservationID: "c", ValidAt: "2026-02-03T06:00:00Z", Value: 11.8},
		{ObservationID: "d", ValidAt: "2026-02-04T06:00:00Z", Value: 12.2},
		{ObservationID: "e", ValidAt: "2026-02-05T06:00:00Z", Value: 12.1},
		{ObservationID: "f", ValidAt: "2026-02-06T06:00:00Z", Value: 11.9},
		{ObservationID: "spike", ValidAt: "2026-02-07T06:00:00Z", Value: 95.0},
	}

	resp, err := client.ScoreSeries(ctx, &ScoreCollectionSeriesRequest{
		TenantID:    opts().TenantID,
		SubjectRef:  "cattle:01HCATTLE000000000000000",
		Quantity:    "VOLUME_LITRES",
		Points:      points,
	}, opts())
	if err != nil {
		t.Fatalf("ScoreSeries: %v", err)
	}

	if resp.ModelVersion == "" {
		t.Error("model_version did not decode")
	}
	if resp.BaselineInsufficient {
		t.Error("a seven point series was reported as an insufficient baseline")
	}
	if len(resp.Scores) != len(points) {
		t.Fatalf("got %d scores, want %d", len(resp.Scores), len(points))
	}

	var spike *AnomalyScore
	for i := range resp.Scores {
		if resp.Scores[i].ObservationID == "spike" {
			spike = &resp.Scores[i]
		}
	}
	if spike == nil {
		t.Fatal("the spike observation is missing from the response")
	}
	if !spike.Flagged {
		t.Errorf("the spike was not flagged: %+v", spike)
	}
	if spike.Explanation == "" {
		t.Error("explanation did not decode")
	}
	if !spike.Bounded() {
		t.Error("a scored point must carry a tolerance band")
	}
	if *spike.LowerBound >= *spike.UpperBound {
		t.Errorf("band is inverted: [%v, %v]", *spike.LowerBound, *spike.UpperBound)
	}
}

// TestAnomalyUnboundedBandDecodesAsNil is the regression test for the encoding
// trap: an unbounded band must not arrive in Go as [0, 0].
func TestAnomalyUnboundedBandDecodesAsNil(t *testing.T) {
	client := NewAnomalyClient(Config{BaseURL: startService(t, "anomaly-service"), Timeout: 5 * time.Second})

	resp, err := client.ScoreSeries(context.Background(), &ScoreCollectionSeriesRequest{
		TenantID:   opts().TenantID,
		SubjectRef: "cattle:01HCATTLE000000000000000",
		Quantity:   "VOLUME_LITRES",
		Points: []SeriesPoint{
			{ObservationID: "a", ValidAt: "2026-02-01T06:00:00Z", Value: 12.0},
			{ObservationID: "b", ValidAt: "2026-02-02T06:00:00Z", Value: 900.0},
		},
	}, opts())
	if err != nil {
		t.Fatalf("ScoreSeries: %v", err)
	}

	if !resp.BaselineInsufficient {
		t.Fatal("a two point series must report an insufficient baseline")
	}
	for _, s := range resp.Scores {
		if s.Flagged {
			t.Errorf("observation %s was flagged without a baseline", s.ObservationID)
		}
		if s.Bounded() {
			t.Errorf("observation %s reported a band with no baseline: [%v, %v]",
				s.ObservationID, *s.LowerBound, *s.UpperBound)
		}
	}
}

func TestUncertaintyContract(t *testing.T) {
	client := NewUncertaintyClient(Config{BaseURL: startService(t, "uncertainty-service"), Timeout: 5 * time.Second})
	ctx := context.Background()

	resp, err := client.Estimate(ctx, &EstimateUncertaintyRequest{
		TenantID:           opts().TenantID,
		UncertaintyModelID: "milk.volume.flowmeter.v1",
		Quantity:           "VOLUME_LITRES",
		MeasuredValue:      120.0,
		Unit:               "L",
		Inputs: map[string]float64{
			"max_permissible_error": 0.6,
			"resolution":            0.1,
		},
	}, opts())
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}

	if resp.StandardUncertainty <= 0 {
		t.Errorf("standard uncertainty = %v, want positive", resp.StandardUncertainty)
	}
	if resp.CoverageFactor <= 0 {
		t.Errorf("coverage factor = %v, want positive", resp.CoverageFactor)
	}
	if resp.ExpandedUncertainty <= resp.StandardUncertainty {
		t.Errorf("expanded (%v) must exceed standard (%v)", resp.ExpandedUncertainty, resp.StandardUncertainty)
	}
	if len(resp.Components) == 0 {
		t.Fatal("components did not decode; the estimate would not be auditable")
	}
	for _, c := range resp.Components {
		if c.Name == "" || c.Type == "" || c.Distribution == "" {
			t.Errorf("component did not decode fully: %+v", c)
		}
	}

	// A budget of Type B components only has infinite effective degrees of
	// freedom, which must arrive as nil rather than as a misleading 0.
	if resp.EffectiveDegreesOfFreedom != nil {
		t.Errorf("effective dof = %v, want nil for a pure Type B budget", *resp.EffectiveDegreesOfFreedom)
	}
	for _, c := range resp.Components {
		if c.Type == "B" && c.DegreesOfFreedom != nil {
			t.Errorf("Type B component %s reported %v degrees of freedom, want nil", c.Name, *c.DegreesOfFreedom)
		}
	}

	if lo, hi := resp.LowerBound, resp.UpperBound; lo >= hi {
		t.Errorf("coverage interval is inverted: [%v, %v]", lo, hi)
	}
}

func TestUncertaintyInsufficientEvidenceIsNotRetried(t *testing.T) {
	client := NewUncertaintyClient(Config{BaseURL: startService(t, "uncertainty-service"), Timeout: 5 * time.Second})

	// No usable inputs: the service must refuse rather than assert a perfect
	// measurement, and the refusal must be non-retryable so Go gives up at once.
	_, err := client.Estimate(context.Background(), &EstimateUncertaintyRequest{
		TenantID:           opts().TenantID,
		UncertaintyModelID: "milk.volume.flowmeter.v1",
		Quantity:           "VOLUME_LITRES",
		MeasuredValue:      120.0,
		Unit:               "L",
	}, opts())
	if err == nil {
		t.Fatal("an estimate with no inputs returned a result instead of an error")
	}

	var mlErr *Error
	if !asMLError(err, &mlErr) {
		t.Fatalf("got %v, want a structured *Error", err)
	}
	if mlErr.Retryable() {
		t.Errorf("insufficient evidence was reported as retryable (http %d)", mlErr.HTTPStatus)
	}
	if mlErr.Code != "insufficient_evidence" {
		t.Errorf("code = %q, want insufficient_evidence", mlErr.Code)
	}
}

func TestReconciliationContract(t *testing.T) {
	client := NewReconciliationClient(Config{BaseURL: startService(t, "reconciliation-service"), Timeout: 5 * time.Second})

	resp, err := client.Reconcile(context.Background(), &ReconcileMassBalanceRequest{
		TenantID:      opts().TenantID,
		BalanceWindow: "2026-02",
		Flows: []FlowMeasurement{
			{FlowID: "in", FromNode: "", ToNode: "chiller", Measured: 100.0, StandardUncertainty: 1.0},
			{FlowID: "out", FromNode: "chiller", ToNode: "", Measured: 98.0, StandardUncertainty: 1.0},
		},
	}, opts())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if !resp.Converged {
		t.Fatal("a determined two flow network did not converge")
	}
	if len(resp.Flows) != 2 {
		t.Fatalf("got %d flows, want 2", len(resp.Flows))
	}
	// Equal uncertainties split the 2.0 gap evenly.
	for _, f := range resp.Flows {
		if diff := f.Reconciled - 99.0; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("flow %s reconciled to %v, want 99.0", f.FlowID, f.Reconciled)
		}
		if f.Adjustment == 0 {
			t.Errorf("flow %s reported no adjustment despite an imbalance", f.FlowID)
		}
	}
	if resp.ResidualAfter > 1e-6 {
		t.Errorf("residual_after = %v, want ~0", resp.ResidualAfter)
	}
	if resp.ResidualBefore <= resp.ResidualAfter {
		t.Errorf("residual did not improve: before %v, after %v", resp.ResidualBefore, resp.ResidualAfter)
	}
}

func TestDivergenceContract(t *testing.T) {
	client := NewDivergenceClient(Config{BaseURL: startService(t, "divergence-service"), Timeout: 5 * time.Second})
	ctx := context.Background()

	// A recovery that accounts for essentially the whole delta.
	resp, err := client.Explain(ctx, &ExplainDivergenceRequest{
		TenantID:        opts().TenantID,
		DivergenceID:    "01HDIVERGENCE00000000000",
		Currency:        "INR",
		DeltaMinorUnits: 4510,
		Features: map[string]float64{
			"recovery_amount_minor_units": 4500,
		},
	}, opts())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if resp.Abstained {
		t.Fatal("the service abstained on a clear recovery attribution")
	}
	if len(resp.Hypotheses) == 0 {
		t.Fatal("no hypotheses decoded")
	}
	top := resp.Hypotheses[0]
	if top.Classification != "RECOVERY_DIFFERENCE" {
		t.Errorf("top classification = %q, want RECOVERY_DIFFERENCE", top.Classification)
	}
	if top.Confidence < 0.9 {
		t.Errorf("confidence = %v, want >= 0.9", top.Confidence)
	}
	if top.Confidence > 0.95 {
		t.Errorf("confidence = %v, must never exceed 0.95", top.Confidence)
	}
	if top.Rationale == "" {
		t.Error("rationale did not decode; the hypothesis would not be checkable")
	}
	if len(top.SupportingFields) == 0 {
		t.Error("supporting_fields did not decode")
	}
}

func TestDivergenceAbstainsOnWeakEvidence(t *testing.T) {
	client := NewDivergenceClient(Config{BaseURL: startService(t, "divergence-service"), Timeout: 5 * time.Second})

	resp, err := client.Explain(context.Background(), &ExplainDivergenceRequest{
		TenantID:        opts().TenantID,
		DivergenceID:    "01HDIVERGENCE00000000001",
		Currency:        "INR",
		DeltaMinorUnits: 250000,
		Features:        map[string]float64{},
	}, opts())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !resp.Abstained {
		t.Error("the service produced a hypothesis from no evidence")
	}
	if len(resp.Hypotheses) != 0 {
		t.Errorf("abstention returned %d hypotheses, want 0", len(resp.Hypotheses))
	}
}

func TestMissingTenantIsRejected(t *testing.T) {
	client := NewAnomalyClient(Config{BaseURL: startService(t, "anomaly-service"), Timeout: 5 * time.Second, MaxAttempts: 1})

	_, err := client.ScoreSeries(context.Background(), &ScoreCollectionSeriesRequest{
		SubjectRef: "cattle:01HCATTLE000000000000000",
		Points:     []SeriesPoint{{ObservationID: "a", Value: 12.0}},
	}, CallOptions{})
	if err == nil {
		t.Fatal("an unattributed call was accepted")
	}
	var mlErr *Error
	if !asMLError(err, &mlErr) {
		t.Fatalf("got %v, want a structured *Error", err)
	}
	if mlErr.Code != "permission_denied" {
		t.Errorf("code = %q, want permission_denied", mlErr.Code)
	}
	if mlErr.Retryable() {
		t.Error("a missing tenant was reported as retryable")
	}
}

func TestModelPinMismatchIsRefused(t *testing.T) {
	client := NewAnomalyClient(Config{
		BaseURL:      startService(t, "anomaly-service"),
		Timeout:      5 * time.Second,
		MaxAttempts:  1,
		ModelVersion: "anomaly-robust-z-99.0.0",
	})

	_, err := client.ScoreSeries(context.Background(), &ScoreCollectionSeriesRequest{
		TenantID:   opts().TenantID,
		SubjectRef: "cattle:01HCATTLE000000000000000",
		Points:     []SeriesPoint{{ObservationID: "a", Value: 12.0}},
	}, opts())
	if err == nil {
		t.Fatal("a pinned replay was served by the wrong model version")
	}
	var mlErr *Error
	if !asMLError(err, &mlErr) {
		t.Fatalf("got %v, want a structured *Error", err)
	}
	if mlErr.Code != "not_found" {
		t.Errorf("code = %q, want not_found", mlErr.Code)
	}
}
