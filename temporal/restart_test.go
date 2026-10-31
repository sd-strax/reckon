package temporal

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sd-strax/reckon/supervisor"
)

// TestClientsSurviveTemporalRestart: when the supervisor restarts the bundled
// Temporal (Stop + Start on the same port and SQLite file), the worker and a
// dispatch client opened before the restart carry on without being restarted
// themselves — gRPC reconnects to the same address. This is why a Temporal
// restart does not cascade to its dependents (adr/0221).
func TestClientsSurviveTemporalRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs the Temporal dev-server binary; skipped in -short")
	}
	ctx := context.Background()
	port := freePort(t)
	binDir := filepath.Join(os.TempDir(), "reckon-temporal-test-bin")
	temp := supervisor.NewTemporal(supervisor.TemporalConfig{
		DataDir:      t.TempDir(),
		BinDir:       binDir,
		FrontendPort: port,
	})
	if err := temp.Start(ctx); err != nil {
		t.Fatalf("temporal start: %v", err)
	}
	t.Cleanup(func() { _ = temp.Stop(ctx) })
	hostPort := supervisor.TemporalHostPort(port)

	w := NewWorker(WorkerConfig{HostPort: hostPort, Namespace: "default"})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("worker start: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop(ctx) })
	cl, err := NewClient(ClientConfig{HostPort: hostPort, Namespace: "default"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(cl.Close)

	if _, err := cl.Ping(ctx, "before"); err != nil {
		t.Fatalf("ping before restart: %v", err)
	}

	// The supervisor's restart: Stop, then Start on the same config.
	if err := temp.Stop(ctx); err != nil {
		t.Fatalf("temporal stop: %v", err)
	}
	if h := w.Health(ctx); h.Ready {
		t.Errorf("worker reports ready while Temporal is down: %s", h.Message)
	}
	if err := temp.Start(ctx); err != nil {
		t.Fatalf("temporal restart: %v", err)
	}

	// The same worker and client, untouched, serve a round trip.
	pingCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := cl.Ping(pingCtx, "after")
	if err != nil {
		t.Fatalf("ping after restart through the pre-restart client and worker: %v", err)
	}
	if out != "pong:after" {
		t.Errorf("ping = %q, want pong:after", out)
	}
	if h := w.Health(ctx); !h.Ready {
		t.Errorf("worker not ready after Temporal restart: %s", h.Message)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
