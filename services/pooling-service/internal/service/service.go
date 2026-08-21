package service

import (
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/pooling-service/internal/repository"
)

// Service pools a marketing period's milk: it aggregates what producers
// delivered, values the pool by what the milk was used for, shares that value
// out, and rules on whether a late correction may reach a pool already settled.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
