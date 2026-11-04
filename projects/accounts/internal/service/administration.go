package service

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"labqueue/projects/accounts/internal/model"
)

type AdministrationRepository interface {
	SetupRequired(context.Context) (bool, error)
	Initialize(context.Context, model.Setup, string) (model.User, error)
	CreateUser(context.Context, model.NewUser, string) (model.User, error)
	ListUsers(context.Context, model.UserFilter) (model.UserList, error)
}

type AdministrativeGroupRepository interface {
	GroupRepository
	Create(context.Context, string) (model.Group, error)
}

type Administration struct {
	users     AdministrationRepository
	profiles  AccountRepository
	groups    AdministrativeGroupRepository
	passwords Passwords
}

func NewAdministration(users AdministrationRepository, profiles AccountRepository, groups AdministrativeGroupRepository, passwords Passwords) *Administration {
	return &Administration{users: users, profiles: profiles, groups: groups, passwords: passwords}
}

func (s *Administration) CreateGroup(ctx context.Context, actorID int64, name string) (model.Group, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return model.Group{}, err
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return model.Group{}, model.Problem(model.Invalid, "invalid_group", "Укажите название группы длиной от 1 до 100 символов")
	}
	return s.groups.Create(ctx, name)
}

func (s *Administration) SetupRequired(ctx context.Context) (bool, error) {
	return s.users.SetupRequired(ctx)
}

func validateAccount(name, email *string, password string) error {
	*name = strings.TrimSpace(*name)
	if !utf8.ValidString(*name) || utf8.RuneCountInString(*name) < 1 || utf8.RuneCountInString(*name) > 120 || strings.IndexFunc(*name, unicode.IsControl) >= 0 {
		return model.Problem(model.Invalid, "invalid_user", "Укажите имя длиной от 1 до 120 символов")
	}
	normalized, valid := NormalizeEmail(*email)
	if !valid {
		return model.Problem(model.Invalid, "invalid_user", "Укажите корректный email")
	}
	*email = normalized
	if !utf8.ValidString(password) || len(password) < 8 || len(password) > 72 {
		return model.Problem(model.Invalid, "invalid_user", "Пароль должен содержать от 8 до 72 байт")
	}
	return nil
}

func (s *Administration) Setup(ctx context.Context, input model.Setup) (model.User, error) {
	required, err := s.users.SetupRequired(ctx)
	if err != nil {
		return model.User{}, err
	}
	if !required {
		return model.User{}, model.Problem(model.Conflict, "setup_completed", "Первичная настройка уже завершена")
	}
	if err := validateAccount(&input.Name, &input.Email, input.Password); err != nil {
		return model.User{}, err
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return model.User{}, err
	}
	return s.users.Initialize(ctx, input, hash)
}

func (s *Administration) requireAdmin(ctx context.Context, actorID int64) error {
	if actorID <= 0 {
		return model.Problem(model.Unauthorized, "unauthorized", "Войдите в систему")
	}
	user, err := s.profiles.ByID(ctx, actorID)
	if errors.Is(err, model.ErrNotFound) {
		return model.Problem(model.Unauthorized, "unauthorized", "Учетная запись не найдена")
	}
	if err != nil {
		return err
	}
	if user.Role != model.Admin {
		return model.Problem(model.Forbidden, "forbidden", "Действие доступно только администратору")
	}
	return nil
}

func (s *Administration) CreateUser(ctx context.Context, actorID int64, input model.NewUser) (model.User, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return model.User{}, err
	}
	if err := validateAccount(&input.Name, &input.Email, input.Password); err != nil {
		return model.User{}, err
	}
	switch input.Role {
	case model.Student:
		if input.GroupID == nil || *input.GroupID <= 0 {
			return model.User{}, model.Problem(model.Invalid, "invalid_group", "Выберите учебную группу студента")
		}
		if _, err := s.groups.Get(ctx, *input.GroupID); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return model.User{}, model.Problem(model.Invalid, "group_not_found", "Выбранная группа не найдена")
			}
			return model.User{}, err
		}
	case model.Teacher, model.Admin:
		if input.GroupID != nil {
			return model.User{}, model.Problem(model.Invalid, "invalid_group", "Группа указывается только для студента")
		}
	default:
		return model.User{}, model.Problem(model.Invalid, "invalid_role", "Выберите роль студента, преподавателя или администратора")
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return model.User{}, err
	}
	return s.users.CreateUser(ctx, input, hash)
}

func (s *Administration) Users(ctx context.Context, actorID int64, filter model.UserFilter) (model.UserList, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return model.UserList{}, err
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 1_000_000 {
		return model.UserList{}, model.Problem(model.Invalid, "invalid_pagination", "Укажите limit от 1 до 100 и offset от 0 до 1000000")
	}
	return s.users.ListUsers(ctx, filter)
}
