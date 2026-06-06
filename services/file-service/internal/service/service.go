package service

import (
	"github.com/ppusapati/gavya/services/file-service/internal/config"
	"github.com/ppusapati/gavya/services/file-service/internal/repository"
	"p9e.in/samavaya/packages/p9log"
)

type Service struct {
	repo repository.Repository
	cfg  *config.Config
	log  *p9log.Helper
}

func New(repo repository.Repository, cfg *config.Config, log *p9log.Helper) *Service {
	return &Service{repo: repo, cfg: cfg, log: log}
}
