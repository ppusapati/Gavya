package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/libs/integrity/ratecard"

	"github.com/ppusapati/gavya/services/procurement-service/internal/domain"
	"github.com/ppusapati/gavya/services/procurement-service/internal/repository"
	"github.com/ppusapati/gavya/services/procurement-service/internal/service"
)

const ServiceName = "procurement.v1.ProcurementService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("DeclareRateCard", connectjson.Unary(h.DeclareRateCard))
	route("GetRateCard", connectjson.Unary(h.GetRateCard))
	route("ListRateCards", connectjson.Unary(h.ListRateCards))
	route("GetRateCardInForce", connectjson.Unary(h.GetRateCardInForce))
	route("RecordCollection", connectjson.Unary(h.RecordCollection))
	route("GetCollection", connectjson.Unary(h.GetCollection))
	route("ListCollections", connectjson.Unary(h.ListCollections))
	route("CorrectCollection", connectjson.Unary(h.CorrectCollection))
	route("GetCollectionVersions", connectjson.Unary(h.GetCollectionVersions))
}

// PointProto is a measurement, as a decimal literal.
//
// A string rather than a number, all the way to the wire. A chart is
// equality-sensitive at its boundaries and 4.1 does not survive a round trip
// through a JSON number as 4.1 in every client.
type PointProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

func (p PointProto) point() (ratecard.Point, error) {
	if p.Value == "" {
		return ratecard.Point{}, nil
	}
	r, err := money.ParseRate(p.Value, p.Scale)
	if err != nil {
		return ratecard.Point{}, err
	}
	return ratecard.Point{Value: r.Numerator, Scale: r.Scale}, nil
}

func fromPoint(p ratecard.Point) PointProto {
	return PointProto{Value: p.String(), Scale: p.Scale}
}

type CellProto struct {
	Fat  PointProto `json:"fat"`
	SNF  PointProto `json:"snf"`
	Rate string     `json:"rate"`
	// RateScale is how many decimal places the rate is written to. A rate of
	// 42.5 at scale 4 and one at scale 1 are the same number and not the same
	// declaration: the first says the society prices to a hundredth of a paisa.
	RateScale int32 `json:"rate_scale"`
}

type TermProto struct {
	Component string `json:"component"`
	Rate      string `json:"rate"`
	RateScale int32  `json:"rate_scale"`
}

type RateCardProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`

	Kind        string `json:"kind"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`

	Basis         string `json:"basis,omitempty"`
	BetweenPoints string `json:"between_points,omitempty"`
	OutsideChart  string `json:"outside_chart,omitempty"`
	Rounding      string `json:"rounding"`

	Cells []CellProto `json:"cells,omitempty"`
	Terms []TermProto `json:"terms,omitempty"`

	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`
}

type DeclareRateCardRequest struct {
	RateCardProto
	Actor string `json:"actor"`
}

type DeclareRateCardResponse struct {
	RateCard *RateCardProto `json:"rate_card"`
}

// DeclareRateCard records what a society's milk is worth.
func (h *Handler) DeclareRateCard(ctx context.Context, req *connect.Request[DeclareRateCardRequest]) (*connect.Response[DeclareRateCardResponse], error) {
	card, err := toCard(&req.Msg.RateCardProto)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	saved, err := h.svc.DeclareRateCard(ctx, card, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&DeclareRateCardResponse{RateCard: fromCard(saved)}), nil
}

type GetRateCardRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type GetRateCardResponse struct {
	RateCard *RateCardProto `json:"rate_card"`
}

func (h *Handler) GetRateCard(ctx context.Context, req *connect.Request[GetRateCardRequest]) (*connect.Response[GetRateCardResponse], error) {
	card, err := h.svc.GetRateCard(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetRateCardResponse{RateCard: fromCard(card)}), nil
}

type GetRateCardInForceRequest struct {
	TenantID string `json:"tenant_id"`
	// At is the day milk was collected, not the day the question is asked.
	At string `json:"at"`
}

func (h *Handler) GetRateCardInForce(ctx context.Context, req *connect.Request[GetRateCardInForceRequest]) (*connect.Response[GetRateCardResponse], error) {
	at, err := parseTime(req.Msg.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	card, err := h.svc.CardFor(ctx, req.Msg.TenantID, at)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetRateCardResponse{RateCard: fromCard(card)}), nil
}

type ListRateCardsRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit,omitempty"`
	Offset   int32  `json:"offset,omitempty"`
}

type ListRateCardsResponse struct {
	RateCards []*RateCardProto `json:"rate_cards"`
}

func (h *Handler) ListRateCards(ctx context.Context, req *connect.Request[ListRateCardsRequest]) (*connect.Response[ListRateCardsResponse], error) {
	cards, err := h.svc.ListRateCards(ctx, req.Msg.TenantID, int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*RateCardProto, 0, len(cards))
	for _, c := range cards {
		out = append(out, fromCard(c))
	}
	return connect.NewResponse(&ListRateCardsResponse{RateCards: out}), nil
}

type RecordCollectionRequest struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref"`
	SocietyCode string `json:"society_code,omitempty"`

	CollectedOn string `json:"collected_on"`
	Shift       string `json:"shift"`

	Quantity     PointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`

	Fat   PointProto `json:"fat,omitempty"`
	SNF   PointProto `json:"snf,omitempty"`
	FatKg PointProto `json:"fat_kg,omitempty"`
	SNFKg PointProto `json:"snf_kg,omitempty"`

	OriginKind     string `json:"origin_kind,omitempty"`
	SourceSystemID string `json:"source_system_id,omitempty"`
	ImportBatchID  string `json:"import_batch_id,omitempty"`
	SourceRecordID string `json:"source_record_id,omitempty"`

	Actor string `json:"actor"`
}

type PricedCollectionProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref"`
	SocietyCode string `json:"society_code,omitempty"`
	CollectedOn string `json:"collected_on"`
	Shift       string `json:"shift"`

	Quantity     PointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          PointProto `json:"fat,omitempty"`
	SNF          PointProto `json:"snf,omitempty"`

	RateCardID string `json:"rate_card_id"`
	Rate       string `json:"rate,omitempty"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	Amount      string `json:"amount"`
	// AmountMinorUnits is the same number as an integer, so a client that must
	// not parse decimals does not have to.
	AmountMinorUnits int64 `json:"amount_minor_units"`

	Explanation string `json:"explanation"`

	OriginKind     string `json:"origin_kind"`
	SourceRecordID string `json:"source_record_id,omitempty"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`

	// Corrections. A row carrying superseded_at is a version that has since been
	// restated; one carrying supersedes is the restatement.
	SupersededAt     string `json:"superseded_at,omitempty"`
	SupersededBy     string `json:"superseded_by,omitempty"`
	Supersedes       string `json:"supersedes,omitempty"`
	CorrectionReason string `json:"correction_reason,omitempty"`
}

type RecordCollectionResponse struct {
	Collection *PricedCollectionProto `json:"collection"`
}

// RecordCollection prices one delivery against the card in force when it was
// collected.
func (h *Handler) RecordCollection(ctx context.Context, req *connect.Request[RecordCollectionRequest]) (*connect.Response[RecordCollectionResponse], error) {
	m := req.Msg
	when, err := parseTime(m.CollectedOn, "collected_on")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	in := domain.Collection{
		TenantID: m.TenantID, ProducerRef: m.ProducerRef, SocietyCode: m.SocietyCode,
		CollectedOn: when, Shift: domain.Shift(m.Shift),
		Unit:           ratecard.Basis(m.QuantityUnit),
		Origin:         origin.Origin{Kind: origin.Kind(m.OriginKind)},
		SourceSystemID: m.SourceSystemID, ImportBatchID: m.ImportBatchID,
		SourceRecordID: m.SourceRecordID, Actor: m.Actor,
	}
	for _, f := range []struct {
		in  PointProto
		out *ratecard.Point
		who string
	}{
		{m.Quantity, &in.Quantity, "quantity"},
		{m.Fat, &in.Fat, "fat"},
		{m.SNF, &in.SNF, "snf"},
		{m.FatKg, &in.FatKg, "fat_kg"},
		{m.SNFKg, &in.SNFKg, "snf_kg"},
	} {
		p, err := f.in.point()
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New(f.who+": "+err.Error()))
		}
		*f.out = p
	}

	saved, err := h.svc.RecordCollection(ctx, in)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&RecordCollectionResponse{Collection: fromCollection(saved)}), nil
}

type GetCollectionRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type GetCollectionResponse struct {
	Collection *PricedCollectionProto `json:"collection"`
}

func (h *Handler) GetCollection(ctx context.Context, req *connect.Request[GetCollectionRequest]) (*connect.Response[GetCollectionResponse], error) {
	c, err := h.svc.GetCollection(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetCollectionResponse{Collection: fromCollection(c)}), nil
}

type ListCollectionsRequest struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref,omitempty"`
	From        string `json:"from"`
	To          string `json:"to,omitempty"`
	Limit       int32  `json:"limit,omitempty"`
	Offset      int32  `json:"offset,omitempty"`
	// IncludeSuperseded adds the versions that have since been corrected. Off by
	// default, because a caller totalling a fortnight wants each delivery once.
	IncludeSuperseded bool `json:"include_superseded,omitempty"`
}

type ListCollectionsResponse struct {
	Collections []*PricedCollectionProto `json:"collections"`
	// Total is what these collections come to, so a caller does not have to add
	// decimal strings to find out.
	Total           string `json:"total"`
	TotalMinorUnits int64  `json:"total_minor_units"`
	Currency        string `json:"currency,omitempty"`
	AmountScale     int32  `json:"amount_scale,omitempty"`
}

func (h *Handler) ListCollections(ctx context.Context, req *connect.Request[ListCollectionsRequest]) (*connect.Response[ListCollectionsResponse], error) {
	m := req.Msg
	from, err := parseTime(m.From, "from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var to time.Time
	if m.To != "" {
		if to, err = parseTime(m.To, "to"); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	list, err := h.svc.ListCollections(ctx, m.TenantID, m.ProducerRef, from, to,
		int(m.Limit), int(m.Offset), m.IncludeSuperseded)
	if err != nil {
		return nil, classify(err)
	}

	out := &ListCollectionsResponse{Collections: make([]*PricedCollectionProto, 0, len(list))}
	var total money.Money
	for i, c := range list {
		out.Collections = append(out.Collections, fromCollection(c))
		if i == 0 {
			total = money.Zero(c.Amount.Scale, c.Amount.Currency)
		}
		total, err = money.Add(total, c.Amount)
		if err != nil {
			// Amounts in two currencies in one list is not something to sum
			// past. It means a tenant's currency changed mid-period, which is a
			// question for a person.
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
	}
	if len(list) > 0 {
		out.Total = total.String()
		out.TotalMinorUnits = total.Value
		out.Currency = total.Currency
		out.AmountScale = total.Scale
	}
	return connect.NewResponse(out), nil
}

// -------------------------------------------------------------------------

func toCard(p *RateCardProto) (*ratecard.Card, error) {
	from, err := parseTime(p.ValidFrom, "valid_from")
	if err != nil {
		return nil, err
	}
	c := &ratecard.Card{
		TenantID: p.TenantID, Name: p.Name,
		Kind: ratecard.Kind(p.Kind), Currency: p.Currency, Scale: p.AmountScale,
		Basis: ratecard.Basis(p.Basis), Between: ratecard.Between(p.BetweenPoints),
		Outside: ratecard.Outside(p.OutsideChart), Rounding: money.RoundingMode(p.Rounding),
		ValidFrom: from,
	}
	if p.ValidTo != "" {
		to, err := parseTime(p.ValidTo, "valid_to")
		if err != nil {
			return nil, err
		}
		c.ValidTo = &to
	}

	for i, cell := range p.Cells {
		fat, err := cell.Fat.point()
		if err != nil {
			return nil, errors.New("cell " + itoa(i) + " fat: " + err.Error())
		}
		snf, err := cell.SNF.point()
		if err != nil {
			return nil, errors.New("cell " + itoa(i) + " snf: " + err.Error())
		}
		rate, err := money.ParseRate(cell.Rate, cell.RateScale)
		if err != nil {
			return nil, errors.New("cell " + itoa(i) + " rate: " + err.Error())
		}
		c.Cells = append(c.Cells, ratecard.Cell{Fat: fat, SNF: snf, Rate: rate})
	}
	for i, t := range p.Terms {
		rate, err := money.ParseRate(t.Rate, t.RateScale)
		if err != nil {
			return nil, errors.New("term " + itoa(i) + " rate: " + err.Error())
		}
		c.Terms = append(c.Terms, ratecard.Term{Component: t.Component, Rate: rate})
	}
	return c, nil
}

func fromCard(c *ratecard.Card) *RateCardProto {
	p := &RateCardProto{
		ID: c.ID, TenantID: c.TenantID, Name: c.Name,
		Kind: string(c.Kind), Currency: c.Currency, AmountScale: c.Scale,
		Basis: string(c.Basis), BetweenPoints: string(c.Between),
		OutsideChart: string(c.Outside), Rounding: string(c.Rounding),
		ValidFrom: c.ValidFrom.UTC().Format(time.RFC3339),
	}
	if c.ValidTo != nil {
		p.ValidTo = c.ValidTo.UTC().Format(time.RFC3339)
	}
	for _, cell := range c.Cells {
		p.Cells = append(p.Cells, CellProto{
			Fat: fromPoint(cell.Fat), SNF: fromPoint(cell.SNF),
			Rate: cell.Rate.String(), RateScale: cell.Rate.Scale,
		})
	}
	for _, t := range c.Terms {
		p.Terms = append(p.Terms, TermProto{
			Component: t.Component, Rate: t.Rate.String(), RateScale: t.Rate.Scale,
		})
	}
	return p
}

func fromCollection(c *domain.PricedCollection) *PricedCollectionProto {
	p := &PricedCollectionProto{
		ID: c.ID, TenantID: c.TenantID, ProducerRef: c.ProducerRef, SocietyCode: c.SocietyCode,
		CollectedOn: c.CollectedOn.UTC().Format("2006-01-02"), Shift: string(c.Shift),
		Quantity: fromPoint(c.Quantity), QuantityUnit: string(c.Unit),
		Fat: fromPoint(c.Fat), SNF: fromPoint(c.SNF),
		RateCardID: c.RateCardID,
		Currency:   c.Amount.Currency, AmountScale: c.Amount.Scale,
		Amount: c.Amount.String(), AmountMinorUnits: c.Amount.Value,
		Explanation: c.Explanation,
		OriginKind:  string(c.Origin.Kind), SourceRecordID: c.SourceRecordID,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339), CreatedBy: c.CreatedBy,
	}
	if c.Rate.Numerator != 0 {
		p.Rate = c.Rate.String()
	}
	if c.SupersededAt != nil {
		p.SupersededAt = c.SupersededAt.UTC().Format(time.RFC3339)
	}
	p.SupersededBy, p.Supersedes = c.SupersededBy, c.Supersedes
	p.CorrectionReason = c.CorrectionReason
	return p
}

// parseTime accepts a date or a full timestamp.
//
// A society entering yesterday's slips writes a date; a device reporting in real
// time sends an instant. Refusing one of them would push a format decision onto
// whoever is typing.
func parseTime(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New(field + ": " + s + " is neither a date nor a timestamp")
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal leaves a caller unable to tell a card that
// has not decided something from a database that is down, and makes the first
// look like the platform's fault when it is a question for the society.
func classify(err error) error {
	switch {
	case errors.Is(err, ratecard.ErrUnknownBasis), errors.Is(err, ratecard.ErrUnknownUnit):
		// A unit this platform does not know. Refused rather than read as
		// litres, which is what happened before: milk is about 1.03 kilograms
		// to the litre, and three per cent of what a producer is paid is larger
		// than most divergences this platform exists to find.
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrOverlappingCard),
		errors.Is(err, repository.ErrDuplicateCollection):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, domain.ErrNoCard):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNoProducer), errors.Is(err, domain.ErrNoQuantity),
		errors.Is(err, domain.ErrNoShift), errors.Is(err, domain.ErrNoCorrectionReason):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, domain.ErrAlreadySuperseded), errors.Is(err, domain.ErrNothingChanged):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// A card that has not decided, or a reading off the chart: the caller's
	// data or the society's policy, not the platform.
	var offChart *ratecard.ErrOffChart
	var notOnPoint *ratecard.ErrNotOnAPoint
	if errors.As(err, &offChart) || errors.As(err, &notOnPoint) ||
		errors.Is(err, ratecard.ErrNoKind) || errors.Is(err, ratecard.ErrNoBasis) ||
		errors.Is(err, ratecard.ErrNoBetween) || errors.Is(err, ratecard.ErrNoOutside) ||
		errors.Is(err, ratecard.ErrEmptyChart) || errors.Is(err, ratecard.ErrNoTerms) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

type CorrectCollectionRequest struct {
	TenantID string `json:"tenant_id"`
	// ID is the collection being corrected, which must be the current version.
	ID string `json:"id"`

	Quantity     PointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          PointProto `json:"fat,omitempty"`
	SNF          PointProto `json:"snf,omitempty"`
	FatKg        PointProto `json:"fat_kg,omitempty"`
	SNFKg        PointProto `json:"snf_kg,omitempty"`

	// Reason is required, and it goes on the producer's record.
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

type CorrectCollectionResponse struct {
	Collection *PricedCollectionProto `json:"collection"`
	// Supersedes is the id of the version this replaced, so a caller can fetch
	// what the figure was before without searching for it.
	Supersedes string `json:"supersedes"`
}

// CorrectCollection restates a delivery recorded wrongly.
//
// The producer, the day and the shift are not in this request on purpose. They
// identify which delivery is being talked about, and a request that could
// change them would let one member's milk be moved to another under a field
// marked "reason".
func (h *Handler) CorrectCollection(ctx context.Context, req *connect.Request[CorrectCollectionRequest]) (*connect.Response[CorrectCollectionResponse], error) {
	m := req.Msg
	in := domain.Correction{
		TenantID: m.TenantID, ID: m.ID,
		Unit:   ratecard.Basis(m.QuantityUnit),
		Reason: m.Reason, Actor: m.Actor,
	}
	for _, f := range []struct {
		in  PointProto
		out *ratecard.Point
		who string
	}{
		{m.Quantity, &in.Quantity, "quantity"},
		{m.Fat, &in.Fat, "fat"},
		{m.SNF, &in.SNF, "snf"},
		{m.FatKg, &in.FatKg, "fat_kg"},
		{m.SNFKg, &in.SNFKg, "snf_kg"},
	} {
		p, err := f.in.point()
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New(f.who+": "+err.Error()))
		}
		*f.out = p
	}

	saved, err := h.svc.CorrectCollection(ctx, in)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CorrectCollectionResponse{
		Collection: fromCollection(saved), Supersedes: saved.Supersedes,
	}), nil
}

type GetCollectionVersionsRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type GetCollectionVersionsResponse struct {
	// Versions are every version of the delivery, oldest first. The last is the
	// one in force unless it too has been superseded.
	Versions []*PricedCollectionProto `json:"versions"`
}

// GetCollectionVersions is what a producer disputing a figure is shown: every
// version of their delivery, what each came to, and why it changed.
func (h *Handler) GetCollectionVersions(ctx context.Context, req *connect.Request[GetCollectionVersionsRequest]) (*connect.Response[GetCollectionVersionsResponse], error) {
	list, err := h.svc.Versions(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*PricedCollectionProto, 0, len(list))
	for _, c := range list {
		out = append(out, fromCollection(c))
	}
	return connect.NewResponse(&GetCollectionVersionsResponse{Versions: out}), nil
}
