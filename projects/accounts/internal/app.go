// Package app is the composition root for the accounts microservice.
package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform"
	"labqueue/projects/accounts/internal/adapter/postgres"
	"labqueue/projects/accounts/internal/adapter/security"
	"labqueue/projects/accounts/internal/handler"
	"labqueue/projects/accounts/internal/service"
	"labqueue/projects/accounts/internal/usecase"
	"net/http"
)

func New(db *pgxpool.Pool, tokens *platform.Tokens) http.Handler {
	users, groups := postgres.NewUsers(db), postgres.NewGroups(db)
	accounts := service.NewAccounts(users, groups, security.Passwords{}, security.NewTokens(tokens.Tokens))
	administration := service.NewAdministration(users, users, groups, security.Passwords{})
	app := usecase.New(accounts, service.NewGroups(groups), administration)
	mux := http.NewServeMux()
	platform.Health(mux, db)
	mux.Handle("/", handler.New(app, tokens))
	return mux
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error { return postgres.Migrate(ctx, db) }
func Seed(ctx context.Context, db *pgxpool.Pool) error    { return postgres.Seed(ctx, db) }
