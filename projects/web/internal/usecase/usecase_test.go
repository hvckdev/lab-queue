package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"labqueue/projects/web/internal/model"
	"labqueue/projects/web/internal/service"
	"labqueue/projects/web/internal/usecase"
)

type fakeUpstream struct {
	request  model.Request
	target   model.Target
	calls    int
	response model.Response
	auth     model.AuthExchange
	err      error
	ready    func(context.Context, model.Target) error
}

func (f *fakeUpstream) Exchange(_ context.Context, target model.Target, request model.Request) (model.Response, error) {
	f.calls++
	f.target = target
	f.request = request
	return f.response, f.err
}
func (f *fakeUpstream) Authenticate(_ context.Context, request model.Request) (model.AuthExchange, error) {
	f.calls++
	f.request = request
	return f.auth, f.err
}
func (f *fakeUpstream) Ready(ctx context.Context, target model.Target) error {
	if f.ready != nil {
		return f.ready(ctx, target)
	}
	return nil
}

func wantFault(t *testing.T, err error, status int, code string) {
	t.Helper()
	var fault *model.Fault
	if !errors.As(err, &fault) || fault.Status != status || fault.Code != code {
		t.Fatalf("expected %d %s, got %#v", status, code, err)
	}
}

func TestProxyAppliesAllowlistAndAuthenticationBeforeCallingPort(t *testing.T) {
	for _, test := range []struct {
		name, method, path string
		status             int
		code               string
	}{
		{"protected route", "GET", "/api/sessions", 401, "unauthorized"},
		{"admin user list", "GET", "/api/admin/users", 401, "unauthorized"},
		{"admin user creation", "POST", "/api/admin/users", 401, "unauthorized"},
		{"admin group creation", "POST", "/api/admin/groups", 401, "unauthorized"},
		{"admin subject creation", "POST", "/api/admin/subjects", 401, "unauthorized"},
		{"unknown route", "GET", "/api/internal", 404, "not_found"},
		{"wrong method", "DELETE", "/api/sessions/1", 404, "not_found"},
		{"auth cannot use raw proxy", "POST", "/api/auth/login", 404, "not_found"},
		{"empty path id", "GET", "/api/sessions//queue", 404, "not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &fakeUpstream{}
			_, err := usecase.New(service.New(port)).Proxy(context.Background(), model.Request{Method: test.method, Path: test.path})
			wantFault(t, err, test.status, test.code)
			if port.calls != 0 {
				t.Fatal("rejected request reached outbound port")
			}
		})
	}
}

func TestProxyRoutesToTheCorrectServiceAndSanitizesPublicCredentials(t *testing.T) {
	port := &fakeUpstream{response: model.Response{Status: 200, Body: []byte(`{"groups":[]}`)}}
	u := usecase.New(service.New(port))
	response, err := u.Proxy(context.Background(), model.Request{Method: "GET", Path: "/api/groups/1", Token: "old-cookie-token", Query: "extra=value"})
	if err != nil || response.Status != 200 {
		t.Fatalf("proxy: %v", err)
	}
	if port.target != model.Accounts || port.request.Token != "" {
		t.Fatal("public lookup must reach accounts without a credential")
	}
	request := model.Request{Method: "POST", Path: "/api/sessions/1/entries/4/complete", Token: "cookie-token", Body: []byte(`{}`), IdempotencyKey: "one-action", ContentType: "application/json"}
	if _, err = u.Proxy(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if port.target != model.Queue || port.request.Token != request.Token || port.request.IdempotencyKey != request.IdempotencyKey || string(port.request.Body) != string(request.Body) {
		t.Fatal("protected command lost trusted request values")
	}
}

func TestAuthenticateReturnsCredentialOnlyAsSession(t *testing.T) {
	port := &fakeUpstream{auth: model.AuthExchange{Response: model.Response{Status: 201, Body: []byte(`{"token":"private-token"}`)}, Token: "private-token", User: &model.User{ID: 1, Name: "Преподаватель", Role: "teacher"}}}
	result, err := usecase.New(service.New(port)).Authenticate(context.Background(), model.Request{Method: "POST", Path: "/api/auth/login", Token: "old-cookie"})
	if err != nil {
		t.Fatal(err)
	}
	if port.request.Token != "" {
		t.Fatal("old credential sent to login")
	}
	if result.Session == nil || result.Session.Token != "private-token" || result.Session.MaxAgeSeconds != 28800 {
		t.Fatal("session policy missing")
	}
	if len(result.Response.Body) != 0 || result.User == nil || result.User.Name != "Преподаватель" {
		t.Fatal("authentication must return a sanitized result, never upstream JSON")
	}
}

func TestAuthenticateRejectsMalformedIdentityAndKeepsFailuresCookieless(t *testing.T) {
	group := int64(1)
	valid := &model.User{ID: 1, Name: "Студент", Role: "student", GroupID: &group}
	for _, test := range []struct {
		name, token string
		user        *model.User
	}{
		{"missing token", "", valid},
		{"missing user", "token", nil},
		{"invalid identity", "token", &model.User{ID: 0, Name: "Студент", Role: "teacher"}},
		{"unknown role", "token", &model.User{ID: 1, Name: "Студент", Role: "owner"}},
		{"admin with group", "token", &model.User{ID: 1, Name: "Администратор", Role: "admin", GroupID: &group}},
		{"teacher with group", "token", &model.User{ID: 1, Name: "Преподаватель", Role: "teacher", GroupID: &group}},
		{"student without group", "token", &model.User{ID: 1, Name: "Студент", Role: "student"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &fakeUpstream{auth: model.AuthExchange{Response: model.Response{Status: 200}, Token: test.token, User: test.user}}
			_, err := usecase.New(service.New(port)).Authenticate(context.Background(), model.Request{Method: "POST", Path: "/api/auth/register"})
			wantFault(t, err, 502, "upstream_response")
		})
	}
	port := &fakeUpstream{auth: model.AuthExchange{Response: model.Response{Status: 401, Body: []byte(`{"error":{"code":"unauthorized"}}`)}, Token: "must-not-be-used", User: valid}}
	result, err := usecase.New(service.New(port)).Authenticate(context.Background(), model.Request{Method: "POST", Path: "/api/auth/login"})
	if err != nil || result.Response.Status != 401 || result.Session != nil || result.User != nil {
		t.Fatal("failed authentication produced a browser session")
	}
}

func TestUnknownPortErrorsAreSanitized(t *testing.T) {
	port := &fakeUpstream{err: errors.New("private connection credential")}
	_, err := usecase.New(service.New(port)).Proxy(context.Background(), model.Request{Method: "GET", Path: "/api/groups"})
	wantFault(t, err, 502, "upstream_unavailable")
	if strings.Contains(err.Error(), "credential") {
		t.Fatal("port error leaked")
	}
}

func TestReadinessChecksBothDependenciesWithinScenarioDeadline(t *testing.T) {
	var targets []model.Target
	port := &fakeUpstream{ready: func(ctx context.Context, target model.Target) error {
		targets = append(targets, target)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("scenario readiness deadline missing")
		}
		if target == model.Queue {
			return errors.New("unavailable")
		}
		return nil
	}}
	u := usecase.New(service.New(port))
	wantFault(t, u.Ready(context.Background()), 503, "not_ready")
	if len(targets) != 2 || targets[0] != model.Accounts || targets[1] != model.Queue {
		t.Fatal("readiness skipped a dependency")
	}
	if session := u.Logout(); session.Token != "" || session.MaxAgeSeconds != -1 {
		t.Fatal("logout must request session removal")
	}
}
