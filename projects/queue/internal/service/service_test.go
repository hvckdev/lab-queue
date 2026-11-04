package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"labqueue/projects/queue/internal/model"
)

// Methods outside the tested scenario stay unimplemented through the embedded
// port. An unexpected persistence operation therefore fails the test immediately.
type fakeRepository struct {
	Repository
	session      model.Session
	now          time.Time
	afterLockNow time.Time
	active       *model.Entry
	completed    bool
	called       *model.Entry
	first        *model.Entry
	entries      map[int64]model.Entry
	calls        []string
	inserted     []model.Entry
	advanced     []int64
	updated      []int64
	cancelled    []int64
	stateChanges int
}

func (r *fakeRepository) LockSession(_ context.Context, _ int64) (model.Session, error) {
	r.calls = append(r.calls, "lock")
	if !r.afterLockNow.IsZero() {
		r.now = r.afterLockNow
	}
	return r.session, nil
}

func (r *fakeRepository) Session(_ context.Context, _ int64) (model.Session, error) {
	return r.session, nil
}

func (r *fakeRepository) Now(context.Context) (time.Time, error) {
	r.calls = append(r.calls, "now")
	return r.now, nil
}

func (r *fakeRepository) ActiveEntry(context.Context, int64, int64) (*model.Entry, error) {
	r.calls = append(r.calls, "active")
	return r.active, nil
}

func (r *fakeRepository) HasCompleted(context.Context, int64, int64) (bool, error) {
	r.calls = append(r.calls, "completed")
	return r.completed, nil
}

func (r *fakeRepository) InsertEntry(_ context.Context, entry model.Entry) (int64, error) {
	r.calls = append(r.calls, "insert")
	entry.ID = int64(100 + len(r.inserted))
	r.inserted = append(r.inserted, entry)
	if r.entries == nil {
		r.entries = make(map[int64]model.Entry)
	}
	r.entries[entry.ID] = entry
	return entry.ID, nil
}

func (r *fakeRepository) AdvanceTicket(_ context.Context, id int64) error {
	r.calls = append(r.calls, "advance")
	r.advanced = append(r.advanced, id)
	r.session.NextTicket++
	return nil
}

func (r *fakeRepository) Entry(_ context.Context, id int64) (model.Entry, error) {
	r.calls = append(r.calls, "entry")
	entry, ok := r.entries[id]
	if !ok {
		return model.Entry{}, model.ErrNotFound
	}
	return entry, nil
}

func (r *fakeRepository) CalledEntry(context.Context, int64) (*model.Entry, error) {
	r.calls = append(r.calls, "called")
	return r.called, nil
}

func (r *fakeRepository) FirstWaiting(context.Context, int64) (*model.Entry, error) {
	r.calls = append(r.calls, "first")
	return r.first, nil
}

func (r *fakeRepository) SetEntryStatus(_ context.Context, id int64, status model.EntryStatus) error {
	r.calls = append(r.calls, "update-entry")
	r.updated = append(r.updated, id)
	entry := r.entries[id]
	entry.Status = status
	r.entries[id] = entry
	return nil
}

func (r *fakeRepository) CancelWaiting(_ context.Context, sessionID int64) error {
	r.calls = append(r.calls, "cancel-waiting")
	r.cancelled = append(r.cancelled, sessionID)
	for id, entry := range r.entries {
		if entry.SessionID == sessionID && entry.Status == model.Waiting {
			entry.Status = model.Cancelled
			r.entries[id] = entry
		}
	}
	return nil
}

func (r *fakeRepository) SetSessionState(_ context.Context, _ int64, closed, finished bool) error {
	r.calls = append(r.calls, "update-session")
	r.stateChanges++
	r.session.RegistrationClosed = closed
	r.session.Finished = finished
	return nil
}

var (
	testNow     = time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	testStudent = model.Actor{ID: 21, Name: "Анна Петрова", Role: model.Student, GroupID: 4}
	testTeacher = model.Actor{ID: 7, Name: "Преподаватель", Role: model.Teacher}
)

func baseRepository() *fakeRepository {
	return &fakeRepository{
		session: model.Session{ID: 3, TeacherID: testTeacher.ID, GroupID: testStudent.GroupID, StartsAt: testNow.Add(-time.Hour), EndsAt: testNow.Add(time.Hour), NextTicket: 42},
		now:     testNow,
		entries: make(map[int64]model.Entry),
	}
}

func requireError(t *testing.T, err error, kind model.ErrorKind, code string) {
	t.Helper()
	var problem *model.Error
	if !errors.As(err, &problem) || problem.Kind != kind || problem.Code != code {
		t.Fatalf("error=%v; want kind=%s code=%s", err, kind, code)
	}
}

func callIndex(calls []string, call string) int {
	for i, value := range calls {
		if value == call {
			return i
		}
	}
	return -1
}

func TestAccessAndListScopeCannotEscapeIdentity(t *testing.T) {
	svc := New()
	session := baseRepository().session
	for _, test := range []struct {
		name    string
		actor   model.Actor
		allowed bool
	}{
		{"own teacher", testTeacher, true},
		{"own group", testStudent, true},
		{"another teacher", model.Actor{ID: 8, Role: model.Teacher}, false},
		{"another group", model.Actor{ID: 22, Role: model.Student, GroupID: 5}, false},
		{"student without group", model.Actor{ID: 23, Role: model.Student}, false},
		{"unknown role", model.Actor{ID: testTeacher.ID, Role: "administrator", GroupID: 4}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := svc.Authorize(test.actor, session)
			if test.allowed && err != nil {
				t.Fatal(err)
			}
			if !test.allowed {
				requireError(t, err, model.Forbidden, "forbidden")
			}
		})
	}
	filter, err := svc.ScopeSessions(testStudent, model.SessionFilter{SubjectID: 9, TeacherID: 999})
	if err != nil || filter.GroupID != testStudent.GroupID || filter.TeacherID != 0 || filter.SubjectID != 9 {
		t.Fatalf("student filter=%+v err=%v", filter, err)
	}
	_, err = svc.ScopeSessions(testStudent, model.SessionFilter{GroupID: 5})
	requireError(t, err, model.Forbidden, "forbidden")
	filter, err = svc.ScopeSessions(testTeacher, model.SessionFilter{GroupID: 5, TeacherID: 999})
	if err != nil || filter.TeacherID != testTeacher.ID || filter.GroupID != 5 {
		t.Fatalf("teacher filter=%+v err=%v", filter, err)
	}
}

func TestJoinUsesFreshTimeAfterLockAndNeverInsertsIntoClosedQueue(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*fakeRepository)
	}{
		{"deadline elapsed while waiting", func(repo *fakeRepository) { repo.afterLockNow = repo.session.EndsAt }},
		{"finished", func(repo *fakeRepository) { repo.session.Finished = true }},
		{"manually closed", func(repo *fakeRepository) { repo.session.RegistrationClosed = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := baseRepository()
			test.prepare(repo)
			_, err := New().Join(context.Background(), repo, testStudent, repo.session.ID)
			requireError(t, err, model.Conflict, "registration_closed")
			if len(repo.inserted) != 0 || len(repo.advanced) != 0 {
				t.Fatal("rejected registration changed the queue")
			}
			lock, clock := callIndex(repo.calls, "lock"), callIndex(repo.calls, "now")
			if lock < 0 || clock <= lock {
				t.Fatalf("clock must be read after session lock, calls=%v", repo.calls)
			}
		})
	}
}

func TestJoinReusesActiveTicketAndCompletedStudentCannotRejoin(t *testing.T) {
	repo := baseRepository()
	repo.active = &model.Entry{ID: 81, SessionID: repo.session.ID, StudentID: testStudent.ID, TicketNumber: 12, Status: model.Called}
	repo.session.RegistrationClosed = true // Re-reading an existing registration is still safe after closure.
	result, err := New().Join(context.Background(), repo, testStudent, repo.session.ID)
	if err != nil || result.Entry == nil || result.Entry.ID != 81 || result.Entry.TicketNumber != 12 {
		t.Fatalf("active entry was not reused: result=%+v err=%v", result, err)
	}
	if len(repo.inserted) != 0 || len(repo.advanced) != 0 {
		t.Fatal("duplicate registration consumed a new ticket")
	}
	repo = baseRepository()
	repo.completed = true
	_, err = New().Join(context.Background(), repo, testStudent, repo.session.ID)
	requireError(t, err, model.Conflict, "already_completed")
	if len(repo.inserted) != 0 || len(repo.advanced) != 0 {
		t.Fatal("completed student was registered again")
	}
}

func TestJoinAssignsNextPermanentTicketFromLockedSession(t *testing.T) {
	repo := baseRepository()
	result, err := New().Join(context.Background(), repo, testStudent, repo.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry == nil || result.Entry.TicketNumber != 42 || result.Entry.StudentID != testStudent.ID || result.Entry.StudentName != testStudent.Name || result.Entry.Status != model.Waiting {
		t.Fatalf("incorrect new registration: %+v", result.Entry)
	}
	if len(repo.inserted) != 1 || !reflect.DeepEqual(repo.advanced, []int64{repo.session.ID}) || repo.session.NextTicket != 43 {
		t.Fatalf("ticket allocation was not advanced exactly once: inserts=%v advanced=%v", repo.inserted, repo.advanced)
	}
	if callIndex(repo.calls, "lock") > callIndex(repo.calls, "insert") {
		t.Fatal("registration was inserted before locking the session")
	}
	_, err = New().Join(context.Background(), baseRepository(), testTeacher, 3)
	requireError(t, err, model.Forbidden, "forbidden")
}

func TestCallNextPreservesCurrentStudentAndWaitsUntilStart(t *testing.T) {
	t.Run("already called", func(t *testing.T) {
		repo := baseRepository()
		repo.called = &model.Entry{ID: 81, SessionID: 3, Status: model.Called, TicketNumber: 3}
		result, err := New().CallNext(context.Background(), repo, testTeacher, 3)
		if err != nil || result.Entry == nil || result.Entry.ID != repo.called.ID {
			t.Fatalf("current called entry lost: %+v %v", result, err)
		}
		if len(repo.updated) != 0 || callIndex(repo.calls, "first") >= 0 {
			t.Fatal("calling again advanced past the current student")
		}
	})
	t.Run("not started", func(t *testing.T) {
		repo := baseRepository()
		repo.session.StartsAt = testNow.Add(time.Minute)
		_, err := New().CallNext(context.Background(), repo, testTeacher, 3)
		requireError(t, err, model.Conflict, "not_started")
		if len(repo.updated) != 0 || callIndex(repo.calls, "first") >= 0 {
			t.Fatal("student called before session start")
		}
	})
	t.Run("serve remaining after registration deadline", func(t *testing.T) {
		repo := baseRepository()
		repo.session.EndsAt = testNow.Add(-time.Second)
		repo.session.RegistrationClosed = true
		repo.first = &model.Entry{ID: 91, SessionID: 3, Status: model.Waiting, TicketNumber: 7}
		repo.entries[91] = *repo.first
		result, err := New().CallNext(context.Background(), repo, testTeacher, 3)
		if err != nil || result.Entry == nil || result.Entry.ID != 91 || result.Entry.Status != model.Called || !reflect.DeepEqual(repo.updated, []int64{91}) {
			t.Fatalf("remaining waiting student was not called: %+v updates=%v err=%v", result, repo.updated, err)
		}
	})
}

func TestEntryTransitionTargetsExactEntryAndPreservesFollowingStudent(t *testing.T) {
	repo := baseRepository()
	repo.entries[81] = model.Entry{ID: 81, SessionID: 3, StudentID: 21, Status: model.Completed}
	repo.entries[82] = model.Entry{ID: 82, SessionID: 3, StudentID: 22, Status: model.Called}
	result, err := New().ChangeEntry(context.Background(), repo, testTeacher, 3, 81, model.Completed)
	if err != nil || result.Entry == nil || result.Entry.ID != 81 || result.Entry.Status != model.Completed {
		t.Fatalf("terminal repeat did not return original entry: %+v %v", result, err)
	}
	if len(repo.updated) != 0 || repo.entries[82].Status != model.Called {
		t.Fatal("repeating completion affected the next student")
	}
	_, err = New().ChangeEntry(context.Background(), repo, testTeacher, 3, 81, model.Skipped)
	requireError(t, err, model.Conflict, "invalid_entry_state")
	if len(repo.updated) != 0 {
		t.Fatal("conflicting terminal command changed an entry")
	}
	repo.entries[83] = model.Entry{ID: 83, SessionID: 3, StudentID: 22, Status: model.Waiting}
	_, err = New().ChangeEntry(context.Background(), repo, testStudent, 3, 83, model.Cancelled)
	requireError(t, err, model.Forbidden, "forbidden")
	repo.entries[84] = model.Entry{ID: 84, SessionID: 4, StudentID: 21, Status: model.Waiting}
	_, err = New().ChangeEntry(context.Background(), repo, testStudent, 3, 84, model.Cancelled)
	requireError(t, err, model.NotFound, "not_found")
	if len(repo.updated) != 0 {
		t.Fatal("unauthorized cancellation changed an entry")
	}
}

func TestFinishRequiresNoCalledStudentAndCancelsOnlyWaiting(t *testing.T) {
	t.Run("called student prevents finish", func(t *testing.T) {
		repo := baseRepository()
		repo.called = &model.Entry{ID: 81, SessionID: 3, Status: model.Called}
		_, err := New().Close(context.Background(), repo, testTeacher, 3, true)
		requireError(t, err, model.Conflict, "student_called")
		if len(repo.cancelled) != 0 || repo.stateChanges != 0 {
			t.Fatal("rejected finish changed queue state")
		}
	})
	t.Run("clean finish cancels waiting but preserves history", func(t *testing.T) {
		repo := baseRepository()
		repo.entries[81] = model.Entry{ID: 81, SessionID: 3, Status: model.Waiting}
		repo.entries[82] = model.Entry{ID: 82, SessionID: 3, Status: model.Completed}
		repo.entries[83] = model.Entry{ID: 83, SessionID: 4, Status: model.Waiting}
		result, err := New().Close(context.Background(), repo, testTeacher, 3, true)
		if err != nil || result.Session == nil || !result.Session.Finished || !result.Session.RegistrationClosed || result.Session.RegistrationOpen {
			t.Fatalf("session was not finished: %+v %v", result, err)
		}
		if repo.entries[81].Status != model.Cancelled || repo.entries[82].Status != model.Completed || repo.entries[83].Status != model.Waiting {
			t.Fatal("finish changed history or another session")
		}
		if !reflect.DeepEqual(repo.cancelled, []int64{3}) || repo.stateChanges != 1 {
			t.Fatalf("finish not applied once: cancelled=%v changes=%d", repo.cancelled, repo.stateChanges)
		}
		_, err = New().Close(context.Background(), repo, testTeacher, 3, true)
		if err != nil || len(repo.cancelled) != 1 || repo.stateChanges != 1 {
			t.Fatal("repeated finish changed the terminal session")
		}
	})
	t.Run("closing registration keeps waiting queue", func(t *testing.T) {
		repo := baseRepository()
		repo.entries[81] = model.Entry{ID: 81, SessionID: 3, Status: model.Waiting}
		result, err := New().Close(context.Background(), repo, testTeacher, 3, false)
		if err != nil || result.Session == nil || !result.Session.RegistrationClosed || result.Session.Finished || len(repo.cancelled) != 0 || repo.entries[81].Status != model.Waiting {
			t.Fatalf("registration close discarded queue: %+v %v", result, err)
		}
	})
}
