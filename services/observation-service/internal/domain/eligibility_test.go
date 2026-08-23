package domain

import (
	"strings"
	"testing"
	"time"
)

var (
	issued  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expires = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
)

func certificate() *VerificationCertificate {
	return &VerificationCertificate{
		ID:                 "cert-1",
		TenantID:           "tnt",
		InstrumentID:       "ins-1",
		CertificateNumber:  "AP/LM/2026/00417",
		VerifyingAuthority: "Controller of Legal Metrology, Andhra Pradesh",
		IssuedAt:           issued,
		ExpiresAt:          expires,
	}
}

func TestAssessEligibility(t *testing.T) {
	noAuthority := certificate()
	noAuthority.VerifyingAuthority = "   "

	noNumber := certificate()
	noNumber.CertificateNumber = ""

	noIssueDate := certificate()
	noIssueDate.IssuedAt = time.Time{}

	noExpiryDate := certificate()
	noExpiryDate.ExpiresAt = time.Time{}

	inverted := certificate()
	inverted.IssuedAt, inverted.ExpiresAt = expires, issued

	instantaneous := certificate()
	instantaneous.ExpiresAt = instantaneous.IssuedAt

	cases := []struct {
		name        string
		cert        *VerificationCertificate
		observedAt  time.Time
		quantity    QuantityKind
		want        EligibilityVerdict
		wantCertID  string
		reasonHolds string
	}{
		{
			name:        "mid-period trade quantity on a valid certificate",
			cert:        certificate(),
			observedAt:  time.Date(2026, 6, 15, 5, 30, 0, 0, time.UTC),
			quantity:    QuantityVolumeLitres,
			want:        EligibilityEligible,
			wantCertID:  "cert-1",
			reasonHolds: "AP/LM/2026/00417",
		},
		{
			name:        "exactly at the issue instant is inside the period",
			cert:        certificate(),
			observedAt:  issued,
			quantity:    QuantityMassKG,
			want:        EligibilityEligible,
			wantCertID:  "cert-1",
			reasonHolds: "covers",
		},
		{
			name:        "one nanosecond before issue is outside the period",
			cert:        certificate(),
			observedAt:  issued.Add(-time.Nanosecond),
			quantity:    QuantityMassKG,
			want:        EligibilityNotEligible,
			wantCertID:  "cert-1",
			reasonHolds: "before certificate",
		},
		{
			name:        "exactly at the expiry instant is outside the period",
			cert:        certificate(),
			observedAt:  expires,
			quantity:    QuantityFatPercent,
			want:        EligibilityNotEligible,
			wantCertID:  "cert-1",
			reasonHolds: "expired",
		},
		{
			name:        "one nanosecond before expiry is still inside the period",
			cert:        certificate(),
			observedAt:  expires.Add(-time.Nanosecond),
			quantity:    QuantityFatPercent,
			want:        EligibilityEligible,
			wantCertID:  "cert-1",
			reasonHolds: "covers",
		},
		{
			name:        "long after expiry",
			cert:        certificate(),
			observedAt:  time.Date(2029, 3, 1, 0, 0, 0, 0, time.UTC),
			quantity:    QuantitySNFPercent,
			want:        EligibilityNotEligible,
			wantCertID:  "cert-1",
			reasonHolds: "expired",
		},
		{
			name:        "no certificate at all is unknown, not ineligible",
			cert:        nil,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityVolumeLitres,
			want:        EligibilityUnknown,
			reasonHolds: "no verification certificate",
		},
		{
			name:        "certificate without a verifying authority",
			cert:        noAuthority,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityVolumeLitres,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "verifying authority",
		},
		{
			name:        "certificate without a number",
			cert:        noNumber,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityMassKG,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "certificate number",
		},
		{
			name:        "certificate without an issue date",
			cert:        noIssueDate,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityMassKG,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "issue date",
		},
		{
			name:        "certificate without an expiry date",
			cert:        noExpiryDate,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityMassKG,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "expiry date",
		},
		{
			name:        "certificate whose expiry precedes its issue covers nothing",
			cert:        inverted,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityProteinPercent,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "covers no period",
		},
		{
			name:        "certificate expiring at its own issue instant covers nothing",
			cert:        instantaneous,
			observedAt:  issued,
			quantity:    QuantityProteinPercent,
			want:        EligibilityUnknown,
			wantCertID:  "cert-1",
			reasonHolds: "covers no period",
		},
		{
			name:        "an observation with no instant cannot be placed",
			cert:        certificate(),
			observedAt:  time.Time{},
			quantity:    QuantityLactosePercent,
			want:        EligibilityUnknown,
			reasonHolds: "valid_from",
		},
		{
			name:        "temperature is outside legal metrology even with no certificate",
			cert:        nil,
			observedAt:  time.Date(2029, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityTemperatureC,
			want:        EligibilityEligible,
			reasonHolds: "does not determine payment",
		},
		{
			name:        "somatic cell count is outside legal metrology even on an expired certificate",
			cert:        certificate(),
			observedAt:  time.Date(2029, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantitySomaticCellCount,
			want:        EligibilityEligible,
			reasonHolds: "does not determine payment",
		},
		{
			name:        "adulteration index is outside legal metrology",
			cert:        nil,
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityAdulterationIndex,
			want:        EligibilityEligible,
			reasonHolds: "does not determine payment",
		},
		{
			name:        "an unrecognised quantity cannot be classified either way",
			cert:        certificate(),
			observedAt:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			quantity:    QuantityKind("BUTTERFAT_POINTS"),
			want:        EligibilityUnknown,
			reasonHolds: "not recognised",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AssessEligibility(RegimeIndiaLegalMetrology, c.cert, c.observedAt, c.quantity)
			if got.Verdict != c.want {
				t.Errorf("verdict = %s, want %s (reason: %s)", got.Verdict, c.want, got.Reason)
			}
			if got.Reason == "" {
				t.Error("verdict carries no reason; an auditor would have nothing to read")
			}
			if !strings.Contains(got.Reason, c.reasonHolds) {
				t.Errorf("reason %q does not mention %q", got.Reason, c.reasonHolds)
			}
			if got.CertificateID != c.wantCertID {
				t.Errorf("certificate_id = %q, want %q", got.CertificateID, c.wantCertID)
			}
		})
	}
}

// The instant is compared in UTC, so the same moment expressed in IST must
// reach the same verdict as it does in UTC.
func TestAssessEligibilityIsZoneIndependent(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	// 2027-01-01T05:29:59+05:30 is 2026-12-31T23:59:59Z, still inside.
	inside := time.Date(2027, 1, 1, 5, 29, 59, 0, ist)
	if got := AssessEligibility(RegimeIndiaLegalMetrology, certificate(), inside, QuantityVolumeLitres); got.Verdict != EligibilityEligible {
		t.Errorf("%s: verdict = %s, want ELIGIBLE (%s)", inside, got.Verdict, got.Reason)
	}
	// 2027-01-01T05:30:00+05:30 is exactly the expiry instant.
	atExpiry := time.Date(2027, 1, 1, 5, 30, 0, 0, ist)
	if got := AssessEligibility(RegimeIndiaLegalMetrology, certificate(), atExpiry, QuantityVolumeLitres); got.Verdict != EligibilityNotEligible {
		t.Errorf("%s: verdict = %s, want NOT_ELIGIBLE (%s)", atExpiry, got.Verdict, got.Reason)
	}
}

func TestAssessEligibilityIsDeterministic(t *testing.T) {
	cert := certificate()
	at := time.Date(2026, 6, 15, 5, 30, 0, 0, time.UTC)
	first := AssessEligibility(RegimeIndiaLegalMetrology, cert, at, QuantityFatPercent)
	for i := 0; i < 100; i++ {
		again := AssessEligibility(RegimeIndiaLegalMetrology, cert, at, QuantityFatPercent)
		if again != first {
			t.Fatalf("call %d returned %+v, want the identical %+v", i, again, first)
		}
	}
}

// No input may produce a verdict outside the closed vocabulary, and none may
// produce an empty reason.
func TestEveryVerdictIsInTheVocabularyAndExplained(t *testing.T) {
	quantities := []QuantityKind{
		QuantityVolumeLitres, QuantityMassKG, QuantityFatPercent, QuantitySNFPercent,
		QuantityLactosePercent, QuantityProteinPercent, QuantityTemperatureC,
		QuantitySomaticCellCount, QuantityAdulterationIndex, QuantityKind(""), QuantityKind("NONSENSE"),
	}
	instants := []time.Time{
		{}, issued.Add(-time.Hour), issued, issued.Add(time.Hour), expires.Add(-time.Hour), expires, expires.Add(time.Hour),
	}
	blank := &VerificationCertificate{ID: "cert-blank"}
	certs := []*VerificationCertificate{nil, certificate(), blank}

	for _, q := range quantities {
		for _, at := range instants {
			for _, cert := range certs {
				got := AssessEligibility(RegimeIndiaLegalMetrology, cert, at, q)
				switch got.Verdict {
				case EligibilityEligible, EligibilityNotEligible, EligibilityUnknown:
				default:
					t.Fatalf("quantity %q at %s: verdict %q is outside the closed vocabulary", q, at, got.Verdict)
				}
				if got.Reason == "" {
					t.Fatalf("quantity %q at %s: verdict %s carries no reason", q, at, got.Verdict)
				}
			}
		}
	}
}

func TestTradeCriticalQuantities(t *testing.T) {
	critical := []QuantityKind{
		QuantityVolumeLitres, QuantityMassKG, QuantityFatPercent,
		QuantitySNFPercent, QuantityLactosePercent, QuantityProteinPercent,
	}
	for _, q := range critical {
		if !q.IsTradeCritical() {
			t.Errorf("%s determines payment and must be trade critical", q)
		}
	}
	for _, q := range []QuantityKind{QuantityTemperatureC, QuantitySomaticCellCount, QuantityAdulterationIndex} {
		if q.IsTradeCritical() {
			t.Errorf("%s does not determine payment and must not be trade critical", q)
		}
	}
	if QuantityKind("NONSENSE").IsTradeCritical() {
		t.Error("an unrecognised quantity must not be treated as trade critical")
	}
}

func TestQuantityUnitsAreFixed(t *testing.T) {
	want := map[QuantityKind]string{
		QuantityVolumeLitres:      "L",
		QuantityMassKG:            "kg",
		QuantityFatPercent:        "%",
		QuantitySNFPercent:        "%",
		QuantityLactosePercent:    "%",
		QuantityProteinPercent:    "%",
		QuantityTemperatureC:      "degC",
		QuantitySomaticCellCount:  "cells/mL",
		QuantityAdulterationIndex: "index",
	}
	for q, unit := range want {
		if got := q.Unit(); got != unit {
			t.Errorf("%s unit = %q, want %q", q, got, unit)
		}
	}
	if got := QuantityKind("NONSENSE").Unit(); got != "" {
		t.Errorf("unrecognised quantity unit = %q, want empty", got)
	}
}

func TestSubjectRefValidation(t *testing.T) {
	valid := []SubjectRef{
		{Kind: SubjectCattle, ID: "cow-1"},
		{Kind: SubjectProducer, ID: "prd-1"},
		{Kind: SubjectRoute, ID: "rte-1"},
		{Kind: SubjectTanker, ID: "tnk-1"},
		{Kind: SubjectBatch, ID: "bat-1"},
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%+v should be valid", s)
		}
	}
	invalid := []SubjectRef{
		{Kind: SubjectCattle},
		{ID: "cow-1"},
		{Kind: SubjectKind("VILLAGE"), ID: "vil-1"},
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("%+v should not be valid", s)
		}
	}
	if got := (SubjectRef{Kind: SubjectCattle, ID: "01HZ"}).Ref(); got != "cattle:01HZ" {
		t.Errorf("Ref() = %q, want cattle:01HZ", got)
	}
}
