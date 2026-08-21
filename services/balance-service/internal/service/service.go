package service

import (
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/balance-service/internal/repository"
)

// Service closes mass balances: it records what each leg of a route measured,
// works out where the period fails to balance, and asks the reconciler which
// leg most likely carries the gross error.
//
// Only the deterministic part is authoritative — validation, the locally
// computed imbalance and the persisted run. The reconciler is advisory: when it
// is nil or unreachable the run is still recorded, because an investigation into
// milk that has gone missing must not stall on a model being up.
type Service struct {
	repo repository.Repository
	log  *p9log.Helper
	ml   *mlclient.ReconciliationClient
}

func New(repo repository.Repository, log *p9log.Helper, ml *mlclient.ReconciliationClient) *Service {
	return &Service{repo: repo, log: log, ml: ml}
}
