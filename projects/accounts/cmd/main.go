package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"labqueue/pkg/platform"
	app "labqueue/projects/accounts/internal"
)

func run() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}

	tokens, err := platform.NewTokens(os.Getenv("JWT_SECRET"))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := platform.Connect(ctx, url)
	if err != nil {
		return err
	}

	defer db.Close()

	if err := app.Migrate(ctx, db); err != nil {
		return err
	}
	if err := app.Seed(ctx, db); err != nil {
		return err
	}

	return platform.Serve(app.New(db, tokens))
}

func main() {
	if err := run(); err != nil {
		slog.Error("accounts stopped", "error", err)
		os.Exit(1)
	}
}
