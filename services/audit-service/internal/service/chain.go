package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/services/audit-service/internal/repository"
)

// SealAndAnchor brings a tenant's chain up to date and records where it got to.
//
// One operation rather than two, because a seal without an anchor leaves the
// newly sealed rows detectable-if-edited and undetectable-if-removed, and that
// is the weaker half of the guarantee. Anything that runs the sealer wants both.
func (s *Service) SealAndAnchor(ctx context.Context, tenantID string, limit int) (repository.Seal, repository.Checkpoint, error) {
	if tenantID == "" {
		return repository.Seal{}, repository.Checkpoint{}, errors.New("a tenant has to be named")
	}
	sealed, err := s.repo.Seal(ctx, tenantID, limit)
	if err != nil {
		return repository.Seal{}, repository.Checkpoint{}, err
	}
	anchor, err := s.repo.Checkpoint(ctx, tenantID)
	if err != nil {
		// The seal happened and stands. Reporting the failure rather than
		// discarding it, because an operator needs to know the anchor is behind.
		return sealed, repository.Checkpoint{}, fmt.Errorf("sealed %d rows but could not anchor them: %w",
			sealed.Sealed, err)
	}
	return sealed, anchor, nil
}

// VerifyChain reports whether a tenant's trail still hangs together, and how
// much of it is not yet covered.
//
// The two go together on purpose. "Intact" on its own invites the reading that
// everything is accounted for, when what it means is that everything sealed is
// accounted for — and if sealing stopped running a week ago, that is a very
// different statement.
type ChainReport struct {
	repository.Verification
	repository.Status
}

func (s *Service) VerifyChain(ctx context.Context, tenantID string) (*ChainReport, error) {
	if tenantID == "" {
		return nil, errors.New("a tenant has to be named")
	}
	v, err := s.repo.Verify(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	st, err := s.repo.Status(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &ChainReport{Verification: v, Status: st}, nil
}

// UnsealedFor is how long the oldest uncovered row has been waiting.
//
// Zero when there is nothing waiting. A caller watching this is watching for the
// sealer having stopped, which is the failure that makes the trail quietly stop
// being tamper-evident while every other signal stays green.
func (r *ChainReport) UnsealedFor(now time.Time) time.Duration {
	if r.OldestUnsealedAt == nil {
		return 0
	}
	return now.Sub(*r.OldestUnsealedAt)
}
