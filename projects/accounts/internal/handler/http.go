// Package handler maps HTTP DTOs and authentication context to account use cases.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"labqueue/pkg/platform"
	"labqueue/projects/accounts/internal/model"
)

type UseCases interface {
	Register(context.Context, model.Registration) (model.Authentication, error)
	Login(context.Context, model.Login) (model.Authentication, error)
	Profile(context.Context, int64) (model.User, error)
	Groups(context.Context) ([]model.Group, error)
	Group(context.Context, int64) (model.Group, error)
	SetupRequired(context.Context) (bool, error)
	Setup(context.Context, model.Setup) (model.Authentication, error)
	Users(context.Context, int64, model.UserFilter) (model.UserList, error)
	CreateUser(context.Context, int64, model.NewUser) (model.User, error)
	CreateGroup(context.Context, int64, string) (model.Group, error)
}

type server struct{ usecases UseCases }

func New(usecases UseCases, tokens *platform.Tokens) http.Handler {
	s := &server{usecases: usecases}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/groups", s.groups)
	mux.HandleFunc("GET /api/groups/{id}", s.group)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.Handle("GET /api/auth/me", tokens.RequireAuth(http.HandlerFunc(s.me)))
	mux.HandleFunc("GET /api/setup", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.Handle("GET /api/admin/users", tokens.RequireAuth(http.HandlerFunc(s.users)))
	mux.Handle("POST /api/admin/users", tokens.RequireAuth(http.HandlerFunc(s.createUser)))
	mux.Handle("POST /api/admin/groups", tokens.RequireAuth(http.HandlerFunc(s.createGroup)))
	return mux
}

type userDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Role    string `json:"role"`
	GroupID *int64 `json:"group_id"`
}
type groupDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func userResponse(user model.User) userDTO {
	return userDTO{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role, GroupID: user.GroupID}
}
func groupResponse(group model.Group) groupDTO { return groupDTO{ID: group.ID, Name: group.Name} }

func fail(w http.ResponseWriter, err error) {
	var problem *model.Fault
	if errors.As(err, &problem) {
		status := map[model.ErrorKind]int{model.Invalid: 400, model.Unauthorized: 401, model.Forbidden: 403, model.NotFound: 404, model.Conflict: 409, model.Internal: 500}[problem.Kind]
		if status == 0 {
			status = 500
		}
		if status >= 500 {
			slog.Error("accounts operation failed", "error", err, "cause", problem.Cause)
		}
		platform.Error(w, status, problem.Code, problem.Message)
		return
	}
	slog.Error("accounts database operation failed", "error", err)
	platform.Error(w, 503, "database_unavailable", "Сервис аккаунтов временно недоступен")
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var dto struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		GroupID  int64  `json:"group_id"`
	}
	if !platform.DecodeJSON(w, r, &dto) {
		return
	}
	result, err := s.usecases.Register(r.Context(), model.Registration{Name: dto.Name, Email: dto.Email, Password: dto.Password, GroupID: dto.GroupID})
	if err != nil {
		fail(w, err)
		return
	}
	authResponse(w, 201, result)
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var dto struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !platform.DecodeJSON(w, r, &dto) {
		return
	}
	result, err := s.usecases.Login(r.Context(), model.Login{Email: dto.Email, Password: dto.Password})
	if err != nil {
		fail(w, err)
		return
	}
	authResponse(w, 200, result)
}
func authResponse(w http.ResponseWriter, status int, result model.Authentication) {
	w.Header().Set("Cache-Control", "no-store")
	platform.JSON(w, status, map[string]any{"token": result.Token, "user": userResponse(result.User)})
}
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.usecases.Profile(r.Context(), platform.CurrentUser(r.Context()).ID)
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"user": userResponse(user)})
}
func (s *server) groups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.usecases.Groups(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	dtos := make([]groupDTO, len(groups))
	for i, group := range groups {
		dtos[i] = groupResponse(group)
	}
	platform.JSON(w, 200, map[string]any{"groups": dtos})
}
func (s *server) group(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		platform.Error(w, 400, "invalid_id", "Некорректный номер группы")
		return
	}
	group, err := s.usecases.Group(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"group": groupResponse(group)})
}
