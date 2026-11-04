package model

import "time"

type Role string

const (
	Student Role = "student"
	Teacher Role = "teacher"
	Admin   Role = "admin"
)

type Actor struct {
	ID      int64
	Name    string
	Role    Role
	GroupID int64
}
type Subject struct {
	ID   int64
	Name string
}
type Group struct {
	ID   int64
	Name string
}

type Session struct {
	ID, TeacherID, SubjectID, GroupID              int64
	SubjectName, GroupName, LabTitle               string
	StartsAt, EndsAt                               time.Time
	RegistrationClosed, Finished, RegistrationOpen bool
	ActiveCount, NextTicket                        int64
}

type EntryStatus string

const (
	Waiting   EntryStatus = "waiting"
	Called    EntryStatus = "called"
	Completed EntryStatus = "completed"
	Skipped   EntryStatus = "skipped"
	Cancelled EntryStatus = "cancelled"
)

type Entry struct {
	ID, SessionID, StudentID, TicketNumber, Position int64
	StudentName                                      string
	Status                                           EntryStatus
	CreatedAt                                        time.Time
}

type CreateSession struct {
	SubjectID, GroupID int64
	LabTitle           string
	StartsAt, EndsAt   time.Time
}
type SessionFilter struct{ SubjectID, GroupID, TeacherID int64 }
type SessionList struct {
	Sessions   []Session
	ObservedAt time.Time
}
type Snapshot struct {
	Session      Session
	Entries      []Entry
	MyEntry      *Entry
	NextPosition int64
	ObservedAt   time.Time
}
type Result struct {
	Session *Session
	Entry   *Entry
}

type Operation string

const (
	Create            Operation = "create"
	Join              Operation = "join"
	CallNext          Operation = "call-next"
	Cancel            Operation = "cancel"
	Complete          Operation = "complete"
	Skip              Operation = "skip"
	CloseRegistration Operation = "close-registration"
	Finish            Operation = "finish"
)

// Mutation identifies an application command. Protocol serialization belongs
// to adapters; no HTTP method, status, fingerprint or JSON is carried here.
type Mutation struct {
	ActorID   int64
	Key       string
	Operation Operation
	SessionID int64
	Create    CreateSession
}

type ErrorKind string

const (
	Invalid     ErrorKind = "invalid"
	Forbidden   ErrorKind = "forbidden"
	Conflict    ErrorKind = "conflict"
	NotFound    ErrorKind = "not-found"
	Unavailable ErrorKind = "unavailable"
)

type Error struct {
	Kind          ErrorKind
	Code, Message string
}

func (e *Error) Error() string { return e.Code }
func Problem(kind ErrorKind, code, message string) error {
	return &Error{Kind: kind, Code: code, Message: message}
}

var ErrNotFound = &Error{Kind: NotFound, Code: "not_found", Message: "Сеанс или запись не найдены"}
