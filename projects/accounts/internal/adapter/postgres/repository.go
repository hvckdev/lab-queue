package postgres

import (
	"context"
	"errors"

	"labqueue/projects/accounts/internal/model"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Users struct{ db *pgxpool.Pool }
type Groups struct{ db *pgxpool.Pool }

func NewUsers(db *pgxpool.Pool) *Users   { return &Users{db: db} }
func NewGroups(db *pgxpool.Pool) *Groups { return &Groups{db: db} }

type scanner interface{ Scan(...any) error }

var dialect = goqu.Dialect("postgres")

var userColumns = []any{"id", "name", "email", "role", "group_id"}

func scanUser(row scanner) (model.User, error) {
	var user model.User
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.GroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, model.ErrNotFound
	}
	return user, err
}

func (r *Users) CreateStudent(ctx context.Context, input model.Registration, hash string) (model.User, error) {
	statement, args, err := dialect.Insert("users").Rows(goqu.Record{
		"name": input.Name, "email": input.Email, "password_hash": hash,
		"role": "student", "group_id": input.GroupID,
	}).Returning(userColumns...).Prepared(true).ToSQL()
	if err != nil {
		return model.User{}, err
	}
	user, err := scanUser(r.db.QueryRow(ctx, statement, args...))
	return user, registrationError(err)
}

func registrationError(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		switch pgerr.Code {
		case "23505":
			return model.Problem(model.Conflict, "email_exists", "Пользователь с таким email уже зарегистрирован")
		case "23503":
			return model.Problem(model.Invalid, "group_not_found", "Выбранная группа не найдена")
		}
	}
	return err
}

func (r *Users) ByEmail(ctx context.Context, email string) (model.Credentials, error) {
	statement, args, err := dialect.From("users").
		Select("id", "name", "email", "role", "group_id", "password_hash").
		Where(goqu.Ex{"email": email}).Prepared(true).ToSQL()
	if err != nil {
		return model.Credentials{}, err
	}
	var credentials model.Credentials
	user := &credentials.User
	err = r.db.QueryRow(ctx, statement, args...).Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.GroupID, &credentials.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Credentials{}, model.ErrNotFound
	}
	return credentials, err
}
func (r *Users) ByID(ctx context.Context, id int64) (model.User, error) {
	statement, args, err := dialect.From("users").Select(userColumns...).Where(goqu.Ex{"id": id}).Prepared(true).ToSQL()
	if err != nil {
		return model.User{}, err
	}
	return scanUser(r.db.QueryRow(ctx, statement, args...))
}
func (r *Groups) List(ctx context.Context) ([]model.Group, error) {
	statement, args, err := dialect.From("student_groups").Select("id", "name").
		Order(goqu.C("name").Asc(), goqu.C("id").Asc()).Prepared(true).ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []model.Group{}
	for rows.Next() {
		var group model.Group
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}
func (r *Groups) Get(ctx context.Context, id int64) (model.Group, error) {
	statement, args, err := dialect.From("student_groups").Select("id", "name").
		Where(goqu.Ex{"id": id}).Prepared(true).ToSQL()
	if err != nil {
		return model.Group{}, err
	}
	var group model.Group
	err = r.db.QueryRow(ctx, statement, args...).Scan(&group.ID, &group.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Group{}, model.ErrNotFound
	}
	return group, err
}
