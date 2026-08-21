package service

import (
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/canonical-service/internal/repository"
)

// Service canonicalises identity: it maps an incumbent system's identifiers
// onto platform entities, and decides which record is the authoritative
// collection for a producer, date and shift.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
