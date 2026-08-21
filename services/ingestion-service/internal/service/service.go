package service

import (
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/ingestion-service/internal/repository"
)

// Service is the ingestion boundary: it turns deliveries from field devices
// into either admitted records, recognised replays, or quarantined records
// awaiting a human.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
