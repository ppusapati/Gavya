package pgxpostgres

import (
	"context"
	"database/sql"

	"p9e.in/samavaya/packages/saas"
	"p9e.in/samavaya/packages/saas/data"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HasTenant sql.NullString

// MultiTenancy entity
type MultiTenancy struct {
	TenantId HasTenant
}

type DbProvider saas.DbProvider[*pgxpool.Pool]
type ClientProvider saas.ClientProvider[*pgxpool.Pool]
type ClientProviderFunc saas.ClientProviderFunc[*pgxpool.Pool]

// type DbProvider saas.DbProvider[*gorm.DB]
// type ClientProvider saas.ClientProvider[*gorm.DB]
// type ClientProviderFunc saas.ClientProviderFunc[*gorm.DB]

func (c ClientProviderFunc) Get(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return c(ctx, dsn)
}

// func (c ClientProviderFunc) Get(ctx context.Context, dsn string) (*gorm.DB, error) {
// 	return c(ctx, dsn)
// }

func NewDbProvider(cs data.ConnStrResolver, cp ClientProvider) DbProvider {
	return saas.NewDbProvider[*pgxpool.Pool](cs, cp)
}

// func NewDbProvider(cs data.ConnStrResolver, cp ClientProvider) DbProvider {
// 	return saas.NewDbProvider[*gorm.DB](cs, cp)
// }

type DbWrap struct {
	*pgxpool.Pool
	// *gorm.DB
}

// NewDbWrap wrap gorm.DB into io.Close
//
//	func NewDbWrap(db *gorm.DB) *DbWrap {
//		return &DbWrap{db}
//	}
func NewDbWrap(db *pgxpool.Pool) *DbWrap {
	return &DbWrap{db}
}

func (d *DbWrap) Close() error {
	return closeDb(d.Pool)
}

// func closeDb(d *gorm.DB) error {
func closeDb(d *pgxpool.Pool) error {
	// sqlDB, err := d.B()
	// if err != nil {
	// 	return err
	// }
	d.Close()
	// cErr := d.Close()
	// if cErr != nil {
	// 	//todo logging
	// 	//logger.Errorf("Gorm db close error: %s", err.Error())
	// 	return cErr
	// }
	return nil
}
