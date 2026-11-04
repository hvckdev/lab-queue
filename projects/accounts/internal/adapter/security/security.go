package security

import (
	"labqueue/pkg/platform/infra"
	"labqueue/projects/accounts/internal/model"
)

// Tokens maps the account model into the JWT infrastructure's identity claims.
type Tokens struct{ client *infra.Tokens }

func NewTokens(client *infra.Tokens) *Tokens { return &Tokens{client: client} }
func (t *Tokens) Issue(user model.User) (string, error) {
	return t.client.Issue(infra.User{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role, GroupID: user.GroupID})
}

// Passwords supplies the service port using the shared cryptography client.
type Passwords struct{ infra.Passwords }
