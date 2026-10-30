package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDecodeBody: the one decode path bounds the body, rejects unknown fields
// and trailing data, and tells a required body from an optional one (adr/0219).
func TestDecodeBody(t *testing.T) {
	type dst struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name     string
		body     string
		optional bool
		wantOK   bool
		status   int
		wantName string
	}{
		{name: "valid", body: `{"name":"a"}`, wantOK: true, wantName: "a"},
		{name: "valid optional", body: `{"name":"a"}`, optional: true, wantOK: true, wantName: "a"},
		{name: "empty required", body: ``, status: http.StatusBadRequest},
		{name: "empty optional", body: ``, optional: true, wantOK: true},
		{name: "whitespace optional", body: " \n", optional: true, wantOK: true},
		{name: "malformed", body: `{"name":`, status: http.StatusBadRequest},
		{name: "malformed optional", body: `{"name":`, optional: true, status: http.StatusBadRequest},
		{name: "unknown field", body: `{"title":"a"}`, status: http.StatusBadRequest},
		{name: "unknown field optional", body: `{"title":"a"}`, optional: true, status: http.StatusBadRequest},
		{name: "trailing value", body: `{"name":"a"}{"name":"b"}`, status: http.StatusBadRequest},
		{name: "trailing garbage", body: `{"name":"a"} x`, status: http.StatusBadRequest},
		{name: "trailing whitespace", body: "{\"name\":\"a\"}\n", wantOK: true, wantName: "a"},
		{name: "over the limit", body: `{"name":"` + strings.Repeat("x", 64) + `"}`, status: http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(c.body))
			w := httptest.NewRecorder()
			var got dst
			var ok bool
			if c.optional {
				ok = decodeOptionalBody(w, r, &got, 32)
			} else {
				ok = decodeBody(w, r, &got, 32)
			}
			if ok != c.wantOK {
				t.Fatalf("ok = %v; want %v (response %d %s)", ok, c.wantOK, w.Code, w.Body.String())
			}
			if !ok && w.Code != c.status {
				t.Errorf("status = %d; want %d (%s)", w.Code, c.status, w.Body.String())
			}
			if ok && got.Name != c.wantName {
				t.Errorf("name = %q; want %q", got.Name, c.wantName)
			}
		})
	}
}

// TestServerTimeouts: the listener carries read, write and idle timeouts, and
// the write timeout outlasts the agent client's 60s request budget.
func TestServerTimeouts(t *testing.T) {
	srv := newHTTPServer("localhost:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("timeouts = header %s read %s write %s idle %s; want all set",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout <= 60*time.Second {
		t.Errorf("WriteTimeout = %s; want above the agent client's 60s", srv.WriteTimeout)
	}
}
