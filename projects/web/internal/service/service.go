// Package service owns gateway routing, authentication, and session policies.
package service

import (
	"context"
	"errors"
	"strings"

	"labqueue/projects/web/internal/model"
)

// Upstream is the outbound port. Its concrete implementation handles HTTP/JSON.
type Upstream interface {
	Exchange(context.Context, model.Target, model.Request) (model.Response, error)
	Authenticate(context.Context, model.Request) (model.AuthExchange, error)
	Ready(context.Context, model.Target) error
}

type Service struct{ upstream Upstream }

func New(upstream Upstream) *Service { return &Service{upstream: upstream} }

// Routes is the single allowlist used by both HTTP registration and use cases.
func Routes() []model.Route {
	return []model.Route{
		{Method: "GET", Path: "/api/setup", Target: model.Accounts, Operation: model.Proxy},
		{Method: "POST", Path: "/api/setup", Target: model.Accounts, Operation: model.Authenticate},
		{Method: "POST", Path: "/api/auth/login", Target: model.Accounts, Operation: model.Authenticate},
		{Method: "POST", Path: "/api/auth/register", Target: model.Accounts, Operation: model.Authenticate},
		{Method: "POST", Path: "/api/auth/logout", Operation: model.Logout},
		{Method: "GET", Path: "/api/groups", Target: model.Accounts, Operation: model.Proxy},
		{Method: "GET", Path: "/api/groups/{id}", Target: model.Accounts, Operation: model.Proxy},
		{Method: "GET", Path: "/api/auth/me", Target: model.Accounts, Operation: model.Proxy, Authenticated: true},
		{Method: "GET", Path: "/api/admin/users", Target: model.Accounts, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/admin/users", Target: model.Accounts, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/admin/groups", Target: model.Accounts, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/admin/subjects", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "GET", Path: "/api/subjects", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "GET", Path: "/api/sessions", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "GET", Path: "/api/sessions/{id}/queue", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/entries", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/call-next", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/entries/{entry_id}/cancel", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/entries/{entry_id}/complete", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/entries/{entry_id}/skip", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/close-registration", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
		{Method: "POST", Path: "/api/sessions/{id}/finish", Target: model.Queue, Operation: model.Proxy, Authenticated: true},
	}
}

func (s *Service) Resolve(request model.Request, operation model.Operation) (model.Route, error) {
	method := request.Method
	if method == "HEAD" {
		method = "GET"
	}
	for _, route := range Routes() {
		if route.Method == method && route.Operation == operation && matches(route.Path, request.Path) {
			return route, nil
		}
	}
	return model.Route{}, model.NotFound()
}

func matches(pattern, path string) bool {
	want, got := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}
	for i, part := range want {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			if got[i] == "" {
				return false
			}
		} else if part != got[i] {
			return false
		}
	}
	return true
}

func (s *Service) Prepare(route model.Route, request model.Request) (model.Request, error) {
	if route.Authenticated && request.Token == "" {
		return model.Request{}, model.Unauthorized()
	}
	if !route.Authenticated {
		request.Token = ""
	}
	return request, nil
}

func (s *Service) Exchange(ctx context.Context, route model.Route, request model.Request) (model.Response, error) {
	response, err := s.upstream.Exchange(ctx, route.Target, request)
	return response, safeError(err)
}

func (s *Service) Authenticate(ctx context.Context, request model.Request) (model.AuthExchange, error) {
	exchange, err := s.upstream.Authenticate(ctx, request)
	return exchange, safeError(err)
}

// BrowserAuthentication never forwards the credential-bearing upstream body.
func (s *Service) BrowserAuthentication(exchange model.AuthExchange) (model.Result, error) {
	if exchange.Response.Status >= 400 {
		return model.Result{Response: exchange.Response}, nil
	}
	user := exchange.User
	if exchange.Response.Status < 200 || exchange.Response.Status >= 300 || exchange.Token == "" || user == nil || user.ID <= 0 || strings.TrimSpace(user.Name) == "" || (user.Role != "student" && user.Role != "teacher" && user.Role != "admin") || (user.Role == "student" && (user.GroupID == nil || *user.GroupID <= 0)) || (user.Role != "student" && user.GroupID != nil) {
		return model.Result{}, model.InvalidAuthentication()
	}
	return model.Result{Response: model.Response{Status: exchange.Response.Status}, User: user, Session: &model.Session{Token: exchange.Token, MaxAgeSeconds: 8 * 60 * 60}}, nil
}

func (s *Service) Logout() model.Session { return model.Session{MaxAgeSeconds: -1} }

func (s *Service) Ready(ctx context.Context) error {
	for _, target := range []model.Target{model.Accounts, model.Queue} {
		if s.upstream.Ready(ctx, target) != nil {
			return model.NotReady()
		}
	}
	return nil
}

func safeError(err error) error {
	if err == nil {
		return nil
	}
	var fault *model.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return model.Unavailable()
}
