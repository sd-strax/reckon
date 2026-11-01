package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sd-strax/reckon/aggregate"
)

// writeJSON writes a JSON response with the given status. Every handler's JSON
// response goes through here (or writeJSONError) so the Content-Type is set
// uniformly — a bare Encode after WriteHeader gets content-sniffed to
// text/plain by net/http.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a stable error JSON shape for all non-2xx responses.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// writeInternalError answers a server-side failure with a 500 that names what
// failed but not why (adr/0220): the error text can carry table, constraint
// and connection details the client has no use for. The cause is logged with a
// short reference, and the same reference rides the response so a reported
// failure can be found in the log.
func writeInternalError(w http.ResponseWriter, what string, err error) {
	ref := newErrorRef()
	slog.Error("internal error", "what", what, "ref", ref, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": what + ": internal error (ref " + ref + ")",
		"ref":   ref,
	})
}

// newErrorRef returns a short random reference for one internal error.
func newErrorRef() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// writeCommandError maps an aggregate.Handler.Handle failure onto the HTTP
// outcome for every /api write path, so a domain rejection, a permission
// denial, a lost OCC race, and an infrastructure fault are never conflated.
// Order matters: the sentinels (which arrive wrapped in a *RejectedError) are
// matched before the generic rejection, and infrastructure failures — which
// Handle returns unwrapped — fall through to 500. The action label prefixes the
// message ("conclude", "request action", …). A nil err is a caller bug, treated
// as 500.
func writeCommandError(w http.ResponseWriter, action string, err error) {
	var rejected *aggregate.RejectedError
	switch {
	case errors.Is(err, aggregate.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, action+": investigation not found")
	case errors.Is(err, aggregate.ErrAIDenied):
		writeJSONError(w, http.StatusForbidden, action+": "+err.Error())
	case errors.Is(err, aggregate.ErrConcurrent):
		writeJSONError(w, http.StatusConflict, action+": "+err.Error())
	case errors.As(err, &rejected):
		writeJSONError(w, http.StatusUnprocessableEntity, action+": "+err.Error())
	default:
		writeInternalError(w, action, err)
	}
}
