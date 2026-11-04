package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenIdentityAndTampering(t *testing.T) {
	tokens, err := NewTokens(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	groupID := int64(1)
	u := User{ID: 42, Name: "Студент", Role: "student", GroupID: &groupID}
	token, err := tokens.Issue(u)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokens.Parse(token)
	if err != nil || got.ID != u.ID || got.GroupID == nil || *got.GroupID != groupID {
		t.Fatalf("identity = %+v, %v", got, err)
	}
	other, _ := NewTokens(strings.Repeat("x", 32))
	if _, err := other.Parse(token); err == nil {
		t.Fatal("token signed by another key accepted")
	}
	if _, err := tokens.Parse("not.a.token"); err == nil {
		t.Fatal("malformed token accepted")
	}
	if _, err := NewTokens("short"); err == nil {
		t.Fatal("short signing secret accepted")
	}
}

func TestAdminTokenIdentityAndRoleGroupValidation(t *testing.T) {
	tokens, _ := NewTokens(strings.Repeat("s", 32))
	admin := User{ID: 7, Name: "Администратор", Role: "admin"}
	token, err := tokens.Issue(admin)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokens.Parse(token)
	if err != nil || got.ID != admin.ID || got.Role != "admin" || got.GroupID != nil {
		t.Fatalf("admin token identity=%+v err=%v", got, err)
	}
	groupID := int64(1)
	for _, invalid := range []User{
		{ID: 7, Role: "admin", GroupID: &groupID},
		{ID: 7, Role: "teacher", GroupID: &groupID},
		{ID: 7, Role: "student"},
		{ID: 7, Role: "superadmin"},
	} {
		token, err := tokens.Issue(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tokens.Parse(token); err == nil {
			t.Fatalf("invalid role/group identity accepted: %+v", invalid)
		}
	}
}

func TestTokenRejectsExpiredAndUnsupportedAlgorithm(t *testing.T) {
	secret := strings.Repeat("s", 32)
	tokens, _ := NewTokens(secret)
	for _, tc := range []struct {
		name    string
		method  jwt.SigningMethod
		expires time.Time
	}{
		{"expired", jwt.SigningMethodHS256, time.Now().Add(-time.Minute)},
		{"algorithm", jwt.SigningMethodHS384, time.Now().Add(time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := Claims{User: User{ID: 1, Role: "teacher"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "1", Issuer: "labqueue-accounts", Audience: jwt.ClaimStrings{"labqueue"}, ExpiresAt: jwt.NewNumericDate(tc.expires)}}
			token, _ := jwt.NewWithClaims(tc.method, claims).SignedString([]byte(secret))
			if _, err := tokens.Parse(token); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}

func TestAuthMiddlewareAndStrictJSON(t *testing.T) {
	tokens, _ := NewTokens(strings.Repeat("s", 32))
	token, _ := tokens.Issue(User{ID: 1, Role: "teacher"})
	handler := tokens.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r.Context()).ID != 1 {
			t.Fatal("missing context user")
		}
		w.WriteHeader(204)
	}))
	for _, tc := range []struct {
		authorization string
		want          int
	}{{"", 401}, {"Bearer bad", 401}, {"Bearer " + token, 204}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", tc.authorization)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("status %d, want %d", w.Code, tc.want)
		}
	}
	for _, body := range []string{`{"name":"a","role":"teacher"}`, `{"name":"a"} {"name":"b"}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		var dst struct {
			Name string `json:"name"`
		}
		if DecodeJSON(w, r, &dst) {
			t.Errorf("invalid body accepted: %s", body)
		}
	}
}
