package service

import (
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/milk-service/internal/repository"
)

type Service struct {
	repo repository.Repository
	log  *p9log.Helper
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}
