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

func TestMigrateAdoptsLegacyQueue(t *testing.T) {
	url := os.Getenv("TEST_QUEUE_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_QUEUE_DATABASE_URL for legacy migration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("queue_legacy_%d", time.Now().UnixNano())
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
	if _, err := db.Exec(ctx, legacy+`
INSERT INTO subjects(id,name) VALUES (1,'Существующий предмет');
INSERT INTO sessions(teacher_id,subject_id,group_id,group_name,lab_title,starts_at,ends_at,next_ticket)
VALUES (1,1,1,'ИВТ-101','Существующий сеанс',clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 hour',8);
INSERT INTO entries(session_id,student_id,student_name,ticket_number,status) VALUES (1,10,'Существующий студент',7,'called');
INSERT INTO idempotency(actor_id,key,fingerprint,status_code,response) VALUES (10,'original-key','original-fingerprint',201,'{"entry":{"id":1}}');
`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var nextTicket, ticket int64
	var status string
	if err := db.QueryRow(ctx, "SELECT s.next_ticket,e.ticket_number,e.status FROM sessions s JOIN entries e ON e.session_id=s.id WHERE s.id=1").Scan(&nextTicket, &ticket, &status); err != nil {
		t.Fatal(err)
	}
	if nextTicket != 8 || ticket != 7 || status != "called" {
		t.Fatalf("legacy queue changed: next=%d ticket=%d status=%s", nextTicket, ticket, status)
	}
	var version, saved int
	if err := db.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil || version != 2 {
		t.Fatalf("goose version=%d err=%v", version, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM idempotency WHERE key='original-key' AND response->'entry'->>'id'='1'").Scan(&saved); err != nil || saved != 1 {
		t.Fatalf("saved response changed: count=%d err=%v", saved, err)
	}
}
