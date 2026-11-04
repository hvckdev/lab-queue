// Package model contains transport-independent account entities and errors.
package model

import "errors"

const (
	Student = "student"
	Teacher = "teacher"
	Admin   = "admin"
)

type User struct {
	ID                int64
	Name, Email, Role string
	GroupID           *int64
}

type Group struct {
	ID   int64
	Name string
}
type Registration struct {
	Name, Email, Password string
	GroupID               int64
}
type Login struct{ Email, Password string }
type Setup struct{ Name, Email, Password string }
type NewUser struct {
	Name, Email, Password, Role string
	GroupID                     *int64
}
type UserFilter struct{ Limit, Offset int }
type UserList struct {
	Users []User
	Total int64
}
type Credentials struct {
	User         User
	PasswordHash string
}
type Authentication struct {
	User  User
	Token string
}

type ErrorKind string

const (
	Invalid      ErrorKind = "invalid"
	Unauthorized ErrorKind = "unauthorized"
	Forbidden    ErrorKind = "forbidden"
	NotFound     ErrorKind = "not_found"
	Conflict     ErrorKind = "conflict"
	Internal     ErrorKind = "internal"
)

type Fault struct {
	Kind          ErrorKind
	Code, Message string
	Cause         error
}

func (e *Fault) Error() string { return e.Code }
func (e *Fault) Unwrap() error { return e.Cause }
func Problem(kind ErrorKind, code, message string) error {
	return &Fault{Kind: kind, Code: code, Message: message}
}

// ErrNotFound is the repository port's missing-record result; services decide
// whether a missing record means invalid input, failed authentication or 404.
var ErrNotFound = errors.New("account record not found")
