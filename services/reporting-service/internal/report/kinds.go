package report

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"
)

// Sources are the services a report draws on.
//
// Each may be nil, for a deployment that was not told how to reach it. A type
// whose source is nil fails with the setting named, rather than rendering an
// empty file — an empty collections report and a collections report the runner
// could not fetch look identical on a screen, and only one of them means
// nobody delivered any milk.
type Sources struct {
	Collections Collections
	Payables    Payables
	Divergences Divergences
}

// Collections reads priced deliveries over a period. Served by
// procurement-service.
type Collections interface {
	Collections(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]Collection, error)
}

// Payables reads what each producer is owed for one cycle. Served by
// settlement-service.
type Payables interface {
	Payables(ctx context.Context, tenantID, cycleID string) ([]Payable, Cycle, error)
}

// Divergences reads where the incumbent's figure and this platform's
// recomputation disagreed. Served by shadow-settlement-service.
type Divergences interface {
	Divergences(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]Divergence, error)
}

// Collection is one priced delivery, as much of it as a report prints.
type Collection struct {
	ID           string
	ProducerRef  string
	SocietyCode  string
	CollectedOn  string
	Shift        string
	Quantity     string
	QuantityUnit string
	Fat          string
	SNF          string
	Rate         string
	Currency     string
	Amount       string
	RateCardID   string
	OriginKind   string
	Explanation  string
}

// Payable is what one producer is owed.
type Payable struct {
	ProducerRef      string
	Kind             string
	Currency         string
	Gross            string
	Deducted         string
	Net              string
	CarriedForward   string
	Status           string
	HeldReason       string
	PaymentReference string
}

// Cycle is enough of a payment cycle to head a report with.
type Cycle struct {
	ID          string
	Name        string
	SocietyCode string
	PeriodStart string
	PeriodEnd   string
	Status      string
	Currency    string
	TotalNet    string
}

// Divergence is one settlement the two sides disagreed about.
type Divergence struct {
	ID             string
	ProducerRef    string
	Currency       string
	Delta          string
	Classification string
	Rationale      string
	Status         string
	NeedsReview    bool
	CreatedAt      string
}

var kinds = map[string]Kind{
	"collections": {
		Name: "collections",
		Summary: "Every priced delivery in a period: who brought it, when, how much, " +
			"what it tested at and what it was paid.",
		// Both ends, not just the start. A report running to "now" gives a
		// different answer every time it is re-run, which is the trap the
		// scheduled windows are shaped to avoid — and a total nobody can
		// reproduce is a total nobody can defend.
		Needs:       []string{"from", "to"},
		Schedulable: true,
		Window:      true,
		render:      renderCollections,
	},
	"settlement_summary": {
		Name:    "settlement_summary",
		Summary: "What each producer is owed for one payment cycle, and what was taken off.",
		Needs:   []string{"cycle_id"},
		// A cycle is an identifier, and a schedule firing at two in the morning
		// has no way to know which one is meant. Refused when the schedule is
		// written rather than when it fires.
		Schedulable: false,
		render:      renderSettlementSummary,
	},
	"divergences": {
		Name: "divergences",
		Summary: "Settlements where the incumbent's figure and this platform's recomputation " +
			"did not agree, with the classification and why.",
		Needs:       []string{"from", "to"},
		Schedulable: true,
		Window:      true,
		render:      renderDivergences,
	},
}

// csvOf writes a header and rows, and reports whether it stopped short.
//
// The ceiling is applied here rather than in each renderer so that every type
// stops the same way and every type reports it the same way.
func csvOf(header []string, rows [][]string) (Rendered, error) {
	truncated := false
	if len(rows) > MaxRows {
		rows = rows[:MaxRows]
		truncated = true
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return Rendered{}, err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return Rendered{}, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return Rendered{}, err
	}

	return Rendered{
		Content: buf.Bytes(),
		// With the charset, because a producer's name is not ASCII in most of
		// the places this runs and a spreadsheet that guesses gets it wrong.
		ContentType: "text/csv; charset=utf-8",
		Rows:        int64(len(rows)),
		Truncated:   truncated,
	}, nil
}

// fetchLimit is one more than the ceiling.
//
// Asking for exactly MaxRows makes a result of exactly MaxRows ambiguous:
// it could be all of the answer or the first part of a longer one, and
// reporting it as complete would be a guess. One more row settles it.
const fetchLimit = MaxRows + 1

func renderCollections(ctx context.Context, src *Sources, tenantID string, p Params) (Rendered, error) {
	if src.Collections == nil {
		return Rendered{}, fmt.Errorf("%w: a collections report comes from procurement-service "+
			"and PROCUREMENT_URL is not set on reporting-service", ErrNoSource)
	}
	// The dates are read as whole days in UTC here. A period given as dates has
	// no zone of its own; the schedule that produced them resolved them in the
	// co-operative's zone before they were written, which is where that
	// decision belongs.
	w, err := ParseWindow(p, time.UTC)
	if err != nil {
		return Rendered{}, err
	}

	list, err := src.Collections.Collections(ctx, tenantID, w.From, w.To, fetchLimit)
	if err != nil {
		return Rendered{}, err
	}

	rows := make([][]string, 0, len(list))
	for _, c := range list {
		rows = append(rows, []string{
			c.CollectedOn, c.Shift, c.ProducerRef, c.SocietyCode,
			c.Quantity, c.QuantityUnit, c.Fat, c.SNF,
			c.Rate, c.Amount, c.Currency, c.RateCardID, c.OriginKind, c.Explanation,
		})
	}
	return csvOf([]string{
		"collected_on", "shift", "producer_ref", "society_code",
		"quantity", "quantity_unit", "fat", "snf",
		"rate", "amount", "currency", "rate_card_id", "origin", "explanation",
	}, rows)
}

func renderSettlementSummary(ctx context.Context, src *Sources, tenantID string, p Params) (Rendered, error) {
	if src.Payables == nil {
		return Rendered{}, fmt.Errorf("%w: a settlement summary comes from settlement-service "+
			"and SETTLEMENT_URL is not set on reporting-service", ErrNoSource)
	}

	list, cycle, err := src.Payables.Payables(ctx, tenantID, p["cycle_id"])
	if err != nil {
		return Rendered{}, err
	}

	rows := make([][]string, 0, len(list))
	for _, pay := range list {
		rows = append(rows, []string{
			cycle.ID, cycle.Name, cycle.PeriodStart, cycle.PeriodEnd,
			pay.ProducerRef, pay.Kind,
			pay.Gross, pay.Deducted, pay.Net, pay.CarriedForward, pay.Currency,
			pay.Status, pay.HeldReason, pay.PaymentReference,
		})
	}
	return csvOf([]string{
		"cycle_id", "cycle", "period_start", "period_end",
		"producer_ref", "kind",
		"gross", "deducted", "net", "carried_forward", "currency",
		"status", "held_reason", "payment_reference",
	}, rows)
}

func renderDivergences(ctx context.Context, src *Sources, tenantID string, p Params) (Rendered, error) {
	if src.Divergences == nil {
		return Rendered{}, fmt.Errorf("%w: a divergence report comes from "+
			"shadow-settlement-service and SHADOW_SETTLEMENT_URL is not set on "+
			"reporting-service", ErrNoSource)
	}
	w, err := ParseWindow(p, time.UTC)
	if err != nil {
		return Rendered{}, err
	}

	list, err := src.Divergences.Divergences(ctx, tenantID, w.From, w.To, fetchLimit)
	if err != nil {
		return Rendered{}, err
	}

	rows := make([][]string, 0, len(list))
	for _, d := range list {
		needs := "no"
		if d.NeedsReview {
			needs = "yes"
		}
		rows = append(rows, []string{
			d.CreatedAt, d.ProducerRef, d.Classification, d.Delta, d.Currency,
			d.Status, needs, d.Rationale,
		})
	}
	return csvOf([]string{
		"raised_at", "producer_ref", "classification", "delta", "currency",
		"status", "needs_review", "rationale",
	}, rows)
}
