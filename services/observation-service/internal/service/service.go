package service

import (
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
)

// Service records measured facts and the evidence that qualifies them.
//
// Only the deterministic part is authoritative: validation, the eligibility
// verdict under this deployment's own measurement-control regime, and the
// append-only write. Both ML clients are optional and advisory
// — when either is nil, or fails, the observation is still recorded in full.
type Service struct {
	repo        repository.Repository
	log         *p9log.Helper
	uncertainty *mlclient.UncertaintyClient
	anomaly     *mlclient.AnomalyClient
	// regime is the measurement-control law this deployment operates under. It
	// is held here rather than looked up per call so that every verdict a
	// running service issues cites the same statute.
	regime domain.Regime
}

func New(repo repository.Repository, log *p9log.Helper, uncertainty *mlclient.UncertaintyClient, anomaly *mlclient.AnomalyClient, regime domain.Regime) *Service {
	return &Service{repo: repo, log: log, uncertainty: uncertainty, anomaly: anomaly, regime: regime}
}

// Regime reports the measurement-control law this service applies, so a handler
// can record it against a verdict.
func (s *Service) Regime() domain.Regime { return s.regime }
