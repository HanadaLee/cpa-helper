package app

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io/fs"

	backendMigrations "cpa-helper/backend/migrations"
	"github.com/pressly/goose/v3"
)

const postgresMigrationLock int64 = 7218317001

func (a *App) runPostgresMigrations(ctx context.Context) error {
	// A dedicated connection owns the lock; the migration provider uses the pool.
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Never return a session-scoped advisory lock to the connection pool, even
		// after a cancelled or interrupted migration. Closing releases server locks.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", postgresMigrationLock); err != nil {
		return err
	}
	migrations, err := fs.Sub(backendMigrations.PostgresFS, "postgres")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, a.db.DB, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if current > backendMigrations.LatestVersion {
		return fmt.Errorf("%w: current version %d", ErrDatabaseTooNew, current)
	}
	_, err = provider.Up(ctx)
	return err
}
