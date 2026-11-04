package handler

import (
	"net/http"
	"strconv"

	"labqueue/pkg/platform"
	"labqueue/projects/accounts/internal/model"
)

func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	required, err := s.usecases.SetupRequired(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 200, map[string]bool{"required": required})
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var dto struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !platform.DecodeJSON(w, r, &dto) {
		return
	}
	result, err := s.usecases.Setup(r.Context(), model.Setup{Name: dto.Name, Email: dto.Email, Password: dto.Password})
	if err != nil {
		fail(w, err)
		return
	}
	authResponse(w, 201, result)
}

func (s *server) users(w http.ResponseWriter, r *http.Request) {
	filter := model.UserFilter{Limit: 50}
	for key, target := range map[string]*int{"limit": &filter.Limit, "offset": &filter.Offset} {
		if raw := r.URL.Query().Get(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 || key == "limit" && value == 0 {
				fail(w, model.Problem(model.Invalid, "invalid_pagination", "Некорректные параметры списка пользователей"))
				return
			}
			*target = value
		}
	}
	result, err := s.usecases.Users(r.Context(), platform.CurrentUser(r.Context()).ID, filter)
	if err != nil {
		fail(w, err)
		return
	}
	dtos := make([]userDTO, len(result.Users))
	for i, user := range result.Users {
		dtos[i] = userResponse(user)
	}
	w.Header().Set("Cache-Control", "no-store")
	platform.JSON(w, 200, map[string]any{"users": dtos, "total": result.Total})
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var dto struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
		GroupID  *int64 `json:"group_id"`
	}
	if !platform.DecodeJSON(w, r, &dto) {
		return
	}
	user, err := s.usecases.CreateUser(r.Context(), platform.CurrentUser(r.Context()).ID,
		model.NewUser{Name: dto.Name, Email: dto.Email, Password: dto.Password, Role: dto.Role, GroupID: dto.GroupID})
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	platform.JSON(w, 201, map[string]any{"user": userResponse(user)})
}

func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	var dto struct {
		Name string `json:"name"`
	}
	if !platform.DecodeJSON(w, r, &dto) {
		return
	}
	group, err := s.usecases.CreateGroup(r.Context(), platform.CurrentUser(r.Context()).ID, dto.Name)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	platform.JSON(w, 201, map[string]any{"group": groupResponse(group)})
}
