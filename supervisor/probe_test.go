package supervisor

import (
	"context"
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
