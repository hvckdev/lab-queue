package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/pkg/platform"
	"labqueue/projects/queue/internal/usecase"
)

type fixture struct {
	t       *testing.T
	db      *pgxpool.Pool
	tokens  *platform.Tokens
	handler http.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("TEST_QUEUE_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_QUEUE_DATABASE_URL to a PostgreSQL database to run integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("queue_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 40
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
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	tokens, err := platform.NewTokens("queue-integration-tests-secret-at-least-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/groups/1" {
			platform.Error(w, 404, "not_found", "Группа не найдена")
			return
		}
		platform.JSON(w, 200, map[string]any{"group": map[string]any{"id": 1, "name": "ИВТ-101"}})
	}))
	t.Cleanup(accounts.Close)
	return &fixture{t: t, db: db, tokens: tokens, handler: New(db, tokens, accounts.URL)}
}

func teacher(id int64) platform.User {
	return platform.User{ID: id, Name: "Преподаватель", Role: "teacher"}
}
func student(id, group int64) platform.User {
	return platform.User{ID: id, Name: fmt.Sprintf("Студент %d", id), Role: "student", GroupID: &group}
}

func (f *fixture) request(user platform.User, method, path, key string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			f.t.Error(err)
			return httptest.NewRecorder()
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	token, err := f.tokens.Issue(user)
	if err != nil {
		f.t.Error(err)
		return httptest.NewRecorder()
	}
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
}

func (f *fixture) session(starts, ends time.Time) int64 {
	f.t.Helper()
	var id int64
	err := f.db.QueryRow(context.Background(), `INSERT INTO sessions(teacher_id,subject_id,group_id,group_name,lab_title,starts_at,ends_at) VALUES(1,1,1,'ИВТ-101','ЛР №1',$1,$2) RETURNING id`, starts, ends).Scan(&id)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) activeSession() int64 {
	return f.session(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
}
func route(id int64, suffix string) string { return fmt.Sprintf("/api/sessions/%d/%s", id, suffix) }
func entryRoute(id, entryID int64, action string) string {
	return route(id, fmt.Sprintf("entries/%d/%s", entryID, action))
}

func decodeEntry(t *testing.T, w *httptest.ResponseRecorder) Entry {
	t.Helper()
	var result struct {
		Entry Entry `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Entry
}

func (f *fixture) join(id, studentID int64, key string) Entry {
	f.t.Helper()
	w := f.request(student(studentID, 1), "POST", route(id, "entries"), key, struct{}{})
	expectStatus(f.t, w, 201)
	return decodeEntry(f.t, w)
}

func TestMutationKey(t *testing.T) {
	if err := usecase.ValidateKey(""); err == nil {
		t.Fatal("missing key accepted")
	}
	if err := usecase.ValidateKey("key-1"); err != nil {
		t.Fatal(err)
	}
	if err := usecase.ValidateKey(strings.Repeat("x", 129)); err == nil {
		t.Fatal("long key accepted")
	}
}

// These records use the pre-refactor fingerprint and JSON schema rather than
// the new persistence codec. They must remain replayable after an upgrade.
func TestPreviouslyPersistedIdempotencyResultsRemainReplayable(t *testing.T) {
	f := newFixture(t)
	starts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("+0300", 3*3600))
	ends := starts.Add(time.Hour)
	type legacyCreate struct {
		SubjectID int64     `json:"subject_id"`
		GroupID   int64     `json:"group_id"`
		LabTitle  string    `json:"lab_title"`
		StartsAt  time.Time `json:"starts_at"`
		EndsAt    time.Time `json:"ends_at"`
	}
	payload := legacyCreate{1, 1, "ЛР сохраненная", starts, ends}
	data, _ := json.Marshal(payload)
	hash := sha256.Sum256(append([]byte("POST /api/sessions\n"), data...))
	response := `{"session":{"id":987,"teacher_id":1,"subject_id":1,"subject_name":"Программирование","group_id":1,"group_name":"ИВТ-101","lab_title":"ЛР сохраненная","starts_at":"2026-01-01T10:00:00+03:00","ends_at":"2026-01-01T11:00:00+03:00","registration_closed":false,"finished":false,"registration_open":true,"active_count":0}}`
	if _, err := f.db.Exec(context.Background(), "INSERT INTO idempotency(actor_id,key,fingerprint,status_code,response) VALUES($1,$2,$3,$4,$5::jsonb)", 1, "legacy-create", hex.EncodeToString(hash[:]), 201, response); err != nil {
		t.Fatal(err)
	}
	payload.LabTitle = "  ЛР сохраненная  "
	w := f.request(teacher(1), "POST", "/api/sessions", "legacy-create", payload)
	expectStatus(t, w, 201)
	var result struct {
		Session Session `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Session.ID != 987 || !result.Session.RegistrationOpen {
		t.Fatalf("legacy create changed: %s", w.Body.String())
	}
	var created int
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM sessions").Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatal("replay ran create rules instead of restoring the stored result")
	}

	id := f.activeSession()
	path := fmt.Sprintf("/api/sessions/%04d/entries", id)
	hash = sha256.Sum256([]byte("POST " + path + "\n{}"))
	entryJSON := fmt.Sprintf(`{"entry":{"id":321,"session_id":%d,"student_id":101,"student_name":"Студент 101","ticket_number":12,"status":"waiting","position":4,"created_at":"2026-01-01T07:00:00Z"}}`, id)
	if _, err := f.db.Exec(context.Background(), "INSERT INTO idempotency(actor_id,key,fingerprint,status_code,response) VALUES($1,$2,$3,$4,$5::jsonb)", 101, "legacy-join", hex.EncodeToString(hash[:]), 201, entryJSON); err != nil {
		t.Fatal(err)
	}
	w = f.request(student(101, 1), "POST", path, "legacy-join", struct{}{})
	expectStatus(t, w, 201)
	if entry := decodeEntry(t, w); entry.ID != 321 || entry.Position != 4 || entry.TicketNumber != 12 {
		t.Fatalf("legacy join changed: %+v", entry)
	}
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE session_id=$1", id).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatal("legacy join replay enqueued another student")
	}

	path = route(id, "call-next")
	hash = sha256.Sum256([]byte("POST " + path + "\n{}"))
	if _, err := f.db.Exec(context.Background(), "INSERT INTO idempotency(actor_id,key,fingerprint,status_code,response) VALUES($1,$2,$3,$4,$5::jsonb)", 1, "legacy-next", hex.EncodeToString(hash[:]), 200, `{"entry":null}`); err != nil {
		t.Fatal(err)
	}
	w = f.request(teacher(1), "POST", path, "legacy-next", struct{}{})
	expectStatus(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"entry":null}` {
		t.Fatalf("legacy empty call changed: %s", w.Body.String())
	}
}

func TestConcurrentFIFOAndDuplicateStudent(t *testing.T) {
	f := newFixture(t)
	id := f.activeSession()
	const n = 24
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- f.request(student(int64(100+i), 1), "POST", route(id, "entries"), "join", struct{}{})
		}(i)
	}
	wg.Wait()
	close(results)
	tickets := map[int64]bool{}
	for response := range results {
		expectStatus(t, response, 201)
		entry := decodeEntry(t, response)
		if tickets[entry.TicketNumber] {
			t.Fatal("duplicate ticket", entry.TicketNumber)
		}
		tickets[entry.TicketNumber] = true
	}
	for ticket := int64(1); ticket <= n; ticket++ {
		if !tickets[ticket] {
			t.Fatal("missing ticket", ticket)
		}
	}
	const duplicateRequests = 8
	duplicates := make(chan *httptest.ResponseRecorder, duplicateRequests)
	for i := 0; i < duplicateRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			duplicates <- f.request(student(200, 1), "POST", route(id, "entries"), fmt.Sprintf("duplicate-%d", i), struct{}{})
		}(i)
	}
	wg.Wait()
	close(duplicates)
	var entryID int64
	for response := range duplicates {
		expectStatus(t, response, 201)
		entry := decodeEntry(t, response)
		if entryID == 0 {
			entryID = entry.ID
		}
		if entry.ID != entryID || entry.TicketNumber != n+1 {
			t.Fatalf("duplicate active registration: %+v", entry)
		}
	}
	var count int
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE session_id=$1", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != n+1 {
		t.Fatal("unexpected count", count)
	}
}

func TestConcurrentSameKeyAndStaleCommands(t *testing.T) {
	f := newFixture(t)
	id := f.activeSession()
	first := f.join(id, 101, "join")
	second := f.join(id, 102, "join")
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.request(teacher(1), "POST", route(id, "call-next"), "call-one", struct{}{})
		}()
	}
	wg.Wait()
	close(results)
	for response := range results {
		expectStatus(t, response, 200)
		entry := decodeEntry(t, response)
		if entry.ID != first.ID {
			t.Fatal("FIFO violated")
		}
	}
	w := f.request(teacher(1), "POST", entryRoute(id, first.ID, "complete"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(teacher(1), "POST", route(id, "call-next"), "call-two", struct{}{})
	expectStatus(t, w, 200)
	if decodeEntry(t, w).ID != second.ID {
		t.Fatal("wrong next student")
	}
	w = f.request(teacher(1), "POST", route(id, "call-next"), "call-one", struct{}{})
	expectStatus(t, w, 200)
	if decodeEntry(t, w).ID != first.ID {
		t.Fatal("stored call result was not replayed")
	}
	w = f.request(teacher(1), "POST", entryRoute(id, first.ID, "complete"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(student(101, 1), "POST", route(id, "entries"), "join", struct{}{})
	expectStatus(t, w, 201)
	if decodeEntry(t, w).ID != first.ID {
		t.Fatal("join replay created a new entry")
	}
	w = f.request(student(101, 1), "POST", route(id, "entries"), "join-again", struct{}{})
	expectStatus(t, w, 409)
	w = f.request(teacher(1), "POST", entryRoute(id, first.ID, "skip"), "", struct{}{})
	expectStatus(t, w, 409)
	var current int64
	if err := f.db.QueryRow(context.Background(), "SELECT id FROM entries WHERE session_id=$1 AND status='called'", id).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != second.ID {
		t.Fatal("stale completion changed the next student")
	}
	// A key is scoped by actor, but cannot be reused for a different operation.
	w = f.request(teacher(1), "POST", route(id+1, "call-next"), "call-one", struct{}{})
	expectStatus(t, w, 409)
}

func TestCancellationSkippingFinishingAndAccess(t *testing.T) {
	f := newFixture(t)
	id := f.activeSession()
	first := f.join(id, 101, "join")
	second := f.join(id, 102, "join")
	for _, test := range []struct {
		user              platform.User
		method, path, key string
		status            int
	}{
		{student(201, 2), "GET", route(id, "queue"), "", 403},
		{student(201, 2), "POST", route(id, "entries"), "join", 403},
		{teacher(2), "POST", route(id, "call-next"), "next", 403},
		{student(102, 1), "POST", entryRoute(id, first.ID, "cancel"), "", 403},
		{student(101, 1), "POST", route(id, "finish"), "", 403},
		{teacher(1), "POST", route(id, "entries"), "join", 403},
	} {
		t.Run(test.method+test.path+strconv.Itoa(test.status), func(t *testing.T) {
			expectStatus(t, f.request(test.user, test.method, test.path, test.key, struct{}{}), test.status)
		})
	}
	w := f.request(student(101, 1), "POST", entryRoute(id, first.ID, "cancel"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(student(101, 1), "POST", entryRoute(id, first.ID, "cancel"), "", struct{}{})
	expectStatus(t, w, 200)
	rejoined := f.join(id, 101, "rejoin")
	if rejoined.TicketNumber != 3 || rejoined.Position != 2 {
		t.Fatalf("bad rejoin: %+v", rejoined)
	}
	w = f.request(teacher(1), "POST", route(id, "call-next"), "next", struct{}{})
	expectStatus(t, w, 200)
	if decodeEntry(t, w).ID != second.ID {
		t.Fatal("cancelled ticket was called")
	}
	w = f.request(teacher(1), "POST", route(id, "finish"), "", struct{}{})
	expectStatus(t, w, 409)
	w = f.request(student(102, 1), "POST", entryRoute(id, second.ID, "cancel"), "", struct{}{})
	expectStatus(t, w, 409)
	w = f.request(teacher(1), "POST", entryRoute(id, second.ID, "skip"), "", struct{}{})
	expectStatus(t, w, 200)
	afterSkip := f.join(id, 102, "after-skip")
	if afterSkip.TicketNumber != 4 {
		t.Fatal("skip did not send student to end")
	}
	w = f.request(teacher(1), "POST", route(id, "finish"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(teacher(1), "POST", route(id, "finish"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(student(103, 1), "POST", route(id, "entries"), "join", struct{}{})
	expectStatus(t, w, 409)
	var active int
	f.db.QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE session_id=$1 AND status IN ('waiting','called')", id).Scan(&active)
	if active != 0 {
		t.Fatal("finish did not cancel waiting entries")
	}
}

func TestTimeBoundariesAndDynamicPosition(t *testing.T) {
	f := newFixture(t)
	id := f.session(time.Now().Add(time.Hour), time.Now().Add(2*time.Hour))
	first := f.join(id, 101, "join")
	second := f.join(id, 102, "join")
	w := f.request(teacher(1), "POST", route(id, "call-next"), "early", struct{}{})
	expectStatus(t, w, 409)
	if _, err := f.db.Exec(context.Background(), "UPDATE sessions SET starts_at=clock_timestamp()-interval '1 hour',ends_at=clock_timestamp()+interval '1 hour' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	w = f.request(teacher(1), "POST", route(id, "call-next"), "next", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(student(102, 1), "GET", route(id, "queue"), "", nil)
	expectStatus(t, w, 200)
	var snapshot struct {
		Entries []Entry `json:"entries"`
		MyEntry Entry   `json:"my_entry"`
		Next    int     `json:"next_position"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.MyEntry.Position != 2 || snapshot.Next != 3 {
		t.Fatalf("called student must count ahead: %s", w.Body.String())
	}
	w = f.request(teacher(1), "POST", entryRoute(id, first.ID, "complete"), "", struct{}{})
	expectStatus(t, w, 200)
	w = f.request(student(102, 1), "GET", route(id, "queue"), "", nil)
	expectStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &snapshot)
	if snapshot.MyEntry.Position != 1 || snapshot.MyEntry.TicketNumber != second.TicketNumber {
		t.Fatal("position and ticket were conflated")
	}
	if _, err := f.db.Exec(context.Background(), "UPDATE sessions SET ends_at=clock_timestamp()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	w = f.request(student(103, 1), "POST", route(id, "entries"), "late", struct{}{})
	expectStatus(t, w, 409)
	w = f.request(teacher(1), "POST", route(id, "call-next"), "after-end", struct{}{})
	expectStatus(t, w, 200)
	if decodeEntry(t, w).ID != second.ID {
		t.Fatal("end time prevented serving waiting students")
	}
}

func TestWaitingForLockPastDeadlineRejectsJoin(t *testing.T) {
	f := newFixture(t)
	ends := time.Now().Add(900 * time.Millisecond)
	id := f.session(time.Now().Add(-time.Hour), ends)
	ctx := context.Background()
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT id FROM sessions WHERE id=$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	blockerPID := int32(tx.Conn().PgConn().PID())
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- f.request(student(101, 1), "POST", route(id, "entries"), "waiting", struct{}{}) }()
	lockObserved := false
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		var n int
		err = f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND $1::integer = ANY(pg_blocking_pids(pid))`, blockerPID).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			lockObserved = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !lockObserved {
		t.Fatal("join did not wait for the session row lock")
	}
	if remaining := time.Until(ends) + 30*time.Millisecond; remaining > 0 {
		time.Sleep(remaining)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, <-result, 409)
	var n int
	if err = f.db.QueryRow(ctx, "SELECT count(*) FROM entries WHERE session_id=$1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expired join created an entry")
	}
}

func TestCloseVersusJoinIsSerialized(t *testing.T) {
	f := newFixture(t)
	id := f.activeSession()
	start := make(chan struct{})
	joined := make(chan *httptest.ResponseRecorder, 1)
	closed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		<-start
		joined <- f.request(student(101, 1), "POST", route(id, "entries"), "join", struct{}{})
	}()
	go func() {
		<-start
		closed <- f.request(teacher(1), "POST", route(id, "close-registration"), "", struct{}{})
	}()
	close(start)
	expectStatus(t, <-closed, 200)
	join := <-joined
	if join.Code != 201 && join.Code != 409 {
		t.Fatalf("unexpected join result %d: %s", join.Code, join.Body.String())
	}
	w := f.request(student(102, 1), "POST", route(id, "entries"), "later", struct{}{})
	expectStatus(t, w, 409)
	var n int
	f.db.QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE session_id=$1", id).Scan(&n)
	if join.Code == 201 && n != 1 || join.Code == 409 && n != 0 {
		t.Fatal("close/join outcome was inconsistent")
	}
}

func TestCreateSessionIdempotencyAndValidation(t *testing.T) {
	f := newFixture(t)
	body := map[string]any{"subject_id": 1, "group_id": 1, "lab_title": "ЛР №3", "starts_at": time.Now().Add(-time.Minute).UTC(), "ends_at": time.Now().Add(time.Hour).UTC()}
	w := f.request(teacher(1), "POST", "/api/sessions", "create", body)
	expectStatus(t, w, 201)
	var first struct {
		Session Session `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	w = f.request(teacher(1), "POST", "/api/sessions", "create", body)
	expectStatus(t, w, 201)
	var replayed struct {
		Session Session `json:"session"`
	}
	json.Unmarshal(w.Body.Bytes(), &replayed)
	if first.Session.ID != replayed.Session.ID {
		t.Fatal("session duplicated on retry")
	}
	body["lab_title"] = "Другая работа"
	w = f.request(teacher(1), "POST", "/api/sessions", "create", body)
	expectStatus(t, w, 409)
	body["group_id"] = 999
	w = f.request(teacher(1), "POST", "/api/sessions", "new", body)
	expectStatus(t, w, 400)
	w = f.request(student(101, 1), "POST", "/api/sessions", "student-create", body)
	expectStatus(t, w, 403)
	w = f.request(teacher(1), "POST", "/api/sessions", "", body)
	expectStatus(t, w, 400)
}
