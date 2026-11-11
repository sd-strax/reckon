package adapterplugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
)

// echoBin is the compiled reckon-adapter-echo, built once for the whole package.
var echoBin string

func TestMain(m *testing.M) {
	if slices.Contains(os.Args[1:], fakeIgnoreEOF) {
		runFakeIgnoringEOF()
		return
	}
	dir, err := os.MkdirTemp("", "adapterplugin-echo")
	if err != nil {
		panic(err)
	}
	echoBin = filepath.Join(dir, "reckon-adapter-echo")
	build := exec.Command("go", "build", "-o", echoBin, "github.com/sd-strax/reckon/cmd/reckon-adapter-echo")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build echo adapter: " + err.Error())
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// silentLogger keeps adapter stderr and lifecycle noise out of test output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newEchoPlugin builds a Plugin backed by the compiled echo adapter. Extra opts
// append after the silent logger.
func newEchoPlugin(t *testing.T, config map[string]any, opts ...Option) *Plugin {
	t.Helper()
	man := Manifest{
		ManifestVersion:  1,
		Name:             "echo",
		Version:          "0.0.1",
		ProtocolVersions: []int{1},
		Class:            "CUSTOM",
		Exec:             []string{echoBin},
	}
	installed := Installed{Dir: filepath.Dir(echoBin), Manifest: man}
	opts = append([]Option{WithLogger(silentLogger())}, opts...)
	p, err := New("echo", installed, config, "test-engine", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestHandshakeAndDescribe(t *testing.T) {
	p := newEchoPlugin(t, nil)
	desc, err := p.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Verbs) != 1 || desc.Verbs[0].Verb != "get_host_context" {
		t.Fatalf("verbs = %+v, want get_host_context", desc.Verbs)
	}
	if len(desc.ActionTypes) != 1 || desc.ActionTypes[0].ActionType != "host.isolate" {
		t.Fatalf("action_types = %+v, want host.isolate", desc.ActionTypes)
	}
	if len(desc.Operations) != 2 {
		t.Fatalf("operations = %+v, want 2", desc.Operations)
	}
}

func TestInvokeEchoesOCSF(t *testing.T) {
	p := newEchoPlugin(t, nil)
	resp, err := p.Invoke(context.Background(), "get_host_context", map[string]any{"host": "WIN-1"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.SourceTool != "echo:get_host_context" {
		t.Fatalf("source_tool = %q", resp.SourceTool)
	}
	if len(resp.Events) != 1 || resp.Events[0].ClassUID != 3002 {
		t.Fatalf("events = %+v, want one class 3002", resp.Events)
	}
}

func TestInvokeErrorClassified(t *testing.T) {
	p := newEchoPlugin(t, nil)
	_, err := p.Invoke(context.Background(), "boom_fallthrough", nil)
	var ae *capability.AdapterError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *capability.AdapterError", err)
	}
	if ae.Class != capability.ErrFallthrough {
		t.Fatalf("class = %q, want FALLTHROUGH", ae.Class)
	}
}

func TestDispatchSucceeds(t *testing.T) {
	p := newEchoPlugin(t, nil)
	res, err := p.Dispatch(context.Background(), "host.isolate", map[string]any{"device": "abc"}, "idem-1")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.FinalOutcome != action.OutcomeSucceeded {
		t.Fatalf("outcome = %q, want SUCCEEDED", res.FinalOutcome)
	}
	if res.AdapterRequestID != "echo-idem-1" {
		t.Fatalf("adapter_request_id = %q", res.AdapterRequestID)
	}
}

func TestDispatchErrorClassified(t *testing.T) {
	p := newEchoPlugin(t, nil)
	_, err := p.Dispatch(context.Background(), "boom_fatal", nil, "idem-2")
	var we *action.WriteError
	if !errors.As(err, &we) {
		t.Fatalf("err = %v, want *action.WriteError", err)
	}
	if we.Class != action.WriteFatal {
		t.Fatalf("class = %q, want FATAL_ERROR", we.Class)
	}
}

func TestHealthReady(t *testing.T) {
	p := newEchoPlugin(t, nil)
	if h := p.Health(); !h.Healthy {
		t.Fatalf("health = %+v, want healthy", h)
	}
}

// TestHealthDegraded: the health result's degraded list is mapped onto
// capability.HealthStatus.Degraded; a healthy result without it maps to nil.
func TestHealthDegraded(t *testing.T) {
	p := newEchoPlugin(t, map[string]any{"degraded": true})
	h := p.Health()
	want := []capability.DegradedCapability{{Operation: "get_host_context", Capability: "host filter", Reason: "degraded by test config"}}
	if !h.Healthy || len(h.Degraded) != 1 || h.Degraded[0] != want[0] {
		t.Fatalf("health = %+v, want healthy with %+v", h, want)
	}
	if h := newEchoPlugin(t, nil).Health(); h.Degraded != nil {
		t.Fatalf("health degraded = %+v, want nil", h.Degraded)
	}
}

// TestHealthDegradedMalformed: a degraded value that is not a list of entries
// is dropped — the instance stays healthy — and ProbeHealth reports it.
func TestHealthDegradedMalformed(t *testing.T) {
	for name, v := range map[string]any{"string": "none", "object": map[string]any{}} {
		t.Run(name, func(t *testing.T) {
			p := newEchoPlugin(t, map[string]any{"degraded_value": v})
			if h := p.Health(); !h.Healthy || h.Degraded != nil {
				t.Fatalf("health = %+v, want healthy with nothing degraded", h)
			}
			h, err := p.ProbeHealth()
			if err == nil || !h.Healthy || h.Degraded != nil {
				t.Fatalf("ProbeHealth = %+v, %v; want healthy, nothing degraded, and an error", h, err)
			}
		})
	}
}

// warnCounter counts warn-level records.
type warnCounter struct {
	mu    sync.Mutex
	warns []string
}

func (w *warnCounter) Enabled(context.Context, slog.Level) bool { return true }
func (w *warnCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		w.mu.Lock()
		w.warns = append(w.warns, r.Message)
		w.mu.Unlock()
	}
	return nil
}
func (w *warnCounter) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnCounter) WithGroup(string) slog.Handler      { return w }

func (w *warnCounter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.warns)
}

// TestHealthDegradedWarnLatch: a persistent malformed degraded list is warned
// about once, not on every probe; a probe the adapter did not answer (here an
// instance latched unhealthy) leaves the latch, so the same problem after it
// is not warned again; a different problem, or the same one after a clean
// decode, is.
func TestHealthDegradedWarnLatch(t *testing.T) {
	w := &warnCounter{}
	p := newEchoPlugin(t, map[string]any{"degraded_value": "none"}, WithLogger(slog.New(w)))
	for range 3 {
		p.Health()
	}
	if n := w.count(); n != 1 {
		t.Fatalf("warns after three probes = %d; want 1", n)
	}

	// An unanswered probe: stop the process and latch the instance unhealthy.
	p.Close()
	p.mu.Lock()
	p.unhealthy, p.unhealthyMsg = true, "test latch"
	p.mu.Unlock()
	if h := p.Health(); h.Healthy {
		t.Fatalf("health with the latch = %+v; want unhealthy", h)
	}
	p.mu.Lock()
	p.unhealthy, p.unhealthyMsg = false, ""
	p.mu.Unlock()
	if h := p.Health(); !h.Healthy {
		t.Fatalf("health after the latch = %+v; want healthy (respawned)", h)
	}
	if n := w.count(); n != 1 {
		t.Fatalf("warns after an unanswered probe and the same problem = %d; want 1", n)
	}

	p.noteDegradedErr(errors.New("another problem"))
	p.noteDegradedErr(errors.New("another problem"))
	if n := w.count(); n != 2 {
		t.Fatalf("warns after a changed problem = %d; want 2", n)
	}
	p.noteDegradedErr(nil)
	p.noteDegradedErr(errors.New("another problem"))
	if n := w.count(); n != 3 {
		t.Fatalf("warns after a clean decode and the problem again = %d; want 3", n)
	}
}

// TestDescribeSchemaSource: describe's schema_source is decoded.
func TestDescribeSchemaSource(t *testing.T) {
	desc, err := newEchoPlugin(t, nil).Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if desc.SchemaSource == nil || desc.SchemaSource.Kind != SchemaSourceResponse {
		t.Fatalf("schema_source = %+v, want kind response", desc.SchemaSource)
	}
}

func TestConfigureRejectionIsUnhealthy(t *testing.T) {
	p := newEchoPlugin(t, map[string]any{"reject": true})
	// A rejected configure fails the handshake → the adapter is UNHEALTHY, and
	// an invoke against it surfaces that rather than dispatching.
	_, err := p.Invoke(context.Background(), "get_host_context", nil)
	var ae *capability.AdapterError
	if !errors.As(err, &ae) || ae.Class != capability.ErrUnhealthy {
		t.Fatalf("err = %v, want UNHEALTHY AdapterError", err)
	}
	if h := p.Health(); h.Healthy {
		t.Fatalf("health = %+v, want unhealthy", h)
	}
}

func TestCrashRespawns(t *testing.T) {
	p := newEchoPlugin(t, nil)
	// Prime the process.
	if _, err := p.Invoke(context.Background(), "get_host_context", nil); err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	// Crash it: the adapter exits without replying, so this call errors.
	if _, err := p.Invoke(context.Background(), "crash", nil); err == nil {
		t.Fatal("crash invoke: want error, got nil")
	}
	// Give the read loop a moment to observe EOF and close `done`.
	waitProcExit(t, p)
	// The next call respawns within budget and succeeds.
	if _, err := p.Invoke(context.Background(), "get_host_context", nil); err != nil {
		t.Fatalf("post-crash invoke: %v", err)
	}
}

func TestRestartBudgetExhausts(t *testing.T) {
	p := newEchoPlugin(t, nil, WithRestartBudget(1, time.Minute))
	if _, err := p.Invoke(context.Background(), "get_host_context", nil); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// First crash: one respawn is allowed.
	_, _ = p.Invoke(context.Background(), "crash", nil)
	waitProcExit(t, p)
	if _, err := p.Invoke(context.Background(), "get_host_context", nil); err != nil {
		t.Fatalf("respawn 1: %v", err)
	}
	// Second crash: budget (1) is now spent — the next demand refuses.
	_, _ = p.Invoke(context.Background(), "crash", nil)
	waitProcExit(t, p)
	_, err := p.Invoke(context.Background(), "get_host_context", nil)
	var ae *capability.AdapterError
	if !errors.As(err, &ae) || ae.Class != capability.ErrUnhealthy {
		t.Fatalf("err = %v, want UNHEALTHY after budget exhaustion", err)
	}
}

// waitProcExit blocks briefly until the plugin's current process read loop has
// observed the crash, so the subsequent ensure sees a dead proc deterministically.
func waitProcExit(t *testing.T, p *Plugin) {
	t.Helper()
	p.mu.Lock()
	proc := p.proc
	p.mu.Unlock()
	if proc == nil {
		return
	}
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for adapter process to exit")
	}
}
