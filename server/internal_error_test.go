package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteInternalError: a 500 names what failed and carries a reference,
// never the cause; the cause is logged under the same reference (adr/0220).
func TestWriteInternalError(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cause := errors.New(`pq: duplicate key value violates unique constraint "events_pkey"`)
	w := httptest.NewRecorder()
	writeInternalError(w, "load action", cause)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", w.Code)
	}
	var body struct {
		Error string `json:"error"`
		Ref   string `json:"ref"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if strings.Contains(w.Body.String(), "events_pkey") || strings.Contains(w.Body.String(), "pq:") {
		t.Errorf("response leaks the cause: %s", w.Body.String())
	}
	if body.Ref == "" || body.Error != "load action: internal error (ref "+body.Ref+")" {
		t.Errorf("body = %+v; want the action, a generic message and the ref", body)
	}
	if !strings.Contains(logged.String(), "ref="+body.Ref) || !strings.Contains(logged.String(), "events_pkey") {
		t.Errorf("log = %q; want the cause under ref %s", logged.String(), body.Ref)
	}
}
