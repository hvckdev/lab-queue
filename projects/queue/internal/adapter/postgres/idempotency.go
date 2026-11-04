package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/jackc/pgx/v5"
	"labqueue/projects/queue/internal/adapter/legacyhttp"
	"labqueue/projects/queue/internal/model"
)

// The structs below describe the previously persisted response format. They
// deliberately live at the persistence edge, never in the business model.
type savedSession struct {
	ID                 int64     `json:"id"`
	TeacherID          int64     `json:"teacher_id"`
	SubjectID          int64     `json:"subject_id"`
	SubjectName        string    `json:"subject_name"`
	GroupID            int64     `json:"group_id"`
	GroupName          string    `json:"group_name"`
	LabTitle           string    `json:"lab_title"`
	StartsAt           time.Time `json:"starts_at"`
	EndsAt             time.Time `json:"ends_at"`
	RegistrationClosed bool      `json:"registration_closed"`
	Finished           bool      `json:"finished"`
	RegistrationOpen   bool      `json:"registration_open"`
	ActiveCount        int64     `json:"active_count"`
}

type savedEntry struct {
	ID           int64     `json:"id"`
	SessionID    int64     `json:"session_id"`
	StudentID    int64     `json:"student_id"`
	StudentName  string    `json:"student_name"`
	TicketNumber int64     `json:"ticket_number"`
	Status       string    `json:"status"`
	Position     int64     `json:"position"`
	CreatedAt    time.Time `json:"created_at"`
}

func persistSession(s model.Session) savedSession {
	return savedSession{ID: s.ID, TeacherID: s.TeacherID, SubjectID: s.SubjectID, SubjectName: s.SubjectName, GroupID: s.GroupID, GroupName: s.GroupName, LabTitle: s.LabTitle, StartsAt: s.StartsAt, EndsAt: s.EndsAt, RegistrationClosed: s.RegistrationClosed, Finished: s.Finished, RegistrationOpen: s.RegistrationOpen, ActiveCount: s.ActiveCount}
}
func (s savedSession) domain() model.Session {
	return model.Session{ID: s.ID, TeacherID: s.TeacherID, SubjectID: s.SubjectID, SubjectName: s.SubjectName, GroupID: s.GroupID, GroupName: s.GroupName, LabTitle: s.LabTitle, StartsAt: s.StartsAt, EndsAt: s.EndsAt, RegistrationClosed: s.RegistrationClosed, Finished: s.Finished, RegistrationOpen: s.RegistrationOpen, ActiveCount: s.ActiveCount}
}
func persistEntry(e model.Entry) savedEntry {
	return savedEntry{ID: e.ID, SessionID: e.SessionID, StudentID: e.StudentID, StudentName: e.StudentName, TicketNumber: e.TicketNumber, Status: string(e.Status), Position: e.Position, CreatedAt: e.CreatedAt}
}
func (e savedEntry) domain() model.Entry {
	return model.Entry{ID: e.ID, SessionID: e.SessionID, StudentID: e.StudentID, StudentName: e.StudentName, TicketNumber: e.TicketNumber, Status: model.EntryStatus(e.Status), Position: e.Position, CreatedAt: e.CreatedAt}
}

// Field order and normalization must match the original createRequest JSON.
type savedCreate struct {
	SubjectID int64     `json:"subject_id"`
	GroupID   int64     `json:"group_id"`
	LabTitle  string    `json:"lab_title"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
}

func fingerprint(ctx context.Context, m model.Mutation) (string, error) {
	method, path, ok := legacyhttp.Identity(ctx)
	if !ok {
		method = "POST"
		switch m.Operation {
		case model.Create:
			path = "/api/sessions"
		case model.Join:
			path = fmt.Sprintf("/api/sessions/%d/entries", m.SessionID)
		case model.CallNext:
			path = fmt.Sprintf("/api/sessions/%d/call-next", m.SessionID)
		default:
			return "", errors.New("unsupported keyed queue operation")
		}
	}
	var payload any = struct{}{}
	if m.Operation == model.Create {
		c := m.Create
		payload = savedCreate{c.SubjectID, c.GroupID, c.LabTitle, c.StartsAt, c.EndsAt}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(append([]byte(method+" "+path+"\n"), data...))
	return hex.EncodeToString(hash[:]), nil
}

func lockRequest(ctx context.Context, tx pgx.Tx, m model.Mutation) error {
	scope := fmt.Sprintf("queue:%d:%s", m.ActorID, m.Key)
	return execute(ctx, tx, dialect.Select(goqu.Func("pg_advisory_xact_lock", goqu.Func("hashtextextended", scope, int64(0)))).Prepared(true))
}

func lookup(ctx context.Context, q querier, m model.Mutation) (model.Result, bool, error) {
	fp, err := fingerprint(ctx, m)
	if err != nil {
		return model.Result{}, false, err
	}
	var stored string
	var status int
	var data []byte
	dataset := dialect.From("idempotency").Select("fingerprint", "status_code", "response").Where(goqu.C("actor_id").Eq(m.ActorID), goqu.C("key").Eq(m.Key)).Prepared(true)
	err = queryRow(ctx, q, dataset).Scan(&stored, &status, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Result{}, false, nil
	}
	if err != nil {
		return model.Result{}, false, err
	}
	if stored != fp {
		return model.Result{}, true, model.Problem(model.Conflict, "idempotency_conflict", "Этот ключ уже использован для другого запроса")
	}
	result, err := restore(m.Operation, status, data)
	return result, true, err
}

func restore(op model.Operation, status int, data []byte) (model.Result, error) {
	if op == model.Create {
		if status != 201 {
			return model.Result{}, errors.New("invalid persisted create status")
		}
		var envelope struct {
			Session *savedSession `json:"session"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return model.Result{}, err
		}
		if envelope.Session == nil {
			return model.Result{}, errors.New("missing persisted session")
		}
		session := envelope.Session.domain()
		return model.Result{Session: &session}, nil
	}
	expected := 200
	if op == model.Join {
		expected = 201
	}
	if status != expected {
		return model.Result{}, errors.New("invalid persisted entry status")
	}
	var envelope struct {
		Entry *savedEntry `json:"entry"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return model.Result{}, err
	}
	if envelope.Entry == nil {
		return model.Result{}, nil
	}
	entry := envelope.Entry.domain()
	return model.Result{Entry: &entry}, nil
}

func serialize(op model.Operation, result model.Result) (int, []byte, error) {
	if op == model.Create {
		if result.Session == nil {
			return 0, nil, errors.New("missing create result")
		}
		data, err := json.Marshal(struct {
			Session savedSession `json:"session"`
		}{persistSession(*result.Session)})
		return 201, data, err
	}
	var entry *savedEntry
	if result.Entry != nil {
		value := persistEntry(*result.Entry)
		entry = &value
	}
	data, err := json.Marshal(struct {
		Entry *savedEntry `json:"entry"`
	}{entry})
	status := 200
	if op == model.Join {
		status = 201
	}
	return status, data, err
}

func save(ctx context.Context, tx pgx.Tx, m model.Mutation, result model.Result) error {
	fp, err := fingerprint(ctx, m)
	if err != nil {
		return err
	}
	status, data, err := serialize(m.Operation, result)
	if err != nil {
		return err
	}
	dataset := dialect.Insert("idempotency").Rows(goqu.Record{"actor_id": m.ActorID, "key": m.Key, "fingerprint": fp, "status_code": status, "response": string(data)}).Prepared(true)
	return execute(ctx, tx, dataset)
}
