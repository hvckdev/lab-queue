package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"labqueue/pkg/platform"
)

func TestFirstRunAdministratorIsAtomicAndPasswordsStayHashed(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	tokens, err := platform.NewTokens(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	if got := request(handler, "GET", "/api/setup", "", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"required":true`) {
		t.Fatalf("initial setup state: %d %s", got.Code, got.Body.String())
	}
	// A collision must neither replace a seeded account nor close setup.
	var originalHash string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE email='teacher@example.com'").Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	conflict := request(handler, "POST", "/api/setup", `{"name":"Admin","email":"teacher@example.com","password":"Password123!"}`, "")
	if conflict.Code != 409 {
		t.Fatalf("existing account collision: %d %s", conflict.Code, conflict.Body.String())
	}
	var unchanged string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE email='teacher@example.com'").Scan(&unchanged); err != nil || unchanged != originalHash {
		t.Fatal("setup changed an existing password")
	}
	if got := request(handler, "GET", "/api/setup", "", ""); !strings.Contains(got.Body.String(), `"required":true`) {
		t.Fatal("failed setup closed initialization")
	}

	const attempts = 5
	type response struct {
		status int
		body   string
	}
	results := make(chan response, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"name":"Администратор","email":"admin%d@example.com","password":"FirstPassword123!"}`, index)
			got := request(handler, "POST", "/api/setup", body, "")
			results <- response{got.Code, got.Body.String()}
		}(i)
	}
	wg.Wait()
	close(results)
	winners := 0
	var auth struct {
		Token string        `json:"token"`
		User  platform.User `json:"user"`
	}
	for result := range results {
		if result.status == 201 {
			winners++
			if err := json.Unmarshal([]byte(result.body), &auth); err != nil {
				t.Fatal(err)
			}
		} else if result.status != 409 || !strings.Contains(result.body, "setup_completed") {
			t.Fatalf("concurrent setup returned %d %s", result.status, result.body)
		}
		if strings.Contains(result.body, "FirstPassword123!") || strings.Contains(result.body, "password_hash") || strings.Contains(result.body, "$2") {
			t.Fatal("password material was returned by setup")
		}
	}
	if winners != 1 || auth.User.Role != "admin" || auth.User.GroupID != nil {
		t.Fatalf("setup winners=%d user=%+v", winners, auth.User)
	}
	var hash string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", auth.User.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "FirstPassword123!" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("FirstPassword123!")) != nil {
		t.Fatal("administrator password is not bcrypt hashed")
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	restarted := New(db, tokens)
	if got := request(restarted, "GET", "/api/setup", "", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"required":false`) {
		t.Fatalf("setup reopened on restart: %d %s", got.Code, got.Body.String())
	}
	if got := request(restarted, "POST", "/api/setup", `{"name":"Other","email":"other@example.com","password":"ChangedPassword!"}`, ""); got.Code != 409 {
		t.Fatalf("repeated setup returned %d %s", got.Code, got.Body.String())
	}
	if got := request(restarted, "GET", "/api/admin/users", "", auth.Token); got.Code != 200 {
		t.Fatalf("initial administrator cannot use admin API: %d %s", got.Code, got.Body.String())
	}
}

func TestAdministratorCreatesAccountsAndOtherRolesAreDenied(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	tokens, err := platform.NewTokens(strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	setup := request(handler, "POST", "/api/setup", `{"name":"Администратор","email":"admin@example.com","password":"AdminPassword123!"}`, "")
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"student", "teacher", "admin"} {
		group := "null"
		if role == "student" {
			group = "1"
		}
		body := fmt.Sprintf(`{"name":" Новый пользователь ","email":" NEW-%s@Example.com ","password":"SamePassword123!","role":%q,"group_id":%s}`, role, role, group)
		created := request(handler, "POST", "/api/admin/users", body, auth.Token)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", role, created.Code, created.Body.String())
		}
		for _, forbidden := range []string{"token", "password", "$2"} {
			if strings.Contains(created.Body.String(), forbidden) {
				t.Fatalf("create returned forbidden material %q", forbidden)
			}
		}
		var hash string
		if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE email=$1", "new-"+role+"@example.com").Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte("SamePassword123!")) != nil {
			t.Fatal("new account password is not bcrypt hashed")
		}
		login := request(handler, "POST", "/api/auth/login", fmt.Sprintf(`{"email":"new-%s@example.com","password":"SamePassword123!"}`, role), "")
		if login.Code != 200 {
			t.Fatalf("new account cannot log in: %d %s", login.Code, login.Body.String())
		}
		var account struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(login.Body.Bytes(), &account); err != nil {
			t.Fatal(err)
		}
		if role != "admin" {
			if got := request(handler, "GET", "/api/admin/users", "", account.Token); got.Code != 403 {
				t.Fatalf("%s listed users: %d %s", role, got.Code, got.Body.String())
			}
			if got := request(handler, "POST", "/api/admin/users", body, account.Token); got.Code != 403 {
				t.Fatalf("%s created a user: %d %s", role, got.Code, got.Body.String())
			}
		}
		if got := request(handler, "POST", "/api/admin/users", body, auth.Token); got.Code != 409 {
			t.Fatalf("duplicate account: %d %s", got.Code, got.Body.String())
		}
	}
	var distinctHashes int
	if err := db.QueryRow(ctx, "SELECT count(DISTINCT password_hash) FROM users WHERE email IN ('new-student@example.com','new-teacher@example.com','new-admin@example.com')").Scan(&distinctHashes); err != nil || distinctHashes != 3 {
		t.Fatalf("equal passwords did not receive independent bcrypt salts: count=%d err=%v", distinctHashes, err)
	}
	listed := request(handler, "GET", "/api/admin/users?limit=2&offset=1", "", auth.Token)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), "password") || strings.Contains(listed.Body.String(), "$2") {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	var list struct {
		Users []platform.User `json:"users"`
		Total int64           `json:"total"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil || len(list.Users) != 2 || list.Total != 9 || list.Users[0].ID >= list.Users[1].ID {
		t.Fatalf("paginated list: %+v %v", list, err)
	}
	for _, query := range []string{"limit=0", "limit=101", "offset=-1", "offset=1000001", "limit=x"} {
		if got := request(handler, "GET", "/api/admin/users?"+query, "", auth.Token); got.Code != 400 {
			t.Fatalf("invalid page %s: %d", query, got.Code)
		}
	}
	if got := request(handler, "GET", "/api/admin/users", "", ""); got.Code != 401 {
		t.Fatalf("anonymous listed users: %d", got.Code)
	}
	for _, body := range []string{
		`{"name":"N","email":"unknown-role@example.com","password":"Password123!","role":"owner"}`,
		`{"name":"N","email":"bad-group@example.com","password":"Password123!","role":"student","group_id":999}`,
		`{"name":"N","email":"bad-teacher@example.com","password":"Password123!","role":"teacher","group_id":1}`,
	} {
		if got := request(handler, "POST", "/api/admin/users", body, auth.Token); got.Code != 400 {
			t.Fatalf("invalid account: %d %s", got.Code, got.Body.String())
		}
	}
}
