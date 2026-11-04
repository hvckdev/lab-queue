package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"labqueue/projects/queue/internal/model"
	"labqueue/projects/queue/internal/service"
)

var dialect = goqu.Dialect("postgres")

type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

type scanner interface{ Scan(...any) error }
type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
type sqlBuilder interface {
	ToSQL() (string, []any, error)
}

type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }

// Build errors travel through Scan just like driver errors; no malformed SQL is
// sent to PostgreSQL. All datasets use prepared SQL and separate arguments.
func queryRow(ctx context.Context, q querier, dataset sqlBuilder) pgx.Row {
	sql, args, err := dataset.ToSQL()
	if err != nil {
		return failedRow{err: err}
	}
	return q.QueryRow(ctx, sql, args...)
}

func query(ctx context.Context, q querier, dataset sqlBuilder) (pgx.Rows, error) {
	sql, args, err := dataset.ToSQL()
	if err != nil {
		return nil, err
	}
	return q.Query(ctx, sql, args...)
}

func execute(ctx context.Context, q executor, dataset sqlBuilder) error {
	sql, args, err := dataset.ToSQL()
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, sql, args...)
	return persistenceError(err)
}

func persistenceError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		switch pgerr.ConstraintName {
		case "entries_one_active_student":
			return model.Problem(model.Conflict, "already_in_queue", "Вы уже записаны в эту очередь")
		case "entries_one_called":
			return model.Problem(model.Conflict, "student_called", "Сначала завершите или пропустите вызванного студента")
		case "subjects_name_key", "subjects_name_normalized_key":
			return model.Problem(model.Conflict, "subject_exists", "Предмет с таким названием уже существует")
		}
	}
	return err
}

func sessionSelect() *goqu.SelectDataset {
	active := dialect.From(goqu.T("entries").As("e")).Select(goqu.COUNT(goqu.Star())).
		Where(goqu.I("e.session_id").Eq(goqu.I("s.id")), goqu.I("e.status").In("waiting", "called"))
	return dialect.From(goqu.T("sessions").As("s")).
		Join(goqu.T("subjects").As("p"), goqu.On(goqu.I("p.id").Eq(goqu.I("s.subject_id")))).
		Select("s.id", "s.teacher_id", "s.subject_id", "p.name", "s.group_id", "s.group_name", "s.lab_title", "s.starts_at", "s.ends_at", "s.registration_closed", "s.finished", active, "s.next_ticket").
		Prepared(true)
}

func scanSession(row scanner) (model.Session, error) {
	var s model.Session
	err := row.Scan(&s.ID, &s.TeacherID, &s.SubjectID, &s.SubjectName, &s.GroupID, &s.GroupName, &s.LabTitle, &s.StartsAt, &s.EndsAt, &s.RegistrationClosed, &s.Finished, &s.ActiveCount, &s.NextTicket)
	return s, persistenceError(err)
}

func getSession(ctx context.Context, q querier, id int64) (model.Session, error) {
	return scanSession(queryRow(ctx, q, sessionSelect().Where(goqu.I("s.id").Eq(id))))
}

func entrySelect() *goqu.SelectDataset {
	position := dialect.From(goqu.T("entries").As("a")).Select(goqu.COUNT(goqu.Star())).
		Where(goqu.I("a.session_id").Eq(goqu.I("e.session_id")), goqu.I("a.status").In("waiting", "called"), goqu.I("a.ticket_number").Lte(goqu.I("e.ticket_number")))
	return dialect.From(goqu.T("entries").As("e")).
		Select("e.id", "e.session_id", "e.student_id", "e.student_name", "e.ticket_number", "e.status", "e.created_at",
			goqu.Case().When(goqu.I("e.status").In("waiting", "called"), position).Else(0)).
		Prepared(true)
}

func scanEntry(row scanner) (model.Entry, error) {
	var e model.Entry
	err := row.Scan(&e.ID, &e.SessionID, &e.StudentID, &e.StudentName, &e.TicketNumber, &e.Status, &e.CreatedAt, &e.Position)
	return e, persistenceError(err)
}

func getEntry(ctx context.Context, q querier, id int64) (model.Entry, error) {
	return scanEntry(queryRow(ctx, q, entrySelect().Where(goqu.I("e.id").Eq(id))))
}

func (s *Store) Subjects(ctx context.Context) ([]model.Subject, error) {
	rows, err := query(ctx, s.db, dialect.From("subjects").Select("id", "name").Order(goqu.C("id").Asc()).Prepared(true))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Subject{}
	for rows.Next() {
		var subject model.Subject
		if err = rows.Scan(&subject.ID, &subject.Name); err != nil {
			return nil, err
		}
		result = append(result, subject)
	}
	return result, rows.Err()
}

func (s *Store) CreateSubject(ctx context.Context, name string) (model.Subject, error) {
	var subject model.Subject
	dataset := dialect.Insert("subjects").Rows(goqu.Record{"name": name}).Returning("id", "name").Prepared(true)
	err := queryRow(ctx, s.db, dataset).Scan(&subject.ID, &subject.Name)
	return subject, persistenceError(err)
}

func (s *Store) Sessions(ctx context.Context, filter model.SessionFilter) (model.SessionList, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return model.SessionList{}, err
	}
	defer tx.Rollback(ctx)
	dataset := sessionSelect()
	if filter.SubjectID != 0 {
		dataset = dataset.Where(goqu.I("s.subject_id").Eq(filter.SubjectID))
	}
	if filter.GroupID != 0 {
		dataset = dataset.Where(goqu.I("s.group_id").Eq(filter.GroupID))
	}
	if filter.TeacherID != 0 {
		dataset = dataset.Where(goqu.I("s.teacher_id").Eq(filter.TeacherID))
	}
	rows, err := query(ctx, tx, dataset.Order(goqu.I("s.finished").Asc(), goqu.I("s.starts_at").Asc(), goqu.I("s.id").Asc()))
	if err != nil {
		return model.SessionList{}, err
	}
	list := model.SessionList{Sessions: []model.Session{}}
	for rows.Next() {
		item, scanErr := scanSession(rows)
		if scanErr != nil {
			rows.Close()
			return model.SessionList{}, scanErr
		}
		list.Sessions = append(list.Sessions, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return model.SessionList{}, err
	}
	if err = queryRow(ctx, tx, dialect.Select(goqu.Func("clock_timestamp")).Prepared(true)).Scan(&list.ObservedAt); err != nil {
		return model.SessionList{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.SessionList{}, err
	}
	return list, nil
}

func (s *Store) Snapshot(ctx context.Context, id, studentID int64) (model.Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return model.Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	snapshot := model.Snapshot{Entries: []model.Entry{}}
	snapshot.Session, err = getSession(ctx, tx, id)
	if err != nil {
		return model.Snapshot{}, err
	}
	rows, err := query(ctx, tx, entrySelect().Where(goqu.I("e.session_id").Eq(id), goqu.I("e.status").In("waiting", "called")).Order(goqu.I("e.ticket_number").Asc()))
	if err != nil {
		return model.Snapshot{}, err
	}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			rows.Close()
			return model.Snapshot{}, scanErr
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return model.Snapshot{}, err
	}
	if studentID > 0 {
		entry, entryErr := scanEntry(queryRow(ctx, tx, entrySelect().Where(goqu.I("e.session_id").Eq(id), goqu.I("e.student_id").Eq(studentID)).Order(goqu.I("e.id").Desc()).Limit(1)))
		if entryErr != nil && !errors.Is(entryErr, model.ErrNotFound) {
			return model.Snapshot{}, entryErr
		}
		if entryErr == nil {
			snapshot.MyEntry = &entry
		}
	}
	if err = queryRow(ctx, tx, dialect.Select(goqu.Func("clock_timestamp")).Prepared(true)).Scan(&snapshot.ObservedAt); err != nil {
		return model.Snapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.Snapshot{}, err
	}
	return snapshot, nil
}

type repository struct{ tx pgx.Tx }

func (r *repository) Session(ctx context.Context, id int64) (model.Session, error) {
	return getSession(ctx, r.tx, id)
}
func (r *repository) LockSession(ctx context.Context, id int64) (model.Session, error) {
	var locked int64
	dataset := dialect.From("sessions").Select("id").Where(goqu.C("id").Eq(id)).ForUpdate(goqu.Wait).Prepared(true)
	if err := queryRow(ctx, r.tx, dataset).Scan(&locked); err != nil {
		return model.Session{}, persistenceError(err)
	}
	return r.Session(ctx, id)
}
func (r *repository) Now(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := queryRow(ctx, r.tx, dialect.Select(goqu.Func("clock_timestamp")).Prepared(true)).Scan(&now)
	return now, err
}
func (r *repository) SubjectExists(ctx context.Context, id int64) (bool, error) {
	var valid bool
	subject := dialect.From("subjects").Select(goqu.L("1")).Where(goqu.C("id").Eq(id))
	err := queryRow(ctx, r.tx, dialect.Select(goqu.L("EXISTS ?", subject)).Prepared(true)).Scan(&valid)
	return valid, err
}
func (r *repository) CreateSession(ctx context.Context, actor model.Actor, input model.CreateSession, group model.Group) (int64, error) {
	var id int64
	dataset := dialect.Insert("sessions").Rows(goqu.Record{"teacher_id": actor.ID, "subject_id": input.SubjectID, "group_id": input.GroupID, "group_name": group.Name, "lab_title": input.LabTitle, "starts_at": input.StartsAt, "ends_at": input.EndsAt}).Returning("id").Prepared(true)
	err := queryRow(ctx, r.tx, dataset).Scan(&id)
	return id, persistenceError(err)
}
func optionalEntry(row pgx.Row) (*model.Entry, error) {
	entry, err := scanEntry(row)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}
func (r *repository) ActiveEntry(ctx context.Context, sessionID, studentID int64) (*model.Entry, error) {
	return optionalEntry(queryRow(ctx, r.tx, entrySelect().Where(goqu.I("e.session_id").Eq(sessionID), goqu.I("e.student_id").Eq(studentID), goqu.I("e.status").In("waiting", "called"))))
}
func (r *repository) HasCompleted(ctx context.Context, sessionID, studentID int64) (bool, error) {
	var completed bool
	entries := dialect.From("entries").Select(goqu.L("1")).Where(goqu.C("session_id").Eq(sessionID), goqu.C("student_id").Eq(studentID), goqu.C("status").Eq("completed"))
	err := queryRow(ctx, r.tx, dialect.Select(goqu.L("EXISTS ?", entries)).Prepared(true)).Scan(&completed)
	return completed, err
}
func (r *repository) InsertEntry(ctx context.Context, entry model.Entry) (int64, error) {
	var id int64
	dataset := dialect.Insert("entries").Rows(goqu.Record{"session_id": entry.SessionID, "student_id": entry.StudentID, "student_name": entry.StudentName, "ticket_number": entry.TicketNumber, "status": string(entry.Status)}).Returning("id").Prepared(true)
	err := queryRow(ctx, r.tx, dataset).Scan(&id)
	return id, persistenceError(err)
}
func (r *repository) AdvanceTicket(ctx context.Context, id int64) error {
	return execute(ctx, r.tx, dialect.Update("sessions").Set(goqu.Record{"next_ticket": goqu.L("? + ?", goqu.C("next_ticket"), 1)}).Where(goqu.C("id").Eq(id)).Prepared(true))
}
func (r *repository) Entry(ctx context.Context, id int64) (model.Entry, error) {
	return getEntry(ctx, r.tx, id)
}
func (r *repository) CalledEntry(ctx context.Context, id int64) (*model.Entry, error) {
	return optionalEntry(queryRow(ctx, r.tx, entrySelect().Where(goqu.I("e.session_id").Eq(id), goqu.I("e.status").Eq("called"))))
}
func (r *repository) FirstWaiting(ctx context.Context, id int64) (*model.Entry, error) {
	return optionalEntry(queryRow(ctx, r.tx, entrySelect().Where(goqu.I("e.session_id").Eq(id), goqu.I("e.status").Eq("waiting")).Order(goqu.I("e.ticket_number").Asc()).Limit(1)))
}
func (r *repository) SetEntryStatus(ctx context.Context, id int64, status model.EntryStatus) error {
	return execute(ctx, r.tx, dialect.Update("entries").Set(goqu.Record{"status": string(status)}).Where(goqu.C("id").Eq(id)).Prepared(true))
}
func (r *repository) CancelWaiting(ctx context.Context, id int64) error {
	return execute(ctx, r.tx, dialect.Update("entries").Set(goqu.Record{"status": "cancelled"}).Where(goqu.C("session_id").Eq(id), goqu.C("status").Eq("waiting")).Prepared(true))
}
func (r *repository) SetSessionState(ctx context.Context, id int64, closed, finished bool) error {
	return execute(ctx, r.tx, dialect.Update("sessions").Set(goqu.Record{"registration_closed": closed, "finished": finished}).Where(goqu.C("id").Eq(id)).Prepared(true))
}

func (s *Store) Within(ctx context.Context, mutation model.Mutation, fn func(service.Repository) (model.Result, error)) (model.Result, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return model.Result{}, err
	}
	defer tx.Rollback(ctx)
	if mutation.Key != "" {
		if err = lockRequest(ctx, tx, mutation); err != nil {
			return model.Result{}, err
		}
		if result, found, lookupErr := lookup(ctx, tx, mutation); found || lookupErr != nil {
			return result, lookupErr
		}
	}
	result, err := fn(&repository{tx: tx})
	if err != nil {
		return model.Result{}, err
	}
	if mutation.Key != "" {
		if err = save(ctx, tx, mutation, result); err != nil {
			return model.Result{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return model.Result{}, persistenceError(err)
	}
	return result, nil
}

func (s *Store) Lookup(ctx context.Context, mutation model.Mutation) (model.Result, bool, error) {
	return lookup(ctx, s.db, mutation)
}
