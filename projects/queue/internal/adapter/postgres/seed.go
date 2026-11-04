package postgres

import (
	"context"

	"github.com/doug-martin/goqu/v9"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Seed(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Explicit demo IDs require advancing the identity sequence. Block writers
	// until both the seed and sequence adjustment commit, including other seeds.
	if _, err = tx.Exec(ctx, "LOCK TABLE subjects IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	if err = execute(ctx, tx, dialect.Insert("subjects").Cols("id", "name").Vals(
		goqu.Vals{1, "Программирование"},
		goqu.Vals{2, "Базы данных"},
		goqu.Vals{3, "Компьютерные сети"},
	).OnConflict(goqu.DoNothing()).Prepared(true)); err != nil {
		return err
	}
	maximum := dialect.From("subjects").Select(goqu.MAX("id"))
	if err = execute(ctx, tx, dialect.Select(goqu.Func("setval", goqu.Func("pg_get_serial_sequence", "subjects", "id"), maximum, true)).Prepared(true)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
