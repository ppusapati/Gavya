package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Formulation is one version of a recipe: what a plant says it makes a thing
// out of, and what it expects to get.
//
// Versioned because recipes change and reports do not travel back in time. A
// coagulant is switched in April; the vats made in March were made under the
// old recipe and their variance has to keep being measured against the old
// target, or last month's signed-off report says something different this month.
type Formulation struct {
	ID       string
	TenantID string

	// Code is stable across versions. PANEER-STD is the same recipe in March
	// and in September, at two versions.
	Code string
	Name string

	OutputProductRef string
	OutputUnit       Unit

	// ExpectedYieldPPM is what the plant expects, in parts per million of what
	// it consumes. Nil where it has not said, which is not a failing: a plant
	// that has run a process four times has no business declaring a target from
	// four numbers.
	ExpectedYieldPPM *int64
	// ExpectationBasis says where the figure came from. Required alongside one.
	//
	// A yield a plant derived from its own observed history is a different kind
	// of claim from one somebody read off a supplier's leaflet, and a variance
	// report that treats the two alike invites the same argument every month.
	ExpectationBasis string

	// Status is whether anybody has signed this off.
	//
	// A batch may only be made against an APPROVED recipe. Without that a vat
	// can be measured against a target somebody was halfway through typing, and
	// the variance report says the process is wrong when what is wrong is that
	// nobody has agreed to the number yet.
	Status FormulationStatus
	// ApprovedBy and ApprovedAt are required alongside APPROVED, so a target
	// that priced somebody's variance has a name against it.
	ApprovedBy string
	ApprovedAt *time.Time
	// ApprovalNote is what was signed off and on what basis.
	ApprovalNote string
	// WithdrawnReason is required alongside WITHDRAWN. A recipe that stopped
	// being used with no reason recorded is one nobody can explain
	// reintroducing.
	WithdrawnReason string

	ValidFrom time.Time
	ValidTo   *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
}

// FormulationStatus is the whole lifecycle of a recipe version.
//
// A version is never edited once written, here as everywhere else in this
// platform: a recipe changes by superseding, and a draft that turns out wrong is
// withdrawn rather than corrected in place. So three states cover it — drafted,
// signed off, stopped.
type FormulationStatus string

const (
	// Draft is written down and not yet agreed to. Several may exist for one
	// period at once: a draft is a piece of paper on somebody's desk, and two of
	// them for the same quarter is an ordinary afternoon.
	Draft FormulationStatus = "DRAFT"
	// Approved is signed off, and exclusive: one approved version of a code is
	// in force at any moment.
	Approved FormulationStatus = "APPROVED"
	// Withdrawn is stopped. Batches already made under it keep pointing at it —
	// that is their history — but no new batch may name it.
	Withdrawn FormulationStatus = "WITHDRAWN"
)

func ValidFormulationStatus(s FormulationStatus) bool {
	switch s {
	case Draft, Approved, Withdrawn:
		return true
	}
	return false
}

// UsableForProduction says whether a batch may be made against this version.
func (s FormulationStatus) UsableForProduction() bool { return s == Approved }

// Unit here is quantity's, restated so the domain reads without the import in
// every signature. Same strings, same meanings.
type Unit = string

// InForceAt says whether this version of the recipe was the one on the wall.
//
// Half-open: a recipe ending on the first of April and its replacement starting
// there do not both apply on that morning. That is how a plant actually
// replaces one, and the alternative is a day on which two targets are in force.
func (f *Formulation) InForceAt(t time.Time) bool {
	if t.Before(f.ValidFrom) {
		return false
	}
	return f.ValidTo == nil || t.Before(*f.ValidTo)
}

// FormulationInput is one expected ingredient, named by what the plant calls it.
//
// By product rather than by batch: a recipe names milk and rennet; which lot of
// milk is a fact about Tuesday.
type FormulationInput struct {
	ID            string
	TenantID      string
	FormulationID string

	ProductRef string

	// ExpectedSharePPM is what share of the total input this is expected to be.
	// Nil where the plant has not said. The shares of a recipe are not required
	// to sum to a million: a recipe naming its two main ingredients and leaving
	// the salt undeclared is an ordinary recipe, and demanding the rest would
	// push somebody into inventing a figure for the salt.
	ExpectedSharePPM *int64

	// ShareTolerancePPM is how far from the declared share a batch may be before
	// it is worth mentioning. Only meaningful beside a declared share.
	//
	// Required in order to get a finding at all, and deliberately so. A real vat
	// never hits a declared proportion exactly, so comparing for equality would
	// report every batch a plant ever made and the report would stop being read
	// within a month. How close is close enough is a question about this plant's
	// process, its scales and what it is trying to control — not one this
	// platform can answer, and inventing a figure would be putting the platform's
	// opinion into a report with the plant's name on it.
	//
	// Where none is declared the observed and declared shares are still both
	// reported side by side. The numbers are shown; the judgement is not made.
	ShareTolerancePPM *int64

	// Required says whether a batch without this is wrong or merely unusual.
	// Paneer without milk is wrong. Paneer without the optional culture is a
	// Tuesday.
	Required bool

	CreatedAt time.Time
	CreatedBy string
}

var (
	ErrNoFormulationCode      = errors.New("a recipe must carry the code the plant knows it by")
	ErrNoOutputProduct        = errors.New("a recipe must say what it makes")
	ErrNoValidFrom            = errors.New("a recipe must say when it came into force; a recipe with no period is one that silently applies to every batch ever made")
	ErrBackwardsPeriod        = errors.New("a recipe's period ends before it starts")
	ErrExpectationNeedsBasis  = errors.New("a declared yield must say where the figure came from; a target derived from a plant's own vats and one read off a supplier's leaflet are different claims, and a report that treats them alike invites the same argument every month")
	ErrIngredientIsTheProduct = errors.New("a recipe whose ingredient is its own product has no first ingredient")

	ErrNoApprover         = errors.New("an approved recipe must say who signed it off and when; a target that prices somebody's variance with no name against it is one nobody can be asked about")
	ErrNoWithdrawalReason = errors.New("withdrawing a recipe must say why; one that stopped being used with no reason recorded is one nobody can explain reintroducing")
	ErrNotApproved        = errors.New("a batch may only be made against an approved recipe")
)

func (f *Formulation) Validate() error {
	switch {
	case f.TenantID == "":
		return errors.New("tenant_id is required")
	case f.Code == "":
		return ErrNoFormulationCode
	case f.Name == "":
		return errors.New("a recipe must have a name a person recognises")
	case f.OutputProductRef == "":
		return ErrNoOutputProduct
	case f.OutputUnit != "LITRES" && f.OutputUnit != "KILOGRAMS":
		return errors.New("a recipe must say whether it makes litres or kilograms")
	case f.ValidFrom.IsZero():
		return ErrNoValidFrom
	case f.ValidTo != nil && !f.ValidTo.After(f.ValidFrom):
		return ErrBackwardsPeriod
	case f.ExpectedYieldPPM != nil && *f.ExpectedYieldPPM <= 0:
		return errors.New("a declared yield of nothing is not an expectation")
	case f.ExpectedYieldPPM != nil && f.ExpectationBasis == "":
		return ErrExpectationNeedsBasis
	case f.ExpectedYieldPPM == nil && f.ExpectationBasis != "":
		return errors.New("a basis with no figure beside it explains nothing")
	case !ValidFormulationStatus(f.Status):
		return errors.New("a recipe must be a DRAFT, APPROVED or WITHDRAWN")
	case f.Status == Approved && (f.ApprovedBy == "" || f.ApprovedAt == nil):
		return ErrNoApprover
	case f.Status != Approved && (f.ApprovedBy != "" || f.ApprovedAt != nil):
		return errors.New("an approver on a recipe nobody approved says a thing that did not happen")
	case f.Status == Withdrawn && f.WithdrawnReason == "":
		return ErrNoWithdrawalReason
	case f.Status != Withdrawn && f.WithdrawnReason != "":
		return errors.New("a withdrawal reason on a recipe nobody withdrew")
	case f.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

func (i *FormulationInput) Validate(f *Formulation) error {
	switch {
	case i.TenantID == "":
		return errors.New("tenant_id is required")
	case i.FormulationID == "":
		return errors.New("an ingredient must say which recipe it belongs to")
	case i.ProductRef == "":
		return errors.New("an ingredient must say what it is")
	case f != nil && i.ProductRef == f.OutputProductRef:
		return ErrIngredientIsTheProduct
	case i.ExpectedSharePPM != nil && (*i.ExpectedSharePPM <= 0 || *i.ExpectedSharePPM > ppm):
		return errors.New("a share is between one part per million and all of it")
	case i.ShareTolerancePPM != nil && i.ExpectedSharePPM == nil:
		return errors.New("a tolerance with no share beside it has nothing to be a tolerance on")
	case i.ShareTolerancePPM != nil && (*i.ShareTolerancePPM < 0 || *i.ShareTolerancePPM > ppm):
		return errors.New("a tolerance is between nothing and all of it")
	case i.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Was the recipe followed
// ---------------------------------------------------------------------------

// Lot is one thing that actually went into a batch: what it was, and how much.
type Lot struct {
	BatchID    string
	BatchCode  string
	ProductRef string
	Consumed   quantityValue
}

// quantityValue is the measured amount and its unit, kept small so this file
// does not need the quantity package in every signature.
type quantityValue struct {
	Value int64
	Unit  Unit
}

// NewLot builds a Lot from what the repository read.
func NewLot(batchID, batchCode, productRef string, value int64, unit Unit) Lot {
	return Lot{BatchID: batchID, BatchCode: batchCode, ProductRef: productRef,
		Consumed: quantityValue{Value: value, Unit: unit}}
}

// FindingKind is what is wrong, or merely worth mentioning.
type FindingKind string

const (
	// MissingRequired: the recipe says this goes in and nothing of it did.
	MissingRequired FindingKind = "MISSING_REQUIRED"
	// MissingOptional: an ingredient the recipe marks optional was not used.
	MissingOptional FindingKind = "MISSING_OPTIONAL"
	// Unexpected: something went in that the recipe does not name.
	Unexpected FindingKind = "UNEXPECTED"
	// ShareDiffers: the ingredient is there, in a proportion further from the
	// declared one than the plant's own declared tolerance allows. Never raised
	// where no tolerance was declared — see FormulationInput.ShareTolerancePPM.
	ShareDiffers FindingKind = "SHARE_DIFFERS"
)

// Finding is one difference between the recipe and the vat.
type Finding struct {
	Kind       FindingKind
	ProductRef string
	// ExpectedSharePPM and ObservedSharePPM are set on ShareDiffers.
	ExpectedSharePPM *int64
	ObservedSharePPM *int64
	// Explanation is written for whoever has to act on it.
	Explanation string
}

// Serious says whether this is a finding somebody has to answer for, as opposed
// to one worth knowing.
func (f Finding) Serious() bool { return f.Kind == MissingRequired }

// ShareReading is what a recipe expected of one ingredient and what the vat
// actually held, side by side.
//
// Reported for every declared share whether or not it is a finding. The
// platform's job here is to put the two numbers next to each other; deciding
// whether the gap matters needs a tolerance, and the tolerance is the plant's.
type ShareReading struct {
	ProductRef       string
	ExpectedSharePPM int64
	ObservedSharePPM int64
	// DifferencePPM is observed minus expected. The sign says which way.
	DifferencePPM int64
	// TolerancePPM is what the plant declared, if it declared one.
	TolerancePPM *int64
}

// RecipeCheck is what a batch looks like held up against the recipe it followed.
type RecipeCheck struct {
	// Findings are the differences worth acting on, most serious first.
	Findings []Finding
	// Shares are the declared proportions against the observed ones, for every
	// ingredient the recipe put a share on and the batch actually contained.
	Shares []ShareReading
	// SharesUnavailableReason says why Shares is empty when it is. The one that
	// happens: a vat filled from a litre lot and a kilogram lot, where the total
	// they would be shares of is a sum of two different things.
	SharesUnavailableReason string
}

// Serious returns the findings somebody has to answer for.
func (c *RecipeCheck) Serious() []Finding {
	var out []Finding
	for _, f := range c.Findings {
		if f.Serious() {
			out = append(out, f)
		}
	}
	return out
}

// CheckRecipe compares what went into a batch with what the recipe says.
//
// Reported, never refused. A plant substitutes: the usual coagulant is out of
// stock, a second silo is drawn on because the first ran dry, something is
// added that nobody wrote into the recipe three years ago. Refusing those would
// mean the vat gets recorded wrongly or not at all, and a genealogy with a gap
// in it is worse than one with a note attached.
//
// Shares are computed only where every input is in one unit. Totalling litres
// and kilograms to get a denominator would need a density, this has none, and a
// share computed on an assumed one would be a difference nobody could act on.
func CheckRecipe(f *Formulation, expected []FormulationInput, actual []Lot) (*RecipeCheck, error) {
	if f == nil {
		return nil, errors.New("no recipe to check against")
	}

	want := map[string]FormulationInput{}
	for _, e := range expected {
		want[e.ProductRef] = e
	}

	// What actually went in, totalled by product, and whether it can be
	// totalled at all.
	got := map[string]int64{}
	var total int64
	unit, mixed := "", false
	for _, l := range actual {
		got[l.ProductRef] += l.Consumed.Value
		total += l.Consumed.Value
		if unit == "" {
			unit = l.Consumed.Unit
		} else if unit != l.Consumed.Unit {
			mixed = true
		}
	}

	check := &RecipeCheck{}
	for ref, e := range want {
		if _, present := got[ref]; present {
			continue
		}
		kind, why := MissingOptional, fmt.Sprintf(
			"recipe %s lists %s as optional and none went in", f.Code, ref)
		if e.Required {
			kind, why = MissingRequired, fmt.Sprintf(
				"recipe %s says %s goes into every %s and none went into this one",
				f.Code, ref, f.OutputProductRef)
		}
		check.Findings = append(check.Findings, Finding{Kind: kind, ProductRef: ref, Explanation: why})
	}

	for ref := range got {
		if _, named := want[ref]; !named {
			check.Findings = append(check.Findings, Finding{
				Kind: Unexpected, ProductRef: ref, Explanation: fmt.Sprintf(
					"%s went into this batch and recipe %s does not name it; a substitution is an "+
						"ordinary thing and this is a note rather than a fault", ref, f.Code)})
		}
	}

	switch {
	case mixed:
		check.SharesUnavailableReason = fmt.Sprintf(
			"the lots that went into this batch are measured in more than one unit, so the total " +
				"they would be shares of is a sum of litres and kilograms; totalling them needs a " +
				"density and none was supplied")
	case total == 0:
		check.SharesUnavailableReason = "nothing is recorded as having gone into this batch"
	default:
		for ref, e := range want {
			amount, present := got[ref]
			if !present || e.ExpectedSharePPM == nil {
				continue
			}
			observed := amount * ppm / total
			diff := observed - *e.ExpectedSharePPM
			check.Shares = append(check.Shares, ShareReading{
				ProductRef: ref, ExpectedSharePPM: *e.ExpectedSharePPM,
				ObservedSharePPM: observed, DifferencePPM: diff,
				TolerancePPM: e.ShareTolerancePPM,
			})

			// A finding needs a tolerance the plant declared. Without one there
			// is nothing to be outside of: a real vat never hits a declared
			// proportion exactly, and integer division moves the observed figure
			// by up to a part per million on its own, so comparing for equality
			// would report every batch ever made.
			if e.ShareTolerancePPM == nil {
				continue
			}
			// The arithmetic's own resolution is absorbed on top of whatever
			// the plant declared, including a declared zero. The observed share
			// is an integer division and truncates, so a vat holding exactly the
			// declared proportion can still read one part per million short —
			// 1000 of 101000 is 9900.99 ppm and comes back as 9900 against a
			// recipe that writes the same proportion as 9901.
			//
			// This is not a tolerance in the sense the field means. A tolerance
			// is a judgement about a process and belongs to the plant; this is a
			// property of integer division, nobody chose it, and reporting it
			// would be reporting the platform's own rounding as a finding about
			// somebody's milk.
			if abs64(diff) <= *e.ShareTolerancePPM+1 {
				continue
			}
			w, o := *e.ExpectedSharePPM, observed
			check.Findings = append(check.Findings, Finding{
				Kind: ShareDiffers, ProductRef: ref,
				ExpectedSharePPM: &w, ObservedSharePPM: &o,
				Explanation: shareExplanation(f.Code, ref, w, o, *e.ShareTolerancePPM),
			})
		}
	}

	// A stable order, so two runs of the same report read the same way.
	sort.Slice(check.Findings, func(i, j int) bool {
		if check.Findings[i].Kind != check.Findings[j].Kind {
			return check.Findings[i].Kind < check.Findings[j].Kind
		}
		return check.Findings[i].ProductRef < check.Findings[j].ProductRef
	})
	sort.Slice(check.Shares, func(i, j int) bool {
		return check.Shares[i].ProductRef < check.Shares[j].ProductRef
	})
	return check, nil
}

// shareExplanation words a share finding for whoever has to act on it. A
// declared tolerance of zero reads as "exactly" rather than as "give or take
// 0.0000%", which is a phrase that makes a reader stop and wonder what it means.
func shareExplanation(code, ref string, expected, observed, tolerance int64) string {
	if tolerance == 0 {
		return fmt.Sprintf("recipe %s puts %s at exactly %s of the input and this batch was %s",
			code, ref, PercentString(expected), PercentString(observed))
	}
	return fmt.Sprintf(
		"recipe %s puts %s at %s of the input give or take %s, and this batch was %s",
		code, ref, PercentString(expected), PercentString(tolerance), PercentString(observed))
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// ---------------------------------------------------------------------------
// What the plant actually got
// ---------------------------------------------------------------------------

// ObservedHistory is what every batch under a recipe yielded, summarised.
//
// This is the platform's answer to a question it cannot answer for anybody:
// what should this process yield. It does not say. It shows the plant its own
// vats and lets the plant say — which is the only place the number can honestly
// come from.
type ObservedHistory struct {
	FormulationID    string
	Code             string
	OutputProductRef string

	// BatchesCounted is how many batches the figures below rest on.
	BatchesCounted int64
	// BatchesNeedingADensity is how many were left out because their inputs and
	// their output are in different units. Reported beside the count, because a
	// recipe with three usable observations and forty unconvertible ones is not
	// one anybody should set a target from, and a summary showing only the
	// three would not say so.
	BatchesNeedingADensity int64

	LowestPPM        *int64
	LowerQuartilePPM *int64
	MedianPPM        *int64
	UpperQuartilePPM *int64
	HighestPPM       *int64

	// ExpectedPPM is what the plant has declared, if anything.
	ExpectedPPM *int64
	// ExpectationBasis is where it said that came from.
	ExpectationBasis string
}

// EnoughToSetATarget is a judgement this deliberately does not make.
//
// It reports the count and says nothing about whether it is enough, because how
// many vats a plant needs before it trusts a median is a question about that
// plant's process and not one this platform can answer. What it does is make
// the count impossible to miss.
func (h *ObservedHistory) Note() string {
	switch {
	case h.BatchesCounted == 0 && h.BatchesNeedingADensity > 0:
		return fmt.Sprintf(
			"no batch under recipe %s has a yield that can be computed: all %d of them were "+
				"measured in one unit going in and another coming out, and converting needs a "+
				"density nobody has supplied",
			h.Code, h.BatchesNeedingADensity)
	case h.BatchesCounted == 0:
		return fmt.Sprintf("no batch has been made under recipe %s yet", h.Code)
	case h.BatchesNeedingADensity > 0:
		return fmt.Sprintf(
			"these figures rest on %d batches; a further %d were left out because their inputs "+
				"and output are in different units and converting needs a density",
			h.BatchesCounted, h.BatchesNeedingADensity)
	default:
		return fmt.Sprintf("these figures rest on %d batches", h.BatchesCounted)
	}
}
