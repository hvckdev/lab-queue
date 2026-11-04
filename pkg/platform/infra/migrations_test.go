package infra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3/lock"
)

func migrationTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_QUEUE_DATABASE_URL")
	if url == "" {
		url = os.Getenv("TEST_ACCOUNTS_DATABASE_URL")
	}
	if url == "" {
		t.Skip("set TEST_QUEUE_DATABASE_URL or TEST_ACCOUNTS_DATABASE_URL for PostgreSQL migration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 12
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	return db
}

func migrationSource(sql string) fstest.MapFS {
	return fstest.MapFS{"001_initial.sql": {Data: []byte(sql)}}
}

const probeMigration = `-- +goose Up
CREATE TABLE migration_probe (id INTEGER PRIMARY KEY);
INSERT INTO migration_probe(id) VALUES (1);
-- +goose Down
DROP TABLE migration_probe;
`

func TestApplyMigrationsConcurrentAndRepeated(t *testing.T) {
	db := migrationTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const workers = 6
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errors <- ApplyMigrations(ctx, db, migrationSource(probeMigration))
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyMigrations(ctx, db, migrationSource(probeMigration)); err != nil {
		t.Fatal(err)
	}
	var count, versionCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM migration_probe").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id=1 AND is_applied").Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if count != 1 || versionCount != 1 {
		t.Fatalf("migration ran more than once: rows=%d versions=%d", count, versionCount)
	}
	// ApplyMigrations must close its SQL bridge, not the service's pool.
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("migration closed the caller's pool: %v", err)
	}
}

func TestApplyMigrationsRollsBackFailedVersion(t *testing.T) {
	db := migrationTestDB(t)
	ctx := context.Background()
	source := migrationSource(probeMigration)
	source["002_change.sql"] = &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE migration_partial (id INTEGER);
INSERT INTO migration_missing_table VALUES (1);
-- +goose Down
DROP TABLE migration_partial;
`)}
	if err := ApplyMigrations(ctx, db, source); err == nil {
		t.Fatal("invalid migration unexpectedly succeeded")
	}
	var partial *string
	var version int
	if err := db.QueryRow(ctx, "SELECT to_regclass('migration_partial')::text").Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if partial != nil || version != 1 {
		t.Fatalf("failed version was not atomic: partial=%v version=%d", partial, version)
	}
	source["002_change.sql"] = &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE migration_partial (id INTEGER);
INSERT INTO migration_partial VALUES (1);
-- +goose Down
DROP TABLE migration_partial;
`)}
	if err := ApplyMigrations(ctx, db, source); err != nil {
		t.Fatalf("could not retry a corrected migration: %v", err)
	}
}

func TestApplyMigrationsLockRespectsContext(t *testing.T) {
	db := migrationTestDB(t)
	ctx := context.Background()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	defer connection.Exec(ctx, "SELECT pg_advisory_unlock($1)", lock.DefaultLockID)
	deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	err = ApplyMigrations(deadline, db, migrationSource(probeMigration))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait ignored cancellation: %v", err)
	}
}
