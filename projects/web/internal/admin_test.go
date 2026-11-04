package app_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirstSetupKeepsCredentialsInHTTPOnlyCookie(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/setup" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			t.Error("setup must reach the public accounts setup endpoint without old credentials")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"token":"setup-private-token","user":{"id":9,"name":"Администратор","email":"admin@example.com","role":"admin","group_id":null},"password_hash":"must-not-leak"}`)
	}, false)
	r := browserRequest("POST", "/api/setup", `{"name":"Администратор","email":"admin@example.com","password":"test-only-setup-password"}`)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "old-teacher-session"})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"role":"admin"`) {
		t.Fatalf("setup failed: %d %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{"setup-private-token", "test-only-setup-password", "password_hash", `"token"`} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("setup exposed credentials to browser JavaScript")
		}
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName || cookies[0].Value != "setup-private-token" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("setup did not establish the protected admin cookie")
	}
}

func TestAdminCreationKeepsTheAdministratorSession(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/users" || r.Header.Get("Authorization") != "Bearer administrator-session" {
			t.Fatal("admin creation lost the caller's identity")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"user":{"id":12,"name":"Преподаватель","email":"new@example.com","role":"teacher","group_id":null}}`)
	}, false)
	r := browserRequest("POST", "/api/admin/users", `{"name":"Преподаватель","email":"new@example.com","password":"test-only-user-password","role":"teacher","group_id":null}`)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "administrator-session"})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusCreated || len(w.Result().Cookies()) != 0 {
		t.Fatalf("creating a user must not log in as that user: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "test-only-user-password") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("created user response exposed a password")
	}
}
