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

	ExpectedYieldPPM *int64 `json:"expected_yield_ppm,omitempty"`

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

	// ExpectedYieldPPM is what the plant expects this process to give, in parts
	// per million of what it consumes. Optional, and there is no table of
	// standard yields behind it: a yield is a property of a plant's milk, its
	// process and its equipment, and a figure invented here would be a number
	// nobody measured sitting in a variance report.
	ExpectedYieldPPM *int64 `json:"expected_yield_ppm,omitempty"`

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
	b, err := h.svc.CreateBatch(ctx, &domain.Batch{
		TenantID: m.TenantID, Code: m.Code,
		Kind: domain.Kind(m.Kind), ProductRef: m.ProductRef,
		Produced: produced, ProducedAt: producedAt, ProducedBy: m.ProducedBy,
		SourceKind: domain.SourceKind(m.SourceKind), SourceRef: m.SourceRef,
		ExpectedYieldPPM: m.ExpectedYieldPPM,
		Status:           status, StatusReason: m.StatusReason,
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
		ExpectedYieldPPM: b.ExpectedYieldPPM,
		Status:           string(b.Status), StatusReason: b.StatusReason,
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
		errors.Is(err, repository.ErrDuplicateInput):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, repository.ErrRefused),
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
		errors.Is(err, quantity.ErrNoUnit), errors.Is(err, quantity.ErrNoDensity),
		errors.Is(err, quantity.ErrBadDensity):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
