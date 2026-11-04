package usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"labqueue/projects/queue/internal/model"
	"labqueue/projects/queue/internal/service"
)

type fakeStore struct {
	Store
	lookupFn   func(context.Context, model.Mutation) (model.Result, bool, error)
	withinFn   func(context.Context, model.Mutation, func(service.Repository) (model.Result, error)) (model.Result, error)
	sessionsFn func(context.Context, model.SessionFilter) (model.SessionList, error)
	snapshotFn func(context.Context, int64, int64) (model.Snapshot, error)
	lookups    int
	writes     int
	reads      int
}

func (s *fakeStore) Lookup(ctx context.Context, mutation model.Mutation) (model.Result, bool, error) {
	s.lookups++
	if s.lookupFn != nil {
		return s.lookupFn(ctx, mutation)
	}
	return model.Result{}, false, nil
}

func (s *fakeStore) Within(ctx context.Context, mutation model.Mutation, fn func(service.Repository) (model.Result, error)) (model.Result, error) {
	s.writes++
	if s.withinFn != nil {
		return s.withinFn(ctx, mutation, fn)
	}
	return model.Result{}, nil
}

func (s *fakeStore) Sessions(ctx context.Context, filter model.SessionFilter) (model.SessionList, error) {
	s.reads++
	return s.sessionsFn(ctx, filter)
}

func (s *fakeStore) Snapshot(ctx context.Context, id, studentID int64) (model.Snapshot, error) {
	s.reads++
	return s.snapshotFn(ctx, id, studentID)
}

type fakeGroups struct {
	fn    func(context.Context, int64) (model.Group, error)
	calls int
}

func (g *fakeGroups) Group(ctx context.Context, id int64) (model.Group, error) {
	g.calls++
	if g.fn != nil {
		return g.fn(ctx, id)
	}
	return model.Group{ID: id, Name: "ИВТ-101"}, nil
}

var (
	appNow     = time.Date(2026, time.October, 29, 13, 0, 0, 0, time.UTC)
	appTeacher = model.Actor{ID: 9, Role: model.Teacher, Name: "Преподаватель"}
	appStudent = model.Actor{ID: 21, Role: model.Student, GroupID: 4, Name: "Студент"}
)

func createInput() model.CreateSession {
	return model.CreateSession{SubjectID: 2, GroupID: 4, LabTitle: "  Лабораторная №3  ", StartsAt: appNow.Add(-time.Hour), EndsAt: appNow.Add(time.Hour)}
}

func wantProblem(t *testing.T, err error, kind model.ErrorKind, code string) {
	t.Helper()
	var problem *model.Error
	if !errors.As(err, &problem) || problem.Kind != kind || problem.Code != code {
		t.Fatalf("err=%v; want %s/%s", err, kind, code)
	}
}

func TestCreateReplayDoesNotDependOnGroupsOrStartTransaction(t *testing.T) {
	stored := model.Result{Session: &model.Session{ID: 81, LabTitle: "Лабораторная №3"}}
	store := &fakeStore{lookupFn: func(_ context.Context, mutation model.Mutation) (model.Result, bool, error) {
		if mutation.ActorID != appTeacher.ID || mutation.Key != "same-request" || mutation.Operation != model.Create || mutation.Create.LabTitle != "Лабораторная №3" {
			t.Fatalf("lookup did not receive the normalized command: %+v", mutation)
		}
		return stored, true, nil
	}}
	groups := &fakeGroups{fn: func(context.Context, int64) (model.Group, error) {
		t.Fatal("replay must not request a group")
		return model.Group{}, nil
	}}
	result, err := New(store, groups).Create(context.Background(), appTeacher, createInput(), "same-request")
	if err != nil || !reflect.DeepEqual(result, stored) || store.lookups != 1 || store.writes != 0 || groups.calls != 0 {
		t.Fatalf("replay=%+v err=%v lookup=%d writes=%d groups=%d", result, err, store.lookups, store.writes, groups.calls)
	}
}

type createRepository struct {
	service.Repository
	input model.CreateSession
	group model.Group
	actor model.Actor
}

func (r *createRepository) SubjectExists(context.Context, int64) (bool, error) { return true, nil }
func (r *createRepository) Now(context.Context) (time.Time, error)             { return appNow, nil }
func (r *createRepository) CreateSession(_ context.Context, actor model.Actor, input model.CreateSession, group model.Group) (int64, error) {
	r.input, r.group, r.actor = input, group, actor
	return 81, nil
}
func (r *createRepository) Session(context.Context, int64) (model.Session, error) {
	return model.Session{ID: 81, TeacherID: r.actor.ID, GroupID: r.group.ID, GroupName: r.group.Name, LabTitle: r.input.LabTitle, StartsAt: r.input.StartsAt, EndsAt: r.input.EndsAt}, nil
}

func TestCreateValidatesExternalGroupBeforeUnitOfWork(t *testing.T) {
	var events []string
	repo := &createRepository{}
	store := &fakeStore{
		lookupFn: func(context.Context, model.Mutation) (model.Result, bool, error) {
			events = append(events, "lookup")
			return model.Result{}, false, nil
		},
		withinFn: func(_ context.Context, mutation model.Mutation, fn func(service.Repository) (model.Result, error)) (model.Result, error) {
			events = append(events, "transaction")
			if mutation.Create.LabTitle != "Лабораторная №3" || mutation.Create.GroupID != 4 || mutation.ActorID != appTeacher.ID || mutation.Key != "create-1" {
				t.Fatalf("incorrect command passed to transaction: %+v", mutation)
			}
			return fn(repo)
		},
	}
	groups := &fakeGroups{fn: func(_ context.Context, id int64) (model.Group, error) {
		events = append(events, "group")
		if id != 4 {
			t.Fatal("wrong group requested", id)
		}
		return model.Group{ID: 4, Name: "ИВТ-101"}, nil
	}}
	result, err := New(store, groups).Create(context.Background(), appTeacher, createInput(), "create-1")
	if err != nil || result.Session == nil || result.Session.ID != 81 || !result.Session.RegistrationOpen {
		t.Fatalf("create=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(events, []string{"lookup", "group", "transaction"}) || repo.input.LabTitle != "Лабораторная №3" || repo.group.ID != 4 || repo.group.Name != "ИВТ-101" {
		t.Fatalf("incorrect orchestration: events=%v input=%+v group=%+v", events, repo.input, repo.group)
	}
}

func TestCreateGroupFailureDoesNotStartUnitOfWork(t *testing.T) {
	blocked := model.Problem(model.Unavailable, "accounts_unavailable", "Сервис групп недоступен")
	store := &fakeStore{}
	groups := &fakeGroups{fn: func(context.Context, int64) (model.Group, error) { return model.Group{}, blocked }}
	_, err := New(store, groups).Create(context.Background(), appTeacher, createInput(), "create-1")
	if !errors.Is(err, blocked) || store.lookups != 1 || store.writes != 0 || groups.calls != 1 {
		t.Fatalf("group error not propagated safely: err=%v writes=%d groups=%d", err, store.writes, groups.calls)
	}
}

func TestInvalidIdempotencyKeysHaveNoExternalEffects(t *testing.T) {
	for _, key := range []string{"", "two words", "line\nbreak", "tab\tkey", strings.Repeat("x", 129)} {
		t.Run(key, func(t *testing.T) {
			store, groups := &fakeStore{}, &fakeGroups{}
			u := New(store, groups)
			_, err := u.Create(context.Background(), appTeacher, createInput(), key)
			wantProblem(t, err, model.Invalid, "idempotency_key")
			_, err = u.Join(context.Background(), appStudent, 3, key)
			wantProblem(t, err, model.Invalid, "idempotency_key")
			_, err = u.CallNext(context.Background(), appTeacher, 3, key)
			wantProblem(t, err, model.Invalid, "idempotency_key")
			if store.lookups != 0 || store.writes != 0 || groups.calls != 0 {
				t.Fatalf("invalid key reached a port: lookups=%d writes=%d groups=%d", store.lookups, store.writes, groups.calls)
			}
		})
	}
}

func TestRoleCannotDispatchAnotherRolesCommand(t *testing.T) {
	store, groups := &fakeStore{}, &fakeGroups{}
	u := New(store, groups)
	commands := []struct {
		name string
		run  func() (model.Result, error)
	}{
		{"student creates session", func() (model.Result, error) { return u.Create(context.Background(), appStudent, createInput(), "key") }},
		{"teacher joins queue", func() (model.Result, error) { return u.Join(context.Background(), appTeacher, 3, "key") }},
		{"student calls next", func() (model.Result, error) { return u.CallNext(context.Background(), appStudent, 3, "key") }},
		{"student completes", func() (model.Result, error) {
			return u.ChangeEntry(context.Background(), appStudent, 3, 81, model.Completed)
		}},
		{"student skips", func() (model.Result, error) {
			return u.ChangeEntry(context.Background(), appStudent, 3, 81, model.Skipped)
		}},
		{"teacher cancels for student", func() (model.Result, error) {
			return u.ChangeEntry(context.Background(), appTeacher, 3, 81, model.Cancelled)
		}},
		{"student closes registration", func() (model.Result, error) { return u.Close(context.Background(), appStudent, 3, false) }},
		{"student finishes", func() (model.Result, error) { return u.Close(context.Background(), appStudent, 3, true) }},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) { _, err := command.run(); wantProblem(t, err, model.Forbidden, "forbidden") })
	}
	_, err := u.ChangeEntry(context.Background(), appTeacher, 3, 81, model.Waiting)
	wantProblem(t, err, model.Invalid, "invalid_entry_state")
	if store.lookups != 0 || store.writes != 0 || groups.calls != 0 {
		t.Fatal("forbidden command reached an external port")
	}
}

func TestSessionReadUsesIdentityScopeAndDatabaseObservationTime(t *testing.T) {
	var captured []model.SessionFilter
	store := &fakeStore{sessionsFn: func(_ context.Context, filter model.SessionFilter) (model.SessionList, error) {
		captured = append(captured, filter)
		return model.SessionList{ObservedAt: appNow, Sessions: []model.Session{
			{ID: 3, EndsAt: appNow.Add(time.Hour)},
			{ID: 4, EndsAt: appNow, RegistrationOpen: true},
		}}, nil
	}}
	u := New(store, &fakeGroups{})
	list, err := u.Sessions(context.Background(), appStudent, model.SessionFilter{SubjectID: 2, TeacherID: 999})
	if err != nil || len(list) != 2 || !list[0].RegistrationOpen || list[1].RegistrationOpen {
		t.Fatalf("session availability ignored database clock: list=%+v err=%v", list, err)
	}
	if captured[0].GroupID != appStudent.GroupID || captured[0].TeacherID != 0 || captured[0].SubjectID != 2 {
		t.Fatalf("student escaped group scope: %+v", captured[0])
	}
	_, err = u.Sessions(context.Background(), appTeacher, model.SessionFilter{GroupID: 4, TeacherID: 999})
	if err != nil || captured[1].TeacherID != appTeacher.ID || captured[1].GroupID != 4 {
		t.Fatalf("teacher escaped ownership scope: filters=%+v err=%v", captured, err)
	}
	_, err = u.Sessions(context.Background(), appStudent, model.SessionFilter{GroupID: 5})
	wantProblem(t, err, model.Forbidden, "forbidden")
	if store.reads != 2 {
		t.Fatal("forbidden group filter reached the store")
	}
}

func TestQueueReadRejectsOtherGroupWithoutReturningItsData(t *testing.T) {
	store := &fakeStore{snapshotFn: func(_ context.Context, id, studentID int64) (model.Snapshot, error) {
		if id != 3 || studentID != appStudent.ID {
			t.Fatalf("wrong queue read identity: session=%d student=%d", id, studentID)
		}
		return model.Snapshot{Session: model.Session{ID: 3, GroupID: 5}, Entries: []model.Entry{{ID: 81, StudentName: "Чужой студент"}}, ObservedAt: appNow}, nil
	}}
	result, err := New(store, &fakeGroups{}).Queue(context.Background(), appStudent, 3)
	wantProblem(t, err, model.Forbidden, "forbidden")
	if !reflect.DeepEqual(result, model.Snapshot{}) {
		t.Fatalf("unauthorized queue data returned: %+v", result)
	}
}

type lockFailureRepository struct {
	service.Repository
	err error
}

func (r lockFailureRepository) LockSession(context.Context, int64) (model.Session, error) {
	return model.Session{}, r.err
}

func TestWritePassesCommandAndCallbackToUnitOfWorkAndPropagatesFailure(t *testing.T) {
	for _, invokeCallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "transaction cannot start", true: "business callback fails"}[invokeCallback], func(t *testing.T) {
			failure := errors.New("transaction rolled back")
			store := &fakeStore{withinFn: func(ctx context.Context, mutation model.Mutation, fn func(service.Repository) (model.Result, error)) (model.Result, error) {
				if mutation.ActorID != appStudent.ID || mutation.Operation != model.Join || mutation.SessionID != 3 || mutation.Key != "join-1" || fn == nil {
					t.Fatalf("write command or callback lost: %+v", mutation)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 12*time.Second {
					t.Fatal("write must have a bounded transaction context")
				}
				if invokeCallback {
					return fn(lockFailureRepository{err: failure})
				}
				return model.Result{}, failure
			}}
			result, err := New(store, &fakeGroups{}).Join(context.Background(), appStudent, 3, "join-1")
			if !errors.Is(err, failure) || !reflect.DeepEqual(result, model.Result{}) || store.writes != 1 {
				t.Fatalf("transaction failure swallowed: result=%+v err=%v writes=%d", result, err, store.writes)
			}
		})
	}
}
