package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
	"github.com/ppusapati/gavya/services/breeding-service/internal/repository"
	"github.com/ppusapati/gavya/services/breeding-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "breeding.v1.BreedingService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateBreedingCycle", connectjson.Unary(h.CreateBreedingCycle))
	route("RecordInsemination", connectjson.Unary(h.RecordInsemination))
	route("ConfirmPregnancy", connectjson.Unary(h.ConfirmPregnancy))
	route("RecordCalving", connectjson.Unary(h.RecordCalving))
	route("GetBreedingHistory", connectjson.Unary(h.GetBreedingHistory))
	route("ListActivePregnancies", connectjson.Unary(h.ListActivePregnancies))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing cycle from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrCycleNotOpen), errors.Is(err, repository.ErrPregnancyClosed):
		// The request was well formed and the animal's state refused it. Retrying
		// changes nothing until the animal's state does.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateBreedingCycleRequest struct {
	TenantID  string    `json:"tenant_id"`
	CattleID  string    `json:"cattle_id"`
	HeatDate  time.Time `json:"heat_date"`
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
	CreatedBy string    `json:"created_by"`
}

type BreedingCycleResponse struct {
	Cycle *domain.BreedingCycle `json:"cycle"`
}

type RecordInseminationRequest struct {
	TenantID      string    `json:"tenant_id"`
	CycleID       string    `json:"cycle_id"`
	CattleID      string    `json:"cattle_id"`
	BullID        *string   `json:"bull_id"`
	SemenBatchID  *string   `json:"semen_batch_id"`
	InseminatedAt time.Time `json:"inseminated_at"`
	Method        string    `json:"method"`
	CreatedBy     string    `json:"created_by"`
}

type InseminationResponse struct {
	Insemination *domain.Insemination `json:"insemination"`
}

type ConfirmPregnancyRequest struct {
	TenantID            string    `json:"tenant_id"`
	CattleID            string    `json:"cattle_id"`
	InseminationID      string    `json:"insemination_id"`
	ConfirmedAt         time.Time `json:"confirmed_at"`
	ExpectedCalvingDate time.Time `json:"expected_calving_date"`
	CreatedBy           string    `json:"created_by"`
}

type PregnancyResponse struct {
	Pregnancy *domain.Pregnancy `json:"pregnancy"`
}

type RecordCalvingRequest struct {
	TenantID    string  `json:"tenant_id"`
	PregnancyID string  `json:"pregnancy_id"`
	CattleID    string  `json:"cattle_id"`
	CalfID      *string `json:"calf_id"`
	CalfGender  string  `json:"calf_gender"`
	// CalfWeight is read from the digits that were sent, whether as a JSON
	// string or a bare number, and goes back out as a decimal literal.
	CalfWeight    exact.Fixed `json:"calf_weight"`
	Complications string      `json:"complications"`
	Status        string      `json:"status"`
	CreatedBy     string      `json:"created_by"`
}

type CalvingRecordResponse struct {
	CalvingRecord *domain.CalvingRecord `json:"calving_record"`
}

type GetBreedingHistoryRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type GetBreedingHistoryResponse struct {
	Cycles []*domain.BreedingCycle `json:"cycles"`
}

type ListActivePregnanciesRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListActivePregnanciesResponse struct {
	Pregnancies []*domain.Pregnancy `json:"pregnancies"`
}

func (h *Handler) CreateBreedingCycle(ctx context.Context, req *connect.Request[CreateBreedingCycleRequest]) (*connect.Response[BreedingCycleResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateBreedingCycle(ctx, &domain.BreedingCycle{
		TenantID:  m.TenantID,
		CattleID:  m.CattleID,
		HeatDate:  m.HeatDate,
		Status:    m.Status,
		Notes:     m.Notes,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BreedingCycleResponse{Cycle: out}), nil
}

func (h *Handler) RecordInsemination(ctx context.Context, req *connect.Request[RecordInseminationRequest]) (*connect.Response[InseminationResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordInsemination(ctx, &domain.Insemination{
		TenantID:      m.TenantID,
		CycleID:       m.CycleID,
		CattleID:      m.CattleID,
		BullID:        m.BullID,
		SemenBatchID:  m.SemenBatchID,
		InseminatedAt: m.InseminatedAt,
		Method:        m.Method,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InseminationResponse{Insemination: out}), nil
}

func (h *Handler) ConfirmPregnancy(ctx context.Context, req *connect.Request[ConfirmPregnancyRequest]) (*connect.Response[PregnancyResponse], error) {
	m := req.Msg
	out, err := h.svc.ConfirmPregnancy(ctx, &domain.Pregnancy{
		TenantID:            m.TenantID,
		CattleID:            m.CattleID,
		InseminationID:      m.InseminationID,
		ConfirmedAt:         m.ConfirmedAt,
		ExpectedCalvingDate: m.ExpectedCalvingDate,
		CreatedBy:           m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PregnancyResponse{Pregnancy: out}), nil
}

func (h *Handler) RecordCalving(ctx context.Context, req *connect.Request[RecordCalvingRequest]) (*connect.Response[CalvingRecordResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordCalving(ctx, &domain.CalvingRecord{
		TenantID:      m.TenantID,
		PregnancyID:   m.PregnancyID,
		CattleID:      m.CattleID,
		CalfID:        m.CalfID,
		CalfGender:    m.CalfGender,
		CalfWeight:    m.CalfWeight,
		Complications: m.Complications,
		Status:        m.Status,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CalvingRecordResponse{CalvingRecord: out}), nil
}

func (h *Handler) GetBreedingHistory(ctx context.Context, req *connect.Request[GetBreedingHistoryRequest]) (*connect.Response[GetBreedingHistoryResponse], error) {
	out, err := h.svc.GetBreedingHistory(ctx, req.Msg.TenantID, req.Msg.CattleID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetBreedingHistoryResponse{Cycles: out}), nil
}

func (h *Handler) ListActivePregnancies(ctx context.Context, req *connect.Request[ListActivePregnanciesRequest]) (*connect.Response[ListActivePregnanciesResponse], error) {
	out, err := h.svc.ListActivePregnancies(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListActivePregnanciesResponse{Pregnancies: out}), nil
}
