package service

import (
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
)

// Service records measured facts and the evidence that qualifies them.
//
// Only the deterministic part is authoritative: validation, the legal-metrology
// verdict and the append-only write. Both ML clients are optional and advisory
// — when either is nil, or fails, the observation is still recorded in full.
type Service struct {
	repo        repository.Repository
	log         *p9log.Helper
	uncertainty *mlclient.UncertaintyClient
	anomaly     *mlclient.AnomalyClient
}

func New(repo repository.Repository, log *p9log.Helper, uncertainty *mlclient.UncertaintyClient, anomaly *mlclient.AnomalyClient) *Service {
	return &Service{repo: repo, log: log, uncertainty: uncertainty, anomaly: anomaly}
}
