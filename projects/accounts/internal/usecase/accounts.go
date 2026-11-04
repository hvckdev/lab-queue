// Package usecase coordinates account scenarios without knowing HTTP or storage.
package usecase

import (
	"context"
	"labqueue/projects/accounts/internal/model"
)

type AccountService interface {
	Register(context.Context, model.Registration) (model.User, error)
	Authenticate(context.Context, model.Login) (model.User, error)
	StartSession(model.User) (model.Authentication, error)
	Profile(context.Context, int64) (model.User, error)
}
type GroupService interface {
	List(context.Context) ([]model.Group, error)
	Get(context.Context, int64) (model.Group, error)
}
type AdministrationService interface {
	SetupRequired(context.Context) (bool, error)
	Setup(context.Context, model.Setup) (model.User, error)
	Users(context.Context, int64, model.UserFilter) (model.UserList, error)
	CreateUser(context.Context, int64, model.NewUser) (model.User, error)
	CreateGroup(context.Context, int64, string) (model.Group, error)
}

type Accounts struct {
	accounts AccountService
	groups   GroupService
	admin    AdministrationService
}

func New(accounts AccountService, groups GroupService, administration ...AdministrationService) *Accounts {
	u := &Accounts{accounts: accounts, groups: groups}
	if len(administration) > 0 {
		u.admin = administration[0]
	}
	return u
}
func (u *Accounts) Register(ctx context.Context, input model.Registration) (model.Authentication, error) {
	user, err := u.accounts.Register(ctx, input)
	if err != nil {
		return model.Authentication{}, err
	}
	return u.accounts.StartSession(user)
}
func (u *Accounts) Login(ctx context.Context, input model.Login) (model.Authentication, error) {
	user, err := u.accounts.Authenticate(ctx, input)
	if err != nil {
		return model.Authentication{}, err
	}
	return u.accounts.StartSession(user)
}
func (u *Accounts) Profile(ctx context.Context, id int64) (model.User, error) {
	return u.accounts.Profile(ctx, id)
}
func (u *Accounts) Groups(ctx context.Context) ([]model.Group, error) { return u.groups.List(ctx) }
func (u *Accounts) Group(ctx context.Context, id int64) (model.Group, error) {
	return u.groups.Get(ctx, id)
}

func (u *Accounts) SetupRequired(ctx context.Context) (bool, error) {
	return u.admin.SetupRequired(ctx)
}
func (u *Accounts) Setup(ctx context.Context, input model.Setup) (model.Authentication, error) {
	user, err := u.admin.Setup(ctx, input)
	if err != nil {
		return model.Authentication{}, err
	}
	return u.accounts.StartSession(user)
}
func (u *Accounts) Users(ctx context.Context, actorID int64, filter model.UserFilter) (model.UserList, error) {
	return u.admin.Users(ctx, actorID, filter)
}
func (u *Accounts) CreateUser(ctx context.Context, actorID int64, input model.NewUser) (model.User, error) {
	return u.admin.CreateUser(ctx, actorID, input)
}
func (u *Accounts) CreateGroup(ctx context.Context, actorID int64, name string) (model.Group, error) {
	return u.admin.CreateGroup(ctx, actorID, name)
}
