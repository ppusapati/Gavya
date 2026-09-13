package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/notification-service/internal/domain"
	"github.com/ppusapati/gavya/services/notification-service/internal/repository"
	"github.com/ppusapati/gavya/services/notification-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "notification.v1.NotificationService"

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

	route("SendNotification", connectjson.Unary(h.SendNotification))
	route("GetNotification", connectjson.Unary(h.GetNotification))
	route("ListNotifications", connectjson.Unary(h.ListNotifications))
	route("MarkAsRead", connectjson.Unary(h.MarkAsRead))
	route("MarkAllRead", connectjson.Unary(h.MarkAllRead))
	route("CreateTemplate", connectjson.Unary(h.CreateTemplate))
	route("ListTemplates", connectjson.Unary(h.ListTemplates))
	route("GetUnreadCount", connectjson.Unary(h.GetUnreadCount))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing notification from an unreachable database — and
// makes an unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
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

type NotificationResponse struct {
	Notification *domain.Notification `json:"notification"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListNotificationsRequest struct {
	TenantID string `json:"tenant_id"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
	// Who the inbox belongs to. Both optional and both filters when set. Until
	// these existed the only listing was the whole tenant's, which is not an
	// inbox — it is everybody's mail on one table. A person reads their own
	// (recipient_type user) and the roles they hold (recipient_type role).
	RecipientID   string `json:"recipient_id,omitempty"`
	RecipientType string `json:"recipient_type,omitempty"`
}

type ListNotificationsResponse struct {
	Notifications []*domain.Notification `json:"notifications"`
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

type MarkAllReadResponse struct {
	Status string `json:"status"`
}

type CreateTemplateRequest struct {
	TenantID     string `json:"tenant_id"`
	EventType    string `json:"event_type"`
	Channel      string `json:"channel"`
	Title        string `json:"title"`
	BodyTemplate string `json:"body_template"`
	CreatedBy    string `json:"created_by"`
}

type TemplateResponse struct {
	Template *domain.NotificationTemplate `json:"template"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListTemplatesResponse struct {
	Templates []*domain.NotificationTemplate `json:"templates"`
}

type UnreadCountRequest struct {
	TenantID    string `json:"tenant_id"`
	RecipientID string `json:"recipient_id"`
}

type UnreadCountResponse struct {
	Count int64 `json:"count"`
}

func (h *Handler) SendNotification(ctx context.Context, req *connect.Request[SendNotificationRequest]) (*connect.Response[NotificationResponse], error) {
	m := req.Msg
	out, err := h.svc.SendNotification(ctx, &domain.Notification{
		TenantID:      m.TenantID,
		RecipientID:   m.RecipientID,
		RecipientType: m.RecipientType,
		Channel:       m.Channel,
		Title:         m.Title,
		Body:          m.Body,
		Priority:      m.Priority,
		ReferenceID:   m.ReferenceID,
		ReferenceType: m.ReferenceType,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NotificationResponse{Notification: out}), nil
}

func (h *Handler) GetNotification(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[NotificationResponse], error) {
	out, err := h.svc.GetNotification(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NotificationResponse{Notification: out}), nil
}

func (h *Handler) ListNotifications(ctx context.Context, req *connect.Request[ListNotificationsRequest]) (*connect.Response[ListNotificationsResponse], error) {
	m := req.Msg
	out, err := h.svc.ListNotifications(ctx, m.TenantID, m.Channel, m.Status, req.Msg.RecipientID, req.Msg.RecipientType)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListNotificationsResponse{Notifications: out}), nil
}

func (h *Handler) MarkAsRead(ctx context.Context, req *connect.Request[MarkReadRequest]) (*connect.Response[NotificationResponse], error) {
	m := req.Msg
	out, err := h.svc.MarkAsRead(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NotificationResponse{Notification: out}), nil
}

func (h *Handler) MarkAllRead(ctx context.Context, req *connect.Request[MarkAllReadRequest]) (*connect.Response[MarkAllReadResponse], error) {
	m := req.Msg
	if err := h.svc.MarkAllRead(ctx, m.RecipientID, m.TenantID, m.UpdatedBy); err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&MarkAllReadResponse{Status: "ok"}), nil
}

func (h *Handler) CreateTemplate(ctx context.Context, req *connect.Request[CreateTemplateRequest]) (*connect.Response[TemplateResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateTemplate(ctx, &domain.NotificationTemplate{
		TenantID:     m.TenantID,
		EventType:    m.EventType,
		Channel:      m.Channel,
		Title:        m.Title,
		BodyTemplate: m.BodyTemplate,
		CreatedBy:    m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TemplateResponse{Template: out}), nil
}

func (h *Handler) ListTemplates(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListTemplatesResponse], error) {
	out, err := h.svc.ListTemplates(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListTemplatesResponse{Templates: out}), nil
}

func (h *Handler) GetUnreadCount(ctx context.Context, req *connect.Request[UnreadCountRequest]) (*connect.Response[UnreadCountResponse], error) {
	out, err := h.svc.GetUnreadCount(ctx, req.Msg.TenantID, req.Msg.RecipientID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&UnreadCountResponse{Count: out}), nil
}
