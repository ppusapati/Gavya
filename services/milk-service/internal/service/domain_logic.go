package service

import (
	"context"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/milk-service/internal/domain"
)

func (s *Service) CreateSession(ctx context.Context, sess *domain.MilkSession) (*domain.MilkSession, error) {
	if sess.TenantID == "" || sess.CattleID == "" {
		return nil, fmt.Errorf("tenant_id and cattle_id are required")
	}
	if sess.ShiftType == "" {
		return nil, fmt.Errorf("shift_type is required")
	}
	sess.ID = ulidpkg.New().String()
	if sess.Status == "" {
		sess.Status = "pending"
	}
	sess.UpdatedBy = sess.CreatedBy
	now := time.Now()
	sess.CreatedAt = now
	sess.UpdatedAt = now
	if sess.SessionDate.IsZero() {
		sess.SessionDate = now
	}
	return s.repo.CreateSession(ctx, sess)
}

func (s *Service) GetSession(ctx context.Context, id, tenantID string) (*domain.MilkSession, error) {
	if id == "" || tenantID == "" {
		return nil, fmt.Errorf("id and tenant_id are required")
	}
	return s.repo.GetSession(ctx, id, tenantID)
}

func (s *Service) ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]*domain.MilkSession, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	return s.repo.ListSessions(ctx, tenantID, limit, offset)
}

func (s *Service) UpdateSessionStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.MilkSession, error) {
	if id == "" || tenantID == "" || status == "" {
		return nil, fmt.Errorf("id, tenant_id and status are required")
	}
	return s.repo.UpdateSessionStatus(ctx, id, tenantID, status, updatedBy)
}

func (s *Service) RecordMilk(ctx context.Context, r *domain.MilkRecord) (*domain.MilkRecord, error) {
	// quantity_liters is stored as NUMERIC(8,3). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(r.QuantityLiters, 3, 8); err != nil {
		return nil, exact.Field("quantity_liters", err)
	}
	if r.TenantID == "" || r.SessionID == "" || r.CattleID == "" {
		return nil, fmt.Errorf("tenant_id, session_id and cattle_id are required")
	}
	if r.QuantityLiters <= 0 {
		return nil, fmt.Errorf("quantity_liters must be positive")
	}
	r.ID = ulidpkg.New().String()
	r.UpdatedBy = r.CreatedBy
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	if r.RecordedAt.IsZero() {
		r.RecordedAt = now
	}
	return s.repo.CreateRecord(ctx, r)
}

func (s *Service) GetRecord(ctx context.Context, id, tenantID string) (*domain.MilkRecord, error) {
	return s.repo.GetRecord(ctx, id, tenantID)
}

func (s *Service) ListSessionRecords(ctx context.Context, sessionID, tenantID string) ([]*domain.MilkRecord, error) {
	return s.repo.ListSessionRecords(ctx, sessionID, tenantID)
}

func (s *Service) GetDailyYield(ctx context.Context, tenantID, cattleID string, date time.Time) (float64, error) {
	return s.repo.GetDailyYield(ctx, tenantID, cattleID, date)
}

func (s *Service) RecordQuality(ctx context.Context, mq *domain.MilkQuality) (*domain.MilkQuality, error) {
	// fat_percent is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.FatPercent, 2, 5); err != nil {
		return nil, exact.Field("fat_percent", err)
	}
	// snf_percent is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.SNFPercent, 2, 5); err != nil {
		return nil, exact.Field("snf_percent", err)
	}
	// lactose is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.Lactose, 2, 5); err != nil {
		return nil, exact.Field("lactose", err)
	}
	if mq.TenantID == "" || mq.RecordID == "" {
		return nil, fmt.Errorf("tenant_id and record_id are required")
	}
	mq.ID = ulidpkg.New().String()
	mq.UpdatedBy = mq.CreatedBy
	now := time.Now()
	mq.CreatedAt = now
	mq.UpdatedAt = now
	if mq.TestedAt.IsZero() {
		mq.TestedAt = now
	}
	return s.repo.CreateQuality(ctx, mq)
}
