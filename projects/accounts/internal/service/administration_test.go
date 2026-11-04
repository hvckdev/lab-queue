package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"labqueue/projects/accounts/internal/model"
)

type administrationStore struct {
	required bool
	created  int
	input    model.NewUser
	hash     string
}

func (s *administrationStore) SetupRequired(context.Context) (bool, error) { return s.required, nil }
func (s *administrationStore) Initialize(_ context.Context, input model.Setup, hash string) (model.User, error) {
	s.required = false
	s.created++
	s.hash = hash
	return model.User{ID: 1, Name: input.Name, Email: input.Email, Role: model.Admin}, nil
}
func (s *administrationStore) CreateUser(_ context.Context, input model.NewUser, hash string) (model.User, error) {
	s.created++
	s.input, s.hash = input, hash
	return model.User{ID: 2, Name: input.Name, Email: input.Email, Role: input.Role, GroupID: input.GroupID}, nil
}
func (s *administrationStore) ListUsers(context.Context, model.UserFilter) (model.UserList, error) {
	return model.UserList{Users: []model.User{}}, nil
}

func TestSetupChecksPermanentMarkerBeforePasswordWork(t *testing.T) {
	store := &administrationStore{required: true}
	passwords := &passwordClient{}
	svc := NewAdministration(store, &userStore{}, groupStore{}, passwords)
	input := model.Setup{Name: "  Администратор ", Email: " ADMIN@EXAMPLE.COM ", Password: "Password123!"}
	user, err := svc.Setup(context.Background(), input)
	if err != nil || user.Role != model.Admin || user.Name != "Администратор" || user.Email != "admin@example.com" || store.hash != "encoded" {
		t.Fatalf("setup result=%+v error=%v", user, err)
	}
	if _, err := svc.Setup(context.Background(), input); err == nil {
		t.Fatal("repeated setup accepted")
	}
	if passwords.hashed != 1 || store.created != 1 {
		t.Fatal("completed setup performed password or account work again")
	}
}

func TestAdministrationUsesCurrentDatabaseRoleBeforeHashing(t *testing.T) {
	store := &administrationStore{}
	passwords := &passwordClient{}
	profiles := &userStore{credentials: model.Credentials{User: model.User{ID: 1, Role: model.Teacher}}}
	svc := NewAdministration(store, profiles, groupStore{}, passwords)
	input := model.NewUser{Name: "Преподаватель", Email: "teacher-new@example.com", Password: "Password123!", Role: model.Teacher}
	if _, err := svc.CreateUser(context.Background(), 1, input); err == nil {
		t.Fatal("non-admin created an account")
	}
	if _, err := svc.Users(context.Background(), 1, model.UserFilter{}); err == nil {
		t.Fatal("non-admin listed accounts")
	}
	if passwords.hashed != 0 || store.created != 0 {
		t.Fatal("denied actor caused password or persistence work")
	}
	profiles.credentials.User.Role = model.Admin
	user, err := svc.CreateUser(context.Background(), 1, input)
	if err != nil || user.Role != model.Teacher || store.hash != "encoded" {
		t.Fatalf("admin create failed: %+v %v", user, err)
	}
	profiles.err = model.ErrNotFound
	_, err = svc.Users(context.Background(), 1, model.UserFilter{})
	var problem *model.Fault
	if !errors.As(err, &problem) || problem.Kind != model.Unauthorized {
		t.Fatalf("missing actor error=%v", err)
	}
}

func TestAdminAccountValidationRejectsInvalidRolesGroupsAndPasswordBytes(t *testing.T) {
	group := int64(1)
	base := model.NewUser{Name: "Пользователь", Email: "user@example.com", Password: "Password123!", Role: model.Teacher}
	for _, mutate := range []func(*model.NewUser){
		func(u *model.NewUser) { u.Role = "owner" },
		func(u *model.NewUser) { u.Role = model.Student },
		func(u *model.NewUser) { u.GroupID = &group },
		func(u *model.NewUser) { u.Password = strings.Repeat("я", 37) },
		func(u *model.NewUser) { u.Name = "Name\nHidden" },
	} {
		store, passwords := &administrationStore{}, &passwordClient{}
		profiles := &userStore{credentials: model.Credentials{User: model.User{ID: 1, Role: model.Admin}}}
		svc := NewAdministration(store, profiles, groupStore{}, passwords)
		input := base
		mutate(&input)
		if _, err := svc.CreateUser(context.Background(), 1, input); err == nil {
			t.Fatalf("invalid input accepted for role %q", input.Role)
		}
		if passwords.hashed != 0 || store.created != 0 {
			t.Fatal("invalid input was hashed or persisted")
		}
	}
}
