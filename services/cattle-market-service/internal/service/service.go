package service

import (
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/cattle-market-service/internal/repository"
)

// Service holds business logic for cattle-market-service.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

// New creates a new Service with the given repository and logger.
func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
