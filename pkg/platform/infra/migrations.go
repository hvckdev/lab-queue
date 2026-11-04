package infra

import (
	"context"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// ApplyMigrations runs embedded SQL migrations without changing goose's global
// configuration. The SQL bridge retains the pool's credentials and search_path;
// closing it releases connections but never closes the caller's pgx pool.
func ApplyMigrations(ctx context.Context, db *pgxpool.Pool, source fs.FS) (retErr error) {
	if db == nil {
		return errors.New("migration database is required")
	}
	sqlDB := stdlib.OpenDBFromPool(db)
	defer func() { retErr = errors.Join(retErr, sqlDB.Close()) }()

	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 30), lock.WithUnlockTimeout(1, 3))
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, source,
		goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
