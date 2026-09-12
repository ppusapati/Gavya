//go:build e2e

// The probes, on services that are really running.
//
// libs/integrity/observe proves the handlers behave. It does not prove they are
// mounted: a platform can have a correct readiness handler, a correct metrics
// handler, and nothing serving either, and every test in that package passes.
//
// /healthz returned 200 unconditionally on every service before this, which is
// a liveness probe that cannot fail — the same shape as a control that reports
// success while doing nothing. A pod whose database had gone stayed in the
// Service's endpoints and was sent traffic it could not serve.
package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// get fetches one path from a running service.
func get(t *testing.T, base, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// Every running service answers all three probes.
func TestEveryServiceServesItsProbes(t *testing.T) {
	p := startPlatform(t)

	if len(p.baseURLs) == 0 {
		t.Fatal("the harness recorded no service addresses, so this check would " +
			"have examined nothing and passed")
	}
	for name, base := range p.baseURLs {
		t.Run(name, func(t *testing.T) {
			if code, _ := get(t, base, "/healthz"); code != http.StatusOK {
				t.Errorf("/healthz answered %d", code)
			}

			// Ready, and saying what it checked. A readiness endpoint that
			// answers 200 having checked nothing is the endpoint this replaced.
			code, body := get(t, base, "/readyz")
			if code != http.StatusOK {
				t.Errorf("/readyz answered %d: %s", code, body)
			}
			if strings.Contains(body, "nothing to check") {
				t.Errorf("/readyz passed without checking anything: %q\n"+
					"A probe that cannot fail is not a probe.", body)
			}
			if !strings.Contains(body, "database") && !strings.Contains(body, "dependency") {
				t.Errorf("/readyz does not say what it checked: %q", body)
			}

			code, body = get(t, base, "/metrics")
			if code != http.StatusOK {
				t.Errorf("/metrics answered %d", code)
			}
			if !strings.Contains(body, "gavya_requests_total") {
				t.Errorf("/metrics carries no request counter: %q", body)
			}
			if !strings.Contains(body, "# TYPE") {
				t.Error("/metrics is not in Prometheus exposition format, so nothing " +
					"that scrapes Prometheus can read it")
			}
		})
	}
}

// The probes need no permission, and a metrics scrape is not a way in.
//
// They are reached before anybody signs in — a deployment cannot present a
// session to find out whether a pod is alive — so they must be outside the
// authorisation check. That is a hole if anything sensitive is behind them,
// which is why the metrics carry counts and procedure names and nothing else.
func TestTheProbesAreReachableWithoutASessionAndCarryNothingPrivate(t *testing.T) {
	p := startPlatform(t)
	base := p.baseURLs["milk-service"]

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		if code, _ := get(t, base, path); code == http.StatusForbidden {
			t.Errorf("%s is behind the authorisation check, so nothing can probe this "+
				"service until it has a session — and a deployment has none", path)
		}
	}

	_, body := get(t, base, "/metrics")
	// A tenant id or a producer reference in a metric label would be a
	// disclosure through an endpoint that needs no credential.
	if strings.Contains(body, p.tenant) {
		t.Errorf("the metrics carry a tenant id, on an endpoint that needs no "+
			"credential:\n%s", body)
	}
}

// Metrics record what actually happened, including refusals.
//
// A counter that only counts successes says a service is healthy while it
// refuses everything.
func TestMetricsRecordRefusalsAndNotOnlySuccesses(t *testing.T) {
	p := startPlatform(t)
	base := p.baseURLs["settlement-service"]

	// A refusal: a collector cannot approve a cycle.
	_, _ = post(t, base, "/settlement.v1.SettlementService/ApproveCycle",
		`{"tenant_id":"`+p.tenant+`","cycle_id":"none","actor":"collector"}`,
		map[string]string{
			"X-Gavya-Tenant":      p.tenant,
			"X-Gavya-Permissions": "milk.read",
		})

	_, body := get(t, base, "/metrics")
	if !strings.Contains(body, `code="403"`) {
		t.Errorf("a refused request left no trace in the metrics, so a service "+
			"refusing everything looks like one serving everything:\n%s", body)
	}
}

func post(t *testing.T, base, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}
