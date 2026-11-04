// Package app is the composition facade for the queue microservice.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform"
	"labqueue/pkg/platform/infra"
	"labqueue/projects/queue/internal/adapter/accounts"
	"labqueue/projects/queue/internal/adapter/legacyhttp"
	"labqueue/projects/queue/internal/adapter/postgres"
	"labqueue/projects/queue/internal/handler"
	"labqueue/projects/queue/internal/usecase"
)

// Response aliases retain the existing facade for callers and integration tests.
type Session = handler.Session
type Entry = handler.Entry

func Migrate(ctx context.Context, db *pgxpool.Pool) error { return postgres.Migrate(ctx, db) }
func Seed(ctx context.Context, db *pgxpool.Pool) error    { return postgres.Seed(ctx, db) }

func New(db *pgxpool.Pool, tokens *platform.Tokens, accountsURL string) http.Handler {
	store := postgres.New(db)
	groups := accounts.New(accountsURL, infra.NewHTTPClient(3*time.Second))
	usecases := usecase.New(store, groups)
	mux := http.NewServeMux()
	platform.Health(mux, db)
	mux.Handle("/api/", tokens.RequireAuth(handler.New(usecases)))
	return legacyhttp.Capture(mux)
}
