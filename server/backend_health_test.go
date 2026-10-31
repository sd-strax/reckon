package server

import (
	"context"
	"net"
	"net/http"
	"testing"
)

// TestBackendHealth_ServeExit: an HTTP server that stops serving on its own
// makes the backend unhealthy, so the supervisor restarts it.
func TestBackendHealth_ServeExit(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{}
	b := &Backend{srv: srv, started: true}
	_ = l.Close() // Serve fails at once on a closed listener
	b.serve(srv, l)

	if h := b.Health(context.Background()); h.Ready {
		t.Fatalf("backend reports ready after its server exited: %+v", h)
	}
}

// TestBackendHealth_ShutdownIsNotAnExit: a server closed by Shutdown (Stop)
// is not recorded as a failure.
func TestBackendHealth_ShutdownIsNotAnExit(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{}
	b := &Backend{srv: srv, started: true}
	_ = srv.Shutdown(context.Background())
	b.serve(srv, l)

	if h := b.Health(context.Background()); !h.Ready {
		t.Fatalf("a shutdown was recorded as an exit: %+v", h)
	}
}
