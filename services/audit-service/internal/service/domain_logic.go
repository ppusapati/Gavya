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
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

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
