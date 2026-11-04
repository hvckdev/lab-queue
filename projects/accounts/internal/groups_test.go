package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"labqueue/pkg/platform"
)

func groupNameBody(t *testing.T, name string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestAdministratorCreatesGroupsAndTheyCanBeAssignedToStudents(t *testing.T) {
	db := integrationDB(t)
	tokens, err := platform.NewTokens(strings.Repeat("g", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	setup := request(handler, "POST", "/api/setup", `{"name":"Администратор","email":"group-admin@example.com","password":"AdminPassword123!"}`, "")
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	var administrator struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &administrator); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"student", "teacher"} {
		login := request(handler, "POST", "/api/auth/login", fmt.Sprintf(`{"email":"%s@example.com","password":"%s123!"}`, role, strings.ToUpper(role[:1])+role[1:]), "")
		if login.Code != 200 {
			t.Fatalf("login %s: %d %s", role, login.Code, login.Body.String())
		}
		var user struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(login.Body.Bytes(), &user); err != nil {
			t.Fatal(err)
		}
		if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, "Запрещённая группа"), user.Token); got.Code != 403 {
			t.Fatalf("%s created group: %d %s", role, got.Code, got.Body.String())
		}
	}
	if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, "Запрещённая группа"), ""); got.Code != 401 {
		t.Fatalf("anonymous created group: %d", got.Code)
	}
	staleAdministrator, err := tokens.Issue(platform.User{ID: 1, Name: "Teacher", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, "Запрещённая группа"), staleAdministrator); got.Code != 403 {
		t.Fatalf("current DB role did not override stale token: %d", got.Code)
	}
	created := request(handler, "POST", "/api/admin/groups", groupNameBody(t, "  ИВТ-201  "), administrator.Token)
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	var result struct {
		Group struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"group"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.Group.ID < 3 || result.Group.Name != "ИВТ-201" {
		t.Fatalf("created group: %+v %v", result, err)
	}
	for _, duplicate := range []string{"ИВТ-201", " ивт-201 ", " ивт-101 "} {
		got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, duplicate), administrator.Token)
		if got.Code != 409 || !strings.Contains(got.Body.String(), "group_exists") {
			t.Fatalf("duplicate %q: %d %s", duplicate, got.Code, got.Body.String())
		}
	}
	for _, invalid := range []string{"", "   ", "Группа\nСкрытая", strings.Repeat("Г", 101)} {
		if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, invalid), administrator.Token); got.Code != 400 {
			t.Fatalf("invalid name %q: %d %s", invalid, got.Code, got.Body.String())
		}
	}
	if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, strings.Repeat("Г", 100)), administrator.Token); got.Code != 201 {
		t.Fatalf("100-rune name rejected: %d %s", got.Code, got.Body.String())
	}
	for _, malformed := range []string{`null`, `{"name":"Группа","role":"admin"}`, `{"name":22}`} {
		if got := request(handler, "POST", "/api/admin/groups", malformed, administrator.Token); got.Code != 400 {
			t.Fatalf("malformed request accepted: %d %s", got.Code, got.Body.String())
		}
	}
	if got := request(handler, "GET", "/api/groups", "", ""); got.Code != 200 || !strings.Contains(got.Body.String(), "ИВТ-201") {
		t.Fatalf("new group missing from public catalog: %d %s", got.Code, got.Body.String())
	}
	registration := fmt.Sprintf(`{"name":"Самостоятельный студент","email":"new-group-student@example.com","password":"StudentPassword123!","group_id":%d}`, result.Group.ID)
	if got := request(handler, "POST", "/api/auth/register", registration, ""); got.Code != 201 {
		t.Fatalf("registration in new group failed: %d %s", got.Code, got.Body.String())
	}
	adminStudent := fmt.Sprintf(`{"name":"Студент администратора","email":"admin-new-group-student@example.com","password":"StudentPassword123!","role":"student","group_id":%d}`, result.Group.ID)
	if got := request(handler, "POST", "/api/admin/users", adminStudent, administrator.Token); got.Code != 201 {
		t.Fatalf("admin assignment to new group failed: %d %s", got.Code, got.Body.String())
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(context.Background(), "SELECT name FROM student_groups WHERE id=$1", result.Group.ID).Scan(&stored); err != nil || stored != "ИВТ-201" {
		t.Fatalf("restart changed new group: %q %v", stored, err)
	}
}

func TestConcurrentGroupsAndSeedsKeepUniqueIDs(t *testing.T) {
	db := integrationDB(t)
	tokens, err := platform.NewTokens(strings.Repeat("c", 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, tokens)
	setup := request(handler, "POST", "/api/setup", `{"name":"Администратор","email":"group-admin@example.com","password":"AdminPassword123!"}`, "")
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	var administrator struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &administrator); err != nil {
		t.Fatal(err)
	}
	const creates, seeds = 12, 4
	start := make(chan struct{})
	results := make(chan error, creates+seeds)
	var wg sync.WaitGroup
	for i := 0; i < creates; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			got := request(handler, "POST", "/api/admin/groups", fmt.Sprintf(`{"name":"Группа-%02d"}`, index), administrator.Token)
			if got.Code != http.StatusCreated {
				results <- fmt.Errorf("create %d: %d %s", index, got.Code, got.Body.String())
				return
			}
			results <- nil
		}(i)
	}
	for i := 0; i < seeds; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- Seed(context.Background(), db)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count, distinct int
	if err := db.QueryRow(context.Background(), "SELECT count(*),count(DISTINCT id) FROM student_groups").Scan(&count, &distinct); err != nil || count != creates+2 || distinct != count {
		t.Fatalf("groups after concurrent writes: count=%d distinct=%d error=%v", count, distinct, err)
	}
	if got := request(handler, "POST", "/api/admin/groups", groupNameBody(t, "Группа после повторного запуска"), administrator.Token); got.Code != 201 {
		t.Fatalf("sequence not advanced after seed: %d %s", got.Code, got.Body.String())
	}
}
