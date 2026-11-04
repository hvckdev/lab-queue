package main

import (
	"log/slog"
	"os"
	"strconv"

	"labqueue/pkg/platform"
	app "labqueue/projects/web/internal"
)

func main() {
	secure, err := strconv.ParseBool(platform.Env("COOKIE_SECURE", "false"))
	if err != nil {
		slog.Error("COOKIE_SECURE must be true or false")
		os.Exit(1)
	}
	handler, err := app.New(platform.Env("ACCOUNTS_URL", "http://accounts:8080"), platform.Env("QUEUE_URL", "http://queue:8080"), platform.Env("PUBLIC_ORIGIN", "http://localhost:8080"), secure)
	if err == nil {
		err = platform.Serve(handler)
	}
	if err != nil {
		slog.Error("web service failed", "error", err)
		os.Exit(1)
	}
}
