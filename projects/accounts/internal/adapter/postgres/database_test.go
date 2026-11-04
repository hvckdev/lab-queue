package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateAdoptsLegacyAccounts(t *testing.T) {
	url := os.Getenv("TEST_ACCOUNTS_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_ACCOUNTS_DATABASE_URL for legacy migration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("accounts_legacy_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial, err := migrations.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Split(string(initial), "-- +goose Down")[0]
	legacy = strings.ReplaceAll(legacy, "CREATE TABLE IF NOT EXISTS", "CREATE TABLE")
	if _, err := db.Exec(ctx, legacy+`
CREATE TABLE accounts_schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp());
INSERT INTO accounts_schema_migrations(name) VALUES ('migrations/001_initial.sql');
INSERT INTO student_groups(id,name) VALUES (10,'Существующая группа');
INSERT INTO users(name,email,password_hash,role,group_id) VALUES ('Существующий студент','legacy@example.com','original-password-hash','student',10);
`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var hash string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE email='legacy@example.com'").Scan(&hash); err != nil || hash != "original-password-hash" {
		t.Fatalf("legacy account was changed: hash=%q err=%v", hash, err)
	}
	var version, legacyHistory int
	if err := db.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil || version != 3 {
		t.Fatalf("goose version=%d err=%v", version, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM accounts_schema_migrations").Scan(&legacyHistory); err != nil || legacyHistory != 1 {
		t.Fatalf("legacy history was changed: count=%d err=%v", legacyHistory, err)
	}
	var groupID int64
	if err := db.QueryRow(ctx, "SELECT group_id FROM users WHERE email='legacy@example.com'").Scan(&groupID); err != nil || groupID != 10 {
		t.Fatalf("legacy group reference was changed: id=%d error=%v", groupID, err)
	}
	group, err := NewGroups(db).Create(ctx, "Новая группа после миграции")
	if err != nil || group.ID <= 10 {
		t.Fatalf("group identity was not advanced past legacy IDs: group=%+v error=%v", group, err)
	}
	// A rollback must refuse incompatible administrator data atomically rather
	// than deleting accounts to restore the earlier two-role schema.
	if _, err := db.Exec(ctx, "INSERT INTO users(name,email,password_hash,role) VALUES('Администратор','admin@example.com','existing-admin-hash','admin')"); err != nil {
		t.Fatal(err)
	}
	adminMigration, err := migrations.ReadFile("migrations/002_administration.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, downErr := tx.Exec(ctx, strings.Split(string(adminMigration), "-- +goose Down")[1])
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if downErr == nil {
		t.Fatal("rollback accepted incompatible administrator data")
	}
	var administrators int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM users WHERE role='admin'").Scan(&administrators); err != nil || administrators != 1 {
		t.Fatalf("failed rollback changed administrators: count=%d err=%v", administrators, err)
	}
}
