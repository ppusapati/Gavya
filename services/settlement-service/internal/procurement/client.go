// Package procurement reads a period's priced collections from the service that
// priced them.
//
// Settlement does not recompute what milk was worth. Two implementations of the
// same rate card would eventually disagree, and the disagreement would surface
// as a producer's statement differing from the slip they were handed at the
// collection centre — which is the one number in this platform a farmer checks.
//
// So this is a call rather than a shared query: the earnings come from
// procurement, as procurement recorded them, and settlement copies them.
package procurement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	serviceName    = "procurement.v1.ProcurementService"
	listCollection = "/" + serviceName + "/ListCollections"
	// page is how many collections are asked for at a time. A fortnight for a
	// two-hundred-member society is around six thousand rows, so this is
	// several requests and the paging has to actually work.
	page = 500
)

// Collection is one priced delivery, as much of it as settlement needs.
type Collection struct {
	ID          string
	ProducerRef string
	CollectedOn time.Time
	Shift       string

	Quantity     string
	QuantityUnit string
	Rate         string

	Amount money.Money
}

type Client struct {
	svc  *svcclient.Client
	opts func() svcclient.CallOptions
}

// New builds a reader over an already-configured client.
//
// The options are a function rather than a value because a request id belongs to
// one call, and a client that reused one across a whole settlement would make
// every log line in a six-thousand-row gather correlate to the same request.
func New(c *svcclient.Client, opts func() svcclient.CallOptions) *Client {
	return &Client{svc: c, opts: opts}
}

type listRequest struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref,omitempty"`
	From        string `json:"from"`
	To          string `json:"to"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

type pointProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

type collectionProto struct {
	ID          string `json:"id"`
	ProducerRef string `json:"producer_ref"`
	SocietyCode string `json:"society_code"`
	CollectedOn string `json:"collected_on"`
	Shift       string `json:"shift"`

	Quantity     pointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Rate         string     `json:"rate"`

	Currency         string `json:"currency"`
	AmountScale      int32  `json:"amount_scale"`
	AmountMinorUnits int64  `json:"amount_minor_units"`
}

type listResponse struct {
	Collections []collectionProto `json:"collections"`
}

// ErrNoCollections says a period held no milk at all.
var ErrNoCollections = errors.New("no collections were found in that period")

// ErrMilkBelongsToNoSociety names collections in the period that record no
// society at all.
//
// Skipping them quietly is the failure worth refusing over. A society's cycle
// settles its own milk, and milk belonging to no society is settled by nobody —
// so a producer whose three of ten deliveries were entered without a society
// code is paid for seven, and the statement they are handed looks complete.
// There is no figure on it that is wrong, only deliveries that are absent.
type ErrMilkBelongsToNoSociety struct {
	Count   int
	Example string
}

func (e *ErrMilkBelongsToNoSociety) Error() string {
	return fmt.Sprintf("%d collections in this period record no society (for example %s), "+
		"so a society's cycle would silently leave them out and nobody would be paid for "+
		"that milk; give them a society before settling", e.Count, e.Example)
}

// Collections returns everything a society collected over a period, inclusive of
// both ends.
//
// Whether a society_code narrows the query is deliberately not decided here.
// Procurement records a society on a collection but does not index by it, and a
// tenant that runs one society would get the same rows either way. The filtering
// happens on the way out, so a society settling its own fortnight cannot be
// handed another society's milk by a query that forgot to say which.
func (c *Client) Collections(ctx context.Context, tenantID, societyCode string, from, to time.Time) ([]Collection, error) {
	var out []Collection
	var orphaned int
	var orphanExample string
	for offset := int32(0); ; offset += page {
		resp, err := svcclient.Call[listRequest, listResponse](ctx, c.svc, listCollection,
			listRequest{
				TenantID: tenantID,
				From:     from.UTC().Format("2006-01-02"),
				To:       to.UTC().Format("2006-01-02"),
				Limit:    page, Offset: offset,
			}, c.opts())
		if err != nil {
			return nil, fmt.Errorf("read the period's collections from procurement: %w", err)
		}
		if len(resp.Collections) == 0 {
			break
		}
		for _, p := range resp.Collections {
			if societyCode != "" && p.SocietyCode != societyCode {
				// Another society's milk is skipped, which is the point of the
				// filter. Milk belonging to no society is not another society's
				// — it is nobody's, and is counted rather than skipped.
				if p.SocietyCode == "" {
					orphaned++
					if orphanExample == "" {
						orphanExample = p.ProducerRef + " on " + p.CollectedOn
					}
				}
				continue
			}
			col, err := convert(p)
			if err != nil {
				return nil, err
			}
			out = append(out, col)
		}
		if len(resp.Collections) < page {
			break
		}
	}
	if orphaned > 0 {
		return nil, &ErrMilkBelongsToNoSociety{Count: orphaned, Example: orphanExample}
	}
	if len(out) == 0 {
		return nil, ErrNoCollections
	}
	return out, nil
}

func convert(p collectionProto) (Collection, error) {
	when, err := time.Parse("2006-01-02", p.CollectedOn)
	if err != nil {
		return Collection{}, fmt.Errorf("collection %s: collected_on %q is not a date", p.ID, p.CollectedOn)
	}
	// Built from the minor units rather than by parsing the decimal string.
	// Both are on the wire and they are the same number; the integer is the one
	// that cannot be misread, and settlement adds thousands of these together.
	amount, err := money.New(p.AmountMinorUnits, p.AmountScale, p.Currency)
	if err != nil {
		return Collection{}, fmt.Errorf("collection %s: %w", p.ID, err)
	}
	return Collection{
		ID: p.ID, ProducerRef: p.ProducerRef,
		CollectedOn: when.UTC(), Shift: p.Shift,
		Quantity: p.Quantity.Value, QuantityUnit: p.QuantityUnit, Rate: p.Rate,
		Amount: amount,
	}, nil
}

// ---------------------------------------------------------------------------
// Explaining a figure: every version of a delivery, and the card it was priced by
// ---------------------------------------------------------------------------

const (
	collectionVersions = "/" + serviceName + "/GetCollectionVersions"
	getRateCard        = "/" + serviceName + "/GetRateCard"
)

// Version is one version of a priced delivery: what it came to, why, and
// whether it has since been restated.
type Version struct {
	ID          string
	ProducerRef string
	CollectedOn time.Time
	Shift       string

	Quantity     string
	QuantityUnit string
	Fat, SNF     string

	RateCardID string
	Rate       string
	Amount     money.Money

	// Explanation is procurement's own account of how the amount was arrived
	// at, written when it priced the delivery. Carried through verbatim: a
	// second explanation composed here would be a second thing to disagree.
	Explanation string

	OriginKind     string
	SourceSystemID string
	SourceRecordID string

	CreatedAt        time.Time
	SupersededAt     *time.Time
	SupersededBy     string
	Supersedes       string
	CorrectionReason string
}

// RateCard is as much of a rate card as an explanation needs: enough to name
// it and say what kind of pricing it was, not enough to re-price anything.
type RateCard struct {
	ID       string
	Name     string
	Kind     string
	Basis    string
	Currency string

	ValidFrom time.Time
	ValidTo   *time.Time
}

type byIDRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type versionProto struct {
	ID          string `json:"id"`
	ProducerRef string `json:"producer_ref"`
	CollectedOn string `json:"collected_on"`
	Shift       string `json:"shift"`

	Quantity     pointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          pointProto `json:"fat"`
	SNF          pointProto `json:"snf"`

	RateCardID string `json:"rate_card_id"`
	Rate       string `json:"rate"`

	Currency         string `json:"currency"`
	AmountScale      int32  `json:"amount_scale"`
	AmountMinorUnits int64  `json:"amount_minor_units"`

	Explanation string `json:"explanation"`

	OriginKind     string `json:"origin_kind"`
	SourceSystemID string `json:"source_system_id"`
	SourceRecordID string `json:"source_record_id"`
	CreatedAt      string `json:"created_at"`

	SupersededAt     string `json:"superseded_at"`
	SupersededBy     string `json:"superseded_by"`
	Supersedes       string `json:"supersedes"`
	CorrectionReason string `json:"correction_reason"`
}

type versionsResponse struct {
	Versions []versionProto `json:"versions"`
}

type rateCardProto struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Basis     string `json:"basis"`
	Currency  string `json:"currency"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
}

type rateCardResponse struct {
	RateCard *rateCardProto `json:"rate_card"`
}

// Versions returns every version of one delivery, oldest first.
//
// This is what makes a paid figure explainable after the delivery has been
// corrected. The cycle line holds what was paid; this holds what procurement
// says now and every step between, each with the reason it was restated.
func (c *Client) Versions(ctx context.Context, tenantID, collectionID string) ([]Version, error) {
	resp, err := svcclient.Call[byIDRequest, versionsResponse](ctx, c.svc, collectionVersions,
		byIDRequest{TenantID: tenantID, ID: collectionID}, c.opts())
	if err != nil {
		return nil, fmt.Errorf("read the versions of collection %s from procurement: %w", collectionID, err)
	}
	out := make([]Version, 0, len(resp.Versions))
	for _, p := range resp.Versions {
		v, err := convertVersion(p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// RateCard names the card a delivery was priced against.
func (c *Client) RateCard(ctx context.Context, tenantID, id string) (*RateCard, error) {
	resp, err := svcclient.Call[byIDRequest, rateCardResponse](ctx, c.svc, getRateCard,
		byIDRequest{TenantID: tenantID, ID: id}, c.opts())
	if err != nil {
		return nil, fmt.Errorf("read rate card %s from procurement: %w", id, err)
	}
	if resp.RateCard == nil {
		return nil, fmt.Errorf("procurement returned no rate card for %s", id)
	}
	p := resp.RateCard
	from, err := time.Parse(time.RFC3339, p.ValidFrom)
	if err != nil {
		return nil, fmt.Errorf("rate card %s: valid_from %q is not a time", id, p.ValidFrom)
	}
	card := &RateCard{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Basis: p.Basis, Currency: p.Currency,
		ValidFrom: from.UTC(),
	}
	if p.ValidTo != "" {
		to, err := time.Parse(time.RFC3339, p.ValidTo)
		if err != nil {
			return nil, fmt.Errorf("rate card %s: valid_to %q is not a time", id, p.ValidTo)
		}
		to = to.UTC()
		card.ValidTo = &to
	}
	return card, nil
}

func convertVersion(p versionProto) (Version, error) {
	when, err := time.Parse("2006-01-02", p.CollectedOn)
	if err != nil {
		return Version{}, fmt.Errorf("collection %s: collected_on %q is not a date", p.ID, p.CollectedOn)
	}
	amount, err := money.New(p.AmountMinorUnits, p.AmountScale, p.Currency)
	if err != nil {
		return Version{}, fmt.Errorf("collection %s: %w", p.ID, err)
	}
	v := Version{
		ID: p.ID, ProducerRef: p.ProducerRef, CollectedOn: when.UTC(), Shift: p.Shift,
		Quantity: p.Quantity.Value, QuantityUnit: p.QuantityUnit,
		Fat: p.Fat.Value, SNF: p.SNF.Value,
		RateCardID: p.RateCardID, Rate: p.Rate, Amount: amount,
		Explanation:    p.Explanation,
		OriginKind:     p.OriginKind,
		SourceSystemID: p.SourceSystemID, SourceRecordID: p.SourceRecordID,
		SupersededBy: p.SupersededBy, Supersedes: p.Supersedes,
		CorrectionReason: p.CorrectionReason,
	}
	if p.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, p.CreatedAt); err == nil {
			v.CreatedAt = t.UTC()
		}
	}
	if p.SupersededAt != "" {
		t, err := time.Parse(time.RFC3339, p.SupersededAt)
		if err != nil {
			return Version{}, fmt.Errorf("collection %s: superseded_at %q is not a time", p.ID, p.SupersededAt)
		}
		t = t.UTC()
		v.SupersededAt = &t
	}
	return v, nil
}
