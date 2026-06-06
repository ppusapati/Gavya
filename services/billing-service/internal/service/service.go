package service

import (
	"github.com/ppusapati/gavya/services/billing-service/internal/repository"
	"p9e.in/samavaya/packages/p9log"
)

type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
