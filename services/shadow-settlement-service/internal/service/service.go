package service

import (
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/repository"
)

// Service orchestrates shadow settlement: it records what the incumbent
// asserts, records what this platform computed, and adjudicates the difference.
//
// The divergence classifier it calls is deterministic. The ML client is
// optional and advisory — when it is nil, or when it fails, the service still
// produces a complete and authoritative verdict.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
	ml   *mlclient.DivergenceClient
}

func New(repo repository.Repository, log *p9log.Helper, ml *mlclient.DivergenceClient) *Service {
	return &Service{repo: repo, log: log, ml: ml}
}
