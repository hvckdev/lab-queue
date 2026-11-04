package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"testing/fstest"

	"labqueue/pkg/platform/infra"
	app "labqueue/projects/web/internal"
	"labqueue/projects/web/internal/adapter/httpclient"
	"labqueue/projects/web/internal/handler"
	"labqueue/projects/web/internal/service"
	"labqueue/projects/web/internal/usecase"
	"strings"
	"testing"
	"time"
)

const cookieName = "labqueue_session"

type testGateway struct {
	http.Handler
	client *http.Client
}

func newTestGateway(t *testing.T, upstreamHandler http.HandlerFunc, secure bool) *testGateway {
	t.Helper()
	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)
	client := infra.NewHTTPClient(8 * time.Second)
	adapter, err := httpclient.New(upstream.URL, upstream.URL, client)
	if err != nil {
		t.Fatal(err)
	}
	// Protocol tests use a deterministic fixture. The composition test below
	// independently verifies the real Vite bundle embedded in the Go binary.
	static := fstest.MapFS{
		"assets/index.html":      {Data: []byte(`<!doctype html><html><head><link rel="stylesheet" href="/assets/unit-bundle.css"></head><body><div id="root"></div><script type="module" src="/assets/unit-bundle.js"></script></body></html>`)},
		"assets/unit-bundle.js":  {Data: []byte(`document.getElementById('root').textContent = 'fixture';`)},
		"assets/unit-bundle.css": {Data: []byte(`body { color: black; }`)},
	}
	h, err := handler.New(usecase.New(service.New(adapter)), "http://localhost:8080", secure, static)
	if err != nil {
		t.Fatal(err)
	}
	return &testGateway{Handler: h, client: client}
}

func browserRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if method != http.MethodGet {
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestAuthenticationKeepsTokenOutOfBody(t *testing.T) {
	for _, path := range []string{"/api/auth/login", "/api/auth/register"} {
		t.Run(path, func(t *testing.T) {
			g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("browser credentials leaked upstream")
				}
				w.Header().Set("Set-Cookie", "upstream=must-not-leak")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"token":"private-token-value","user":{"id":1,"name":"Студент","role":"student","group_id":1}}`)
			}, true)
			w := httptest.NewRecorder()
			r := browserRequest("POST", path, `{"email":"a@example.com","password":"12345678"}`)
			r.Header.Set("Authorization", "Bearer attacker-value")
			g.ServeHTTP(w, r)
			if w.Code != 201 {
				t.Fatalf("status: %d, %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-token-value") || strings.Contains(w.Body.String(), `"token"`) {
				t.Fatal("JWT exposed to JavaScript")
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["user"] == nil {
				t.Fatal("missing user")
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("unexpected cookies: %v", cookies)
			}
			c := cookies[0]
			if c.Name != cookieName || c.Value != "private-token-value" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 28800 {
				t.Fatalf("unsafe cookie: %#v", c)
			}
		})
	}
}

func TestOriginAndContentTypeGuardEveryMutation(t *testing.T) {
	called := false
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = io.WriteString(w, `{}`) }, false)
	for _, path := range []string{"/api/setup", "/api/admin/users", "/api/admin/groups", "/api/admin/subjects", "/api/auth/login", "/api/auth/register", "/api/auth/logout", "/api/sessions", "/api/sessions/1/entries"} {
		for _, origin := range []string{"", "http://evil.example", "http://localhost:8080.evil.example"} {
			r := browserRequest("POST", path, `{}`)
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Errorf("%s Origin=%q: got %d", path, origin, w.Code)
			}
		}
		r := browserRequest("POST", path, `{}`)
		r.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != 415 {
			t.Errorf("%s: expected 415, got %d", path, w.Code)
		}
	}
	if called {
		t.Fatal("guarded request reached service")
	}
}

func TestProxyUsesOnlyCookieAuthorization(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cookie-token" {
			t.Errorf("wrong upstream authorization: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie leaked upstream")
		}
		if r.Header.Get("Idempotency-Key") != "action-123" {
			t.Error("idempotency key lost")
		}
		if r.URL.RawQuery != "subject_id=2" {
			t.Error("query lost")
		}
		_, _ = io.WriteString(w, `{"session":{"id":1}}`)
	}, false)
	r := browserRequest("POST", "/api/sessions?subject_id=2", `{}`)
	r.Header.Set("Authorization", "Bearer browser-controlled")
	r.Header.Set("Idempotency-Key", "action-123")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "cookie-token"})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated response may be cached")
	}
}

func TestBrowserBearerCannotBypassCookie(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unauthenticated request reached upstream") }, false)
	for _, path := range []string{"/api/auth/me", "/api/admin/users", "/api/subjects", "/api/sessions/1/queue"} {
		r := browserRequest("GET", path, "")
		r.Header.Set("Authorization", "Bearer injected")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%s got %d", path, w.Code)
		}
	}
}

func TestLogoutExpiresCookie(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("logout should be local") }, false)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, browserRequest("POST", "/api/auth/logout", `{}`))
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" || !cookies[0].Expires.Before(time.Now()) {
		t.Fatal("session cookie not deleted")
	}
}

func TestExpiredProxyRequestDoesNotDeleteNewerLoginCookie(t *testing.T) {
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"Старый сеанс истек"}}`)
	}, false)
	r := browserRequest("GET", "/api/sessions", "")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "expired-old-token"})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("a late 401 must not overwrite or delete a newer login cookie")
	}
}

func TestUpstreamFailures(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "<html>private stack trace</html>")
		}, false)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, browserRequest("POST", "/api/auth/login", `{}`))
		if w.Code != 502 || strings.Contains(w.Body.String(), "stack trace") {
			t.Fatalf("unsafe response: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("timeout", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, false)
		g.client.Timeout = 20 * time.Millisecond
		w := httptest.NewRecorder()
		g.ServeHTTP(w, browserRequest("GET", "/api/groups", ""))
		if w.Code != 502 {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("redirect is not followed", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/unexpected", 302) }, false)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, browserRequest("GET", "/api/groups", ""))
		if w.Code != 502 {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("failed login sets no cookie", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"Неверный пароль"}}`)
		}, false)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, browserRequest("POST", "/api/auth/login", `{}`))
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("failed login created cookie")
		}
	})
}

func TestReadyAndStatic(t *testing.T) {
	status := 200
	g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = io.WriteString(w, `{}`) }, false)
	for _, test := range []struct {
		path string
		want int
	}{{"/", 200}, {"/healthz", 200}, {"/readyz", 200}, {"/api/internal", 404}} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, browserRequest("GET", test.path, ""))
		if w.Code != test.want {
			t.Errorf("%s: got %d", test.path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Error("CSP missing")
		}
	}
	verifyFrontendAssets(t, g)
	status = 503
	w := httptest.NewRecorder()
	g.ServeHTTP(w, browserRequest("GET", "/readyz", ""))
	if w.Code != 503 {
		t.Fatalf("ready while dependency unavailable: %d", w.Code)
	}
}

func TestInvalidOriginsRejectedAtStartup(t *testing.T) {
	for _, value := range []string{"", "file:///tmp/foo", "http://user:pass@service", "http://service/api", "http://service?token=abc"} {
		if _, err := app.New(value, "http://queue", "http://localhost:8080", false); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestCompositionFacadeServesEmbeddedUIAndProtectsAPI(t *testing.T) {
	h, err := app.New("http://accounts.invalid", "http://queue.invalid", "http://localhost:8080", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/healthz", 200}, {"/api/sessions", 401}, {"/api/internal", 404}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, browserRequest("GET", test.path, ""))
		if w.Code != test.status {
			t.Errorf("%s: got %d", test.path, w.Code)
		}
	}
	verifyFrontendAssets(t, h)
}

var frontendRefs = regexp.MustCompile(`(?:src|href)=["'](/assets/[^"'<>]+)["']`)
var frontendMount = regexp.MustCompile(`id=["']root["']`)

func verifyFrontendAssets(t *testing.T, h http.Handler) {
	t.Helper()
	index := httptest.NewRecorder()
	h.ServeHTTP(index, browserRequest("GET", "/", ""))
	if index.Code != http.StatusOK || !strings.HasPrefix(index.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("frontend index unavailable: %d %s", index.Code, index.Header().Get("Content-Type"))
	}
	if !frontendMount.Match(index.Body.Bytes()) {
		t.Fatal("frontend index is missing the React root mount; rebuild the frontend")
	}
	seen := map[string]bool{}
	hasJS, hasCSS := false, false
	for _, match := range frontendRefs.FindAllStringSubmatch(index.Body.String(), -1) {
		ref := match[1]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		extension := path.Ext(strings.SplitN(ref, "?", 2)[0])
		if extension != ".js" && extension != ".css" {
			continue
		}
		asset := httptest.NewRecorder()
		h.ServeHTTP(asset, browserRequest("GET", ref, ""))
		if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
			t.Fatalf("referenced frontend asset %q is unavailable: %d", ref, asset.Code)
		}
		contentType := asset.Header().Get("Content-Type")
		if extension == ".js" {
			hasJS = true
			if !strings.HasPrefix(contentType, "text/javascript") && !strings.HasPrefix(contentType, "application/javascript") {
				t.Errorf("JavaScript asset %q has unexpected type %q", ref, contentType)
			}
		} else {
			hasCSS = true
			if !strings.HasPrefix(contentType, "text/css") {
				t.Errorf("stylesheet %q has unexpected type %q", ref, contentType)
			}
		}
		assertFrontendCSP(t, asset.Header().Get("Content-Security-Policy"))
	}
	if !hasJS || !hasCSS {
		t.Fatal("frontend index must reference its built JavaScript and CSS bundles")
	}
	assertFrontendCSP(t, index.Header().Get("Content-Security-Policy"))
}

func assertFrontendCSP(t *testing.T, policy string) {
	t.Helper()
	for _, directive := range []string{"script-src 'self'", "style-src 'self'", "font-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("CSP is missing %q: %s", directive, policy)
		}
	}
	if strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "unsafe-eval") {
		t.Errorf("frontend CSP was weakened: %s", policy)
	}
}
