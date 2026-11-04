package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"labqueue/pkg/platform"
)

type subjectResponse struct {
	Subject struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"subject"`
}

func TestAdminSubjectPermissionsValidationAndSequence(t *testing.T) {
	f := newFixture(t)
	admin := platform.User{ID: 99, Name: "Администратор", Role: "admin"}
	body := map[string]string{"name": " Distributed systems "}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/admin/subjects", strings.NewReader(`{"name":"Subject"}`)))
	expectStatus(t, w, 401)
	for _, user := range []platform.User{teacher(1), student(10, 1)} {
		expectStatus(t, f.request(user, "POST", "/api/admin/subjects", "", body), 403)
	}
	created := f.request(admin, "POST", "/api/admin/subjects", "", body)
	expectStatus(t, created, 201)
	var result subjectResponse
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Subject.ID < 4 || result.Subject.Name != "Distributed systems" {
		t.Fatalf("unexpected subject after explicit seed IDs: %+v", result.Subject)
	}
	for _, name := range []string{"Distributed systems", "distributed SYSTEMS", "  DISTRIBUTED SYSTEMS  "} {
		expectStatus(t, f.request(admin, "POST", "/api/admin/subjects", "", map[string]string{"name": name}), 409)
	}
	for _, name := range []string{"", "  ", "Bad\nname", strings.Repeat("я", 201)} {
		expectStatus(t, f.request(admin, "POST", "/api/admin/subjects", "", map[string]string{"name": name}), 400)
	}
	expectStatus(t, f.request(admin, "GET", "/api/subjects", "", nil), 200)
	expectStatus(t, f.request(admin, "GET", "/api/sessions", "", nil), 403)
	expectStatus(t, f.request(admin, "POST", "/api/sessions", "key", map[string]any{}), 403)
	expectStatus(t, f.request(admin, "POST", "/api/sessions/1/entries", "key", map[string]any{}), 403)
	expectStatus(t, f.request(admin, "POST", "/api/sessions/1/call-next", "key", map[string]any{}), 403)
	if err := Seed(context.Background(), f.db); err != nil {
		t.Fatal(err)
	}
	second := f.request(admin, "POST", "/api/admin/subjects", "", map[string]string{"name": "Computer graphics"})
	expectStatus(t, second, 201)
	var later subjectResponse
	if err := json.Unmarshal(second.Body.Bytes(), &later); err != nil {
		t.Fatal(err)
	}
	if later.Subject.ID <= result.Subject.ID {
		t.Fatalf("repeated Seed rewound identity: previous=%d later=%d", result.Subject.ID, later.Subject.ID)
	}
	var preserved int
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM subjects WHERE id=$1 AND name=$2", result.Subject.ID, result.Subject.Name).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("Seed lost created subject: count=%d err=%v", preserved, err)
	}
}

func TestConcurrentSubjectCreationAndSeedKeepUniqueIncreasingIDs(t *testing.T) {
	f := newFixture(t)
	admin := platform.User{ID: 99, Role: "admin"}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if err := Seed(context.Background(), f.db); err != nil {
					t.Error(err)
				}
				return
			}
			response := f.request(admin, "POST", "/api/admin/subjects", "", map[string]string{"name": fmt.Sprintf("Concurrent subject %d", i)})
			if response.Code != 201 {
				t.Errorf("concurrent create: status=%d body=%s", response.Code, response.Body.String())
			}
		}(i)
	}
	wg.Wait()
	var count int
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM subjects").Scan(&count); err != nil || count != 6 {
		t.Fatalf("concurrent seed/create lost a subject: count=%d err=%v", count, err)
	}
}
