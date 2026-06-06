package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/notification-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func (s *Service) SendNotification(ctx context.Context, n *domain.Notification) (*domain.Notification, error) {
	if n.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if n.RecipientID == "" {
		return nil, errors.New("recipient_id is required")
	}
	if n.Title == "" {
		return nil, errors.New("title is required")
	}
	n.ID = ulidpkg.New().String()
	n.Status = "sent"
	now := time.Now()
	n.SentAt = &now
	if n.Channel == "" {
		n.Channel = "in_app"
	}
	if n.Priority == "" {
		n.Priority = "normal"
	}
	if n.RecipientType == "" {
		n.RecipientType = "user"
	}
	if n.CreatedBy == "" {
		n.CreatedBy = "system"
	}
	n.UpdatedBy = n.CreatedBy
	return s.repo.CreateNotification(ctx, n)
}

func (s *Service) GetNotification(ctx context.Context, id, tenantID string) (*domain.Notification, error) {
	if id == "" || tenantID == "" {
		return nil, errors.New("id and tenant_id are required")
	}
	return s.repo.GetNotification(ctx, id, tenantID)
}

func (s *Service) ListNotifications(ctx context.Context, tenantID, channel, status string) ([]*domain.Notification, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListNotifications(ctx, tenantID, channel, status)
}

func (s *Service) MarkAsRead(ctx context.Context, id, tenantID, updatedBy string) (*domain.Notification, error) {
	if id == "" || tenantID == "" {
		return nil, errors.New("id and tenant_id are required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.MarkNotificationRead(ctx, id, tenantID, updatedBy)
}

func (s *Service) MarkAllRead(ctx context.Context, recipientID, tenantID, updatedBy string) error {
	if recipientID == "" || tenantID == "" {
		return errors.New("recipient_id and tenant_id are required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.MarkAllNotificationsRead(ctx, recipientID, tenantID, updatedBy)
}

func (s *Service) CreateTemplate(ctx context.Context, t *domain.NotificationTemplate) (*domain.NotificationTemplate, error) {
	if t.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if t.EventType == "" {
		return nil, errors.New("event_type is required")
	}
	t.ID = ulidpkg.New().String()
	t.IsActive = true
	if t.Channel == "" {
		t.Channel = "in_app"
	}
	if t.CreatedBy == "" {
		t.CreatedBy = "system"
	}
	t.UpdatedBy = t.CreatedBy
	return s.repo.CreateNotificationTemplate(ctx, t)
}

func (s *Service) ListTemplates(ctx context.Context, tenantID string) ([]*domain.NotificationTemplate, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListNotificationTemplates(ctx, tenantID)
}

func (s *Service) GetUnreadCount(ctx context.Context, tenantID, recipientID string) (int64, error) {
	if tenantID == "" || recipientID == "" {
		return 0, errors.New("tenant_id and recipient_id are required")
	}
	return s.repo.GetUnreadCount(ctx, tenantID, recipientID)
}
