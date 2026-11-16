// Package migration applies the provider's versioned PostgreSQL schema changes.
// It is used by the dedicated migration command and is not imported by the API.
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Up applies all pending migrations from files. The caller owns db and must
// configure it with a single connection for the session advisory lock.
func Up(ctx context.Context, db *sql.DB, files fs.FS) ([]*goose.MigrationResult, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("configure migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("configure migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return results, nil
}

// CurrentVersion returns the applied migration version using the same schema
// history table as Up. It is useful for task logs and operational checks.
func CurrentVersion(ctx context.Context, db *sql.DB, files fs.FS) (int64, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return 0, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files)
	if err != nil {
		return 0, fmt.Errorf("configure migrations: %w", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	return version, nil
}
