package service

import (
	"context"
	"errors"

	"github.com/ppusapati/gavya/services/audit-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// a rejected argument from a failed query, and would have to report both the
// same way. This service is append-only and validates nothing today, so the
// marker exists for the classification the other services share.
//
// Wrapped with %w where a caller mistake is reported, which is how the other
// services do it. There was a second mechanism here as well — an
// invalidArgument type with its own Is, and a constructor for it — and nothing
// ever called it: the handler matches on the marker and the tests wrap it. Two
// ways to say the same thing, one of them never used, is how the two drift.
var ErrInvalidArgument = errors.New("invalid argument")

func (s *Service) CreateAuditLog(ctx context.Context, tenantID, actorID, actorType, action, resourceType, resourceID, oldValue, newValue, ipAddress, userAgent, serviceName, traceID, createdBy string) (*domain.AuditLog, error) {
	a := &domain.AuditLog{
		ID:           ulidpkg.New().String(),
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OldValue:     oldValue,
		NewValue:     newValue,
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		ServiceName:  serviceName,
		TraceID:      traceID,
		CreatedBy:    createdBy,
	}
	return s.repo.CreateAuditLog(ctx, a)
}

func (s *Service) GetAuditLog(ctx context.Context, id, tenantID string) (*domain.AuditLog, error) {
	return s.repo.GetAuditLog(ctx, id, tenantID)
}

func (s *Service) ListAuditLogs(ctx context.Context, tenantID string) ([]*domain.AuditLog, error) {
	return s.repo.ListAuditLogs(ctx, tenantID)
}

func (s *Service) ListAuditLogsByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*domain.AuditLog, error) {
	return s.repo.ListAuditLogsByResource(ctx, tenantID, resourceType, resourceID)
}

func (s *Service) ListAuditLogsByActor(ctx context.Context, tenantID, actorID string) ([]*domain.AuditLog, error) {
	return s.repo.ListAuditLogsByActor(ctx, tenantID, actorID)
}
