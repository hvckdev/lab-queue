package postgres

import (
	"context"
	"embed"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform/infra"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	source, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	return infra.ApplyMigrations(ctx, db, source)
}
