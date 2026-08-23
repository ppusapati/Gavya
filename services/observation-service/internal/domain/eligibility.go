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
// verified, under the deployment's own measurement-control regime, at the
// instant it measured.
//
// It is pure and total: the same regime, certificate, instant and quantity
// always yield the same verdict, so a payment can be re-justified from the
// stored inputs alone. An ineligible verdict never rejects the observation — it
// records which payments rested on an unverified instrument.
//
// The regime is a parameter rather than a constant because the verdict cites a
// law, and telling a Kenyan co-operative its milk meter was unverified under
// the Indian Legal Metrology Act would be citing a statute that does not reach
// them.
func AssessEligibility(regime Regime, cert *VerificationCertificate, observedAt time.Time, quantity QuantityKind) Eligibility {
	if !quantity.Valid() {
		return Eligibility{
			Verdict: EligibilityUnknown,
			Reason:  fmt.Sprintf("quantity kind %q is not recognised, so whether it determines payment cannot be decided", quantity),
		}
	}

	// A quantity this regime does not regulate needs no certificate, so none is
	// looked for. Under RegimeNone that is every quantity, and the reason says
	// so plainly rather than leaving a reader to wonder whether the check ran.
	if !regime.Regulates(quantity) {
		if regime.ID == RegimeNone.ID {
			return Eligibility{
				Verdict: EligibilityEligible,
				Reason:  "this deployment operates under " + regime.Name + ", so no verification is required",
			}
		}
		return Eligibility{
			Verdict: EligibilityEligible,
			Reason: fmt.Sprintf("%s does not determine payment and is outside %s",
				quantity, regime.Name),
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
