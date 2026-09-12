package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/canonical"
	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
	"github.com/ppusapati/gavya/services/settlement-service/internal/procurement"
)

// Explaining a payment.
//
// The question a member actually asks is "why was I paid this". The figures that
// answer it live in three services: the payable and the lines gathered into it
// are here; what each delivery came to, why, and whether it has since been
// restated is procurement's; and for milk that arrived through an import, what
// the member number on it meant — and whether somebody has since decided it was
// wrong — is canonical's. Until this existed, answering took all three and a
// database console.
//
// # WHAT AN EXPLANATION IS AND IS NOT
//
// It composes what the services already recorded. It re-prices nothing, resolves
// nothing, and decides nothing: two implementations of the same rate card would
// eventually disagree, and the disagreement would surface as a member's
// statement differing from the slip in their pocket.
//
// What it adds is the comparison. Each line holds what was paid, procurement
// holds what it says now, and where the two differ the explanation says so, with
// procurement's own reason for the change. That comparison is the thing nobody
// could see: a cycle line that stopped matching its collection looked exactly
// like one that had not.
//
// # WHAT IT SAYS WHEN IT CANNOT SAY
//
// Every part it could not consult is named as a finding. An explanation
// produced without procurement or without canonical is still worth having and
// is still less than the whole; what would not be worth having is one that
// looked complete. The Consulted block is there so a reader can tell the two
// apart without reading the findings.

// Explainers are the other services an explanation reads from.
type Explainers interface {
	// Versions is every version of one delivery, oldest first.
	Versions(ctx context.Context, tenantID, collectionID string) ([]procurement.Version, error)
	// RateCard names the card a delivery was priced against.
	RateCard(ctx context.Context, tenantID, id string) (*procurement.RateCard, error)
	// IdentityHistory is every mapping ever recorded for a member number in a
	// source system, retired ones included. Returns ErrNoCanonical when this
	// service was started without one.
	IdentityHistory(ctx context.Context, tenantID, sourceSystemID, externalID string) ([]canonical.Mapping, error)
}

// ErrNoCanonical says identity history cannot be read because nothing was
// configured to read it from.
var ErrNoCanonical = errors.New("this service has no canonical to read identity history from; set CANONICAL_URL")

// WithExplainers gives the service the readers an explanation needs.
//
// A setter rather than a New argument so the service's construction, and every
// test that constructs one, is unchanged. Left unset, Explain still answers —
// with the lines as gathered and a finding saying nothing was traced further.
func (s *Service) WithExplainers(e Explainers) *Service {
	s.explain = e
	return s
}

// Explanation is a payable traced to its causes.
type Explanation struct {
	Payable *domain.ProducerPayable
	Cycle   *domain.Cycle

	// Lines is each delivery gathered into this payable, as it was paid and as
	// procurement prices it now.
	Lines []*ExplainedLine
	// Deductions are what was recovered from the fortnight's earnings.
	Deductions []*domain.Deduction

	// RateCards are the cards the lines were priced against, each with how many
	// lines it priced.
	RateCards []*RateCardUse

	// Identities is, for each member number from an external system that
	// appears on these lines, every mapping ever recorded for it.
	Identities []*IdentityTrace

	// Findings is everything a reader should be told in words: figures that no
	// longer agree, mappings that were retired, and anything that could not be
	// consulted. Empty means the payment is explained by its lines alone and
	// every line still matches what procurement says.
	Findings []string

	Consulted Consulted
}

// Consulted says which services actually answered, so an absence reads as an
// absence rather than as a payment with nothing to say about it.
type Consulted struct {
	Procurement bool
	Canonical   bool
	// Why not, when not. Empty when consulted.
	ProcurementWhyNot string
	CanonicalWhyNot   string
}

// ExplainedLine is one gathered delivery beside what procurement says of it now.
type ExplainedLine struct {
	// Line is the delivery as this cycle paid it.
	Line *domain.Line
	// Current is the version of the delivery in force now, or nil if procurement
	// could not be read. When it is not the version that was gathered, the
	// payment stands on a figure that has since been restated.
	Current *procurement.Version
	// Gathered is the version this line was copied from, if procurement still
	// has it. Nil if procurement could not be read.
	Gathered *procurement.Version
	// Versions is every version of the delivery, oldest first.
	Versions []procurement.Version
	// PaidMatchesCurrent is whether the amount paid equals the amount procurement
	// would price it at now.
	PaidMatchesCurrent bool
}

// RateCardUse is a rate card and how much of this payable it priced.
type RateCardUse struct {
	Card        *procurement.RateCard
	ID          string
	LinesPriced int
	// Unreadable carries the error if the card could not be fetched, so a line
	// priced against a card that no longer answers is reported rather than
	// dropped.
	Unreadable string
}

// IdentityTrace is what one external member number has meant.
type IdentityTrace struct {
	SourceSystemID string
	ExternalID     string
	// Lines is how many of this payable's deliveries carry this identifier.
	Lines    int
	Mappings []canonical.Mapping
}

// Explain traces one payable to what produced it.
func (s *Service) Explain(ctx context.Context, tenantID, payableID string) (*Explanation, error) {
	p, err := s.repo.GetPayable(ctx, tenantID, payableID)
	if err != nil {
		return nil, err
	}
	ex := &Explanation{Payable: p}

	if p.Kind == domain.KindAdjustment {
		// An adjustment is money raised against a fortnight after it was
		// settled, with a reason of its own. It gathered no lines: what explains
		// it is the reason on it and the payable it corrects, which is named.
		cycle, err := s.repo.GetCycle(ctx, tenantID, p.CycleID)
		if err != nil {
			return nil, err
		}
		ex.Cycle = cycle
		ex.Findings = append(ex.Findings, fmt.Sprintf(
			"This is an adjustment of %s raised against the %s cycle, not a settlement of "+
				"deliveries. Its reason is: %s", p.Net, cycle.Name, p.Reason))
		if p.AdjustsPayableID != "" {
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"It corrects payable %s; explain that one for the deliveries.", p.AdjustsPayableID))
		}
		return ex, nil
	}

	st, err := s.repo.Statement(ctx, tenantID, p.CycleID, p.ProducerRef)
	if err != nil {
		return nil, err
	}
	ex.Cycle = st.Cycle
	ex.Deductions = st.Deductions
	for _, l := range st.Lines {
		ex.Lines = append(ex.Lines, &ExplainedLine{Line: l})
	}

	// The figures reconcile with each other before anything outside is asked.
	// A payable whose gross is not the sum of its lines is a question for a
	// person, not for procurement, and it is the first thing a reader should
	// be told.
	if finding := reconcile(p, st.Lines); finding != "" {
		ex.Findings = append(ex.Findings, finding)
	}

	if s.explain == nil {
		ex.Consulted.ProcurementWhyNot = "this service has no procurement to read from"
		ex.Consulted.CanonicalWhyNot = ErrNoCanonical.Error()
		ex.Findings = append(ex.Findings,
			"The deliveries were not traced back to procurement: this service was "+
				"started without a reader for it. The lines above are as they were "+
				"gathered, and nothing here can say whether they have since been restated.")
		return ex, nil
	}

	s.traceCollections(ctx, tenantID, ex)
	s.traceRateCards(ctx, tenantID, ex)
	s.traceIdentities(ctx, tenantID, ex)
	return ex, nil
}

// traceCollections reads every version of every gathered delivery.
func (s *Service) traceCollections(ctx context.Context, tenantID string, ex *Explanation) {
	ex.Consulted.Procurement = true
	for _, el := range ex.Lines {
		versions, err := s.explain.Versions(ctx, tenantID, el.Line.CollectionID)
		if err != nil {
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"The delivery on %s (%s) could not be read back from procurement: %v. "+
					"The line stands as gathered and nothing here can say whether it has "+
					"since been restated.",
				day(el.Line.CollectedOn), el.Line.Shift, err))
			continue
		}
		el.Versions = versions
		for i := range versions {
			v := &versions[i]
			if v.ID == el.Line.CollectionID {
				el.Gathered = v
			}
			if v.SupersededAt == nil {
				el.Current = v
			}
		}

		switch {
		case el.Current == nil:
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"Every version of the delivery on %s (%s) is marked superseded and none "+
					"is in force, which should not be possible. The line was paid at %s.",
				day(el.Line.CollectedOn), el.Line.Shift, el.Line.Amount))
		case sameAmount(el.Line.Amount, el.Current.Amount):
			el.PaidMatchesCurrent = true
		default:
			ex.Findings = append(ex.Findings, restated(el))
		}
	}
}

// restated says, in words, that a paid line no longer matches its delivery.
func restated(el *ExplainedLine) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The delivery on %s (%s) was paid at %s and procurement now prices it at %s",
		day(el.Line.CollectedOn), el.Line.Shift, el.Line.Amount, el.Current.Amount)
	if diff, ok := difference(el.Current.Amount, el.Line.Amount); ok {
		fmt.Fprintf(&b, ", a difference of %s", diff)
	}
	b.WriteString(".")

	// The chain of corrections from the gathered version forward, each with
	// procurement's reason. The reasons are what a member is owed; the amounts
	// alone say only that something moved.
	if el.Gathered != nil {
		for _, v := range el.Versions {
			if v.Supersedes == "" || v.CreatedAt.Before(el.Gathered.CreatedAt) {
				continue
			}
			fmt.Fprintf(&b, " Restated on %s", day(v.CreatedAt))
			if v.CorrectionReason != "" {
				fmt.Fprintf(&b, ": %s", v.CorrectionReason)
			}
			b.WriteString(".")
		}
	}
	b.WriteString(" A paid figure is corrected by an adjustment, never by editing the line.")
	return b.String()
}

// traceRateCards names every card the current versions were priced against.
func (s *Service) traceRateCards(ctx context.Context, tenantID string, ex *Explanation) {
	uses := map[string]*RateCardUse{}
	for _, el := range ex.Lines {
		if el.Current == nil || el.Current.RateCardID == "" {
			continue
		}
		u, ok := uses[el.Current.RateCardID]
		if !ok {
			u = &RateCardUse{ID: el.Current.RateCardID}
			uses[u.ID] = u
		}
		u.LinesPriced++
	}
	ids := make([]string, 0, len(uses))
	for id := range uses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		u := uses[id]
		card, err := s.explain.RateCard(ctx, tenantID, id)
		if err != nil {
			u.Unreadable = err.Error()
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"%d of these deliveries were priced against rate card %s, which could not "+
					"be read back from procurement: %v", u.LinesPriced, id, err))
		} else {
			u.Card = card
		}
		ex.RateCards = append(ex.RateCards, u)
	}
}

// traceIdentities reads what each imported member number has meant.
func (s *Service) traceIdentities(ctx context.Context, tenantID string, ex *Explanation) {
	type key struct{ source, external string }
	counts := map[key]int{}
	imported := 0
	native := 0
	for _, el := range ex.Lines {
		switch {
		case el.Current == nil:
			// Already reported under the collections.
		case el.Current.OriginKind == "IMPORTED" && el.Current.SourceSystemID != "":
			counts[key{el.Current.SourceSystemID, el.Current.ProducerRef}]++
			imported++
		case el.Current.OriginKind == "IMPORTED":
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"The delivery on %s (%s) was imported and records no source system, so "+
					"what its member number meant cannot be looked up.",
				day(el.Line.CollectedOn), el.Line.Shift))
		default:
			native++
		}
	}
	if native > 0 {
		ex.Findings = append(ex.Findings, fmt.Sprintf(
			"%d of these deliveries were recorded by the society itself under its own "+
				"code %s. No external identity mapping applies to them.",
			native, ex.Payable.ProducerRef))
	}
	if len(counts) == 0 {
		// Nothing to ask canonical about. Reported as such rather than as
		// consulted, because it was not.
		ex.Consulted.CanonicalWhyNot = "no imported deliveries to look up"
		return
	}

	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].source != keys[j].source {
			return keys[i].source < keys[j].source
		}
		return keys[i].external < keys[j].external
	})

	for _, k := range keys {
		mappings, err := s.explain.IdentityHistory(ctx, tenantID, k.source, k.external)
		if errors.Is(err, ErrNoCanonical) {
			ex.Consulted.CanonicalWhyNot = err.Error()
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"Identity history was not consulted: %v. %d of these deliveries carry "+
					"member numbers from other systems, and nothing here can say what "+
					"those numbers meant or whether a mapping has since been withdrawn.",
				err, imported))
			return
		}
		ex.Consulted.Canonical = true
		trace := &IdentityTrace{SourceSystemID: k.source, ExternalID: k.external, Lines: counts[k]}
		ex.Identities = append(ex.Identities, trace)
		if err != nil {
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"The history of member number %s in %s could not be read from canonical: %v",
				k.external, k.source, err))
			continue
		}
		trace.Mappings = mappings

		if len(mappings) == 0 {
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"No identity mapping is on record for member number %s in %s. The %d "+
					"deliveries carrying it were attributed to this producer by whatever "+
					"imported them, not by a mapping anybody asserted.",
				k.external, k.source, counts[k]))
			continue
		}
		for _, m := range mappings {
			if !m.Retired() {
				continue
			}
			ex.Findings = append(ex.Findings, fmt.Sprintf(
				"The mapping that said member number %s in %s meant producer %s from %s "+
					"was retired on %s by %s. The payment stands as computed under it; if "+
					"the milk belonged to somebody else, that is an adjustment.",
				k.external, k.source, m.EntityID, day(m.ValidFrom),
				day(*m.SupersededAt), m.SupersededBy))
		}
	}
}

// reconcile checks the payable's own arithmetic against its lines.
func reconcile(p *domain.ProducerPayable, lines []*domain.Line) string {
	var sum int64
	for _, l := range lines {
		if l.Amount.Currency != p.Gross.Currency || l.Amount.Scale != p.Gross.Scale {
			return fmt.Sprintf("A line on this payable is in %s at scale %d and the payable is in "+
				"%s at scale %d; the figures cannot be compared, which is a question for a person.",
				l.Amount.Currency, l.Amount.Scale, p.Gross.Currency, p.Gross.Scale)
		}
		sum += l.Amount.Value
	}
	if sum != p.Gross.Value {
		got, _ := money.New(sum, p.Gross.Scale, p.Gross.Currency)
		return fmt.Sprintf("The payable's gross is %s and its %d lines add up to %s. These are "+
			"stored together and forced to agree, so this should not be possible; it is the "+
			"first thing to look at.", p.Gross, len(lines), got)
	}
	return ""
}

func sameAmount(a, b money.Money) bool {
	return a.Currency == b.Currency && a.String() == b.String()
}

// difference is a - b, when the two can be subtracted.
func difference(a, b money.Money) (money.Money, bool) {
	if a.Currency != b.Currency || a.Scale != b.Scale {
		return money.Money{}, false
	}
	d, err := money.New(a.Value-b.Value, a.Scale, a.Currency)
	if err != nil {
		return money.Money{}, false
	}
	return d, true
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }
