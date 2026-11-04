package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"labqueue/projects/accounts/internal/model"
)

func TestRegistrationPostgresErrors(t *testing.T) {
	for _, test := range []struct {
		state string
		kind  model.ErrorKind
		code  string
	}{
		{"23505", model.Conflict, "email_exists"},
		{"23503", model.Invalid, "group_not_found"},
	} {
		t.Run(test.state, func(t *testing.T) {
			err := registrationError(fmt.Errorf("insert user: %w", &pgconn.PgError{Code: test.state}))
			var fault *model.Fault
			if !errors.As(err, &fault) || fault.Kind != test.kind || fault.Code != test.code {
				t.Fatalf("unexpected domain error: %v", err)
			}
		})
	}
	unexpected := &pgconn.PgError{Code: "08006"}
	if err := registrationError(unexpected); !errors.Is(err, unexpected) {
		t.Fatalf("unexpected database errors must be preserved: %v", err)
	}
	if err := registrationError(nil); err != nil {
		t.Fatal(err)
	}
}
