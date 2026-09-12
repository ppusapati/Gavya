//go:build e2e

// The instrument behind a reading, and the paperwork that makes it count.
//
// observation-service's remaining four routes are the legal-metrology half of
// it: which instrument took a reading, what certificate covered it at the
// moment it was taken, and which readings the anomaly tier wants a person to
// look at. None had been called end to end.
//
// The certificate route is the one with real reasoning in it, and it is not the
// reasoning somebody would guess. GetActiveCertificate does not return the
// certificate covering the instant asked about, or nothing — it returns that
// one if there is one, and otherwise the most recently issued certificate, so
// the verdict can say "observed after certificate X expired" rather than "no
// certificate on record". Those are different claims about an instrument and
// they lead somewhere different: the first is an expired verification, the
// second is missing paperwork. Getting the ordering wrong turns one into the
// other silently.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type registerObsInstrumentReq struct {
	TenantID string `json:"tenant_id"`
	Serial   string `json:"serial"`
	Kind     string `json:"kind"`
	Label    string `json:"label,omitempty"`
	Make     string `json:"make,omitempty"`
	Model    string `json:"model,omitempty"`
	Actor    string `json:"actor"`
}

type obsInstrumentProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Serial   string `json:"serial"`
	Kind     string `json:"kind"`
	Label    string `json:"label,omitempty"`
	Make     string `json:"make,omitempty"`
	Model    string `json:"model,omitempty"`
}

type registerObsInstrumentResp struct {
	Instrument *obsInstrumentProto `json:"instrument"`
}

type getObsInstrumentReq struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type getObsInstrumentResp struct {
	Instrument *obsInstrumentProto `json:"instrument"`
}

type recordCertificateReq struct {
	TenantID           string      `json:"tenant_id"`
	InstrumentID       string      `json:"instrument_id"`
	CertificateNumber  string      `json:"certificate_number"`
	VerifyingAuthority string      `json:"verifying_authority"`
	IssuedAt           string      `json:"issued_at"`
	ExpiresAt          string      `json:"expires_at"`
	Origin             originProto `json:"origin"`
	CreatedBy          string      `json:"created_by"`
}

type certificateProto struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenant_id"`
	InstrumentID       string `json:"instrument_id"`
	CertificateNumber  string `json:"certificate_number"`
	VerifyingAuthority string `json:"verifying_authority"`
	IssuedAt           string `json:"issued_at"`
	ExpiresAt          string `json:"expires_at"`
}

type recordCertificateResp struct {
	Certificate *certificateProto `json:"certificate"`
}

type getActiveCertificateReq struct {
	TenantID     string `json:"tenant_id"`
	InstrumentID string `json:"instrument_id"`
	At           string `json:"at,omitempty"`
	Quantity     string `json:"quantity_kind,omitempty"`
}

type eligibilityProto struct {
	Verdict       string `json:"verdict"`
	Reason        string `json:"reason"`
	CertificateID string `json:"certificate_id,omitempty"`
}

type getActiveCertificateResp struct {
	Certificate *certificateProto `json:"certificate"`
	Eligibility *eligibilityProto `json:"eligibility,omitempty"`
}

type listFlaggedReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type flaggedObservationProto struct {
	ID       string  `json:"id"`
	TenantID string  `json:"tenant_id"`
	Value    float64 `json:"value"`
	Quantity string  `json:"quantity_kind"`
}

type listFlaggedResp struct {
	Observations []*flaggedObservationProto `json:"observations"`
}

// An instrument reads back as it was registered, to the tenant that registered
// it.
//
// A serial number is how a physical meter on a dock is tied to the readings
// attributed to it. Two co-operatives buying the same model from the same
// supplier have the same serials, so an instrument lookup that trusted the id
// without the tenant would hand one society's calibration history for another's
// meter.
func TestAnInstrumentReadsBackToTheTenantThatRegisteredIt(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	serial := newID("srl")
	made, err := svcclient.Call[registerObsInstrumentReq, registerObsInstrumentResp](ctx, p.observation(),
		observationSvc+"/RegisterInstrument", registerObsInstrumentReq{
			TenantID: p.tenant, Serial: serial, Kind: "MILK_ANALYSER",
			Label: "dock 2 analyser", Make: "Lactoscan", Model: "SP",
			Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}

	got, err := svcclient.Call[getObsInstrumentReq, getObsInstrumentResp](ctx, p.observation(),
		observationSvc+"/GetInstrument", getObsInstrumentReq{
			ID: made.Instrument.ID, TenantID: p.tenant,
		}, p.opts())
	if err != nil {
		t.Fatalf("GetInstrument: %v", err)
	}
	i := got.Instrument
	if i == nil {
		t.Fatal("GetInstrument answered with no instrument at all")
	}
	for _, c := range []struct{ field, got, want string }{
		{"id", i.ID, made.Instrument.ID},
		{"tenant_id", i.TenantID, p.tenant},
		{"serial", i.Serial, serial},
		{"kind", i.Kind, "MILK_ANALYSER"},
		{"label", i.Label, "dock 2 analyser"},
		{"make", i.Make, "Lactoscan"},
		{"model", i.Model, "SP"},
	} {
		if c.got != c.want {
			t.Errorf("instrument %s is %q, want %q", c.field, c.got, c.want)
		}
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[getObsInstrumentReq, getObsInstrumentResp](ctx, p.observation(),
		observationSvc+"/GetInstrument", getObsInstrumentReq{
			ID: made.Instrument.ID, TenantID: stranger,
		}, actingAs(stranger, "e2e")); err == nil {
		t.Error("another tenant read this instrument\n" +
			"Two societies buying the same analyser from the same supplier hold " +
			"the same serial numbers; the id alone must not be the key.")
	}
}

// The certificate on record at an instant is the one that covered it, and when
// none did, the most recent one — with a verdict saying which.
//
// Two certificates with a gap between them, which is the ordinary case: a
// verification lapses and is renewed some weeks later. Four moments are asked
// about, and each must name the right certificate and reach the right verdict:
//
//	inside the first      -> the first,  ELIGIBLE
//	in the gap            -> the second, NOT_ELIGIBLE (before it was issued)
//	inside the second     -> the second, ELIGIBLE
//	after both            -> the second, NOT_ELIGIBLE (after it expired)
//
// The two NOT_ELIGIBLE cases are where the ordering earns its keep. Both could
// be answered "no certificate on record", which is UNKNOWN — absence of
// evidence. What the platform says instead is that there is a certificate and
// the reading falls outside it, which is evidence of absence, and only one of
// those is a finding somebody can act on.
func TestTheCertificateOnRecordAtAnInstantIsTheOneThatCoveredIt(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	made, err := svcclient.Call[registerObsInstrumentReq, registerObsInstrumentResp](ctx, p.observation(),
		observationSvc+"/RegisterInstrument", registerObsInstrumentReq{
			TenantID: p.tenant, Serial: newID("srl"), Kind: "WEIGHBRIDGE", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}
	instrument := made.Instrument.ID

	record := func(number, issued, expires string) *certificateProto {
		t.Helper()
		out, err := svcclient.Call[recordCertificateReq, recordCertificateResp](ctx, p.observation(),
			observationSvc+"/RecordCertificate", recordCertificateReq{
				TenantID: p.tenant, InstrumentID: instrument,
				CertificateNumber: number, VerifyingAuthority: "Controller of Legal Metrology",
				IssuedAt: issued, ExpiresAt: expires,
				Origin: originProto{Kind: "NATIVE"}, CreatedBy: "e2e",
			}, p.opts())
		if err != nil {
			t.Fatalf("RecordCertificate %s: %v", number, err)
		}
		return out.Certificate
	}

	// Recorded newest first, so a query that simply returns the last row
	// inserted is not accidentally right.
	second := record("LM-2025-B", "2025-06-01T00:00:00Z", "2026-01-01T00:00:00Z")
	first := record("LM-2024-A", "2024-01-01T00:00:00Z", "2025-01-01T00:00:00Z")

	if first.CertificateNumber != "LM-2024-A" || first.InstrumentID != instrument {
		t.Fatalf("the recorded certificate came back as %+v", first)
	}

	for _, c := range []struct {
		what    string
		at      string
		wantID  string
		verdict string
	}{
		{"inside the first certificate", "2024-06-01T00:00:00Z", first.ID, "ELIGIBLE"},
		{"in the gap between them", "2025-03-01T00:00:00Z", second.ID, "NOT_ELIGIBLE"},
		{"inside the second certificate", "2025-08-01T00:00:00Z", second.ID, "ELIGIBLE"},
		{"after both expired", "2026-06-01T00:00:00Z", second.ID, "NOT_ELIGIBLE"},
	} {
		got, err := svcclient.Call[getActiveCertificateReq, getActiveCertificateResp](ctx, p.observation(),
			observationSvc+"/GetActiveCertificate", getActiveCertificateReq{
				TenantID: p.tenant, InstrumentID: instrument, At: c.at,
				Quantity: "VOLUME_LITRES",
			}, p.opts())
		if err != nil {
			t.Fatalf("GetActiveCertificate %s: %v", c.what, err)
		}
		if got.Certificate == nil {
			t.Fatalf("%s: no certificate came back, and two are on record for this instrument", c.what)
		}
		if got.Certificate.ID != c.wantID {
			t.Errorf("%s: the certificate on record is %s, want %s",
				c.what, got.Certificate.CertificateNumber, c.wantID)
		}
		if got.Eligibility == nil {
			t.Fatalf("%s: a quantity was named and no verdict came back", c.what)
		}
		if got.Eligibility.Verdict != c.verdict {
			t.Errorf("%s: the verdict is %s (%s), want %s",
				c.what, got.Eligibility.Verdict, got.Eligibility.Reason, c.verdict)
		}
		if got.Eligibility.CertificateID != c.wantID {
			t.Errorf("%s: the verdict rests on certificate %s and the response carries %s\n"+
				"A verdict citing a different certificate from the one returned is "+
				"unauditable: the reader cannot tell which document was checked.",
				c.what, got.Eligibility.CertificateID, c.wantID)
		}
	}

	// Asking without naming a quantity returns the certificate and no verdict.
	// The verdict depends on the quantity — a quantity outside legal metrology
	// is eligible whatever the paperwork says — so inventing one here would be
	// answering a question nobody asked.
	bare, err := svcclient.Call[getActiveCertificateReq, getActiveCertificateResp](ctx, p.observation(),
		observationSvc+"/GetActiveCertificate", getActiveCertificateReq{
			TenantID: p.tenant, InstrumentID: instrument, At: "2024-06-01T00:00:00Z",
		}, p.opts())
	if err != nil {
		t.Fatalf("GetActiveCertificate with no quantity: %v", err)
	}
	if bare.Eligibility != nil {
		t.Errorf("a verdict (%s) came back for a request that named no quantity; "+
			"the verdict depends on the quantity and cannot be reached without one",
			bare.Eligibility.Verdict)
	}

	// An instrument with no certificate at all is the other case, and it must
	// not be answered with somebody else's paperwork.
	naked, err := svcclient.Call[registerObsInstrumentReq, registerObsInstrumentResp](ctx, p.observation(),
		observationSvc+"/RegisterInstrument", registerObsInstrumentReq{
			TenantID: p.tenant, Serial: newID("srl"), Kind: "WEIGHBRIDGE", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RegisterInstrument: %v", err)
	}
	if _, err := svcclient.Call[getActiveCertificateReq, getActiveCertificateResp](ctx, p.observation(),
		observationSvc+"/GetActiveCertificate", getActiveCertificateReq{
			TenantID: p.tenant, InstrumentID: naked.Instrument.ID,
			At: "2024-06-01T00:00:00Z", Quantity: "VOLUME_LITRES",
		}, p.opts()); err == nil {
		t.Error("an instrument with no certificate on record was answered with one")
	}
}

// A reading the tier flagged is in the queue a person works through.
//
// This needs the ML tier, because nothing else sets the flag: the score and the
// flag come from the anomaly service, and the main platform deliberately runs
// without it. So a steady series is recorded for one subject and then one gross
// outlier, and the outlier — and only it — must appear.
//
// The check that the steady readings stay out is the one that matters. A query
// that lost its `anomaly_flagged` predicate returns every observation the tenant
// has, which looks like a busy review queue rather than a broken one.
func TestAFlaggedReadingIsInTheReviewQueueAndASteadyOneIsNot(t *testing.T) {
	p := startMLPlatform(t)
	ctx := context.Background()

	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	base := time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC)

	// A steady baseline. The values differ slightly because a series with no
	// dispersion at all is scored by a different branch of the detector.
	var steady []string
	for i, v := range []float64{4.0, 4.1, 3.9, 4.05, 4.0, 3.95, 4.1, 4.0} {
		got := recordWithML(t, p, mlObservationReq{
			Subject: subject, Quantity: "FAT_PERCENT", Value: v, Unit: "PERCENT",
			ValidFrom: base.Add(time.Duration(i) * 24 * time.Hour).Format(time.RFC3339),
		})
		steady = append(steady, got.ID)
	}

	// A reading no analyser produces from milk.
	spike := recordWithML(t, p, mlObservationReq{
		Subject: subject, Quantity: "FAT_PERCENT", Value: 95.0, Unit: "PERCENT",
		ValidFrom: base.Add(9 * 24 * time.Hour).Format(time.RFC3339),
	})

	queue, err := svcclient.Call[listFlaggedReq, listFlaggedResp](ctx, p.observation(),
		observationSvc+"/ListFlaggedObservations", listFlaggedReq{
			TenantID: p.tenant, Limit: 200,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListFlaggedObservations: %v", err)
	}

	flagged := map[string]bool{}
	for _, o := range queue.Observations {
		flagged[o.ID] = true
	}
	if !flagged[spike.ID] {
		t.Errorf("a reading of 95 percent fat against a baseline near 4 is not in "+
			"the review queue; %d observations are", len(queue.Observations))
	}
	for _, id := range steady {
		if flagged[id] {
			t.Errorf("a reading inside its own baseline is in the review queue\n" +
				"The queue is what somebody works through by hand. Filling it " +
				"with ordinary readings is how the real outlier stops being " +
				"looked at.")
			break
		}
	}
}
