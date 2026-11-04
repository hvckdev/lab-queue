package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"labqueue/pkg/platform"
)

func TestRegistrationRejectsRoleAndMalformedInput(t *testing.T) {
	tokens, err := platform.NewTokens(strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(nil, tokens)
	for _, body := range []string{
		`{"name":"Student","email":"s@example.com","password":"password123","group_id":1,"role":"teacher"}`,
		`null`,
		`{"name":"Student"} {"name":"Other"}`,
	} {
		response := request(handler, "POST", "/api/auth/register", body, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status=%d, response=%s", body, response.Code, response.Body.String())
		}
	}
}

func integrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_ACCOUNTS_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_ACCOUNTS_DATABASE_URL to a disposable PostgreSQL database to run accounts integration tests")
	}
	ctx := context.Background()
	admin, err := platform.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("accounts_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func request(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestAccountsRegistrationLoginAndPermissions(t *testing.T) {
	db := integrationDB(t)
	tokens, err := platform.NewTokens(strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	groups := request(handler, "GET", "/api/groups", "", "")
	if groups.Code != 200 || !strings.Contains(groups.Body.String(), "ИВТ-101") {
		t.Fatalf("groups: %d %s", groups.Code, groups.Body.String())
	}
	group := request(handler, "GET", "/api/groups/1", "", "")
	if group.Code != 200 || !strings.Contains(group.Body.String(), `"group"`) {
		t.Fatalf("group: %d %s", group.Code, group.Body.String())
	}
	for path, expected := range map[string]int{"/api/groups/9999": 404, "/api/groups/invalid": 400} {
		if got := request(handler, "GET", path, "", ""); got.Code != expected {
			t.Errorf("%s: got %d, want %d", path, got.Code, expected)
		}
	}
	body := `{"name":"  Новый Студент ","email":" NEW@Example.COM ","password":"Password123!","group_id":1}`
	created := request(handler, "POST", "/api/auth/register", body, "")
	if created.Code != 201 {
		t.Fatalf("register: %d %s", created.Code, created.Body.String())
	}
	var result struct {
		Token string        `json:"token"`
		User  platform.User `json:"user"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User.Name != "Новый Студент" || result.User.Email != "new@example.com" || result.User.Role != "student" || result.User.GroupID == nil || *result.User.GroupID != 1 {
		t.Fatalf("unexpected registered user: %+v", result.User)
	}
	identity, err := tokens.Parse(result.Token)
	if err != nil || identity.ID != result.User.ID || identity.Role != "student" {
		t.Fatalf("invalid token identity: %+v %v", identity, err)
	}
	for _, protected := range []string{"password", "hash"} {
		if strings.Contains(created.Body.String(), protected) {
			t.Errorf("response leaked %s", protected)
		}
	}
	var hash string
	if err := db.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE id=$1`, result.User.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "Password123!" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("Password123!")) != nil {
		t.Fatal("password was not bcrypt hashed")
	}
	duplicate := request(handler, "POST", "/api/auth/register", body, "")
	if duplicate.Code != 409 {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}
	login := request(handler, "POST", "/api/auth/login", `{"email":"NEW@EXAMPLE.COM","password":"Password123!"}`, "")
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	me := request(handler, "GET", "/api/auth/me", "", result.Token)
	if me.Code != 200 || !strings.Contains(me.Body.String(), "new@example.com") {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}
	if got := request(handler, "GET", "/api/auth/me", "", ""); got.Code != 401 {
		t.Errorf("unauthenticated me: %d", got.Code)
	}
	for _, credentials := range []string{
		`{"email":"new@example.com","password":"wrong"}`,
		`{"email":"absent@example.com","password":"Password123!"}`,
	} {
		got := request(handler, "POST", "/api/auth/login", credentials, "")
		if got.Code != 401 || !strings.Contains(got.Body.String(), "invalid_credentials") {
			t.Errorf("bad credentials: %d %s", got.Code, got.Body.String())
		}
	}
	unknownGroup := request(handler, "POST", "/api/auth/register", `{"name":"Student","email":"missing@example.com","password":"Password123!","group_id":9999}`, "")
	if unknownGroup.Code != 400 || !strings.Contains(unknownGroup.Body.String(), "group_not_found") {
		t.Errorf("unknown group: %d %s", unknownGroup.Code, unknownGroup.Body.String())
	}
	teacherLogin := request(handler, "POST", "/api/auth/login", `{"email":"teacher@example.com","password":"Teacher123!"}`, "")
	if teacherLogin.Code != 200 {
		t.Fatalf("teacher login: %d %s", teacherLogin.Code, teacherLogin.Body.String())
	}
	if err := json.Unmarshal(teacherLogin.Body.Bytes(), &result); err != nil || result.User.Role != "teacher" || result.User.GroupID != nil {
		t.Fatalf("teacher identity: %+v %v", result.User, err)
	}
}

func TestConcurrentRegistrationKeepsOneUser(t *testing.T) {
	db := integrationDB(t)
	tokens, err := platform.NewTokens(strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	statuses := make(chan int, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got := request(handler, "POST", "/api/auth/register", `{"name":"Student","email":"SAME@example.com","password":"Password123!","group_id":1}`, "")
			statuses <- got.Code
		}()
	}
	workers.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[201] != 1 || counts[409] != 7 {
		t.Fatalf("concurrent responses: %v", counts)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email='same@example.com'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("user count=%d, error=%v", count, err)
	}
}

func TestMigrationsAndSeedAreIdempotent(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `UPDATE users SET name='Измененное имя' WHERE email='student@example.com'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	var name string
	if err := db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("count=%d error=%v", count, err)
	}
	if err := db.QueryRow(ctx, `SELECT name FROM users WHERE email='student@example.com'`).Scan(&name); err != nil || name != "Измененное имя" {
		t.Fatalf("seed replaced existing user: %q error=%v", name, err)
	}
}
