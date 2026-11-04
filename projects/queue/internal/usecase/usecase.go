package usecase

import (
	"context"
	"strings"
	"time"

	"labqueue/projects/queue/internal/model"
	"labqueue/projects/queue/internal/service"
)

// Store is the read side and unit of work required by application scenarios.
// Within persists keyed results in the same transaction as the callback.
type Store interface {
	Subjects(context.Context) ([]model.Subject, error)
	CreateSubject(context.Context, string) (model.Subject, error)
	Sessions(context.Context, model.SessionFilter) (model.SessionList, error)
	Snapshot(context.Context, int64, int64) (model.Snapshot, error)
	Lookup(context.Context, model.Mutation) (model.Result, bool, error)
	Within(context.Context, model.Mutation, func(service.Repository) (model.Result, error)) (model.Result, error)
}

type Groups interface {
	Group(context.Context, int64) (model.Group, error)
}

type UseCase struct {
	store  Store
	groups Groups
	rules  *service.Service
}

func New(store Store, groups Groups) *UseCase {
	return &UseCase{store: store, groups: groups, rules: service.New()}
}

func ValidateKey(key string) error {
	if len(key) < 1 || len(key) > 128 || strings.ContainsAny(key, "\r\n\t ") {
		return model.Problem(model.Invalid, "idempotency_key", "Требуется Idempotency-Key длиной до 128 символов без пробелов")
	}
	return nil
}

func (u *UseCase) Subjects(ctx context.Context) ([]model.Subject, error) {
	return u.store.Subjects(ctx)
}

func (u *UseCase) CreateSubject(ctx context.Context, actor model.Actor, name string) (model.Subject, error) {
	name, err := u.rules.PrepareSubject(actor, name)
	if err != nil {
		return model.Subject{}, err
	}
	return u.store.CreateSubject(ctx, name)
}

func (u *UseCase) Sessions(ctx context.Context, actor model.Actor, filter model.SessionFilter) ([]model.Session, error) {
	filter, err := u.rules.ScopeSessions(actor, filter)
	if err != nil {
		return nil, err
	}
	list, err := u.store.Sessions(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range list.Sessions {
		list.Sessions[i] = u.rules.Describe(list.Sessions[i], list.ObservedAt)
	}
	return list.Sessions, nil
}

func (u *UseCase) Queue(ctx context.Context, actor model.Actor, id int64) (model.Snapshot, error) {
	var studentID int64
	if actor.Role == model.Student {
		studentID = actor.ID
	}
	snapshot, err := u.store.Snapshot(ctx, id, studentID)
	if err != nil {
		return model.Snapshot{}, err
	}
	return u.rules.ViewQueue(actor, snapshot)
}

func (u *UseCase) write(ctx context.Context, mutation model.Mutation, fn func(context.Context, service.Repository) (model.Result, error)) (model.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return u.store.Within(ctx, mutation, func(repo service.Repository) (model.Result, error) { return fn(ctx, repo) })
}

func (u *UseCase) Create(ctx context.Context, actor model.Actor, input model.CreateSession, key string) (model.Result, error) {
	input, err := u.rules.PrepareCreate(actor, input)
	if err != nil {
		return model.Result{}, err
	}
	if err = ValidateKey(key); err != nil {
		return model.Result{}, err
	}
	mutation := model.Mutation{ActorID: actor.ID, Key: key, Operation: model.Create, Create: input}
	// Existing results remain available even when the accounts service is down.
	if result, found, err := u.store.Lookup(ctx, mutation); found || err != nil {
		return result, err
	}
	// External validation intentionally happens before opening the transaction
	// and acquiring any queue lock.
	group, err := u.groups.Group(ctx, input.GroupID)
	if err != nil {
		return model.Result{}, err
	}
	return u.write(ctx, mutation, func(ctx context.Context, repo service.Repository) (model.Result, error) {
		return u.rules.Create(ctx, repo, actor, input, group)
	})
}

func (u *UseCase) Join(ctx context.Context, actor model.Actor, id int64, key string) (model.Result, error) {
	if err := u.rules.RequireRole(actor, model.Student, "Записываться могут только студенты"); err != nil {
		return model.Result{}, err
	}
	if err := ValidateKey(key); err != nil {
		return model.Result{}, err
	}
	return u.write(ctx, model.Mutation{ActorID: actor.ID, Key: key, Operation: model.Join, SessionID: id}, func(ctx context.Context, repo service.Repository) (model.Result, error) {
		return u.rules.Join(ctx, repo, actor, id)
	})
}

func (u *UseCase) CallNext(ctx context.Context, actor model.Actor, id int64, key string) (model.Result, error) {
	if err := u.rules.RequireRole(actor, model.Teacher, "Вызывать студентов может только преподаватель"); err != nil {
		return model.Result{}, err
	}
	if err := ValidateKey(key); err != nil {
		return model.Result{}, err
	}
	return u.write(ctx, model.Mutation{ActorID: actor.ID, Key: key, Operation: model.CallNext, SessionID: id}, func(ctx context.Context, repo service.Repository) (model.Result, error) {
		return u.rules.CallNext(ctx, repo, actor, id)
	})
}

func (u *UseCase) ChangeEntry(ctx context.Context, actor model.Actor, sessionID, entryID int64, target model.EntryStatus) (model.Result, error) {
	role := model.Teacher
	operation := model.Complete
	switch target {
	case model.Cancelled:
		role = model.Student
		operation = model.Cancel
	case model.Completed:
	case model.Skipped:
		operation = model.Skip
	default:
		return model.Result{}, model.Problem(model.Invalid, "invalid_entry_state", "Некорректное действие над записью")
	}
	if err := u.rules.RequireRole(actor, role, "Это действие недоступно для вашей роли"); err != nil {
		return model.Result{}, err
	}
	return u.write(ctx, model.Mutation{ActorID: actor.ID, Operation: operation, SessionID: sessionID}, func(ctx context.Context, repo service.Repository) (model.Result, error) {
		return u.rules.ChangeEntry(ctx, repo, actor, sessionID, entryID, target)
	})
}

func (u *UseCase) Close(ctx context.Context, actor model.Actor, id int64, finish bool) (model.Result, error) {
	if err := u.rules.RequireRole(actor, model.Teacher, "Управлять сеансом может только преподаватель"); err != nil {
		return model.Result{}, err
	}
	op := model.CloseRegistration
	if finish {
		op = model.Finish
	}
	return u.write(ctx, model.Mutation{ActorID: actor.ID, Operation: op, SessionID: id}, func(ctx context.Context, repo service.Repository) (model.Result, error) {
		return u.rules.Close(ctx, repo, actor, id, finish)
	})
}
