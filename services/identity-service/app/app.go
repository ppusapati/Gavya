// Package app is identity-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module that is
// wired differently in the two shapes is a module that is tested in one of them.
package app

import (
	"context"
	"fmt"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/identity-service/internal/config"
	"github.com/ppusapati/gavya/services/identity-service/internal/handler"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
	"github.com/ppusapati/gavya/services/identity-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler, and the function
// that closes what was opened.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, func(), error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("identity-service: db connect: %w", err)
	}
	repo := repository.New(pool)
	svc := service.New(repo, sys.IDs{}, sys.Clock{}, log)
	return handler.New(svc), pool.Close, nil
}
