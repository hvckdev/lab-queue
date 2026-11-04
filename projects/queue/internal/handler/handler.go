package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"labqueue/pkg/platform"
	"labqueue/projects/queue/internal/model"
)

type UseCases interface {
	Subjects(context.Context) ([]model.Subject, error)
	CreateSubject(context.Context, model.Actor, string) (model.Subject, error)
	Sessions(context.Context, model.Actor, model.SessionFilter) ([]model.Session, error)
	Queue(context.Context, model.Actor, int64) (model.Snapshot, error)
	Create(context.Context, model.Actor, model.CreateSession, string) (model.Result, error)
	Join(context.Context, model.Actor, int64, string) (model.Result, error)
	CallNext(context.Context, model.Actor, int64, string) (model.Result, error)
	ChangeEntry(context.Context, model.Actor, int64, int64, model.EntryStatus) (model.Result, error)
	Close(context.Context, model.Actor, int64, bool) (model.Result, error)
}

type Handler struct{ usecases UseCases }

func New(usecases UseCases) http.Handler {
	h := &Handler{usecases: usecases}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/subjects", h.subjects)
	mux.HandleFunc("POST /api/admin/subjects", h.createSubject)
	mux.HandleFunc("GET /api/sessions", h.sessions)
	mux.HandleFunc("POST /api/sessions", h.create)
	mux.HandleFunc("GET /api/sessions/{id}/queue", h.queue)
	mux.HandleFunc("POST /api/sessions/{id}/entries", h.join)
	mux.HandleFunc("POST /api/sessions/{id}/call-next", h.callNext)
	mux.HandleFunc("POST /api/sessions/{id}/entries/{entry_id}/cancel", h.entryAction(model.Cancelled))
	mux.HandleFunc("POST /api/sessions/{id}/entries/{entry_id}/complete", h.entryAction(model.Completed))
	mux.HandleFunc("POST /api/sessions/{id}/entries/{entry_id}/skip", h.entryAction(model.Skipped))
	mux.HandleFunc("POST /api/sessions/{id}/close-registration", h.close(false))
	mux.HandleFunc("POST /api/sessions/{id}/finish", h.close(true))
	return mux
}

func actor(r *http.Request) model.Actor {
	user := platform.CurrentUser(r.Context())
	result := model.Actor{ID: user.ID, Name: user.Name, Role: model.Role(user.Role)}
	if user.GroupID != nil {
		result.GroupID = *user.GroupID
	}
	return result
}

func fail(w http.ResponseWriter, err error) {
	var domain *model.Error
	if errors.As(err, &domain) {
		status := 500
		switch domain.Kind {
		case model.Invalid:
			status = 400
		case model.Forbidden:
			status = 403
		case model.Conflict:
			status = 409
		case model.NotFound:
			status = 404
		case model.Unavailable:
			status = 503
		}
		platform.Error(w, status, domain.Code, domain.Message)
		return
	}
	slog.Error("queue request failed", "error", err)
	platform.Error(w, 500, "internal", "Не удалось выполнить действие. Попробуйте еще раз")
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, model.Problem(model.Invalid, "invalid_id", "Некорректный идентификатор")
	}
	return id, nil
}

func (h *Handler) subjects(w http.ResponseWriter, r *http.Request) {
	subjects, err := h.usecases.Subjects(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	type subject struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	result := make([]subject, 0, len(subjects))
	for _, item := range subjects {
		result = append(result, subject{item.ID, item.Name})
	}
	platform.JSON(w, 200, map[string]any{"subjects": result})
}

func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !platform.DecodeJSON(w, r, &input) {
		return
	}
	subject, err := h.usecases.CreateSubject(r.Context(), actor(r), input.Name)
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 201, map[string]any{"subject": struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}{subject.ID, subject.Name}})
}

func (h *Handler) sessions(w http.ResponseWriter, r *http.Request) {
	var filter model.SessionFilter
	for key, dest := range map[string]*int64{"subject_id": &filter.SubjectID, "group_id": &filter.GroupID} {
		if raw := r.URL.Query().Get(key); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				fail(w, model.Problem(model.Invalid, "invalid_filter", "Некорректный фильтр"))
				return
			}
			*dest = id
		}
	}
	sessions, err := h.usecases.Sessions(r.Context(), actor(r), filter)
	if err != nil {
		fail(w, err)
		return
	}
	result := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, sessionDTO(session))
	}
	platform.JSON(w, 200, map[string]any{"sessions": result})
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	snapshot, err := h.usecases.Queue(r.Context(), actor(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	entries := make([]Entry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		entries = append(entries, entryDTO(entry))
	}
	platform.JSON(w, 200, map[string]any{"session": sessionDTO(snapshot.Session), "entries": entries, "my_entry": optionalEntry(snapshot.MyEntry), "next_position": snapshot.NextPosition})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var input createRequest
	if !platform.DecodeJSON(w, r, &input) {
		return
	}
	result, err := h.usecases.Create(r.Context(), actor(r), model.CreateSession{SubjectID: input.SubjectID, GroupID: input.GroupID, LabTitle: input.LabTitle, StartsAt: input.StartsAt, EndsAt: input.EndsAt}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 201, map[string]any{"session": sessionDTO(*result.Session)})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	result, err := h.usecases.Join(r.Context(), actor(r), id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 201, map[string]any{"entry": optionalEntry(result.Entry)})
}

func (h *Handler) callNext(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	result, err := h.usecases.CallNext(r.Context(), actor(r), id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		fail(w, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"entry": optionalEntry(result.Entry)})
}

func (h *Handler) entryAction(target model.EntryStatus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			fail(w, err)
			return
		}
		entryID, err := pathID(r, "entry_id")
		if err != nil {
			fail(w, err)
			return
		}
		result, err := h.usecases.ChangeEntry(r.Context(), actor(r), id, entryID, target)
		if err != nil {
			fail(w, err)
			return
		}
		platform.JSON(w, 200, map[string]any{"entry": optionalEntry(result.Entry)})
	}
}

func (h *Handler) close(finish bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			fail(w, err)
			return
		}
		result, err := h.usecases.Close(r.Context(), actor(r), id, finish)
		if err != nil {
			fail(w, err)
			return
		}
		platform.JSON(w, 200, map[string]any{"session": sessionDTO(*result.Session)})
	}
}
