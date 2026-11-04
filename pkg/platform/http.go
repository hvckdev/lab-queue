package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform/infra"
)

func Env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("write response", "error", err)
	}
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		Error(w, http.StatusUnsupportedMediaType, "content_type", "Ожидается application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "invalid_json", "Проверьте поля запроса")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		Error(w, http.StatusBadRequest, "invalid_json", "Ожидается один JSON-объект")
		return false
	}
	return true
}

// Connect is retained for compatibility; client construction belongs to Infra.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return infra.Connect(ctx, url)
}

func Health(mux *http.ServeMux, db *pgxpool.Pool) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db != nil && db.Ping(ctx) != nil {
			Error(w, 503, "not_ready", "База данных недоступна")
			return
		}
		JSON(w, 200, map[string]string{"status": "ready"})
	})
}

func Serve(handler http.Handler) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: Env("HTTP_ADDR", ":8080"), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	slog.Info("HTTP service listening", "address", server.Addr)
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		<-result
		return err
	}
}
