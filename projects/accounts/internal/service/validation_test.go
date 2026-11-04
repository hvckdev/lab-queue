package service

import (
	"labqueue/projects/accounts/internal/model"
	"strings"
	"testing"
)

func TestRegistrationValidation(t *testing.T) {
	valid := model.Registration{Name: "  Алексей Иванов  ", Email: "  Student@Example.COM ", Password: "password123", GroupID: 1}
	if message := ValidateRegistration(&valid); message != nil {
		t.Fatal(message)
	}
	if valid.Name != "Алексей Иванов" || valid.Email != "student@example.com" {
		t.Fatalf("input was not normalized: %+v", valid)
	}
	cases := []struct {
		name   string
		modify func(*model.Registration)
	}{
		{"blank name", func(v *model.Registration) { v.Name = " \t " }},
		{"long name", func(v *model.Registration) { v.Name = strings.Repeat("я", 121) }},
		{"control character", func(v *model.Registration) { v.Name = "Имя\nФамилия" }},
		{"invalid email", func(v *model.Registration) { v.Email = "no-at-sign" }},
		{"display name", func(v *model.Registration) { v.Email = "Someone <student@example.com>" }},
		{"short password", func(v *model.Registration) { v.Password = "1234567" }},
		{"long password bytes", func(v *model.Registration) { v.Password = strings.Repeat("я", 37) }},
		{"missing group", func(v *model.Registration) { v.GroupID = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.modify(&input)
			if ValidateRegistration(&input) == nil {
				t.Fatal("expected invalid model.Registration")
			}
		})
	}
	valid.Password = strings.Repeat("x", 72)
	if message := ValidateRegistration(&valid); message != nil {
		t.Fatalf("bcrypt maximum password must work: %s", message)
	}
}

func TestEmailNormalization(t *testing.T) {
	for _, raw := range []string{"", "person", "Name <person@example.com>", "person@example.com\r\nBCC:other@example.com", strings.Repeat("a", 255) + "@example.com"} {
		if email, valid := NormalizeEmail(raw); valid {
			t.Errorf("accepted invalid email %q as %q", raw, email)
		}
	}
}
