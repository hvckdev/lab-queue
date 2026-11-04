package service

import (
	"errors"
	"strings"
	"testing"

	"labqueue/projects/queue/internal/model"
)

func TestSubjectValidationRequiresAdminAndAcceptsOnlyBoundedText(t *testing.T) {
	rules := New()
	admin := model.Actor{ID: 1, Role: model.Admin}
	name, err := rules.PrepareSubject(admin, "  Распределённые системы  ")
	if err != nil || name != "Распределённые системы" {
		t.Fatalf("normalized subject=%q err=%v", name, err)
	}
	for _, role := range []model.Role{model.Student, model.Teacher, "administrator", ""} {
		_, err := rules.PrepareSubject(model.Actor{ID: 1, Role: role}, "Предмет")
		var problem *model.Error
		if !errors.As(err, &problem) || problem.Kind != model.Forbidden {
			t.Fatalf("role %q accepted: %v", role, err)
		}
	}
	for _, value := range []string{"", "  ", "bad\nname", "bad\x00name", strings.Repeat("я", 201), string([]byte{0xff})} {
		_, err := rules.PrepareSubject(admin, value)
		var problem *model.Error
		if !errors.As(err, &problem) || problem.Kind != model.Invalid {
			t.Fatalf("invalid name %q accepted: %v", value, err)
		}
	}
}
