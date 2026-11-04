package postgres

import (
	"context"

	"github.com/doug-martin/goqu/v9"
	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform/infra"
)

// Seed adds demonstration accounts without changing existing passwords or
// profiles. It is safe to call again against an existing database.
func Seed(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	statement, args, err := dialect.Select(goqu.Func("pg_advisory_xact_lock", int64(1831041892))).Prepared(true).ToSQL()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return err
	}
	// Explicit demo IDs and the identity sequence must change together. This
	// lock also prevents a concurrent group insert from racing the adjustment.
	if _, err = tx.Exec(ctx, "LOCK TABLE student_groups IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	statement, args, err = dialect.Insert("student_groups").Cols("id", "name").
		Vals(goqu.Vals{int64(1), "ИВТ-101"}, goqu.Vals{int64(2), "ИВТ-102"}).
		OnConflict(goqu.DoNothing()).Prepared(true).ToSQL()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return err
	}
	maximum := dialect.From("student_groups").Select(goqu.MAX("id"))
	statement, args, err = dialect.Select(goqu.Func("setval", goqu.Func("pg_get_serial_sequence", "student_groups", "id"), maximum, true)).Prepared(true).ToSQL()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return err
	}
	group1, group2 := int64(1), int64(2)
	users := []struct {
		Name, Email, Password, Role string
		GroupID                     *int64
	}{
		{"Анна Сергеевна", "teacher@example.com", "Teacher123!", "teacher", nil},
		{"Михаил Андреевич", "teacher2@example.com", "Teacher123!", "teacher", nil},
		{"Алексей Иванов", "student@example.com", "Student123!", "student", &group1},
		{"Мария Петрова", "student2@example.com", "Student123!", "student", &group1},
		{"Дмитрий Смирнов", "student3@example.com", "Student123!", "student", &group2},
	}
	hashes := make(map[string]string)
	for _, user := range users {
		statement, args, err = dialect.From("users").Select(goqu.COUNT("*")).
			Where(goqu.Ex{"email": user.Email}).Prepared(true).ToSQL()
		if err != nil {
			return err
		}
		var count int64
		if err = tx.QueryRow(ctx, statement, args...).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		hash, found := hashes[user.Password]
		if !found {
			encoded, hashErr := (infra.Passwords{}).Hash(user.Password)
			if hashErr != nil {
				return hashErr
			}
			hash = encoded
			hashes[user.Password] = hash
		}
		statement, args, err = dialect.Insert("users").Rows(goqu.Record{
			"name": user.Name, "email": user.Email, "password_hash": hash,
			"role": user.Role, "group_id": user.GroupID,
		}).OnConflict(goqu.DoNothing()).Prepared(true).ToSQL()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, statement, args...); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
