package tenantdb

import (
	"context"
	"errors"
	"fmt"

	"p9e.in/samavaya/packages/p9log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func CreateDatabaseIfNotExists(ctx context.Context, pgxpool *pgxpool.Pool, databaseName string) error {
	var exists bool
	err := pgxpool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		// Create the database if it doesn't exist
		_, err = pgxpool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", databaseName))
		if err != nil {
			return err
		}
		p9log.Infof("Database '%s' created.\n", databaseName)
	} else {
		p9log.Errorf("Database '%s' already exists.\n", databaseName)
		return errors.New("database already exists")
	}

	return nil
}
