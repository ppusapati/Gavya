package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"

	"github.com/ppusapati/gavya/services/production-service/internal/domain"
	"github.com/ppusapati/gavya/services/production-service/internal/repository"
	"github.com/ppusapati/gavya/services/production-service/internal/service"
)

const ServiceName = "production.v1.ProductionService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("CreateBatch", connectjson.Unary(h.CreateBatch))
	route("GetBatch", connectjson.Unary(h.GetBatch))
	route("ListBatches", connectjson.Unary(h.ListBatches))
	route("SetBatchStatus", connectjson.Unary(h.SetBatchStatus))

	route("RecordInput", connectjson.Unary(h.RecordInput))
	route("GetBatchGenealogy", connectjson.Unary(h.GetBatchGenealogy))
	route("TraceBatch", connectjson.Unary(h.TraceBatch))
	route("GetBatchYield", connectjson.Unary(h.GetBatchYield))

	route("CreateFormulation", connectjson.Unary(h.CreateFormulation))
	route("ApproveFormulation", connectjson.Unary(h.ApproveFormulation))
	route("WithdrawFormulation", connectjson.Unary(h.WithdrawFormulation))
	route("GetFormulation", connectjson.Unary(h.GetFormulation))
	route("ListFormulations", connectjson.Unary(h.ListFormulations))
	route("CheckRecipe", connectjson.Unary(h.CheckRecipe))
	route("GetObservedYield", connectjson.Unary(h.GetObservedYield))
}

// QuantityProto is a measured amount and what it is measured in.
//
// The unit is never optional and never inferred. Litres and kilograms differ by
// about three per cent, which is larger than most of the margins in this
// business.
type QuantityProto struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func (q QuantityProto) quantity(field string) (quantity.Quantity, error) {
	if q.Value == "" {
		return quantity.Quantity{}, errors.New(field + " must have a value")
	}
	if !quantity.ValidUnit(quantity.Unit(q.Unit)) {
		return quantity.Quantity{}, fmt.Errorf("%s: %w", field, quantity.ErrNoUnit)
	}
	return quantity.Parse(q.Value, quantity.Unit(q.Unit))
}

func fromQuantity(q quantity.Quantity) QuantityProto {
	return QuantityProto{Value: q.String(), Unit: string(q.Unit)}
}

// DensityProto is the same shape material-service accepts, down to the field
// names. One vocabulary for one idea: two spellings of a density is how a
// figure crosses a service boundary and loses the scale it was written to.
type DensityProto struct {
	// KgPerLitre is the conversion factor as a decimal literal.
	KgPerLitre string `json:"kg_per_litre"`
	Scale      int32  `json:"scale"`
	// AtCelsius is in tenths of a degree, so 4.0 degrees is 40. An integer
	// because a temperature with a decimal point in a JSON number is the same
	// float problem every quantity here avoids.
	AtCelsius int32 `json:"at_celsius"`
	// Source is LACTOMETER, ANALYSED or DECLARED.
	Source string `json:"source"`
}

func (d *DensityProto) density() (*quantity.Density, error) {
	if d == nil {
		return nil, nil
	}
	r, err := money.ParseRate(d.KgPerLitre, d.Scale)
	if err != nil {
		return nil, errors.New("kg_per_litre: " + err.Error())
	}
	out := quantity.Density{
		KgPerLitre: r, AtCelsius: d.AtCelsius,
		Source: quantity.DensitySource(d.Source),
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Batches
// ---------------------------------------------------------------------------

type BatchProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`

	Kind       string `json:"kind"`
	ProductRef string `json:"product_ref"`

	Produced   QuantityProto `json:"produced"`
	ProducedAt string        `json:"produced_at"`
	ProducedBy string        `json:"produced_by"`

	SourceKind string `json:"source_kind,omitempty"`
	SourceRef  string `json:"source_ref,omitempty"`

	// FormulationID is the exact version of the recipe this batch followed. The
	// expectation lives there, not here: it is a property of the recipe, and a
	// figure typed per vat drifts.
	FormulationID string `json:"formulation_id,omitempty"`

	Status       string `json:"status"`
	StatusReason string `json:"status_reason,omitempty"`
	// Held says whether this lot may be fed into anything. Carried explicitly
	// rather than left for the caller to derive from the status, because a
	// caller that derives it wrongly ships quarantined milk.
	Held bool `json:"held"`
}

type CreateBatchRequest struct {
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`

	Kind       string `json:"kind"`
	ProductRef string `json:"product_ref"`

	Produced   QuantityProto `json:"produced"`
	ProducedAt string        `json:"produced_at"`
	ProducedBy string        `json:"produced_by"`

	// Where raw milk came from. Required on a RAW batch and refused on any
	// other, because a batch made from other batches already says where it came
	// from and two accounts of that would not reconcile.
	SourceKind string `json:"source_kind,omitempty"`
	SourceRef  string `json:"source_ref,omitempty"`

	// FormulationID names the exact version of the recipe this batch followed.
	// Optional: a plant that has not written its recipes down still records what
	// it made. Where it is given the recipe has to be for this product and has
	// to have been in force on the day, both refused by the database.
	FormulationID string `json:"formulation_id,omitempty"`
	// FormulationCode is the alternative, resolved to the version in force at
	// produced_at. That resolution is why produced_at is required beside it: a
	// recipe is several versions and which one applies depends on the day.
	FormulationCode string `json:"formulation_code,omitempty"`

	// Status defaults to OPEN. A batch created straight into a hold has to say
	// why, like any other hold.
	Status       string `json:"status,omitempty"`
	StatusReason string `json:"status_reason,omitempty"`

	Actor string `json:"actor"`
}

type BatchResponse struct {
	Batch *BatchProto `json:"batch"`
}

func (h *Handler) CreateBatch(ctx context.Context, req *connect.Request[CreateBatchRequest]) (*connect.Response[BatchResponse], error) {
	m := req.Msg
	produced, err := m.Produced.quantity("produced")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	producedAt, err := parseTime(m.ProducedAt, "produced_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	status := domain.Status(m.Status)
	if m.Status == "" {
		status = domain.Open
	}

	// A code is resolved to the version in force when the batch was made, which
	// is the whole reason recipes are versioned. A caller that names an id
	// instead has already chosen a version and gets that one.
	formulationID := m.FormulationID
	if formulationID == "" && m.FormulationCode != "" {
		f, err := h.svc.FormulationInForce(ctx, m.TenantID, m.FormulationCode, producedAt)
		if err != nil {
			return nil, classify(fmt.Errorf(
				"no version of recipe %s was in force at %s: %w",
				m.FormulationCode, producedAt.UTC().Format(time.RFC3339), err))
		}
		formulationID = f.ID
	}

	b, err := h.svc.CreateBatch(ctx, &domain.Batch{
		TenantID: m.TenantID, Code: m.Code,
		Kind: domain.Kind(m.Kind), ProductRef: m.ProductRef,
		Produced: produced, ProducedAt: producedAt, ProducedBy: m.ProducedBy,
		SourceKind: domain.SourceKind(m.SourceKind), SourceRef: m.SourceRef,
		FormulationID: formulationID,
		Status:        status, StatusReason: m.StatusReason,
		CreatedBy: m.Actor,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BatchResponse{Batch: fromBatch(b)}), nil
}

type GetBatchRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	// Code is what a person reads off the vessel. Either identifies the batch;
	// the code is what somebody standing in a plant actually has.
	Code string `json:"code,omitempty"`
}

func (h *Handler) GetBatch(ctx context.Context, req *connect.Request[GetBatchRequest]) (*connect.Response[BatchResponse], error) {
	b, err := h.lookup(ctx, req.Msg.TenantID, req.Msg.ID, req.Msg.Code)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BatchResponse{Batch: fromBatch(b)}), nil
}

func (h *Handler) lookup(ctx context.Context, tenantID, id, code string) (*domain.Batch, error) {
	switch {
	case id != "":
		return h.svc.GetBatch(ctx, tenantID, id)
	case code != "":
		return h.svc.GetBatchByCode(ctx, tenantID, code)
	}
	return nil, errors.New("name the batch by id or by the code written on it")
}

type ListBatchesRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
}

type ListBatchesResponse struct {
	Batches []*BatchProto `json:"batches"`
}

func (h *Handler) ListBatches(ctx context.Context, req *connect.Request[ListBatchesRequest]) (*connect.Response[ListBatchesResponse], error) {
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
	list, err := h.svc.ListBatches(ctx, m.TenantID, from, to, int(m.Limit))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*BatchProto, 0, len(list))
	for _, b := range list {
		out = append(out, fromBatch(b))
	}
	return connect.NewResponse(&ListBatchesResponse{Batches: out}), nil
}

type SetBatchStatusRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`

	Status string `json:"status"`
	// Reason is required to put a batch under hold, and required to lift one.
	// A quarantine nobody can explain gets lifted by whoever is on shift; a
	// quarantine lifted with no explanation is one nobody can defend afterwards.
	Reason string `json:"reason,omitempty"`
	Actor  string `json:"actor"`
}

func (h *Handler) SetBatchStatus(ctx context.Context, req *connect.Request[SetBatchStatusRequest]) (*connect.Response[BatchResponse], error) {
	m := req.Msg
	b, err := h.lookup(ctx, m.TenantID, m.ID, m.Code)
	if err != nil {
		return nil, classify(err)
	}
	out, err := h.svc.SetStatus(ctx, m.TenantID, b.ID, domain.Status(m.Status), m.Reason, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BatchResponse{Batch: fromBatch(out)}), nil
}

// ---------------------------------------------------------------------------
// Inputs and genealogy
// ---------------------------------------------------------------------------

type RecordInputRequest struct {
	TenantID string `json:"tenant_id"`
	// The batch being made.
	OutputBatchID string `json:"output_batch_id,omitempty"`
	OutputCode    string `json:"output_code,omitempty"`
	// The lot going into it.
	InputBatchID string `json:"input_batch_id,omitempty"`
	InputCode    string `json:"input_code,omitempty"`

	Consumed QuantityProto `json:"consumed"`
	Actor    string        `json:"actor"`
}

type InputProto struct {
	ID            string        `json:"id"`
	OutputBatchID string        `json:"output_batch_id"`
	InputBatchID  string        `json:"input_batch_id"`
	InputCode     string        `json:"input_code,omitempty"`
	Consumed      QuantityProto `json:"consumed"`
}

type InputResponse struct {
	Input *InputProto `json:"input"`
	// Remaining is what the consumed lot has left afterwards. Returned because
	// the next question anybody asks is whether there is more of it.
	Remaining QuantityProto `json:"remaining"`
}

func (h *Handler) RecordInput(ctx context.Context, req *connect.Request[RecordInputRequest]) (*connect.Response[InputResponse], error) {
	m := req.Msg
	consumed, err := m.Consumed.quantity("consumed")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out, err := h.lookup(ctx, m.TenantID, m.OutputBatchID, m.OutputCode)
	if err != nil {
		return nil, classify(fmt.Errorf("the batch being made: %w", err))
	}
	in, err := h.lookup(ctx, m.TenantID, m.InputBatchID, m.InputCode)
	if err != nil {
		return nil, classify(fmt.Errorf("the lot going in: %w", err))
	}

	line, err := h.svc.RecordInput(ctx, &domain.Input{
		TenantID: m.TenantID, OutputBatchID: out.ID, InputBatchID: in.ID,
		Consumed: consumed, CreatedBy: m.Actor,
	}, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	remaining, err := h.svc.Remaining(ctx, m.TenantID, in.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InputResponse{
		Input: &InputProto{
			ID: line.ID, OutputBatchID: line.OutputBatchID, InputBatchID: line.InputBatchID,
			InputCode: in.Code, Consumed: fromQuantity(line.Consumed),
		},
		Remaining: fromQuantity(remaining),
	}), nil
}

type GetBatchGenealogyRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
}

type GetBatchGenealogyResponse struct {
	Batch  *BatchProto   `json:"batch"`
	Inputs []*InputProto `json:"inputs"`
	// Remaining is what this batch has left after everything drawn from it.
	Remaining QuantityProto `json:"remaining"`
}

// GetBatchGenealogy is one hop: what went directly into this batch. The whole
// tree is TraceBatch.
func (h *Handler) GetBatchGenealogy(ctx context.Context, req *connect.Request[GetBatchGenealogyRequest]) (*connect.Response[GetBatchGenealogyResponse], error) {
	m := req.Msg
	b, err := h.lookup(ctx, m.TenantID, m.ID, m.Code)
	if err != nil {
		return nil, classify(err)
	}
	inputs, err := h.svc.Inputs(ctx, m.TenantID, b.ID)
	if err != nil {
		return nil, classify(err)
	}
	remaining, err := h.svc.Remaining(ctx, m.TenantID, b.ID)
	if err != nil {
		return nil, classify(err)
	}
	out := &GetBatchGenealogyResponse{
		Batch: fromBatch(b), Inputs: make([]*InputProto, 0, len(inputs)),
		Remaining: fromQuantity(remaining),
	}
	for _, in := range inputs {
		p := &InputProto{
			ID: in.ID, OutputBatchID: in.OutputBatchID, InputBatchID: in.InputBatchID,
			Consumed: fromQuantity(in.Consumed),
		}
		if src, err := h.svc.GetBatch(ctx, m.TenantID, in.InputBatchID); err == nil {
			p.InputCode = src.Code
		}
		out.Inputs = append(out.Inputs, p)
	}
	return connect.NewResponse(out), nil
}

type TraceBatchRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`

	// Direction is FORWARD into what was made from this batch, or BACKWARD into
	// what it was made from. Required: the two answer different questions and
	// picking one for the caller would answer the wrong one silently.
	Direction string `json:"direction"`

	MaxDepth   int32 `json:"max_depth,omitempty"`
	MaxBatches int32 `json:"max_batches,omitempty"`
}

type AffectedProto struct {
	Batch *BatchProto `json:"batch"`
	// Depth is how many process steps from the origin, by the shortest route.
	Depth int32 `json:"depth"`
	// Via is a batch one step nearer the origin, so the connection can be
	// explained to whoever is asked to act on it.
	Via string `json:"via,omitempty"`
}

type TraceBatchResponse struct {
	Batch     *BatchProto `json:"batch"`
	Direction string      `json:"direction"`

	Affected []AffectedProto `json:"affected"`
	// Finished is the subset that left the plant. This is the list a recall is
	// actually run to produce.
	Finished []AffectedProto `json:"finished"`

	// Complete is false when the walk stopped at a bound or could not read a
	// batch it reached. A caller that ignores this field and acts on the list is
	// recalling some of what it should.
	Complete bool `json:"complete"`
	// Frontier is where somebody continuing the walk by hand starts.
	Frontier []string `json:"frontier,omitempty"`
	// Warning is the same thing in words, ready to print at the top of a report.
	Warning string `json:"warning,omitempty"`
	// Unreadable names batches the walk reached and could not load.
	Unreadable []string `json:"unreadable,omitempty"`
}

// TraceBatch walks the genealogy and reports what it found, including whether
// it finished.
func (h *Handler) TraceBatch(ctx context.Context, req *connect.Request[TraceBatchRequest]) (*connect.Response[TraceBatchResponse], error) {
	m := req.Msg
	if !domain.ValidDirection(domain.Direction(m.Direction)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(
			"direction must be FORWARD, into what was made from this batch, or BACKWARD, into "+
				"what it was made from"))
	}
	b, err := h.lookup(ctx, m.TenantID, m.ID, m.Code)
	if err != nil {
		return nil, classify(err)
	}

	var lim domain.Limit
	if m.MaxDepth > 0 || m.MaxBatches > 0 {
		lim = domain.Limit{MaxDepth: int(m.MaxDepth), MaxBatches: int(m.MaxBatches)}
		if lim.MaxDepth <= 0 || lim.MaxBatches <= 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(
				"give both max_depth and max_batches or neither; one bound without the other "+
					"leaves the walk unbounded in the direction nobody thought about"))
		}
	}

	r, err := h.svc.Trace(ctx, m.TenantID, b.ID, domain.Direction(m.Direction), lim)
	if err != nil {
		return nil, classify(err)
	}
	out := &TraceBatchResponse{
		Batch: fromBatch(r.Origin), Direction: string(r.Direction),
		Affected: make([]AffectedProto, 0, len(r.Affected)),
		Finished: make([]AffectedProto, 0, len(r.Finished)),
		Complete: r.Complete, Frontier: r.Frontier,
		Warning: r.StoppedBecause, Unreadable: r.Unreadable,
	}
	for _, a := range r.Affected {
		out.Affected = append(out.Affected, fromAffected(a))
	}
	for _, a := range r.Finished {
		out.Finished = append(out.Finished, fromAffected(a))
	}
	return connect.NewResponse(out), nil
}

// ---------------------------------------------------------------------------
// Yield
// ---------------------------------------------------------------------------

type GetBatchYieldRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`

	// A density, where the inputs and the output are not in the same unit.
	// Supplied by the caller, never assumed: a batch weighed in kilograms made
	// from milk measured in litres has no yield without one, and the 1.03
	// everybody quotes is a figure nobody measured for this milk.
	//
	// The same shape material-service uses, deliberately. A platform with two
	// spellings of density is one where somebody eventually feeds one into the
	// other and loses the scale.
	Density *DensityProto `json:"density,omitempty"`
	// RoundingMode is required alongside a density, and unused without one.
	RoundingMode string `json:"rounding_mode,omitempty"`
}

type GetBatchYieldResponse struct {
	Batch    *BatchProto   `json:"batch"`
	Produced QuantityProto `json:"produced"`
	Consumed QuantityProto `json:"consumed"`

	ObservedPPM     *int64 `json:"observed_yield_ppm,omitempty"`
	ObservedPercent string `json:"observed_yield_percent,omitempty"`
	// UnavailableReason says why there is no observed figure. Never a blank
	// where a number should be: a blank cell in a variance report is read as a
	// zero, and a zero yield is an alarm.
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	ExpectedPPM     *int64 `json:"expected_yield_ppm,omitempty"`
	VariancePPM     *int64 `json:"variance_ppm,omitempty"`
	VariancePercent string `json:"variance_percent,omitempty"`
	// NoExpectationDeclared says the plant declared no target for this process,
	// which is not the same as having met one.
	NoExpectationDeclared bool `json:"no_expectation_declared"`
}

func (h *Handler) GetBatchYield(ctx context.Context, req *connect.Request[GetBatchYieldRequest]) (*connect.Response[GetBatchYieldResponse], error) {
	m := req.Msg
	b, err := h.lookup(ctx, m.TenantID, m.ID, m.Code)
	if err != nil {
		return nil, classify(err)
	}

	d, err := m.Density.density()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	y, err := h.svc.Yield(ctx, m.TenantID, b.ID, d, money.RoundingMode(m.RoundingMode))
	if err != nil {
		return nil, classify(err)
	}

	out := &GetBatchYieldResponse{
		Batch: fromBatch(b), Produced: fromQuantity(y.Output),
		Consumed:              fromQuantity(y.Consumed),
		ObservedPPM:           y.ObservedPPM,
		UnavailableReason:     y.UnavailableReason,
		ExpectedPPM:           y.ExpectedPPM,
		VariancePPM:           y.VariancePPM,
		NoExpectationDeclared: y.NoExpectationDeclared,
	}
	if y.ObservedPPM != nil {
		out.ObservedPercent = domain.PercentString(*y.ObservedPPM)
	}
	if y.VariancePPM != nil {
		out.VariancePercent = domain.PercentString(*y.VariancePPM)
	}
	return connect.NewResponse(out), nil
}

// ---------------------------------------------------------------------------

func fromBatch(b *domain.Batch) *BatchProto {
	return &BatchProto{
		ID: b.ID, TenantID: b.TenantID, Code: b.Code,
		Kind: string(b.Kind), ProductRef: b.ProductRef,
		Produced:   fromQuantity(b.Produced),
		ProducedAt: b.ProducedAt.UTC().Format(time.RFC3339), ProducedBy: b.ProducedBy,
		SourceKind: string(b.SourceKind), SourceRef: b.SourceRef,
		FormulationID: b.FormulationID,
		Status:        string(b.Status), StatusReason: b.StatusReason,
		Held: b.Status.Held(),
	}
}

func fromAffected(a service.Affected) AffectedProto {
	return AffectedProto{Batch: fromBatch(a.Batch), Depth: int32(a.Depth), Via: a.Via}
}

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

func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateCode),
		errors.Is(err, repository.ErrDuplicateInput),
		errors.Is(err, repository.ErrOverlappingVersion):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, repository.ErrRefused),
		errors.Is(err, repository.ErrNotADraft),
		errors.Is(err, domain.ErrNotApproved),
		errors.Is(err, domain.ErrHeldInput):
		// The database refused it, or the service saw it coming: a cycle, an
		// overdraw, a unit mismatch, a lot under hold. All of them are true
		// statements about the plant rather than bad requests, and a caller
		// retrying identically will be refused identically.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNoCode), errors.Is(err, domain.ErrNoKind),
		errors.Is(err, domain.ErrNoProduct), errors.Is(err, domain.ErrRawNeedsSource),
		errors.Is(err, domain.ErrSourceOnMade), errors.Is(err, domain.ErrHoldNeedsReason),
		errors.Is(err, domain.ErrNotPositive), errors.Is(err, domain.ErrConsumeSelf),
		errors.Is(err, domain.ErrNoLimit),
		errors.Is(err, domain.ErrNoFormulationCode), errors.Is(err, domain.ErrNoOutputProduct),
		errors.Is(err, domain.ErrNoValidFrom), errors.Is(err, domain.ErrBackwardsPeriod),
		errors.Is(err, domain.ErrExpectationNeedsBasis),
		errors.Is(err, domain.ErrIngredientIsTheProduct),
		errors.Is(err, domain.ErrNoApprover),
		errors.Is(err, domain.ErrNoWithdrawalReason),
		errors.Is(err, quantity.ErrNoUnit), errors.Is(err, quantity.ErrNoDensity),
		errors.Is(err, quantity.ErrBadDensity):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// ---------------------------------------------------------------------------
// Recipes
// ---------------------------------------------------------------------------

type FormulationInputProto struct {
	ProductRef string `json:"product_ref"`
	// ExpectedSharePPM is what share of the total input this is expected to be.
	// Optional; the shares of a recipe need not sum to a million, because a
	// recipe naming its two main ingredients and leaving the salt undeclared is
	// an ordinary recipe and demanding the rest would invent a figure.
	ExpectedSharePPM *int64 `json:"expected_share_ppm,omitempty"`
	// ShareTolerancePPM is how far off is worth mentioning, and without it no
	// share finding is raised at all.
	//
	// A real vat never hits a declared proportion exactly. How close is close
	// enough is a question about this plant's process and its scales, and a
	// figure chosen here would be the platform's opinion in a report with the
	// plant's name on it. The observed and declared shares are reported either
	// way; only the judgement waits on this.
	ShareTolerancePPM *int64 `json:"share_tolerance_ppm,omitempty"`
	// Required says whether a batch without this is wrong or merely unusual.
	Required bool `json:"required"`
}

type FormulationProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`
	Name     string `json:"name"`

	OutputProductRef string `json:"output_product_ref"`
	OutputUnit       string `json:"output_unit"`

	ExpectedYieldPPM *int64 `json:"expected_yield_ppm,omitempty"`
	ExpectedPercent  string `json:"expected_yield_percent,omitempty"`
	// ExpectationBasis is where the figure came from, and is required beside
	// one. A target a plant derived from two hundred of its own vats and one
	// read off a supplier's leaflet are different claims.
	ExpectationBasis string `json:"expectation_basis,omitempty"`

	// Status is DRAFT, APPROVED or WITHDRAWN. A recipe is created as a draft and
	// approved separately, so signing one off is an act with a name against it
	// rather than a field somebody filled in while typing the rest.
	Status       string `json:"status"`
	ApprovedBy   string `json:"approved_by,omitempty"`
	ApprovedAt   string `json:"approved_at,omitempty"`
	ApprovalNote string `json:"approval_note,omitempty"`
	// UsableForProduction says whether a batch may be made against this version.
	// Carried explicitly rather than left for the caller to derive from the
	// status, because a caller that derives it wrongly measures a vat against a
	// target nobody stands behind.
	UsableForProduction bool   `json:"usable_for_production"`
	WithdrawnReason     string `json:"withdrawn_reason,omitempty"`

	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`

	Inputs []FormulationInputProto `json:"inputs,omitempty"`
}

type CreateFormulationRequest struct {
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`
	Name     string `json:"name"`

	OutputProductRef string `json:"output_product_ref"`
	OutputUnit       string `json:"output_unit"`

	ExpectedYieldPPM *int64 `json:"expected_yield_ppm,omitempty"`
	ExpectationBasis string `json:"expectation_basis,omitempty"`

	// ValidFrom is required. A recipe with no period silently applies to every
	// batch ever made, including the ones made before it existed.
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`

	Inputs []FormulationInputProto `json:"inputs,omitempty"`
	Actor  string                  `json:"actor"`
}

type FormulationResponse struct {
	Formulation *FormulationProto `json:"formulation"`
}

func (h *Handler) CreateFormulation(ctx context.Context, req *connect.Request[CreateFormulationRequest]) (*connect.Response[FormulationResponse], error) {
	m := req.Msg
	from, err := parseTime(m.ValidFrom, "valid_from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var to *time.Time
	if m.ValidTo != "" {
		t, err := parseTime(m.ValidTo, "valid_to")
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		to = &t
	}

	f := &domain.Formulation{
		TenantID: m.TenantID, Code: m.Code, Name: m.Name,
		OutputProductRef: m.OutputProductRef, OutputUnit: m.OutputUnit,
		ExpectedYieldPPM: m.ExpectedYieldPPM, ExpectationBasis: m.ExpectationBasis,
		// Always a draft. Approving is a separate act, by somebody who is
		// putting their name to the target — not a field on the form that
		// created it.
		Status:    domain.Draft,
		ValidFrom: from, ValidTo: to, CreatedBy: m.Actor,
	}
	ins := make([]domain.FormulationInput, 0, len(m.Inputs))
	for _, p := range m.Inputs {
		ins = append(ins, domain.FormulationInput{
			ProductRef: p.ProductRef, ExpectedSharePPM: p.ExpectedSharePPM,
			ShareTolerancePPM: p.ShareTolerancePPM, Required: p.Required,
			CreatedBy: m.Actor,
		})
	}

	out, outIns, err := h.svc.CreateFormulation(ctx, f, ins)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FormulationResponse{
		Formulation: fromFormulation(out, outIns)}), nil
}

type GetFormulationRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	// Code with At resolves to the version that was on the wall then. At is
	// required beside a code: a recipe is several versions, and answering with
	// today's when somebody asked about March is the retroactive problem
	// versioning exists to prevent.
	Code string `json:"code,omitempty"`
	At   string `json:"at,omitempty"`
}

func (h *Handler) GetFormulation(ctx context.Context, req *connect.Request[GetFormulationRequest]) (*connect.Response[FormulationResponse], error) {
	m := req.Msg
	f, err := h.formulation(ctx, m.TenantID, m.ID, m.Code, m.At)
	if err != nil {
		return nil, classify(err)
	}
	ins, err := h.svc.FormulationInputs(ctx, m.TenantID, f.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FormulationResponse{Formulation: fromFormulation(f, ins)}), nil
}

func (h *Handler) formulation(ctx context.Context, tenantID, id, code, at string) (*domain.Formulation, error) {
	switch {
	case id != "":
		return h.svc.GetFormulation(ctx, tenantID, id)
	case code != "" && at != "":
		when, err := parseTime(at, "at")
		if err != nil {
			return nil, err
		}
		return h.svc.FormulationInForce(ctx, tenantID, code, when)
	case code != "":
		return nil, errors.New("a recipe named by code must also say which moment to look it " +
			"up for; a recipe is several versions and which one applies depends on the day")
	}
	return nil, errors.New("name the recipe by id, or by code and the moment to look it up for")
}

type ListFormulationsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListFormulationsResponse struct {
	Formulations []*FormulationProto `json:"formulations"`
}

func (h *Handler) ListFormulations(ctx context.Context, req *connect.Request[ListFormulationsRequest]) (*connect.Response[ListFormulationsResponse], error) {
	list, err := h.svc.ListFormulations(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*FormulationProto, 0, len(list))
	for _, f := range list {
		out = append(out, fromFormulation(f, nil))
	}
	return connect.NewResponse(&ListFormulationsResponse{Formulations: out}), nil
}

type ShareReadingProto struct {
	ProductRef       string `json:"product_ref"`
	ExpectedSharePPM int64  `json:"expected_share_ppm"`
	ObservedSharePPM int64  `json:"observed_share_ppm"`
	DifferencePPM    int64  `json:"difference_ppm"`
	ExpectedPercent  string `json:"expected_percent"`
	ObservedPercent  string `json:"observed_percent"`
	// TolerancePPM is what the plant declared, if it declared one. Absent means
	// the two figures above are reported and nothing is judged.
	TolerancePPM *int64 `json:"tolerance_ppm,omitempty"`
}

type FindingProto struct {
	Kind       string `json:"kind"`
	ProductRef string `json:"product_ref"`
	// Serious separates a vat with no milk in it from a note about a
	// substitution. A report where everything is serious is one where nothing is.
	Serious     bool   `json:"serious"`
	Explanation string `json:"explanation"`
}

type CheckRecipeRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
}

type CheckRecipeResponse struct {
	Batch       *batchRef         `json:"batch"`
	Formulation *FormulationProto `json:"formulation"`

	Findings []FindingProto `json:"findings"`
	// SeriousCount is how many of them somebody has to answer for.
	SeriousCount int32 `json:"serious_count"`

	Shares []ShareReadingProto `json:"shares"`
	// SharesUnavailableReason says why there are none. Never a silent empty
	// section: an empty part of a report is read as nothing being wrong.
	SharesUnavailableReason string `json:"shares_unavailable_reason,omitempty"`
}

// batchRef is enough of a batch to know which one is being talked about,
// without repeating the whole thing in every report.
type batchRef struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	ProductRef string `json:"product_ref"`
}

// CheckRecipe holds a batch up against the recipe it followed. Nothing here
// refuses anything; a plant substitutes, and a vat recorded with a note beside
// it beats a vat not recorded at all.
func (h *Handler) CheckRecipe(ctx context.Context, req *connect.Request[CheckRecipeRequest]) (*connect.Response[CheckRecipeResponse], error) {
	m := req.Msg
	b, err := h.lookup(ctx, m.TenantID, m.ID, m.Code)
	if err != nil {
		return nil, classify(err)
	}
	check, f, err := h.svc.CheckRecipe(ctx, m.TenantID, b.ID)
	if err != nil {
		return nil, classify(err)
	}

	out := &CheckRecipeResponse{
		Batch:                   &batchRef{ID: b.ID, Code: b.Code, ProductRef: b.ProductRef},
		Formulation:             fromFormulation(f, nil),
		Findings:                make([]FindingProto, 0, len(check.Findings)),
		Shares:                  make([]ShareReadingProto, 0, len(check.Shares)),
		SeriousCount:            int32(len(check.Serious())),
		SharesUnavailableReason: check.SharesUnavailableReason,
	}
	for _, fd := range check.Findings {
		out.Findings = append(out.Findings, FindingProto{
			Kind: string(fd.Kind), ProductRef: fd.ProductRef,
			Serious: fd.Serious(), Explanation: fd.Explanation,
		})
	}
	for _, sh := range check.Shares {
		out.Shares = append(out.Shares, ShareReadingProto{
			ProductRef:       sh.ProductRef,
			ExpectedSharePPM: sh.ExpectedSharePPM, ObservedSharePPM: sh.ObservedSharePPM,
			DifferencePPM:   sh.DifferencePPM,
			ExpectedPercent: domain.PercentString(sh.ExpectedSharePPM),
			ObservedPercent: domain.PercentString(sh.ObservedSharePPM),
			TolerancePPM:    sh.TolerancePPM,
		})
	}
	return connect.NewResponse(out), nil
}

type ObservedHistoryRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	At       string `json:"at,omitempty"`
}

type ObservedHistoryResponse struct {
	FormulationID    string `json:"formulation_id"`
	Code             string `json:"code"`
	OutputProductRef string `json:"output_product_ref"`

	BatchesCounted         int64 `json:"batches_counted"`
	BatchesNeedingADensity int64 `json:"batches_needing_a_density"`

	LowestPPM        *int64 `json:"lowest_ppm,omitempty"`
	LowerQuartilePPM *int64 `json:"lower_quartile_ppm,omitempty"`
	MedianPPM        *int64 `json:"median_ppm,omitempty"`
	UpperQuartilePPM *int64 `json:"upper_quartile_ppm,omitempty"`
	HighestPPM       *int64 `json:"highest_ppm,omitempty"`

	LowestPercent  string `json:"lowest_percent,omitempty"`
	MedianPercent  string `json:"median_percent,omitempty"`
	HighestPercent string `json:"highest_percent,omitempty"`

	ExpectedPPM      *int64 `json:"expected_yield_ppm,omitempty"`
	ExpectationBasis string `json:"expectation_basis,omitempty"`

	// Note says how many batches the figures rest on and what was left out. It
	// travels with them because a median over three vats and a median over
	// three hundred look identical on a screen.
	Note string `json:"note"`
}

// GetObservedYield is the platform's answer to a question it cannot answer.
//
// What should this process yield? It does not know, and there is no table of
// standard yields anywhere in here. What it can do is show a plant its own
// vats, and let the plant decide.
func (h *Handler) GetObservedYield(ctx context.Context, req *connect.Request[ObservedHistoryRequest]) (*connect.Response[ObservedHistoryResponse], error) {
	m := req.Msg
	f, err := h.formulation(ctx, m.TenantID, m.ID, m.Code, m.At)
	if err != nil {
		return nil, classify(err)
	}
	hist, err := h.svc.ObservedHistory(ctx, m.TenantID, f.ID)
	if err != nil {
		return nil, classify(err)
	}
	out := &ObservedHistoryResponse{
		FormulationID: hist.FormulationID, Code: hist.Code,
		OutputProductRef:       hist.OutputProductRef,
		BatchesCounted:         hist.BatchesCounted,
		BatchesNeedingADensity: hist.BatchesNeedingADensity,
		LowestPPM:              hist.LowestPPM,
		LowerQuartilePPM:       hist.LowerQuartilePPM,
		MedianPPM:              hist.MedianPPM,
		UpperQuartilePPM:       hist.UpperQuartilePPM,
		HighestPPM:             hist.HighestPPM,
		ExpectedPPM:            hist.ExpectedPPM,
		ExpectationBasis:       hist.ExpectationBasis,
		Note:                   hist.Note(),
	}
	if hist.LowestPPM != nil {
		out.LowestPercent = domain.PercentString(*hist.LowestPPM)
	}
	if hist.MedianPPM != nil {
		out.MedianPercent = domain.PercentString(*hist.MedianPPM)
	}
	if hist.HighestPPM != nil {
		out.HighestPercent = domain.PercentString(*hist.HighestPPM)
	}
	return connect.NewResponse(out), nil
}

type ApproveFormulationRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	// Approver is the person putting their name to the target.
	Approver string `json:"approver"`
	// At is when it was agreed. Omitted, the platform stamps now — which is
	// right for somebody approving as they type and wrong for a recipe agreed at
	// a Tuesday meeting and entered on Thursday.
	At   string `json:"at,omitempty"`
	Note string `json:"note,omitempty"`
}

func (h *Handler) ApproveFormulation(ctx context.Context, req *connect.Request[ApproveFormulationRequest]) (*connect.Response[FormulationResponse], error) {
	m := req.Msg
	var at time.Time
	if m.At != "" {
		parsed, err := parseTime(m.At, "at")
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		at = parsed
	}
	f, err := h.svc.Approve(ctx, m.TenantID, m.ID, m.Approver, m.Note, at)
	if err != nil {
		return nil, classify(err)
	}
	ins, err := h.svc.FormulationInputs(ctx, m.TenantID, f.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FormulationResponse{Formulation: fromFormulation(f, ins)}), nil
}

type WithdrawFormulationRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	// Reason is required. A recipe that stopped being used with no reason
	// recorded is one nobody can explain reintroducing.
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

func (h *Handler) WithdrawFormulation(ctx context.Context, req *connect.Request[WithdrawFormulationRequest]) (*connect.Response[FormulationResponse], error) {
	m := req.Msg
	f, err := h.svc.Withdraw(ctx, m.TenantID, m.ID, m.Reason, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	ins, err := h.svc.FormulationInputs(ctx, m.TenantID, f.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FormulationResponse{Formulation: fromFormulation(f, ins)}), nil
}

func fromFormulation(f *domain.Formulation, ins []domain.FormulationInput) *FormulationProto {
	p := &FormulationProto{
		ID: f.ID, TenantID: f.TenantID, Code: f.Code, Name: f.Name,
		OutputProductRef: f.OutputProductRef, OutputUnit: string(f.OutputUnit),
		ExpectedYieldPPM: f.ExpectedYieldPPM, ExpectationBasis: f.ExpectationBasis,
		Status: string(f.Status), ApprovedBy: f.ApprovedBy,
		ApprovalNote:        f.ApprovalNote,
		UsableForProduction: f.Status.UsableForProduction(),
		WithdrawnReason:     f.WithdrawnReason,
		ValidFrom:           f.ValidFrom.UTC().Format(time.RFC3339),
	}
	if f.ApprovedAt != nil {
		p.ApprovedAt = f.ApprovedAt.UTC().Format(time.RFC3339)
	}
	if f.ExpectedYieldPPM != nil {
		p.ExpectedPercent = domain.PercentString(*f.ExpectedYieldPPM)
	}
	if f.ValidTo != nil {
		p.ValidTo = f.ValidTo.UTC().Format(time.RFC3339)
	}
	for _, in := range ins {
		p.Inputs = append(p.Inputs, FormulationInputProto{
			ProductRef: in.ProductRef, ExpectedSharePPM: in.ExpectedSharePPM,
			ShareTolerancePPM: in.ShareTolerancePPM, Required: in.Required,
		})
	}
	return p
}
