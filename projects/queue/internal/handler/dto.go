package handler

import (
	"labqueue/projects/queue/internal/model"
	"time"
)

type Session struct {
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

type Entry struct {
	ID           int64     `json:"id"`
	SessionID    int64     `json:"session_id"`
	StudentID    int64     `json:"student_id"`
	StudentName  string    `json:"student_name"`
	TicketNumber int64     `json:"ticket_number"`
	Status       string    `json:"status"`
	Position     int64     `json:"position"`
	CreatedAt    time.Time `json:"created_at"`
}

type createRequest struct {
	SubjectID int64     `json:"subject_id"`
	GroupID   int64     `json:"group_id"`
	LabTitle  string    `json:"lab_title"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
}

func sessionDTO(s model.Session) Session {
	return Session{ID: s.ID, TeacherID: s.TeacherID, SubjectID: s.SubjectID, SubjectName: s.SubjectName, GroupID: s.GroupID, GroupName: s.GroupName, LabTitle: s.LabTitle, StartsAt: s.StartsAt, EndsAt: s.EndsAt, RegistrationClosed: s.RegistrationClosed, Finished: s.Finished, RegistrationOpen: s.RegistrationOpen, ActiveCount: s.ActiveCount}
}
func entryDTO(e model.Entry) Entry {
	return Entry{ID: e.ID, SessionID: e.SessionID, StudentID: e.StudentID, StudentName: e.StudentName, TicketNumber: e.TicketNumber, Status: string(e.Status), Position: e.Position, CreatedAt: e.CreatedAt}
}
func optionalEntry(e *model.Entry) *Entry {
	if e == nil {
		return nil
	}
	entry := entryDTO(*e)
	return &entry
}
