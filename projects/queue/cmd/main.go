package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"labqueue/pkg/platform"
	"labqueue/pkg/platform/infra"
	app "labqueue/projects/queue/internal"
)

func main() {
	if err := run(); err != nil {
		slog.Error("queue stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tokens, err := platform.NewTokens(os.Getenv("JWT_SECRET"))
	if err != nil {
		return err
	}
	db, err := infra.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err = app.Migrate(ctx, db); err != nil {
		return err
	}
	if err = app.Seed(ctx, db); err != nil {
		return err
	}
	return platform.Serve(app.New(db, tokens, platform.Env("ACCOUNTS_URL", "http://accounts:8080")))
}
