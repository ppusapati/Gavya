package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/audit-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such audit log" from "the
// database is unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const auditLogCols = `id,tenant_id,actor_id,actor_type,action,resource_type,resource_id,` +
	`COALESCE(old_value,''),COALESCE(new_value,''),COALESCE(ip_address,''),COALESCE(user_agent,''),COALESCE(service_name,''),COALESCE(trace_id,''),created_at,created_by`

type Repository interface {
	CreateAuditLog(ctx context.Context, a *domain.AuditLog) (*domain.AuditLog, error)
	GetAuditLog(ctx context.Context, id, tenantID string) (*domain.AuditLog, error)
	ListAuditLogs(ctx context.Context, tenantID string) ([]*domain.AuditLog, error)
	ListAuditLogsByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*domain.AuditLog, error)
	ListAuditLogsByActor(ctx context.Context, tenantID, actorID string) ([]*domain.AuditLog, error)
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

func (r *repo) CreateAuditLog(ctx context.Context, a *domain.AuditLog) (*domain.AuditLog, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO audit_logs (id,tenant_id,actor_id,actor_type,action,resource_type,resource_id,old_value,new_value,ip_address,user_agent,service_name,trace_id,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+auditLogCols,
		a.ID, a.TenantID, a.ActorID, a.ActorType, a.Action, a.ResourceType, a.ResourceID,
		a.OldValue, a.NewValue, a.IPAddress, a.UserAgent, a.ServiceName, a.TraceID, a.CreatedBy,
	)
	return scanAuditLog(row)
}

func (r *repo) GetAuditLog(ctx context.Context, id, tenantID string) (*domain.AuditLog, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+auditLogCols+` FROM audit_logs WHERE id=$1 AND tenant_id=$2`,
		id, tenantID,
	)
	return scanAuditLog(row)
}

func (r *repo) ListAuditLogs(ctx context.Context, tenantID string) ([]*domain.AuditLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+auditLogCols+` FROM audit_logs WHERE tenant_id=$1 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.AuditLog
	for rows.Next() {
		a, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *repo) ListAuditLogsByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*domain.AuditLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+auditLogCols+` FROM audit_logs WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3 ORDER BY created_at DESC`,
		tenantID, resourceType, resourceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.AuditLog
	for rows.Next() {
		a, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *repo) ListAuditLogsByActor(ctx context.Context, tenantID, actorID string) ([]*domain.AuditLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+auditLogCols+` FROM audit_logs WHERE tenant_id=$1 AND actor_id=$2 ORDER BY created_at DESC`,
		tenantID, actorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.AuditLog
	for rows.Next() {
		a, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func scanAuditLog(s scanner) (*domain.AuditLog, error) {
	a := &domain.AuditLog{}
	err := s.Scan(&a.ID, &a.TenantID, &a.ActorID, &a.ActorType, &a.Action,
		&a.ResourceType, &a.ResourceID, &a.OldValue, &a.NewValue, &a.IPAddress,
		&a.UserAgent, &a.ServiceName, &a.TraceID, &a.CreatedAt, &a.CreatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}
