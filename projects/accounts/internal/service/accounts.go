// Package service implements account policies through ports owned by consumers.
package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"labqueue/projects/accounts/internal/model"
)

type AccountRepository interface {
	CreateStudent(context.Context, model.Registration, string) (model.User, error)
	ByEmail(context.Context, string) (model.Credentials, error)
	ByID(context.Context, int64) (model.User, error)
}
type GroupRepository interface {
	List(context.Context) ([]model.Group, error)
	Get(context.Context, int64) (model.Group, error)
}
type Passwords interface {
	Hash(string) (string, error)
	Verify(hash, password string) error
}
type TokenIssuer interface {
	Issue(model.User) (string, error)
}

type Accounts struct {
	users     AccountRepository
	groups    GroupRepository
	passwords Passwords
	tokens    TokenIssuer
}

func NewAccounts(users AccountRepository, groups GroupRepository, passwords Passwords, tokens TokenIssuer) *Accounts {
	return &Accounts{users: users, groups: groups, passwords: passwords, tokens: tokens}
}

func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) == 0 || len(email) > 254 {
		return "", false
	}
	address, err := mail.ParseAddress(email)
	return email, err == nil && address.Address == email && address.Name == ""
}

func ValidateRegistration(input *model.Registration) error {
	input.Name = strings.TrimSpace(input.Name)
	invalid := func(message string) error { return model.Problem(model.Invalid, "invalid_registration", message) }
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 120 || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		return invalid("Укажите имя длиной от 1 до 120 символов")
	}
	email, ok := NormalizeEmail(input.Email)
	if !ok {
		return invalid("Укажите корректный email")
	}
	input.Email = email
	if len(input.Password) < 8 || len(input.Password) > 72 {
		return invalid("Пароль должен содержать от 8 до 72 байт")
	}
	if input.GroupID <= 0 {
		return invalid("Выберите учебную группу")
	}
	return nil
}

func (s *Accounts) Register(ctx context.Context, input model.Registration) (model.User, error) {
	if err := ValidateRegistration(&input); err != nil {
		return model.User{}, err
	}
	if _, err := s.groups.Get(ctx, input.GroupID); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.User{}, model.Problem(model.Invalid, "group_not_found", "Выбранная группа не найдена")
		}
		return model.User{}, err
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return model.User{}, err
	}
	return s.users.CreateStudent(ctx, input, hash)
}

// Unknown accounts execute the same password verifier as existing accounts.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

func invalidCredentials() error {
	return model.Problem(model.Unauthorized, "invalid_credentials", "Неверный email или пароль")
}
func (s *Accounts) Authenticate(ctx context.Context, input model.Login) (model.User, error) {
	email, valid := NormalizeEmail(input.Email)
	if !valid || len(input.Password) == 0 || len(input.Password) > 72 {
		return model.User{}, invalidCredentials()
	}
	credentials, err := s.users.ByEmail(ctx, email)
	missing := errors.Is(err, model.ErrNotFound)
	if err != nil && !missing {
		return model.User{}, err
	}
	if missing {
		credentials.PasswordHash = dummyPasswordHash
	}
	if verifyErr := s.passwords.Verify(credentials.PasswordHash, input.Password); verifyErr != nil || missing {
		return model.User{}, invalidCredentials()
	}
	return credentials.User, nil
}

func (s *Accounts) StartSession(user model.User) (model.Authentication, error) {
	token, err := s.tokens.Issue(user)
	if err != nil {
		return model.Authentication{}, &model.Fault{Kind: model.Internal, Code: "internal_error", Message: "Не удалось выполнить вход", Cause: err}
	}
	return model.Authentication{User: user, Token: token}, nil
}

func (s *Accounts) Profile(ctx context.Context, id int64) (model.User, error) {
	user, err := s.users.ByID(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return model.User{}, model.Problem(model.Unauthorized, "unauthorized", "Учетная запись не найдена")
	}
	return user, err
}

type Groups struct{ repository GroupRepository }

func NewGroups(repository GroupRepository) *Groups                { return &Groups{repository: repository} }
func (s *Groups) List(ctx context.Context) ([]model.Group, error) { return s.repository.List(ctx) }
func (s *Groups) Get(ctx context.Context, id int64) (model.Group, error) {
	group, err := s.repository.Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return model.Group{}, model.Problem(model.NotFound, "group_not_found", "Группа не найдена")
	}
	return group, err
}
