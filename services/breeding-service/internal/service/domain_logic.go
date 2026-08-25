package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply an id" from "the query failed", and would have to report
// both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateBreedingCycle(ctx context.Context, b *domain.BreedingCycle) (*domain.BreedingCycle, error) {
	if b.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if b.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	b.ID = ulidpkg.New().String()
	if b.Status == "" {
		b.Status = "heat"
	}
	if b.HeatDate.IsZero() {
		b.HeatDate = time.Now()
	}
	if b.CreatedBy == "" {
		b.CreatedBy = "system"
	}
	b.UpdatedBy = b.CreatedBy
	return s.repo.CreateBreedingCycle(ctx, b)
}

func (s *Service) RecordInsemination(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, error) {
	if ins.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if ins.CycleID == "" {
		return nil, invalid("cycle_id is required")
	}
	if ins.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	ins.ID = ulidpkg.New().String()
	if ins.Method == "" {
		ins.Method = "AI"
	}
	if ins.InseminatedAt.IsZero() {
		ins.InseminatedAt = time.Now()
	}
	if ins.CreatedBy == "" {
		ins.CreatedBy = "system"
	}
	ins.UpdatedBy = ins.CreatedBy
	// The insemination and the cycle it advances are written together. When they
	// were two calls and the second was logged and discarded, an insemination
	// stood against a cycle still reading open — inviting a second service of an
	// animal that had already been bred.
	result, _, err := s.repo.RecordInseminationInCycle(ctx, ins)
	return result, err
}

func (s *Service) ConfirmPregnancy(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, error) {
	if p.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if p.InseminationID == "" {
		return nil, invalid("insemination_id is required")
	}
	if p.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if p.ExpectedCalvingDate.IsZero() {
		return nil, invalid("expected_calving_date is required")
	}
	p.ID = ulidpkg.New().String()
	p.Status = "active"
	if p.ConfirmedAt.IsZero() {
		p.ConfirmedAt = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy
	// The old path looked the insemination up outside the write and skipped the
	// cycle update entirely when that failed — silently, because the result was
	// discarded with `if err == nil`. Both now happen in one transaction.
	result, _, err := s.repo.ConfirmPregnancyForCycle(ctx, p)
	return result, err
}

func (s *Service) RecordCalving(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, error) {
	// calf_gender is VARCHAR(1) and nothing checked it, so "female" — the obvious
	// value to send for a string field of that name — reached PostgreSQL as a
	// length violation and came back to the caller as an internal failure. The
	// shape of the column is not something a client should have to know.
	sex, ok := domain.NormaliseCalfSex(c.CalfGender)
	if !ok {
		return nil, invalid("calf_gender must be female or male, or left empty if it was not recorded")
	}
	c.CalfGender = sex

	if c.Status == "" {
		c.Status = domain.CalvingNormal
	}
	if !domain.ValidCalvingStatus(c.Status) {
		return nil, invalid("status must be normal, assisted or emergency")
	}
	if c.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if c.PregnancyID == "" {
		return nil, invalid("pregnancy_id is required")
	}
	if c.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	c.ID = ulidpkg.New().String()
	if c.Status == "" {
		c.Status = "normal"
	}
	if c.CalvingDate.IsZero() {
		c.CalvingDate = time.Now()
	}
	if c.CreatedBy == "" {
		c.CreatedBy = "system"
	}
	c.UpdatedBy = c.CreatedBy
	// A calf recorded against a pregnancy still marked ongoing leaves the cow in
	// the population the herd expects to calve, so she is neither dried off nor
	// bred again on time.
	result, _, err := s.repo.RecordCalvingAndClosePregnancy(ctx, c)
	return result, err
}

func (s *Service) GetBreedingHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.BreedingCycle, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if cattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	return s.repo.ListCattleBreedingCycles(ctx, tenantID, cattleID)
}

func (s *Service) ListActivePregnancies(ctx context.Context, tenantID string) ([]*domain.Pregnancy, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListActivePregnancies(ctx, tenantID)
}
