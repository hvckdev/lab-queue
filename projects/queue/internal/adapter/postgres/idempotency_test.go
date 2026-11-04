package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"labqueue/projects/queue/internal/adapter/legacyhttp"
	"labqueue/projects/queue/internal/model"
)

func TestFingerprintRetainsOriginalPayloadOrderAndRequestPath(t *testing.T) {
	m := model.Mutation{Operation: model.Create, Create: model.CreateSession{SubjectID: 1, GroupID: 2, LabTitle: "ЛР №1", StartsAt: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)}}
	want := sha256.Sum256([]byte("POST /api/sessions\n" + `{"subject_id":1,"group_id":2,"lab_title":"ЛР №1","starts_at":"2026-10-29T09:00:00Z","ends_at":"2026-10-29T10:00:00Z"}`))
	got, err := fingerprint(context.Background(), m)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("fingerprint=%s err=%v", got, err)
	}
	path := "/api/sessions/0002/entries"
	m = model.Mutation{Operation: model.Join, SessionID: 2}
	want = sha256.Sum256([]byte("POST " + path + "\n{}"))
	boundary := legacyhttp.Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := fingerprint(r.Context(), m)
		if err != nil || got != hex.EncodeToString(want[:]) {
			t.Fatalf("exact legacy request path was not retained: %s %v", got, err)
		}
	}))
	boundary.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
}
