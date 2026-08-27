package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/notification-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such notification" from "the
// database is unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const notificationCols = `id,tenant_id,recipient_id,recipient_type,channel,title,body,status,priority,` +
	`COALESCE(reference_id,''),COALESCE(reference_type,''),sent_at,read_at,created_at,updated_at,created_by,updated_by,deleted_at`

const templateCols = `id,tenant_id,event_type,channel,title,body_template,is_active,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateNotification(ctx context.Context, n *domain.Notification) (*domain.Notification, error)
	GetNotification(ctx context.Context, id, tenantID string) (*domain.Notification, error)
	UpdateNotificationStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Notification, error)
	MarkNotificationRead(ctx context.Context, id, tenantID, updatedBy string) (*domain.Notification, error)
	MarkAllNotificationsRead(ctx context.Context, recipientID, tenantID, updatedBy string) error
	ListNotifications(ctx context.Context, tenantID, channel, status string) ([]*domain.Notification, error)
	GetUnreadCount(ctx context.Context, tenantID, recipientID string) (int64, error)
	CreateNotificationTemplate(ctx context.Context, t *domain.NotificationTemplate) (*domain.NotificationTemplate, error)
	ListNotificationTemplates(ctx context.Context, tenantID string) ([]*domain.NotificationTemplate, error)
}

type repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateNotification(ctx context.Context, n *domain.Notification) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO notifications (id,tenant_id,recipient_id,recipient_type,channel,title,body,status,priority,reference_id,reference_type,sent_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+notificationCols,
		n.ID, n.TenantID, n.RecipientID, n.RecipientType, n.Channel, n.Title, n.Body,
		n.Status, n.Priority, n.ReferenceID, n.ReferenceType, n.SentAt, n.CreatedBy, n.UpdatedBy,
	)
	return scanNotification(row)
}

func (r *repo) GetNotification(ctx context.Context, id, tenantID string) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+notificationCols+` FROM notifications WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanNotification(row)
}

func (r *repo) UpdateNotificationStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE notifications SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+notificationCols,
		id, tenantID, status, updatedBy,
	)
	return scanNotification(row)
}

func (r *repo) MarkNotificationRead(ctx context.Context, id, tenantID, updatedBy string) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE notifications SET status='read',read_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+notificationCols,
		id, tenantID, updatedBy,
	)
	return scanNotification(row)
}

func (r *repo) MarkAllNotificationsRead(ctx context.Context, recipientID, tenantID, updatedBy string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET status='read',read_at=NOW(),updated_by=$3,updated_at=NOW()
		 WHERE recipient_id=$1 AND tenant_id=$2 AND status != 'read' AND deleted_at IS NULL`,
		recipientID, tenantID, updatedBy,
	)
	return err
}

// ListNotifications returns a tenant's notifications, optionally narrowed by
// channel and status.
//
// An empty filter means "not filtering by that", not "match the empty string".
// It compared both unconditionally, so a caller that passed neither — which the
// request type invites, since neither field is required — got an empty list back
// and no indication why. An empty list is indistinguishable from a tenant with
// no notifications, so the mistake is invisible from the caller's side: the
// obvious call returns the obviously wrong answer and looks right doing it.
func (r *repo) ListNotifications(ctx context.Context, tenantID, channel, status string) ([]*domain.Notification, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+notificationCols+` FROM notifications
		 WHERE tenant_id=$1
		   AND ($2 = '' OR channel = $2)
		   AND ($3 = '' OR status = $3)
		   AND deleted_at IS NULL
		 ORDER BY created_at DESC`,
		tenantID, channel, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (r *repo) GetUnreadCount(ctx context.Context, tenantID, recipientID string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE tenant_id=$1 AND recipient_id=$2 AND status != 'read' AND deleted_at IS NULL`,
		tenantID, recipientID,
	).Scan(&count)
	return count, err
}

func (r *repo) CreateNotificationTemplate(ctx context.Context, t *domain.NotificationTemplate) (*domain.NotificationTemplate, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO notification_templates (id,tenant_id,event_type,channel,title,body_template,is_active,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+templateCols,
		t.ID, t.TenantID, t.EventType, t.Channel, t.Title, t.BodyTemplate, t.IsActive, t.CreatedBy, t.UpdatedBy,
	)
	return scanTemplate(row)
}

func (r *repo) ListNotificationTemplates(ctx context.Context, tenantID string) ([]*domain.NotificationTemplate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+templateCols+` FROM notification_templates WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY event_type`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.NotificationTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func scanNotification(s scanner) (*domain.Notification, error) {
	n := &domain.Notification{}
	err := s.Scan(&n.ID, &n.TenantID, &n.RecipientID, &n.RecipientType, &n.Channel,
		&n.Title, &n.Body, &n.Status, &n.Priority, &n.ReferenceID, &n.ReferenceType,
		&n.SentAt, &n.ReadAt, &n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.UpdatedBy, &n.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return n, nil
}

func scanTemplate(s scanner) (*domain.NotificationTemplate, error) {
	t := &domain.NotificationTemplate{}
	err := s.Scan(&t.ID, &t.TenantID, &t.EventType, &t.Channel, &t.Title, &t.BodyTemplate,
		&t.IsActive, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy, &t.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}
