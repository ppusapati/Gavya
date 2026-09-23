// Package sources reads the data a report is made of, from the services that
// own it.
//
// Nothing here queries another service's database and nothing recomputes what
// another service already computed. A collections report shows what procurement
// priced, as procurement priced it: two implementations of the same rate card
// would eventually disagree, and the disagreement would surface as a report
// differing from the slip a farmer was handed at the collection centre.
//
// Every reader pages. A fortnight for a two-hundred-member society is around
// six thousand collections, so the paging has to actually work rather than
// appear to — a reader that asked once and took what came would produce a
// report that is short by however much a page holds, with nothing anywhere
// saying so.
package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"

	"github.com/ppusapati/gavya/services/reporting-service/internal/report"
)

// page is how many rows are asked for at a time.
const page = 500

// Options builds the per-call options for one tenant.
//
// A function rather than a value because a request id belongs to one call, and
// a reader that reused one across a six-thousand-row report would make every
// log line in it correlate to the same request.
type Options func(ctx context.Context, tenantID string) func() svcclient.CallOptions

// Procurement reads priced collections.
type Procurement struct {
	client *svcclient.Client
	opts   Options
}

func NewProcurement(c *svcclient.Client, opts Options) *Procurement {
	return &Procurement{client: c, opts: opts}
}

type collectionsRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type pointProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

type collectionProto struct {
	ID           string     `json:"id"`
	ProducerRef  string     `json:"producer_ref"`
	SocietyCode  string     `json:"society_code"`
	CollectedOn  string     `json:"collected_on"`
	Shift        string     `json:"shift"`
	Quantity     pointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          pointProto `json:"fat"`
	SNF          pointProto `json:"snf"`
	RateCardID   string     `json:"rate_card_id"`
	Rate         string     `json:"rate"`
	Currency     string     `json:"currency"`
	Amount       string     `json:"amount"`
	Explanation  string     `json:"explanation"`
	OriginKind   string     `json:"origin_kind"`
}

type collectionsResponse struct {
	Collections []collectionProto `json:"collections"`
}

// Collections reads a period's priced deliveries, paging until the period is
// exhausted or limit is reached.
//
// The period is half-open — from inclusive, to exclusive — as the caller
// resolved it, and is sent as dates because that is what the procedure reads.
func (p *Procurement) Collections(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]report.Collection, error) {
	opts := p.opts(ctx, tenantID)
	var out []report.Collection

	// `to` is exclusive here and inclusive at procurement, so the last day is
	// the day before it. A period of one day would otherwise ask for a range
	// that ends before it starts.
	last := to.Add(-24 * time.Hour)
	if last.Before(from) {
		last = from
	}

	for offset := int32(0); len(out) < limit; offset += page {
		resp, err := svcclient.Call[collectionsRequest, collectionsResponse](
			ctx, p.client, "/procurement.v1.ProcurementService/ListCollections",
			collectionsRequest{
				TenantID: tenantID,
				From:     from.UTC().Format("2006-01-02"),
				To:       last.UTC().Format("2006-01-02"),
				Limit:    page, Offset: offset,
			}, opts())
		if err != nil {
			return nil, fmt.Errorf("read the period's collections from procurement: %w", err)
		}
		if len(resp.Collections) == 0 {
			break
		}
		for _, c := range resp.Collections {
			out = append(out, report.Collection{
				ID: c.ID, ProducerRef: c.ProducerRef, SocietyCode: c.SocietyCode,
				CollectedOn: c.CollectedOn, Shift: c.Shift,
				Quantity: c.Quantity.Value, QuantityUnit: c.QuantityUnit,
				Fat: c.Fat.Value, SNF: c.SNF.Value,
				Rate: c.Rate, Currency: c.Currency, Amount: c.Amount,
				RateCardID: c.RateCardID, OriginKind: c.OriginKind,
				Explanation: c.Explanation,
			})
			if len(out) >= limit {
				break
			}
		}
		// A page shorter than asked for is the end of the period. Without this
		// the loop asks once more and relies on an empty reply, which costs a
		// round trip on every report.
		if len(resp.Collections) < page {
			break
		}
	}
	return out, nil
}

// Settlement reads what a cycle owes.
type Settlement struct {
	client *svcclient.Client
	opts   Options
}

func NewSettlement(c *svcclient.Client, opts Options) *Settlement {
	return &Settlement{client: c, opts: opts}
}

type cycleRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type cycleProto struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SocietyCode string `json:"society_code"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Status      string `json:"status"`
	Currency    string `json:"currency"`
}

type cycleResponse struct {
	Cycle cycleProto `json:"cycle"`
}

type payablesRequest struct {
	TenantID string `json:"tenant_id"`
	CycleID  string `json:"cycle_id"`
}

type payableProto struct {
	ProducerRef      string `json:"producer_ref"`
	Kind             string `json:"kind"`
	Currency         string `json:"currency"`
	Gross            string `json:"gross"`
	Deducted         string `json:"deducted"`
	Net              string `json:"net"`
	CarriedForward   string `json:"carried_forward"`
	Status           string `json:"status"`
	HeldReason       string `json:"held_reason"`
	PaymentReference string `json:"payment_reference"`
}

type payablesResponse struct {
	Payables []payableProto `json:"payables"`
	TotalNet string         `json:"total_net"`
	Currency string         `json:"currency"`
}

// Payables reads one cycle's payables and the cycle they belong to.
//
// The cycle is read first, and a failure to read it fails the report. A
// settlement summary headed with an empty period is one somebody will file
// beside another and be unable to tell apart.
func (s *Settlement) Payables(ctx context.Context, tenantID, cycleID string) ([]report.Payable, report.Cycle, error) {
	opts := s.opts(ctx, tenantID)

	cyc, err := svcclient.Call[cycleRequest, cycleResponse](
		ctx, s.client, "/settlement.v1.SettlementService/GetCycle",
		cycleRequest{TenantID: tenantID, ID: cycleID}, opts())
	if err != nil {
		return nil, report.Cycle{}, fmt.Errorf("read cycle %s from settlement: %w", cycleID, err)
	}

	resp, err := svcclient.Call[payablesRequest, payablesResponse](
		ctx, s.client, "/settlement.v1.SettlementService/ListPayables",
		payablesRequest{TenantID: tenantID, CycleID: cycleID}, opts())
	if err != nil {
		return nil, report.Cycle{}, fmt.Errorf("read cycle %s's payables from settlement: %w",
			cycleID, err)
	}

	out := make([]report.Payable, 0, len(resp.Payables))
	for _, p := range resp.Payables {
		out = append(out, report.Payable{
			ProducerRef: p.ProducerRef, Kind: p.Kind, Currency: p.Currency,
			Gross: p.Gross, Deducted: p.Deducted, Net: p.Net,
			CarriedForward: p.CarriedForward, Status: p.Status,
			HeldReason: p.HeldReason, PaymentReference: p.PaymentReference,
		})
	}
	return out, report.Cycle{
		ID: cyc.Cycle.ID, Name: cyc.Cycle.Name, SocietyCode: cyc.Cycle.SocietyCode,
		PeriodStart: cyc.Cycle.PeriodStart, PeriodEnd: cyc.Cycle.PeriodEnd,
		Status: cyc.Cycle.Status, Currency: resp.Currency, TotalNet: resp.TotalNet,
	}, nil
}

// Shadow reads where the two sides disagreed.
type Shadow struct {
	client *svcclient.Client
	opts   Options
}

func NewShadow(c *svcclient.Client, opts Options) *Shadow {
	return &Shadow{client: c, opts: opts}
}

type divergencesRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type divergenceProto struct {
	ID             string `json:"id"`
	ProducerRef    string `json:"producer_ref"`
	Currency       string `json:"currency"`
	Delta          string `json:"delta"`
	Classification string `json:"classification"`
	Rationale      string `json:"rationale"`
	Status         string `json:"status"`
	NeedsReview    bool   `json:"needs_review"`
	CreatedAt      string `json:"created_at"`
}

type divergencesResponse struct {
	Divergences []divergenceProto `json:"divergences"`
}

// Divergences reads the divergences raised in a period.
//
// ListDivergences filters by status, classification and producer but not by
// date, so the period is applied here on the way out. That is worth saying
// plainly: this reader asks for pages and keeps the rows that fall inside the
// window, which means a tenant with a long history pages through all of it to
// produce a report about last week.
//
// It is the honest arrangement available. Narrowing at the source would need a
// filter shadow-settlement does not serve, and a report that silently covered
// "the most recent five hundred" instead of the period asked for would be
// wrong in a way nothing on the page discloses.
func (s *Shadow) Divergences(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]report.Divergence, error) {
	opts := s.opts(ctx, tenantID)
	var out []report.Divergence

	for offset := int32(0); len(out) < limit; offset += page {
		resp, err := svcclient.Call[divergencesRequest, divergencesResponse](
			ctx, s.client, "/shadowsettlement.v1.ShadowSettlementService/ListDivergences",
			divergencesRequest{TenantID: tenantID, Limit: page, Offset: offset}, opts())
		if err != nil {
			return nil, fmt.Errorf("read divergences from shadow-settlement: %w", err)
		}
		if len(resp.Divergences) == 0 {
			break
		}
		for _, d := range resp.Divergences {
			raised, err := time.Parse(time.RFC3339, d.CreatedAt)
			if err != nil {
				// A row whose timestamp cannot be read is kept rather than
				// dropped. Dropping it would remove a divergence from a report
				// about the period it may well belong to, and a divergence
				// missing from a reconciliation report is the one thing this
				// report exists to show.
				out = append(out, toDivergence(d))
				continue
			}
			if raised.Before(from) || !raised.Before(to) {
				continue
			}
			out = append(out, toDivergence(d))
			if len(out) >= limit {
				break
			}
		}
		if len(resp.Divergences) < page {
			break
		}
	}
	return out, nil
}

func toDivergence(d divergenceProto) report.Divergence {
	return report.Divergence{
		ID: d.ID, ProducerRef: d.ProducerRef, Currency: d.Currency,
		Delta: d.Delta, Classification: d.Classification, Rationale: d.Rationale,
		Status: d.Status, NeedsReview: d.NeedsReview, CreatedAt: d.CreatedAt,
	}
}
