package service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"labqueue/projects/queue/internal/model"
)

// Repository is the persistence port needed by queue rules. Implementations
// provide a session row lock and a fresh database clock in the same transaction.
type Repository interface {
	LockSession(context.Context, int64) (model.Session, error)
	Session(context.Context, int64) (model.Session, error)
	Now(context.Context) (time.Time, error)
	SubjectExists(context.Context, int64) (bool, error)
	CreateSession(context.Context, model.Actor, model.CreateSession, model.Group) (int64, error)
	ActiveEntry(context.Context, int64, int64) (*model.Entry, error)
	HasCompleted(context.Context, int64, int64) (bool, error)
	InsertEntry(context.Context, model.Entry) (int64, error)
	AdvanceTicket(context.Context, int64) error
	Entry(context.Context, int64) (model.Entry, error)
	CalledEntry(context.Context, int64) (*model.Entry, error)
	FirstWaiting(context.Context, int64) (*model.Entry, error)
	SetEntryStatus(context.Context, int64, model.EntryStatus) error
	CancelWaiting(context.Context, int64) error
	SetSessionState(context.Context, int64, bool, bool) error
}

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) RequireRole(actor model.Actor, role model.Role, message string) error {
	if actor.Role != role {
		return model.Problem(model.Forbidden, "forbidden", message)
	}
	return nil
}

func (s *Service) PrepareSubject(actor model.Actor, name string) (string, error) {
	if err := s.RequireRole(actor, model.Admin, "Добавлять предметы может только администратор"); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", model.Problem(model.Invalid, "invalid_subject", "Укажите название предмета длиной от 1 до 200 символов без управляющих знаков")
	}
	return name, nil
}

func (s *Service) Authorize(actor model.Actor, session model.Session) error {
	if actor.Role == model.Teacher && actor.ID == session.TeacherID || actor.Role == model.Student && actor.GroupID > 0 && actor.GroupID == session.GroupID {
		return nil
	}
	return model.Problem(model.Forbidden, "forbidden", "Нет доступа к этой очереди")
}

func (s *Service) PrepareCreate(actor model.Actor, input model.CreateSession) (model.CreateSession, error) {
	if err := s.RequireRole(actor, model.Teacher, "Создавать сеансы может только преподаватель"); err != nil {
		return input, err
	}
	input.LabTitle = strings.TrimSpace(input.LabTitle)
	if input.SubjectID <= 0 || input.GroupID <= 0 || utf8.RuneCountInString(input.LabTitle) < 1 || utf8.RuneCountInString(input.LabTitle) > 200 || input.StartsAt.IsZero() || input.EndsAt.IsZero() || !input.EndsAt.After(input.StartsAt) {
		return input, model.Problem(model.Invalid, "invalid_session", "Укажите предмет, группу, лабораторную и корректный интервал приема")
	}
	return input, nil
}

func (s *Service) ScopeSessions(actor model.Actor, filter model.SessionFilter) (model.SessionFilter, error) {
	if actor.Role == model.Student {
		if actor.GroupID <= 0 || filter.GroupID != 0 && filter.GroupID != actor.GroupID {
			return filter, model.Problem(model.Forbidden, "forbidden", "Доступны только сеансы вашей группы")
		}
		filter.GroupID = actor.GroupID
		filter.TeacherID = 0
	} else if actor.Role == model.Teacher {
		filter.TeacherID = actor.ID
	} else {
		return filter, model.Problem(model.Forbidden, "forbidden", "Нет доступа к сеансам")
	}
	return filter, nil
}

func (s *Service) Describe(session model.Session, now time.Time) model.Session {
	session.RegistrationOpen = !session.RegistrationClosed && !session.Finished && now.Before(session.EndsAt)
	return session
}

func (s *Service) ViewQueue(actor model.Actor, snapshot model.Snapshot) (model.Snapshot, error) {
	if err := s.Authorize(actor, snapshot.Session); err != nil {
		return model.Snapshot{}, err
	}
	snapshot.Session = s.Describe(snapshot.Session, snapshot.ObservedAt)
	snapshot.NextPosition = int64(len(snapshot.Entries)) + 1
	return snapshot, nil
}

func (s *Service) Create(ctx context.Context, repo Repository, actor model.Actor, input model.CreateSession, group model.Group) (model.Result, error) {
	input, err := s.PrepareCreate(actor, input)
	if err != nil {
		return model.Result{}, err
	}
	valid, err := repo.SubjectExists(ctx, input.SubjectID)
	if err != nil {
		return model.Result{}, err
	}
	if !valid {
		return model.Result{}, model.Problem(model.Invalid, "invalid_subject", "Предмет не найден")
	}
	now, err := repo.Now(ctx)
	if err != nil {
		return model.Result{}, err
	}
	if !input.EndsAt.After(now) {
		return model.Result{}, model.Problem(model.Invalid, "invalid_time", "Время окончания должно быть в будущем")
	}
	id, err := repo.CreateSession(ctx, actor, input, group)
	if err != nil {
		return model.Result{}, err
	}
	session, err := repo.Session(ctx, id)
	if err != nil {
		return model.Result{}, err
	}
	session = s.Describe(session, now)
	return model.Result{Session: &session}, nil
}

func (s *Service) lock(ctx context.Context, repo Repository, actor model.Actor, id int64) (model.Session, error) {
	session, err := repo.LockSession(ctx, id)
	if err != nil {
		return session, err
	}
	return session, s.Authorize(actor, session)
}

func (s *Service) Join(ctx context.Context, repo Repository, actor model.Actor, id int64) (model.Result, error) {
	if err := s.RequireRole(actor, model.Student, "Записываться могут только студенты"); err != nil {
		return model.Result{}, err
	}
	session, err := s.lock(ctx, repo, actor, id)
	if err != nil {
		return model.Result{}, err
	}
	active, err := repo.ActiveEntry(ctx, id, actor.ID)
	if err != nil {
		return model.Result{}, err
	}
	if active != nil {
		return model.Result{Entry: active}, nil
	}
	completed, err := repo.HasCompleted(ctx, id, actor.ID)
	if err != nil {
		return model.Result{}, err
	}
	if completed {
		return model.Result{}, model.Problem(model.Conflict, "already_completed", "Вы уже завершили сдачу в этом сеансе")
	}
	// Read a fresh time only after acquiring the session lock; a request may
	// have spent the remainder of the registration interval waiting for it.
	now, err := repo.Now(ctx)
	if err != nil {
		return model.Result{}, err
	}
	if session.Finished || session.RegistrationClosed || !now.Before(session.EndsAt) {
		return model.Result{}, model.Problem(model.Conflict, "registration_closed", "Запись на этот сеанс закрыта")
	}
	entryID, err := repo.InsertEntry(ctx, model.Entry{SessionID: id, StudentID: actor.ID, StudentName: actor.Name, TicketNumber: session.NextTicket, Status: model.Waiting})
	if err != nil {
		return model.Result{}, err
	}
	if err = repo.AdvanceTicket(ctx, id); err != nil {
		return model.Result{}, err
	}
	entry, err := repo.Entry(ctx, entryID)
	return model.Result{Entry: &entry}, err
}

func (s *Service) CallNext(ctx context.Context, repo Repository, actor model.Actor, id int64) (model.Result, error) {
	if err := s.RequireRole(actor, model.Teacher, "Вызывать студентов может только преподаватель"); err != nil {
		return model.Result{}, err
	}
	session, err := s.lock(ctx, repo, actor, id)
	if err != nil {
		return model.Result{}, err
	}
	if session.Finished {
		return model.Result{}, model.Problem(model.Conflict, "session_finished", "Сеанс завершен")
	}
	now, err := repo.Now(ctx)
	if err != nil {
		return model.Result{}, err
	}
	if now.Before(session.StartsAt) {
		return model.Result{}, model.Problem(model.Conflict, "not_started", "Время приема еще не наступило")
	}
	called, err := repo.CalledEntry(ctx, id)
	if err != nil {
		return model.Result{}, err
	}
	if called != nil {
		return model.Result{Entry: called}, nil
	}
	first, err := repo.FirstWaiting(ctx, id)
	if err != nil {
		return model.Result{}, err
	}
	if first == nil {
		return model.Result{}, nil
	}
	if err = repo.SetEntryStatus(ctx, first.ID, model.Called); err != nil {
		return model.Result{}, err
	}
	entry, err := repo.Entry(ctx, first.ID)
	return model.Result{Entry: &entry}, err
}

func (s *Service) ChangeEntry(ctx context.Context, repo Repository, actor model.Actor, sessionID, entryID int64, target model.EntryStatus) (model.Result, error) {
	if target != model.Cancelled && target != model.Completed && target != model.Skipped {
		return model.Result{}, model.Problem(model.Invalid, "invalid_entry_state", "Некорректное действие над записью")
	}
	role := model.Teacher
	if target == model.Cancelled {
		role = model.Student
	}
	if err := s.RequireRole(actor, role, "Это действие недоступно для вашей роли"); err != nil {
		return model.Result{}, err
	}
	if _, err := s.lock(ctx, repo, actor, sessionID); err != nil {
		return model.Result{}, err
	}
	entry, err := repo.Entry(ctx, entryID)
	if err != nil {
		return model.Result{}, err
	}
	if entry.SessionID != sessionID {
		return model.Result{}, model.ErrNotFound
	}
	if target == model.Cancelled && entry.StudentID != actor.ID {
		return model.Result{}, model.Problem(model.Forbidden, "forbidden", "Можно отменить только собственную запись")
	}
	if entry.Status == target {
		return model.Result{Entry: &entry}, nil
	}
	expected := model.Called
	if target == model.Cancelled {
		expected = model.Waiting
	}
	if entry.Status != expected {
		return model.Result{}, model.Problem(model.Conflict, "invalid_entry_state", "Состояние записи уже изменилось. Обновите очередь")
	}
	if err = repo.SetEntryStatus(ctx, entryID, target); err != nil {
		return model.Result{}, err
	}
	entry, err = repo.Entry(ctx, entryID)
	return model.Result{Entry: &entry}, err
}

func (s *Service) Close(ctx context.Context, repo Repository, actor model.Actor, id int64, finish bool) (model.Result, error) {
	if err := s.RequireRole(actor, model.Teacher, "Управлять сеансом может только преподаватель"); err != nil {
		return model.Result{}, err
	}
	session, err := s.lock(ctx, repo, actor, id)
	if err != nil {
		return model.Result{}, err
	}
	if !session.Finished {
		if finish {
			called, err := repo.CalledEntry(ctx, id)
			if err != nil {
				return model.Result{}, err
			}
			if called != nil {
				return model.Result{}, model.Problem(model.Conflict, "student_called", "Сначала завершите или пропустите вызванного студента")
			}
			if err = repo.CancelWaiting(ctx, id); err != nil {
				return model.Result{}, err
			}
		}
		if err = repo.SetSessionState(ctx, id, true, finish); err != nil {
			return model.Result{}, err
		}
		session, err = repo.Session(ctx, id)
		if err != nil {
			return model.Result{}, err
		}
	}
	now, err := repo.Now(ctx)
	if err != nil {
		return model.Result{}, err
	}
	session = s.Describe(session, now)
	return model.Result{Session: &session}, nil
}
