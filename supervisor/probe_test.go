package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// hungComponent's Health ignores its context and blocks while hung — the
// worst case a probe deadline has to survive. Start clears the hang.
type hungComponent struct {
	fakeComponent

	hmu     sync.Mutex
	hung    bool
	release chan struct{}
}

func newHungComponent(name string) *hungComponent {
	return &hungComponent{fakeComponent: fakeComponent{name: name}, release: make(chan struct{})}
}

func (h *hungComponent) Start(ctx context.Context) error {
	h.hmu.Lock()
	h.hung = false
	h.hmu.Unlock()
	return h.fakeComponent.Start(ctx)
}

func (h *hungComponent) Health(ctx context.Context) HealthStatus {
	h.hmu.Lock()
	hung := h.hung
	h.hmu.Unlock()
	if hung {
		<-h.release
	}
	return h.fakeComponent.Health(ctx)
}

func (h *hungComponent) setHung() {
	h.hmu.Lock()
	h.hung = true
	h.hmu.Unlock()
}

func withProbeTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := probeTimeout
	probeTimeout = d
	t.Cleanup(func() { probeTimeout = orig })
}

// TestProbe_HungHealthIsAMiss: a Health call that never answers is reported
// not-ready once probeTimeout passes, even though it ignores its context.
func TestProbe_HungHealthIsAMiss(t *testing.T) {
	withProbeTimeout(t, 50*time.Millisecond)
	c := newHungComponent("hung")
	t.Cleanup(func() { close(c.release) })
	c.setHung()

	began := time.Now()
	h := probe(context.Background(), c)
	if h.Ready {
		t.Fatal("a hung probe reported ready")
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("probe took %s, want about probeTimeout", took)
	}
}

// TestHealth_HungProbeDoesNotHideOthers: the rollup answers within one probe
// deadline and still reports the healthy components.
func TestHealth_HungProbeDoesNotHideOthers(t *testing.T) {
	withProbeTimeout(t, 50*time.Millisecond)
	ok := &fakeComponent{name: "ok"}
	hung := newHungComponent("hung")
	t.Cleanup(func() { close(hung.release) })
	sup := New()
	sup.Register(ok, RestartOnExit)
	sup.Register(hung, RestartOnExit)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	hung.setHung()

	began := time.Now()
	got := sup.Health(context.Background())
	if took := time.Since(began); took > time.Second {
		t.Errorf("rollup took %s, want about one probeTimeout", took)
	}
	if !got["ok"].Ready {
		t.Error("healthy component not reported ready")
	}
	if got["hung"].Ready {
		t.Error("hung component reported ready")
	}
}

// TestRunRestartsAHungComponent: a component whose probe hangs is declared
// down and restarted, rather than stalling its watcher forever.
func TestRunRestartsAHungComponent(t *testing.T) {
	withFastWatcher(t)
	withProbeTimeout(t, 50*time.Millisecond)
	c := newHungComponent("hung")
	t.Cleanup(func() { close(c.release) })
	sup := New()
	sup.Register(c, RestartOnExit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- sup.Run(ctx) }()

	waitFor(t, time.Second, "initial start", func() bool { return c.getStartCalls() == 1 })
	c.setHung()
	waitFor(t, 2*time.Second, "restart issued", func() bool {
		return c.getStartCalls() == 2 && c.getStopCalls() == 1
	})

	cancel()
	<-runDone
}

// stuckStopComponent's Stop ignores its context and blocks until release,
// the worst case the stop bound has to survive.
type stuckStopComponent struct {
	fakeComponent
	release chan struct{}
}

func newStuckStopComponent(name string) *stuckStopComponent {
	return &stuckStopComponent{fakeComponent: fakeComponent{name: name}, release: make(chan struct{})}
}

func (s *stuckStopComponent) Stop(ctx context.Context) error {
	<-s.release
	return s.fakeComponent.Stop(ctx)
}

func withStopTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := stopTimeout
	stopTimeout = d
	t.Cleanup(func() { stopTimeout = orig })
}

// TestStop_HungStopReturnsAtTheBound: a Stop that never returns is given up
// on once stopTimeout passes, even though it ignores its context.
func TestStop_HungStopReturnsAtTheBound(t *testing.T) {
	withStopTimeout(t, 50*time.Millisecond)
	c := newStuckStopComponent("stuck")
	t.Cleanup(func() { close(c.release) })

	began := time.Now()
	err := stop(context.Background(), c)
	if !errors.Is(err, errStopTimedOut) {
		t.Fatalf("err = %v, want errStopTimedOut", err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("stop took %s, want about stopTimeout", took)
	}
}

// TestRunDoesNotRestartOverAHungStop: when the Stop before a restart hangs,
// the watcher does not Start the component over it; it escalates, and Run
// still returns.
func TestRunDoesNotRestartOverAHungStop(t *testing.T) {
	withFastWatcher(t)
	withStopTimeout(t, 50*time.Millisecond)
	c := newStuckStopComponent("stuck")
	t.Cleanup(func() { close(c.release) })
	sup := New()
	sup.Register(c, RestartOnExit)

	runDone := make(chan error, 1)
	go func() { runDone <- sup.Run(context.Background()) }()

	waitFor(t, time.Second, "initial start", func() bool { return c.getStartCalls() == 1 })
	c.setHealthy(false)

	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after a restart's Stop hung")
	}
	if n := c.getStartCalls(); n != 1 {
		t.Errorf("Start called %d times, want 1 (no Start over a hung Stop)", n)
	}
}

// TestShutdown_HungStopDoesNotBlockTheRest: a component whose Stop hangs
// does not keep the ones before it from being stopped.
func TestShutdown_HungStopDoesNotBlockTheRest(t *testing.T) {
	withStopTimeout(t, 50*time.Millisecond)
	first := &fakeComponent{name: "first"}
	stuck := newStuckStopComponent("stuck")
	t.Cleanup(func() { close(stuck.release) })
	sup := New()
	sup.Register(first, RestartOnExit)
	sup.Register(stuck, RestartOnExit)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	err := sup.Stop(context.Background())
	if !errors.Is(err, errStopTimedOut) {
		t.Errorf("err = %v, want errStopTimedOut", err)
	}
	if n := first.getStopCalls(); n != 1 {
		t.Errorf("first stopped %d times, want 1", n)
	}
}
