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
// the worst case the stop bound has to survive. entries counts Stop calls
// as they begin, since a stuck one never reaches the embedded counter.
type stuckStopComponent struct {
	fakeComponent
	release chan struct{}

	emu     sync.Mutex
	entries int
}

func newStuckStopComponent(name string) *stuckStopComponent {
	return &stuckStopComponent{fakeComponent: fakeComponent{name: name}, release: make(chan struct{})}
}

func (s *stuckStopComponent) Stop(ctx context.Context) error {
	s.emu.Lock()
	s.entries++
	s.emu.Unlock()
	<-s.release
	return s.fakeComponent.Stop(ctx)
}

func (s *stuckStopComponent) getEntries() int {
	s.emu.Lock()
	defer s.emu.Unlock()
	return s.entries
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
	err := New().stop(context.Background(), &registration{Spec: Spec{Component: c}})
	if !errors.Is(err, errStopTimedOut) {
		t.Fatalf("err = %v, want errStopTimedOut", err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("stop took %s, want about stopTimeout", took)
	}
}

// TestRunDoesNotRestartOverAHungStop: when the Stop before a restart hangs,
// the watcher does not Start the component over it; it escalates, Run still
// returns, and the shutdown waits on the hung Stop instead of calling Stop
// again.
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
	if n := c.getEntries(); n != 1 {
		t.Errorf("Stop entered %d times, want 1 (no second Stop over a hung one)", n)
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
	if n := stuck.getEntries(); n != 1 {
		t.Errorf("stuck Stop entered %d times, want 1", n)
	}
}

// TestStop_FinishedStopIsNotReused: once a Stop returns, the next stop of
// the component calls Stop afresh (a restart's Stop, then the shutdown's).
func TestStop_FinishedStopIsNotReused(t *testing.T) {
	c := &fakeComponent{name: "c"}
	r := &registration{Spec: Spec{Component: c}}
	sup := New()
	for i := 0; i < 2; i++ {
		if err := sup.stop(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.getStopCalls(); n != 2 {
		t.Errorf("Stop called %d times, want 2", n)
	}
}

// sliceComponent is a Component whose concrete type is not comparable,
// which the interface allows.
type sliceComponent []*stuckStopComponent

func (s sliceComponent) Name() string                            { return s[0].Name() }
func (s sliceComponent) Start(ctx context.Context) error         { return s[0].Start(ctx) }
func (s sliceComponent) Stop(ctx context.Context) error          { return s[0].Stop(ctx) }
func (s sliceComponent) Health(ctx context.Context) HealthStatus { return s[0].Health(ctx) }

// TestRun_NonComparableComponent: a component of a non-comparable type
// goes through a hung restart Stop and the shutdown after it without a
// panic, and its Stop is entered once.
func TestRun_NonComparableComponent(t *testing.T) {
	withFastWatcher(t)
	withStopTimeout(t, 50*time.Millisecond)
	inner := newStuckStopComponent("slice")
	t.Cleanup(func() { close(inner.release) })
	sup := New()
	sup.Register(sliceComponent{inner}, RestartOnExit)

	runDone := make(chan error, 1)
	go func() { runDone <- sup.Run(context.Background()) }()

	waitFor(t, time.Second, "initial start", func() bool { return inner.getStartCalls() == 1 })
	inner.setHealthy(false)

	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	if n := inner.getEntries(); n != 1 {
		t.Errorf("Stop entered %d times, want 1", n)
	}
}
