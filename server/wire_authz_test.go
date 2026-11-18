package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sd-strax/reckon/authz"
)

// TestWireAuthzBodies holds the bodies authz writes to the registered error
// bodies (adr/0278). Each is decoded refusing unknown keys, and must give back
// every key, so a renamed key in authz fails here.
func TestWireAuthzBodies(t *testing.T) {
	never := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("the request was let through") })
	run := func(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	t.Run("a 401", func(t *testing.T) {
		w := run(authz.RequireAuth(nil)(never), httptest.NewRequest(http.MethodGet, "/", nil))
		var got ErrorResponse
		strictDecode(t, w, http.StatusUnauthorized, &got)
		if got.Error == "" {
			t.Errorf("error is empty: %s", w.Body)
		}
	})
	t.Run("a 403", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(authz.WithClaims(r.Context(), authz.Claims{Subject: "u-1"}))
		w := run(authz.RequireRole("analyst")(never), r)
		var got ForbiddenResponse
		strictDecode(t, w, http.StatusForbidden, &got)
		if got.Error == "" || !got.RequiredAny || len(got.RequiredRoles) != 1 {
			t.Errorf("a key did not decode: %s", w.Body)
		}
	})
}

// strictDecode reads a recorded body into dst, refusing a key dst lacks.
func strictDecode(t *testing.T, w *httptest.ResponseRecorder, status int, dst any) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d", w.Code, status)
	}
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
}
