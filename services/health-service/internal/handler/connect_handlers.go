package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/health-service/internal/domain"
	"github.com/ppusapati/gavya/services/health-service/internal/repository"
	"github.com/ppusapati/gavya/services/health-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "health.v1.HealthService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("RecordVaccination", connectjson.Unary(h.RecordVaccination))
	route("GetVaccinationHistory", connectjson.Unary(h.GetVaccinationHistory))
	route("RecordTreatment", connectjson.Unary(h.RecordTreatment))
	route("GetTreatmentHistory", connectjson.Unary(h.GetTreatmentHistory))
	route("ScheduleVetVisit", connectjson.Unary(h.ScheduleVetVisit))
	route("ListUpcomingVaccinations", connectjson.Unary(h.ListUpcomingVaccinations))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing record from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrCurrencyMismatch):
		// The caller named a currency this tenant does not record in.
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, repository.ErrCurrencyUnset):
		// Well formed, and refused by the tenant's state rather than by
		// anything in the request. Internal would tell a client to retry.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type RecordVaccinationRequest struct {
	TenantID       string     `json:"tenant_id"`
	CattleID       string     `json:"cattle_id"`
	VaccineName    string     `json:"vaccine_name"`
	BatchNumber    string     `json:"batch_number"`
	AdministeredAt time.Time  `json:"administered_at"`
	NextDueDate    *time.Time `json:"next_due_date"`
	VeterinarianID string     `json:"veterinarian_id"`
	Dosage         string     `json:"dosage"`
	CreatedBy      string     `json:"created_by"`
}

type VaccinationResponse struct {
	Vaccination *domain.Vaccination `json:"vaccination"`
}

type ListVaccinationsResponse struct {
	Vaccinations []*domain.Vaccination `json:"vaccinations"`
}

type RecordTreatmentRequest struct {
	TenantID      string     `json:"tenant_id"`
	CattleID      string     `json:"cattle_id"`
	DiagnosisCode string     `json:"diagnosis_code"`
	Diagnosis     string     `json:"diagnosis"`
	MedicineName  string     `json:"medicine_name"`
	Dosage        string     `json:"dosage"`
	TreatedAt     time.Time  `json:"treated_at"`
	TreatedBy     string     `json:"treated_by"`
	FollowUpDate  *time.Time `json:"follow_up_date"`
	Status        string     `json:"status"`
	CreatedBy     string     `json:"created_by"`
}

type TreatmentResponse struct {
	Treatment *domain.Treatment `json:"treatment"`
}

type ListTreatmentsResponse struct {
	Treatments []*domain.Treatment `json:"treatments"`
}

type ScheduleVetVisitRequest struct {
	TenantID       string    `json:"tenant_id"`
	CattleID       string    `json:"cattle_id"`
	VeterinarianID string    `json:"veterinarian_id"`
	VisitDate      time.Time `json:"visit_date"`
	Purpose        string    `json:"purpose"`
	Notes          string    `json:"notes"`
	// Cost is a decimal literal — "450.00", not 450 — because a JSON number is a
	// float64 by the time Go has read it, and a figure that has been through a
	// float is one nobody can prove was not changed on the way.
	Cost      string `json:"cost"`
	CreatedBy string `json:"created_by"`
	// Currency is required: a cost that does not say what it is in is a number.
	Currency string `json:"currency"`
}

// VetVisitView is what a vet visit looks like on the wire.
//
// The domain model used to be serialised directly, which sent the cost out as a
// JSON number. This exists so it goes out as a decimal literal at its currency's
// scale.
type VetVisitView struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	CattleID       string     `json:"cattle_id"`
	VeterinarianID string     `json:"veterinarian_id"`
	VisitDate      time.Time  `json:"visit_date"`
	Purpose        string     `json:"purpose"`
	Notes          string     `json:"notes"`
	Cost           string     `json:"cost"`
	Currency       string     `json:"currency"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

func viewVetVisit(v *domain.VetVisit) *VetVisitView {
	if v == nil {
		return nil
	}
	return &VetVisitView{
		ID: v.ID, TenantID: v.TenantID, CattleID: v.CattleID,
		VeterinarianID: v.VeterinarianID, VisitDate: v.VisitDate,
		Purpose: v.Purpose, Notes: v.Notes,
		Cost: v.Cost.String(), Currency: v.Cost.Currency,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
		CreatedBy: v.CreatedBy, UpdatedBy: v.UpdatedBy, DeletedAt: v.DeletedAt,
	}
}

type VetVisitResponse struct {
	VetVisit *VetVisitView `json:"vet_visit"`
}

type HistoryRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

func (h *Handler) RecordVaccination(ctx context.Context, req *connect.Request[RecordVaccinationRequest]) (*connect.Response[VaccinationResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordVaccination(ctx, &domain.Vaccination{
		TenantID:       m.TenantID,
		CattleID:       m.CattleID,
		VaccineName:    m.VaccineName,
		BatchNumber:    m.BatchNumber,
		AdministeredAt: m.AdministeredAt,
		NextDueDate:    m.NextDueDate,
		VeterinarianID: m.VeterinarianID,
		Dosage:         m.Dosage,
		CreatedBy:      m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&VaccinationResponse{Vaccination: out}), nil
}

func (h *Handler) GetVaccinationHistory(ctx context.Context, req *connect.Request[HistoryRequest]) (*connect.Response[ListVaccinationsResponse], error) {
	out, err := h.svc.GetVaccinationHistory(ctx, req.Msg.TenantID, req.Msg.CattleID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListVaccinationsResponse{Vaccinations: out}), nil
}

func (h *Handler) RecordTreatment(ctx context.Context, req *connect.Request[RecordTreatmentRequest]) (*connect.Response[TreatmentResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordTreatment(ctx, &domain.Treatment{
		TenantID:      m.TenantID,
		CattleID:      m.CattleID,
		DiagnosisCode: m.DiagnosisCode,
		Diagnosis:     m.Diagnosis,
		MedicineName:  m.MedicineName,
		Dosage:        m.Dosage,
		TreatedAt:     m.TreatedAt,
		TreatedBy:     m.TreatedBy,
		FollowUpDate:  m.FollowUpDate,
		Status:        m.Status,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TreatmentResponse{Treatment: out}), nil
}

func (h *Handler) GetTreatmentHistory(ctx context.Context, req *connect.Request[HistoryRequest]) (*connect.Response[ListTreatmentsResponse], error) {
	out, err := h.svc.GetTreatmentHistory(ctx, req.Msg.TenantID, req.Msg.CattleID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListTreatmentsResponse{Treatments: out}), nil
}

func (h *Handler) ScheduleVetVisit(ctx context.Context, req *connect.Request[ScheduleVetVisitRequest]) (*connect.Response[VetVisitResponse], error) {
	m := req.Msg
	out, err := h.svc.ScheduleVetVisit(ctx, &domain.VetVisit{
		TenantID:       m.TenantID,
		CattleID:       m.CattleID,
		VeterinarianID: m.VeterinarianID,
		VisitDate:      m.VisitDate,
		Purpose:        m.Purpose,
		Notes:          m.Notes,
		CreatedBy:      m.CreatedBy,
	}, m.Cost, m.Currency)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&VetVisitResponse{VetVisit: viewVetVisit(out)}), nil
}

func (h *Handler) ListUpcomingVaccinations(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListVaccinationsResponse], error) {
	out, err := h.svc.ListUpcomingVaccinations(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListVaccinationsResponse{Vaccinations: out}), nil
}
