package postgres

import (
	"context"

	"github.com/doug-martin/goqu/v9"
	"github.com/jackc/pgx/v5"
	"labqueue/projects/accounts/internal/model"
)

type accountQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func setupRequired(ctx context.Context, db accountQuerier) (bool, error) {
	statement, args, err := dialect.From("setup_state").Select(goqu.COUNT("*")).Prepared(true).ToSQL()
	if err != nil {
		return false, err
	}
	var count int64
	if err := db.QueryRow(ctx, statement, args...).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (r *Users) SetupRequired(ctx context.Context) (bool, error) { return setupRequired(ctx, r.db) }

func createUser(ctx context.Context, db accountQuerier, input model.NewUser, hash string) (model.User, error) {
	statement, args, err := dialect.Insert("users").Rows(goqu.Record{
		"name": input.Name, "email": input.Email, "password_hash": hash,
		"role": input.Role, "group_id": input.GroupID,
	}).Returning(userColumns...).Prepared(true).ToSQL()
	if err != nil {
		return model.User{}, err
	}
	user, err := scanUser(db.QueryRow(ctx, statement, args...))
	return user, registrationError(err)
}

// Initialize commits the administrator and the permanent setup marker together.
// The second check is protected by a transaction lock, so concurrent first-run
// requests cannot create multiple initial administrators.
func (r *Users) Initialize(ctx context.Context, input model.Setup, hash string) (model.User, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)
	statement, args, err := dialect.Select(goqu.Func("pg_advisory_xact_lock", int64(1831041893))).Prepared(true).ToSQL()
	if err != nil {
		return model.User{}, err
	}
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return model.User{}, err
	}
	required, err := setupRequired(ctx, tx)
	if err != nil {
		return model.User{}, err
	}
	if !required {
		return model.User{}, model.Problem(model.Conflict, "setup_completed", "Первичная настройка уже завершена")
	}
	user, err := createUser(ctx, tx, model.NewUser{Name: input.Name, Email: input.Email, Role: model.Admin}, hash)
	if err != nil {
		return model.User{}, err
	}
	statement, args, err = dialect.Insert("setup_state").Rows(goqu.Record{"id": true}).Prepared(true).ToSQL()
	if err != nil {
		return model.User{}, err
	}
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return model.User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func (r *Users) CreateUser(ctx context.Context, input model.NewUser, hash string) (model.User, error) {
	return createUser(ctx, r.db, input, hash)
}

func (r *Users) ListUsers(ctx context.Context, filter model.UserFilter) (model.UserList, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return model.UserList{}, err
	}
	defer tx.Rollback(ctx)
	statement, args, err := dialect.From("users").Select(goqu.COUNT("*")).Prepared(true).ToSQL()
	if err != nil {
		return model.UserList{}, err
	}
	result := model.UserList{Users: []model.User{}}
	if err = tx.QueryRow(ctx, statement, args...).Scan(&result.Total); err != nil {
		return model.UserList{}, err
	}
	statement, args, err = dialect.From("users").Select(userColumns...).Order(goqu.C("id").Asc()).
		Limit(uint(filter.Limit)).Offset(uint(filter.Offset)).Prepared(true).ToSQL()
	if err != nil {
		return model.UserList{}, err
	}
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return model.UserList{}, err
	}
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			rows.Close()
			return model.UserList{}, scanErr
		}
		result.Users = append(result.Users, user)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return model.UserList{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.UserList{}, err
	}
	return result, nil
}
