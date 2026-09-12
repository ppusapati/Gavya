package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"

	"github.com/ppusapati/gavya/services/laboratory-service/internal/domain"
	"github.com/ppusapati/gavya/services/laboratory-service/internal/repository"
	"github.com/ppusapati/gavya/services/laboratory-service/internal/service"
)

const ServiceName = "laboratory.v1.LaboratoryService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("DrawSample", connectjson.Unary(h.DrawSample))
	route("GetSample", connectjson.Unary(h.GetSample))
	route("ListSamples", connectjson.Unary(h.ListSamples))
	route("BreakSeal", connectjson.Unary(h.BreakSeal))

	route("RecordHandover", connectjson.Unary(h.RecordHandover))
	route("RecordResult", connectjson.Unary(h.RecordResult))
	route("GetSampleReport", connectjson.Unary(h.GetSampleReport))
}

// ReadingProto is a measured value and the resolution it was read to.
//
// The scale is not cosmetic. 4.1 and 4.10 are the same number and not the same
// claim about how precisely it was read, and a chart indexed at one decimal
// place puts them in different cells from one indexed at two.
type ReadingProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

func (r ReadingProto) reading() (domain.Reading, error) {
	if r.Value == "" {
		return domain.Reading{}, errors.New("a reading must have a value")
	}
	neg := false
	s := r.Value
	if len(s) > 0 && s[0] == '-' {
		neg, s = true, s[1:]
	}
	var whole, frac string
	if i := indexByte(s, '.'); i >= 0 {
		whole, frac = s[:i], s[i+1:]
	} else {
		whole = s
	}
	if int32(len(frac)) != r.Scale {
		return domain.Reading{}, errors.New("the value is written to " +
			itoa(len(frac)) + " decimal places and the scale says " + itoa(int(r.Scale)) +
			"; a reading and the resolution it claims have to agree")
	}
	var v int64
	for _, d := range whole + frac {
		if d < '0' || d > '9' {
			return domain.Reading{}, errors.New(r.Value + " is not a number")
		}
		v = v*10 + int64(d-'0')
	}
	if neg {
		v = -v
	}
	return domain.Reading{Value: v, Scale: r.Scale}, nil
}

func fromReading(r domain.Reading) ReadingProto {
	return ReadingProto{Value: r.String(), Scale: r.Scale}
}

// ---------------------------------------------------------------------------
// Samples
// ---------------------------------------------------------------------------

type SampleProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`

	SourceKind string `json:"source_kind"`
	SourceRef  string `json:"source_ref"`

	DrawnAt string `json:"drawn_at"`
	DrawnBy string `json:"drawn_by"`

	SealNumber       string `json:"seal_number,omitempty"`
	SealBrokenAt     string `json:"seal_broken_at,omitempty"`
	SealBrokenBy     string `json:"seal_broken_by,omitempty"`
	SealBrokenReason string `json:"seal_broken_reason,omitempty"`
	Sealed           bool   `json:"sealed"`

	Purpose            string `json:"purpose"`
	DuplicatesSampleID string `json:"duplicates_sample_id,omitempty"`
}

type DrawSampleRequest struct {
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`

	SourceKind string `json:"source_kind"`
	SourceRef  string `json:"source_ref"`

	DrawnAt string `json:"drawn_at"`
	DrawnBy string `json:"drawn_by"`

	// SealNumber may be empty: a process check is not sealed, and demanding one
	// would make the platform tiresome about a reading that decides nothing.
	// A payment sample without one is recorded and its results are not eligible.
	SealNumber string `json:"seal_number,omitempty"`

	Purpose            string `json:"purpose"`
	DuplicatesSampleID string `json:"duplicates_sample_id,omitempty"`
	Actor              string `json:"actor"`
}

type SampleResponse struct {
	Sample *SampleProto `json:"sample"`
}

func (h *Handler) DrawSample(ctx context.Context, req *connect.Request[DrawSampleRequest]) (*connect.Response[SampleResponse], error) {
	m := req.Msg
	drawnAt, err := parseTime(m.DrawnAt, "drawn_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	s, err := h.svc.DrawSample(ctx, &domain.Sample{
		TenantID: m.TenantID, Code: m.Code,
		SourceKind: domain.SourceKind(m.SourceKind), SourceRef: m.SourceRef,
		DrawnAt: drawnAt, DrawnBy: m.DrawnBy,
		SealNumber: m.SealNumber, Purpose: domain.Purpose(m.Purpose),
		DuplicatesSampleID: m.DuplicatesSampleID, CreatedBy: m.Actor,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SampleResponse{Sample: fromSample(s)}), nil
}

type GetSampleRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

func (h *Handler) GetSample(ctx context.Context, req *connect.Request[GetSampleRequest]) (*connect.Response[SampleResponse], error) {
	s, err := h.svc.GetSample(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SampleResponse{Sample: fromSample(s)}), nil
}

type ListSamplesRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
}

type ListSamplesResponse struct {
	Samples []*SampleProto `json:"samples"`
}

func (h *Handler) ListSamples(ctx context.Context, req *connect.Request[ListSamplesRequest]) (*connect.Response[ListSamplesResponse], error) {
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
	list, err := h.svc.ListSamples(ctx, m.TenantID, from, to, int(m.Limit))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*SampleProto, 0, len(list))
	for _, s := range list {
		out = append(out, fromSample(s))
	}
	return connect.NewResponse(&ListSamplesResponse{Samples: out}), nil
}

type BreakSealRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	// Reason is required: the seal is what rules out the sample having been
	// changed, and breaking it silently defeats it.
	Reason string `json:"reason"`
	// At is when the seal was actually broken. Omitted, the platform stamps
	// now — which is right for a bottle being opened at the bench as somebody
	// types, and wrong for a laboratory book written up at the end of a shift.
	At    string `json:"at,omitempty"`
	Actor string `json:"actor"`
}

func (h *Handler) BreakSeal(ctx context.Context, req *connect.Request[BreakSealRequest]) (*connect.Response[SampleResponse], error) {
	var at time.Time
	if req.Msg.At != "" {
		parsed, err := parseTime(req.Msg.At, "at")
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		at = parsed
	}
	s, err := h.svc.BreakSeal(ctx, req.Msg.TenantID, req.Msg.ID, req.Msg.Reason, req.Msg.Actor, at)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SampleResponse{Sample: fromSample(s)}), nil
}

// ---------------------------------------------------------------------------
// Custody and results
// ---------------------------------------------------------------------------

type HandoverProto struct {
	Sequence int32  `json:"sequence"`
	At       string `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
	Note     string `json:"note,omitempty"`
}

type RecordHandoverRequest struct {
	TenantID string `json:"tenant_id"`
	SampleID string `json:"sample_id"`
	At       string `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
	Note     string `json:"note,omitempty"`
	Actor    string `json:"actor"`
}

type HandoverResponse struct {
	Handover *HandoverProto `json:"handover"`
}

func (h *Handler) RecordHandover(ctx context.Context, req *connect.Request[RecordHandoverRequest]) (*connect.Response[HandoverResponse], error) {
	m := req.Msg
	at, err := parseTime(m.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out, err := h.svc.RecordHandover(ctx, &domain.Handover{
		TenantID: m.TenantID, SampleID: m.SampleID,
		At: at, From: m.From, To: m.To, Note: m.Note,
	}, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&HandoverResponse{Handover: fromHandover(*out)}), nil
}

type ResultProto struct {
	ID       string `json:"id"`
	SampleID string `json:"sample_id"`

	Analyte string       `json:"analyte"`
	Reading ReadingProto `json:"reading"`

	Method         string `json:"method"`
	InstrumentRef  string `json:"instrument_ref"`
	ValidUntil     string `json:"instrument_valid_until,omitempty"`
	CertificateRef string `json:"instrument_certificate,omitempty"`

	AnalysedAt string `json:"analysed_at"`
	AnalysedBy string `json:"analysed_by"`

	// Eligibility is whether this reading is fit to price milk, and the reason
	// when it is not. Never absent: a caller shown a reading with no verdict
	// beside it will use it.
	Eligibility string `json:"eligibility"`
	Reason      string `json:"eligibility_reason"`
}

type RecordResultRequest struct {
	TenantID string `json:"tenant_id"`
	SampleID string `json:"sample_id"`

	Analyte string       `json:"analyte"`
	Reading ReadingProto `json:"reading"`

	Method        string `json:"method"`
	InstrumentRef string `json:"instrument_ref"`
	// ValidUntil is the instrument's calibration as it stands, supplied by the
	// caller because this service does not hold the certificate register. Empty
	// means nobody has recorded one, which the verdict reports as UNKNOWN rather
	// than as a finding.
	ValidUntil     string `json:"instrument_valid_until,omitempty"`
	CertificateRef string `json:"instrument_certificate,omitempty"`

	AnalysedAt string `json:"analysed_at"`
	AnalysedBy string `json:"analysed_by"`
	Actor      string `json:"actor"`
}

type ResultResponse struct {
	Result *ResultProto `json:"result"`
}

func (h *Handler) RecordResult(ctx context.Context, req *connect.Request[RecordResultRequest]) (*connect.Response[ResultResponse], error) {
	m := req.Msg
	reading, err := m.Reading.reading()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("reading: "+err.Error()))
	}
	analysedAt, err := parseTime(m.AnalysedAt, "analysed_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	inst := domain.Instrument{Ref: m.InstrumentRef, CertificateRef: m.CertificateRef}
	if m.ValidUntil != "" {
		until, err := parseTime(m.ValidUntil, "instrument_valid_until")
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		inst.ValidUntil = &until
	}

	out, err := h.svc.RecordResult(ctx, &domain.Result{
		TenantID: m.TenantID, SampleID: m.SampleID,
		Analyte: domain.Analyte(m.Analyte), Reading: reading,
		Method: m.Method, Instrument: inst,
		AnalysedAt: analysedAt, AnalysedBy: m.AnalysedBy, CreatedBy: m.Actor,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ResultResponse{Result: fromResult(out)}), nil
}

type AnalyteGroupProto struct {
	Analyte string         `json:"analyte"`
	Results []*ResultProto `json:"results"`
	// Spread is the difference between the highest and lowest reading where a
	// laboratory read one analyte more than once. Reported, never resolved: a
	// laboratory that ran a sample twice did so to find out whether the two
	// agree, and picking one throws away the answer.
	Spread     *ReadingProto `json:"spread,omitempty"`
	SpreadNote string        `json:"spread_unavailable_reason,omitempty"`
}

type GetSampleReportRequest struct {
	TenantID string `json:"tenant_id"`
	SampleID string `json:"sample_id"`
}

type GetSampleReportResponse struct {
	Sample *SampleProto     `json:"sample"`
	Chain  []*HandoverProto `json:"chain"`

	CustodyIntact bool   `json:"custody_intact"`
	CustodyHolder string `json:"custody_holder,omitempty"`
	CustodyReason string `json:"custody_reason,omitempty"`

	Analytes []AnalyteGroupProto `json:"analytes"`

	// Eligible and Total say how many of this sample's readings are fit to price
	// milk. A caller shown only the readings cannot tell a sample whose figures
	// are usable from one whose figures are not.
	Eligible int32 `json:"eligible"`
	Total    int32 `json:"total"`
}

// GetSampleReport is everything known about one sample: who held it, what it
// read, and which of those readings a payment may rest on.
func (h *Handler) GetSampleReport(ctx context.Context, req *connect.Request[GetSampleReportRequest]) (*connect.Response[GetSampleReportResponse], error) {
	rep, err := h.svc.Report(ctx, req.Msg.TenantID, req.Msg.SampleID)
	if err != nil {
		return nil, classify(err)
	}
	out := &GetSampleReportResponse{
		Sample:        fromSample(rep.Sample),
		Chain:         make([]*HandoverProto, 0, len(rep.Chain)),
		CustodyIntact: rep.Custody.Intact,
		CustodyHolder: rep.Custody.Holder,
		CustodyReason: rep.Custody.Reason,
		Analytes:      make([]AnalyteGroupProto, 0, len(rep.Analytes)),
		Eligible:      int32(rep.Eligible),
		Total:         int32(rep.Total),
	}
	for _, h := range rep.Chain {
		out.Chain = append(out.Chain, fromHandover(h))
	}
	for _, d := range rep.Analytes {
		g := AnalyteGroupProto{
			Analyte:    string(d.Analyte),
			Results:    make([]*ResultProto, 0, len(d.Results)),
			SpreadNote: d.SpreadUnavailableReason,
		}
		for _, r := range d.Results {
			g.Results = append(g.Results, fromResult(r))
		}
		if d.Spread != nil {
			s := fromReading(*d.Spread)
			g.Spread = &s
		}
		out.Analytes = append(out.Analytes, g)
	}
	return connect.NewResponse(out), nil
}

// ---------------------------------------------------------------------------

func fromSample(s *domain.Sample) *SampleProto {
	p := &SampleProto{
		ID: s.ID, TenantID: s.TenantID, Code: s.Code,
		SourceKind: string(s.SourceKind), SourceRef: s.SourceRef,
		DrawnAt: s.DrawnAt.UTC().Format(time.RFC3339), DrawnBy: s.DrawnBy,
		SealNumber: s.SealNumber, SealBrokenBy: s.SealBrokenBy,
		SealBrokenReason: s.SealBrokenReason, Sealed: s.Sealed(),
		Purpose: string(s.Purpose), DuplicatesSampleID: s.DuplicatesSampleID,
	}
	if s.SealBrokenAt != nil {
		p.SealBrokenAt = s.SealBrokenAt.UTC().Format(time.RFC3339)
	}
	return p
}

func fromHandover(h domain.Handover) *HandoverProto {
	return &HandoverProto{
		Sequence: h.Sequence, At: h.At.UTC().Format(time.RFC3339),
		From: h.From, To: h.To, Note: h.Note,
	}
}

func fromResult(r *domain.Result) *ResultProto {
	p := &ResultProto{
		ID: r.ID, SampleID: r.SampleID,
		Analyte: string(r.Analyte), Reading: fromReading(r.Reading),
		Method: r.Method, InstrumentRef: r.Instrument.Ref,
		CertificateRef: r.Instrument.CertificateRef,
		AnalysedAt:     r.AnalysedAt.UTC().Format(time.RFC3339), AnalysedBy: r.AnalysedBy,
		Eligibility: string(r.Eligibility), Reason: r.EligibilityReason,
	}
	if r.Instrument.ValidUntil != nil {
		p.ValidUntil = r.Instrument.ValidUntil.UTC().Format("2006-01-02")
	}
	return p
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
		errors.Is(err, repository.ErrDuplicateReading):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, repository.ErrCustodyIsWritten),
		errors.Is(err, repository.ErrBeforeDrawn),
		errors.Is(err, domain.ErrBeforeDrawn):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNoCode), errors.Is(err, domain.ErrNoSource),
		errors.Is(err, domain.ErrNoPurpose), errors.Is(err, domain.ErrNoAnalyte),
		errors.Is(err, domain.ErrNoMethod), errors.Is(err, domain.ErrNoInstrument),
		errors.Is(err, domain.ErrNoReason):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
