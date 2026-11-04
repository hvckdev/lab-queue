// Package handler adapts browser HTTP, cookies, and static assets to use cases.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"labqueue/projects/web/internal/model"
)

const cookieName = "labqueue_session"

type UseCases interface {
	Routes() []model.Route
	Authenticate(context.Context, model.Request) (model.Result, error)
	Proxy(context.Context, model.Request) (model.Response, error)
	Logout() model.Session
	Ready(context.Context) error
}

type Handler struct {
	usecases UseCases
	origin   string
	secure   bool
	mux      *http.ServeMux
}

func New(usecases UseCases, publicOrigin string, cookieSecure bool, assets fs.FS) (http.Handler, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PUBLIC_ORIGIN must be an HTTP(S) origin without a path")
	}
	static, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		return nil, err
	}
	h := &Handler{usecases: usecases, origin: strings.TrimRight(publicOrigin, "/"), secure: cookieSecure, mux: http.NewServeMux()}
	for _, route := range usecases.Routes() {
		var action http.HandlerFunc
		switch route.Operation {
		case model.Authenticate:
			action = h.authenticate
		case model.Logout:
			action = h.logout
		default:
			action = h.proxy
		}
		h.mux.HandleFunc(route.Method+" "+route.Path, action)
	}
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	h.mux.HandleFunc("GET /readyz", h.ready)
	h.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	h.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Origin") != h.origin {
				writeError(w, &model.Fault{Status: 403, Code: "origin", Message: "Запрос разрешен только со страницы приложения"})
				return
			}
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeError(w, &model.Fault{Status: 415, Code: "content_type", Message: "Ожидается application/json"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
	}
	h.mux.ServeHTTP(w, r)
}

func browserRequest(w http.ResponseWriter, r *http.Request) (model.Request, bool) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				writeError(w, &model.Fault{Status: 413, Code: "body_too_large", Message: "Запрос слишком большой"})
			} else {
				writeError(w, &model.Fault{Status: 400, Code: "request_body", Message: "Не удалось прочитать запрос"})
			}
			return model.Request{}, false
		}
		if len(body) > 1<<20 {
			writeError(w, &model.Fault{Status: 413, Code: "body_too_large", Message: "Запрос слишком большой"})
			return model.Request{}, false
		}
	}
	request := model.Request{Method: r.Method, Path: r.URL.Path, RawPath: r.URL.RawPath, Query: r.URL.RawQuery, Body: body, ContentType: r.Header.Get("Content-Type"), IdempotencyKey: r.Header.Get("Idempotency-Key")}
	if cookie, err := r.Cookie(cookieName); err == nil {
		request.Token = cookie.Value
	}
	return request, true
}

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request) {
	request, ok := browserRequest(w, r)
	if !ok {
		return
	}
	response, err := h.usecases.Proxy(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeResponse(w, response)
}

type userDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Role    string `json:"role"`
	GroupID *int64 `json:"group_id"`
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) {
	request, ok := browserRequest(w, r)
	if !ok {
		return
	}
	result, err := h.usecases.Authenticate(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	if result.Session == nil || result.User == nil {
		writeResponse(w, result.Response)
		return
	}
	h.setCookie(w, *result.Session)
	user := result.User
	writeJSON(w, result.Response.Status, map[string]any{"user": userDTO{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role, GroupID: user.GroupID}})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.setCookie(w, h.usecases.Logout())
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Handler) setCookie(w http.ResponseWriter, session model.Session) {
	expires := time.Now().Add(time.Duration(session.MaxAgeSeconds) * time.Second)
	if session.MaxAgeSeconds < 0 {
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: session.Token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: session.MaxAgeSeconds, Expires: expires})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.usecases.Ready(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func writeResponse(w http.ResponseWriter, response model.Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	var fault *model.Fault
	if !errors.As(err, &fault) {
		fault = model.Unavailable()
	}
	writeJSON(w, fault.Status, map[string]any{"error": map[string]string{"code": fault.Code, "message": fault.Message}})
}
