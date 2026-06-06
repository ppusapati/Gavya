package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/notification-service/internal/domain"
	"github.com/ppusapati/gavya/services/notification-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/notification.v1.NotificationService/SendNotification", h.SendNotification)
	mux.HandleFunc("/notification.v1.NotificationService/GetNotification", h.GetNotification)
	mux.HandleFunc("/notification.v1.NotificationService/ListNotifications", h.ListNotifications)
	mux.HandleFunc("/notification.v1.NotificationService/MarkAsRead", h.MarkAsRead)
	mux.HandleFunc("/notification.v1.NotificationService/MarkAllRead", h.MarkAllRead)
	mux.HandleFunc("/notification.v1.NotificationService/CreateTemplate", h.CreateTemplate)
	mux.HandleFunc("/notification.v1.NotificationService/ListTemplates", h.ListTemplates)
	mux.HandleFunc("/notification.v1.NotificationService/GetUnreadCount", h.GetUnreadCount)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type SendNotificationRequest struct {
	TenantID      string `json:"tenant_id"`
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Channel       string `json:"channel"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Priority      string `json:"priority"`
	ReferenceID   string `json:"reference_id"`
	ReferenceType string `json:"reference_type"`
	CreatedBy     string `json:"created_by"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListNotificationsRequest struct {
	TenantID string `json:"tenant_id"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
}

type MarkReadRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type MarkAllReadRequest struct {
	RecipientID string `json:"recipient_id"`
	TenantID    string `json:"tenant_id"`
	UpdatedBy   string `json:"updated_by"`
}

type CreateTemplateRequest struct {
	TenantID     string `json:"tenant_id"`
	EventType    string `json:"event_type"`
	Channel      string `json:"channel"`
	Title        string `json:"title"`
	BodyTemplate string `json:"body_template"`
	CreatedBy    string `json:"created_by"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type UnreadCountRequest struct {
	TenantID    string `json:"tenant_id"`
	RecipientID string `json:"recipient_id"`
}

func (h *Handler) SendNotification(w http.ResponseWriter, r *http.Request) {
	var req SendNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n := &domain.Notification{
		TenantID:      req.TenantID,
		RecipientID:   req.RecipientID,
		RecipientType: req.RecipientType,
		Channel:       req.Channel,
		Title:         req.Title,
		Body:          req.Body,
		Priority:      req.Priority,
		ReferenceID:   req.ReferenceID,
		ReferenceType: req.ReferenceType,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.SendNotification(r.Context(), n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetNotification(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	var req ListNotificationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListNotifications(r.Context(), req.TenantID, req.Channel, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) MarkAsRead(w http.ResponseWriter, r *http.Request) {
	var req MarkReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.MarkAsRead(r.Context(), req.ID, req.TenantID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	var req MarkAllReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.MarkAllRead(r.Context(), req.RecipientID, req.TenantID, req.UpdatedBy); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := &domain.NotificationTemplate{
		TenantID:     req.TenantID,
		EventType:    req.EventType,
		Channel:      req.Channel,
		Title:        req.Title,
		BodyTemplate: req.BodyTemplate,
		CreatedBy:    req.CreatedBy,
	}
	result, err := h.svc.CreateTemplate(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListTemplates(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	var req UnreadCountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	count, err := h.svc.GetUnreadCount(r.Context(), req.TenantID, req.RecipientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}
