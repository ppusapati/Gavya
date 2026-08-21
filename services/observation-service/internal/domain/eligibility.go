package domain

import (
	"fmt"
	"strings"
	"time"
)

// Eligibility is the verdict on whether one observation may settle a payment,
// with the reason an auditor will read years later.
type Eligibility struct {
	Verdict EligibilityVerdict
	Reason  string
	// CertificateID names the certificate the verdict rests on, when there was
	// one to rest on.
	CertificateID string
}

// AssessEligibility decides whether an observation was taken on an instrument
// verified under the Legal Metrology Act at the instant it measured.
//
// It is pure and total: the same certificate, instant and quantity always yield
// the same verdict, so a payment can be re-justified from the stored inputs
// alone. An ineligible verdict never rejects the observation — it records which
// payments rested on an unverified instrument.
func AssessEligibility(cert *VerificationCertificate, observedAt time.Time, quantity QuantityKind) Eligibility {
	if !quantity.Valid() {
		return Eligibility{
			Verdict: EligibilityUnknown,
			Reason:  fmt.Sprintf("quantity kind %q is not recognised, so whether it determines payment cannot be decided", quantity),
		}
	}

	// A quantity that does not enter the price is outside the Act entirely, so
	// no certificate is required and none is looked for.
	if !quantity.IsTradeCritical() {
		return Eligibility{
			Verdict: EligibilityEligible,
			Reason:  fmt.Sprintf("%s does not determine payment and is outside legal metrology", quantity),
		}
	}

	if observedAt.IsZero() {
		return Eligibility{
			Verdict: EligibilityUnknown,
			Reason:  "observation has no valid_from, so it cannot be placed inside or outside a verification period",
		}
	}

	// Absence of a certificate is absence of evidence, not evidence that the
	// instrument was unverified. Calling it NOT_ELIGIBLE would accuse every
	// instrument whose paperwork has not been imported yet.
	if cert == nil {
		return Eligibility{
			Verdict: EligibilityUnknown,
			Reason:  "no verification certificate is on record for the instrument",
		}
	}

	certID := cert.ID
	var missing []string
	if strings.TrimSpace(cert.VerifyingAuthority) == "" {
		missing = append(missing, "verifying authority")
	}
	if strings.TrimSpace(cert.CertificateNumber) == "" {
		missing = append(missing, "certificate number")
	}
	if cert.IssuedAt.IsZero() {
		missing = append(missing, "issue date")
	}
	if cert.ExpiresAt.IsZero() {
		missing = append(missing, "expiry date")
	}
	if len(missing) > 0 {
		return Eligibility{
			Verdict:       EligibilityUnknown,
			Reason:        fmt.Sprintf("certificate on record is missing its %s and cannot be checked", strings.Join(missing, " and ")),
			CertificateID: certID,
		}
	}
	if !cert.ExpiresAt.After(cert.IssuedAt) {
		return Eligibility{
			Verdict: EligibilityUnknown,
			Reason: fmt.Sprintf("certificate %s expires at %s, at or before its issue at %s, so it covers no period",
				cert.CertificateNumber, utc(cert.ExpiresAt), utc(cert.IssuedAt)),
			CertificateID: certID,
		}
	}

	at := observedAt.UTC()
	if at.Before(cert.IssuedAt.UTC()) {
		return Eligibility{
			Verdict: EligibilityNotEligible,
			Reason: fmt.Sprintf("observed at %s, before certificate %s was issued at %s",
				utc(at), cert.CertificateNumber, utc(cert.IssuedAt)),
			CertificateID: certID,
		}
	}
	// The verification period is half-open: an instrument is unverified from the
	// expiry instant onwards, not from the instant after it.
	if !at.Before(cert.ExpiresAt.UTC()) {
		return Eligibility{
			Verdict: EligibilityNotEligible,
			Reason: fmt.Sprintf("observed at %s, at or after certificate %s expired at %s",
				utc(at), cert.CertificateNumber, utc(cert.ExpiresAt)),
			CertificateID: certID,
		}
	}

	return Eligibility{
		Verdict: EligibilityEligible,
		Reason: fmt.Sprintf("certificate %s from %s covers %s until %s",
			cert.CertificateNumber, cert.VerifyingAuthority, utc(cert.IssuedAt), utc(cert.ExpiresAt)),
		CertificateID: certID,
	}
}

func utc(t time.Time) string { return t.UTC().Format(time.RFC3339) }
