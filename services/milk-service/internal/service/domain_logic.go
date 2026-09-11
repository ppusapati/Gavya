package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/milk-service/internal/domain"
)

// ErrInvalidArgument marks a caller mistake, so the handler can tell "you did
// not supply a cattle_id" from "the database is unreachable".
//
// This service had neither: it returned plain fmt.Errorf values, and the handler
// guessed — CreateSession called every failure an invalid argument, so a database
// that was down looked like a malformed request, and GetDailyYield called every
// failure internal, so a missing field looked like something to retry.
var ErrInvalidArgument = errors.New("invalid argument")

// invalid carries the reason alone. The marker is matched through Is, so the
// message is never compared against.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(format string, args ...any) error {
	return &invalidArgument{reason: fmt.Sprintf(format, args...)}
}

// CreateSession opens a milking session, and is where a tenant says which
// timezone its days are reckoned in.
//
// This is the call that states it because it is the first one that means
// anything locally: a session has a shift — morning or evening — and a date, and
// both are wall-clock facts. Everything downstream that asks what an animal gave
// on a given day reads the answer from the pin this leaves.
//
// The same shape the services holding money use for currency: stated on first
// use, fixed from then on, and refused if a later request disagrees.
func (s *Service) CreateSession(ctx context.Context, sess *domain.MilkSession, zone string) (*domain.MilkSession, error) {
	if sess.TenantID == "" || sess.CattleID == "" {
		return nil, invalid("tenant_id and cattle_id are required")
	}
	if sess.ShiftType == "" {
		return nil, invalid("shift_type is required")
	}
	if err := s.repo.PinTenantTimezone(ctx, sess.TenantID, zone); err != nil {
		return nil, err
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
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetSession(ctx, id, tenantID)
}

func (s *Service) ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]*domain.MilkSession, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	return s.repo.ListSessions(ctx, tenantID, limit, offset)
}

func (s *Service) UpdateSessionStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.MilkSession, error) {
	if id == "" || tenantID == "" || status == "" {
		return nil, invalid("id, tenant_id and status are required")
	}
	return s.repo.UpdateSessionStatus(ctx, id, tenantID, status, updatedBy)
}

func (s *Service) RecordMilk(ctx context.Context, r *domain.MilkRecord) (*domain.MilkRecord, error) {
	// quantity_liters is stored as NUMERIC(8,3). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(r.QuantityLiters, 3, 8); err != nil {
		return nil, invalid("%s", exact.Field("quantity_liters", err))
	}
	if r.TenantID == "" || r.SessionID == "" || r.CattleID == "" {
		return nil, invalid("tenant_id, session_id and cattle_id are required")
	}
	if r.QuantityLiters <= 0 {
		return nil, invalid("quantity_liters must be positive")
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

// GetDailyYield reads the day in the tenant's own timezone.
//
// A tenant that has recorded no milk here has not said which timezone its days
// are reckoned in, and answering anyway would mean picking one. The refusal says
// which fact is missing rather than returning a plausible zero.
func (s *Service) GetDailyYield(ctx context.Context, tenantID, cattleID string, date time.Time) (float64, error) {
	zone, err := s.repo.TenantTimezone(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	return s.repo.GetDailyYield(ctx, tenantID, cattleID, date, zone)
}

func (s *Service) RecordQuality(ctx context.Context, mq *domain.MilkQuality) (*domain.MilkQuality, error) {
	// fat_percent is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.FatPercent, 2, 5); err != nil {
		return nil, invalid("%s", exact.Field("fat_percent", err))
	}
	// snf_percent is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.SNFPercent, 2, 5); err != nil {
		return nil, invalid("%s", exact.Field("snf_percent", err))
	}
	// lactose is stored as NUMERIC(5,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(mq.Lactose, 2, 5); err != nil {
		return nil, invalid("%s", exact.Field("lactose", err))
	}
	if mq.TenantID == "" || mq.RecordID == "" {
		return nil, invalid("tenant_id and record_id are required")
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
