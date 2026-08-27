// Package domain holds what a laboratory is about: a sample, the hands it
// passed through, and what it read.
//
// The claim the whole package exists to make is one sentence: a result is fit
// to price milk when its sample was sealed, its custody is unbroken, and the
// instrument that read it was in calibration on the day. Any of those missing
// and the result is still recorded — a society has to be able to write down what
// happened — but it is recorded as not fit, with the reason attached, so nobody
// finds out afterwards.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// SourceKind is what a sample was drawn from.
//
// Polymorphic on purpose. A sample comes off a producer's can, a tanker at the
// dock, a silo, a batch of paneer; those live in different services and no
// foreign key names four tables. The kind is recorded so the reference can be
// followed by whoever owns it.
type SourceKind string

const (
	FromCollection SourceKind = "COLLECTION"
	FromMovement   SourceKind = "MOVEMENT"
	FromNode       SourceKind = "NODE"
	FromBatch      SourceKind = "BATCH"
)

func ValidSourceKind(k SourceKind) bool {
	switch k {
	case FromCollection, FromMovement, FromNode, FromBatch:
		return true
	}
	return false
}

// Purpose is why the sample was taken.
//
// A routine payment sample, a duplicate drawn to cross-check one, and a sample
// taken because a member disputed a figure carry different weight in an
// argument, and look identical afterwards unless somebody wrote it down.
type Purpose string

const (
	ForPayment     Purpose = "PAYMENT"
	AsDuplicate    Purpose = "DUPLICATE"
	ForDispute     Purpose = "DISPUTE"
	AsProcessCheck Purpose = "PROCESS_CHECK"
	ForRegulator   Purpose = "REGULATORY"
)

func ValidPurpose(p Purpose) bool {
	switch p {
	case ForPayment, AsDuplicate, ForDispute, AsProcessCheck, ForRegulator:
		return true
	}
	return false
}

// PricesMilk says whether a result from this sample would be used to pay
// somebody.
//
// A process check does not need a seal or a chain of custody; a society running
// one is looking at its own plant. A payment sample does, and so does a dispute
// sample — which is a payment sample taken when the first one is being argued
// about.
func (p Purpose) PricesMilk() bool {
	return p == ForPayment || p == AsDuplicate || p == ForDispute
}

// Sample is what was drawn.
type Sample struct {
	ID       string
	TenantID string

	// Code is what is written on the bottle. A person at a bench reads this.
	Code string

	SourceKind SourceKind
	SourceRef  string

	DrawnAt time.Time
	DrawnBy string

	// SealNumber is empty on a sample nobody sealed.
	SealNumber       string
	SealBrokenAt     *time.Time
	SealBrokenBy     string
	SealBrokenReason string

	Purpose            Purpose
	DuplicatesSampleID string

	CreatedAt time.Time
	CreatedBy string
}

func (s *Sample) Sealed() bool { return s.SealNumber != "" }

// Handover is one link in the chain of custody.
//
// The event somebody witnessed, rather than an inference from two of them. A
// holding period is what you compute from a pair of handovers, and computing it
// first throws away the thing that was actually observed.
type Handover struct {
	ID       string
	TenantID string
	SampleID string

	Sequence int32
	At       time.Time
	From     string
	To       string
	Note     string
}

// Analyte is what was measured.
type Analyte string

const (
	Fat         Analyte = "FAT"
	SNF         Analyte = "SNF"
	Protein     Analyte = "PROTEIN"
	Lactose     Analyte = "LACTOSE"
	AddedWater  Analyte = "ADDED_WATER"
	Acidity     Analyte = "ACIDITY"
	MBRT        Analyte = "MBRT"
	Temperature Analyte = "TEMPERATURE"
)

func ValidAnalyte(a Analyte) bool {
	switch a {
	case Fat, SNF, Protein, Lactose, AddedWater, Acidity, MBRT, Temperature:
		return true
	}
	return false
}

// PricesMilk says whether this analyte is one a rate card reads.
//
// Fat and SNF price milk in every society this platform serves; protein does in
// some. The rest are quality and safety measures that do not enter a payment, so
// a temperature reading from an uncertified thermometer is a note rather than a
// problem.
func (a Analyte) PricesMilk() bool { return a == Fat || a == SNF || a == Protein }

// Eligibility is whether a result is fit to price milk.
//
// The same three words observation-service uses, deliberately. A platform with
// two vocabularies for one idea is one where somebody eventually maps the wrong
// one onto the other.
type Eligibility string

const (
	Eligible    Eligibility = "ELIGIBLE"
	NotEligible Eligibility = "NOT_ELIGIBLE"
	// Unknown is for a question the platform cannot answer: an instrument whose
	// calibration nobody recorded either way. Distinct from NOT_ELIGIBLE, which
	// is a finding.
	Unknown Eligibility = "UNKNOWN"
)

// Reading is a measured value at a stated resolution.
//
// A scaled integer, like every other measured figure here. A society's chart
// may be written to one decimal place and another's to two, and rewriting one
// into the other's resolution changes which cell a reading falls into.
type Reading struct {
	Value int64
	Scale int32
}

func (r Reading) String() string {
	if r.Scale <= 0 {
		return fmt.Sprintf("%d", r.Value)
	}
	div := int64(1)
	for i := int32(0); i < r.Scale; i++ {
		div *= 10
	}
	whole, frac := r.Value/div, r.Value%div
	sign := ""
	if r.Value < 0 {
		sign = "-"
		if whole < 0 {
			whole = -whole
		}
		if frac < 0 {
			frac = -frac
		}
	}
	return fmt.Sprintf("%s%d.%0*d", sign, whole, r.Scale, frac)
}

// Instrument is what read the sample, as its certificate stood on the day.
type Instrument struct {
	Ref string
	// ValidUntil is when the calibration stops being current. Nil means nobody
	// recorded one, which is not the same as one that never expires.
	ValidUntil     *time.Time
	CertificateRef string
}

// Result is one analyte, read once, on one instrument.
type Result struct {
	ID       string
	TenantID string
	SampleID string

	Analyte Analyte
	Reading Reading

	Method     string
	Instrument Instrument

	AnalysedAt time.Time
	AnalysedBy string

	Eligibility       Eligibility
	EligibilityReason string

	SupersededAt     *time.Time
	SupersededBy     string
	Supersedes       string
	CorrectionReason string

	CreatedAt time.Time
	CreatedBy string
}

var (
	ErrNoCode       = errors.New("a sample must carry the code written on the bottle; a person at a bench cannot look up an identifier they have never seen")
	ErrNoSource     = errors.New("a sample must say what it was drawn from")
	ErrNoPurpose    = errors.New("a sample must say why it was taken: PAYMENT, DUPLICATE, DISPUTE, PROCESS_CHECK or REGULATORY")
	ErrNoAnalyte    = errors.New("a result must say what was measured")
	ErrNoMethod     = errors.New("a result must say how it was measured")
	ErrNoInstrument = errors.New("a result must say what it was read on")
	ErrBeforeDrawn  = errors.New("a sample cannot be read before it exists")
	ErrNoReason     = errors.New("a correction must say why the figure changed")
)

func (s *Sample) Validate() error {
	switch {
	case s.TenantID == "":
		return errors.New("tenant_id is required")
	case s.Code == "":
		return ErrNoCode
	case !ValidSourceKind(s.SourceKind) || s.SourceRef == "":
		return ErrNoSource
	case s.DrawnAt.IsZero():
		return errors.New("a sample must say when it was drawn")
	case s.DrawnBy == "":
		return errors.New("a sample must say who drew it; the chain of custody starts with them")
	case !ValidPurpose(s.Purpose):
		return ErrNoPurpose
	case s.Purpose == AsDuplicate && s.DuplicatesSampleID == "":
		return errors.New("a duplicate must name the sample it duplicates, or it is not one")
	case s.Purpose != AsDuplicate && s.DuplicatesSampleID != "":
		return errors.New("only a duplicate names another sample")
	case s.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

// CustodyFinding is what the chain of custody says about a sample at a moment.
type CustodyFinding struct {
	// Intact is whether every link joins to the next and the chain starts with
	// whoever drew the sample.
	Intact bool
	// Holder is who had it at the moment asked about, when the chain is intact.
	Holder string
	// Reason says what is wrong, when something is.
	Reason string
}

// Custody walks the chain and says who held the sample at a moment.
//
// Intact means three things together, and each of them is a way a real chain
// breaks:
//
//   - It starts with whoever drew the sample. A chain whose first handover is
//     from somebody else has a gap at the beginning, which is where a sample
//     gets swapped.
//   - Each handover's receiver is the next one's giver. A gap in the middle is
//     a period nobody has accounted for.
//   - The handovers do not go backwards in time. A chain that does is one whose
//     links were written from memory afterwards.
//
// A sample with no handovers at all is intact and held by whoever drew it. That
// is the ordinary case at a village society, where the same person draws the
// sample and tests it twenty minutes later.
func Custody(s *Sample, chain []Handover, at time.Time) CustodyFinding {
	links := append([]Handover(nil), chain...)
	sort.Slice(links, func(i, j int) bool { return links[i].Sequence < links[j].Sequence })

	holder := s.DrawnBy
	var last time.Time
	for i, h := range links {
		if h.Sequence != int32(i+1) {
			return CustodyFinding{Reason: fmt.Sprintf(
				"the chain jumps from link %d to link %d, so a handover is missing", i, h.Sequence)}
		}
		if h.From != holder {
			return CustodyFinding{Reason: fmt.Sprintf(
				"link %d has %s handing the sample over and %s was holding it, so nobody has "+
					"accounted for how it got from one to the other", h.Sequence, h.From, holder)}
		}
		if !last.IsZero() && h.At.Before(last) {
			return CustodyFinding{Reason: fmt.Sprintf(
				"link %d is timed before the one before it, so the chain was written from memory "+
					"rather than as it happened", h.Sequence)}
		}
		if h.At.Before(s.DrawnAt) {
			return CustodyFinding{Reason: fmt.Sprintf(
				"link %d is timed before the sample was drawn", h.Sequence)}
		}
		last = h.At
		if !h.At.After(at) {
			holder = h.To
		}
	}
	return CustodyFinding{Intact: true, Holder: holder}
}

// Judge decides whether a result is fit to price milk, and says why when it is
// not.
//
// The order of the checks is the order somebody would ask them in, and the first
// failure is the one reported: telling a clerk that the instrument is out of
// calibration is not useful when the sample was never sealed in the first place.
//
// A result that fails is still recorded. A society has to be able to write down
// what happened, and a platform that refused would simply be kept alongside a
// paper book — which is the failure this whole thing exists to end.
func Judge(s *Sample, chain []Handover, r *Result) (Eligibility, string) {
	if r.AnalysedAt.Before(s.DrawnAt) {
		return NotEligible, fmt.Sprintf(
			"the analysis is timed %s and the sample was drawn at %s",
			r.AnalysedAt.UTC().Format(time.RFC3339), s.DrawnAt.UTC().Format(time.RFC3339))
	}

	// An analyte nobody prices milk by does not need the apparatus. A
	// temperature read on an uncertified thermometer is a note, and refusing it
	// would make the platform tiresome about a reading that decides nothing.
	if !r.Analyte.PricesMilk() || !s.Purpose.PricesMilk() {
		return Eligible, ""
	}

	if !s.Sealed() {
		return NotEligible, fmt.Sprintf(
			"sample %s was taken for %s and never sealed, so nothing rules out its having been "+
				"changed between the can and the bench", s.Code, s.Purpose)
	}
	if s.SealBrokenAt != nil && s.SealBrokenAt.Before(r.AnalysedAt) {
		// A seal broken at the bench, by the analyst, immediately before the
		// analysis, is the seal doing its job. One broken hours earlier, or by
		// somebody else, is not — but the platform cannot tell those apart from
		// the timestamps alone, so it reports the fact and leaves the judgement
		// where it belongs.
		gap := r.AnalysedAt.Sub(*s.SealBrokenAt)
		if gap > time.Hour {
			return NotEligible, fmt.Sprintf(
				"the seal on sample %s was broken %s before the analysis, by %s: %s",
				s.Code, gap.Round(time.Minute), s.SealBrokenBy, s.SealBrokenReason)
		}
	}

	if finding := Custody(s, chain, r.AnalysedAt); !finding.Intact {
		return NotEligible, "the chain of custody is broken: " + finding.Reason
	} else if r.AnalysedBy != "" && finding.Holder != r.AnalysedBy {
		return NotEligible, fmt.Sprintf(
			"%s recorded this result and %s was holding the sample; a reading taken by somebody "+
				"who did not have the bottle is not evidence of what was in it",
			r.AnalysedBy, finding.Holder)
	}

	if r.Instrument.Ref == "" {
		return NotEligible, "the result does not say what it was read on"
	}
	if r.Instrument.ValidUntil == nil {
		// Not a finding. Nobody has said whether the instrument was certified,
		// which is a different thing from having said it was not — and reporting
		// the two identically is how a gap in the records becomes an accusation.
		return Unknown, fmt.Sprintf(
			"no calibration is recorded for instrument %s, so whether this reading is traceable "+
				"is not a question this platform can answer", r.Instrument.Ref)
	}
	if !r.AnalysedAt.UTC().Before(r.Instrument.ValidUntil.UTC()) {
		return NotEligible, fmt.Sprintf(
			"instrument %s (certificate %s) was out of calibration on %s: it expired on %s",
			r.Instrument.Ref, r.Instrument.CertificateRef,
			r.AnalysedAt.UTC().Format("2006-01-02"),
			r.Instrument.ValidUntil.UTC().Format("2006-01-02"))
	}

	return Eligible, ""
}

// Disagreement is what two readings of one analyte came to.
//
// Reported rather than resolved. A laboratory that ran a sample twice did so to
// find out whether the two agree, and a platform that picked one and moved on
// would be throwing away the answer to the question that was asked.
type Disagreement struct {
	Analyte Analyte
	// Readings are every live result for this analyte, in instrument order.
	Results []*Result
	// Spread is the difference between the largest and smallest, at their shared
	// scale. Nil when the readings are at different resolutions, because the
	// difference between 4.1 and 4.15 depends on what the first one meant.
	Spread *Reading
	// SpreadUnavailableReason says why there is none.
	SpreadUnavailableReason string
}

// Compare groups a sample's live results by analyte and reports what any
// repeats came to.
func Compare(results []*Result) []Disagreement {
	byAnalyte := map[Analyte][]*Result{}
	for _, r := range results {
		if r.SupersededAt != nil {
			continue
		}
		byAnalyte[r.Analyte] = append(byAnalyte[r.Analyte], r)
	}

	analytes := make([]Analyte, 0, len(byAnalyte))
	for a := range byAnalyte {
		analytes = append(analytes, a)
	}
	sort.Slice(analytes, func(i, j int) bool { return analytes[i] < analytes[j] })

	out := make([]Disagreement, 0, len(analytes))
	for _, a := range analytes {
		group := byAnalyte[a]
		sort.Slice(group, func(i, j int) bool {
			return group[i].Instrument.Ref < group[j].Instrument.Ref
		})
		d := Disagreement{Analyte: a, Results: group}
		if len(group) > 1 {
			d.Spread, d.SpreadUnavailableReason = spread(group)
		}
		out = append(out, d)
	}
	return out
}

func spread(group []*Result) (*Reading, string) {
	scale := group[0].Reading.Scale
	lo, hi := group[0].Reading.Value, group[0].Reading.Value
	for _, r := range group[1:] {
		if r.Reading.Scale != scale {
			return nil, fmt.Sprintf(
				"the readings are written to %d and %d decimal places; the difference between "+
					"them depends on what the shorter one meant, so it is not subtracted here",
				scale, r.Reading.Scale)
		}
		if r.Reading.Value < lo {
			lo = r.Reading.Value
		}
		if r.Reading.Value > hi {
			hi = r.Reading.Value
		}
	}
	return &Reading{Value: hi - lo, Scale: scale}, ""
}
