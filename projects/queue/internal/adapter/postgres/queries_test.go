package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"labqueue/projects/queue/internal/model"
)

type invalidSQL struct{ err error }

func (q invalidSQL) ToSQL() (string, []any, error) { return "", nil, q.err }

func TestBuildErrorsNeverReachDriver(t *testing.T) {
	failure := errors.New("invalid dataset")
	ctx := context.Background()
	if err := queryRow(ctx, nil, invalidSQL{failure}).Scan(new(int)); !errors.Is(err, failure) {
		t.Fatalf("row build error = %v", err)
	}
	if _, err := query(ctx, nil, invalidSQL{failure}); !errors.Is(err, failure) {
		t.Fatalf("query build error = %v", err)
	}
	if err := execute(ctx, nil, invalidSQL{failure}); !errors.Is(err, failure) {
		t.Fatalf("exec build error = %v", err)
	}
}

func TestPostgresErrorClassificationIsConstraintSpecific(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
	}{
		{"entries_one_active_student", "already_in_queue"},
		{"entries_one_called", "student_called"},
		{"subjects_name_key", "subject_exists"},
		{"subjects_name_normalized_key", "subject_exists"},
	} {
		err := persistenceError(fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505", ConstraintName: tc.name}))
		var problem *model.Error
		if !errors.As(err, &problem) || problem.Kind != model.Conflict || problem.Code != tc.code {
			t.Fatalf("constraint %s: %v", tc.name, err)
		}
	}
	for _, pgerr := range []*pgconn.PgError{
		{Code: "23505", ConstraintName: "entries_session_id_ticket_number_key"},
		{Code: "23505", ConstraintName: "idempotency_pkey"},
		{Code: "23503", ConstraintName: "sessions_subject_id_fkey"},
		{Code: "40001"},
	} {
		if got := persistenceError(pgerr); got != pgerr {
			t.Fatalf("unexpectedly reclassified %s/%s: %v", pgerr.Code, pgerr.ConstraintName, got)
		}
	}
	if !errors.Is(persistenceError(pgx.ErrNoRows), model.ErrNotFound) {
		t.Fatal("no rows must remain not found")
	}
}
