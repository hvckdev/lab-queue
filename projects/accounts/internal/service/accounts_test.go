package service

import (
	"context"
	"errors"
	"testing"

	"labqueue/projects/accounts/internal/model"
)

type userStore struct {
	created     int
	input       model.Registration
	hash, email string
	credentials model.Credentials
	err         error
}

func (f *userStore) CreateStudent(_ context.Context, input model.Registration, hash string) (model.User, error) {
	f.created++
	f.input = input
	f.hash = hash
	return model.User{ID: 1, Name: input.Name, Email: input.Email, Role: model.Student, GroupID: &input.GroupID}, f.err
}
func (f *userStore) ByEmail(_ context.Context, email string) (model.Credentials, error) {
	f.email = email
	return f.credentials, f.err
}
func (f *userStore) ByID(context.Context, int64) (model.User, error) {
	return f.credentials.User, f.err
}

type groupStore struct{ err error }

func (g groupStore) Get(context.Context, int64) (model.Group, error) {
	return model.Group{ID: 1, Name: "ИВТ-101"}, g.err
}
func (g groupStore) List(context.Context) ([]model.Group, error) {
	return []model.Group{{ID: 1, Name: "ИВТ-101"}}, g.err
}
func (g groupStore) Create(_ context.Context, name string) (model.Group, error) {
	return model.Group{ID: 3, Name: name}, g.err
}

type passwordClient struct {
	hashed, verified int
	lastHash         string
	verifyErr        error
}

func (p *passwordClient) Hash(string) (string, error) { p.hashed++; return "encoded", nil }
func (p *passwordClient) Verify(hash, password string) error {
	p.verified++
	p.lastHash = hash
	return p.verifyErr
}

type tokenClient struct{}

func (tokenClient) Issue(model.User) (string, error) { return "signed", nil }

func TestRegisterChecksGroupBeforeHashingAndPersistsNormalizedStudent(t *testing.T) {
	ctx := context.Background()
	users := &userStore{}
	passwords := &passwordClient{}
	input := model.Registration{Name: "  Студент  ", Email: " STUDENT@EXAMPLE.COM ", Password: "Password123!", GroupID: 1}
	service := NewAccounts(users, groupStore{err: model.ErrNotFound}, passwords, tokenClient{})
	if _, err := service.Register(ctx, input); err == nil {
		t.Fatal("unknown group accepted")
	}
	if passwords.hashed != 0 || users.created != 0 {
		t.Fatal("invalid group caused password or persistence work")
	}
	service = NewAccounts(users, groupStore{}, passwords, tokenClient{})
	user, err := service.Register(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != model.Student || users.input.Name != "Студент" || users.input.Email != "student@example.com" || users.hash != "encoded" {
		t.Fatalf("unexpected persisted student: %+v", users)
	}
}

func TestMissingLoginStillVerifiesPasswordAndKeepsUniformError(t *testing.T) {
	users := &userStore{err: model.ErrNotFound}
	passwords := &passwordClient{}
	service := NewAccounts(users, groupStore{}, passwords, tokenClient{})
	_, err := service.Authenticate(context.Background(), model.Login{Email: " MISSING@EXAMPLE.COM ", Password: "Password123!"})
	var fault *model.Fault
	if !errors.As(err, &fault) || fault.Kind != model.Unauthorized || fault.Code != "invalid_credentials" {
		t.Fatalf("unexpected error: %v", err)
	}
	if passwords.verified != 1 || passwords.lastHash != dummyPasswordHash || users.email != "missing@example.com" {
		t.Fatal("missing-account verification or normalization bypassed")
	}
}

func TestMissingProfileAndGroupHaveDifferentDomainOutcomes(t *testing.T) {
	accounts := NewAccounts(&userStore{err: model.ErrNotFound}, groupStore{}, &passwordClient{}, tokenClient{})
	_, err := accounts.Profile(context.Background(), 42)
	var profile *model.Fault
	if !errors.As(err, &profile) || profile.Kind != model.Unauthorized {
		t.Fatalf("profile error: %v", err)
	}
	_, err = NewGroups(groupStore{err: model.ErrNotFound}).Get(context.Background(), 42)
	var group *model.Fault
	if !errors.As(err, &group) || group.Kind != model.NotFound {
		t.Fatalf("group error: %v", err)
	}
}
