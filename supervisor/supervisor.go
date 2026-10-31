package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Component is one managed dependency or in-process service.
// Implementations must be safe to Start once, Stop once, and accept
// concurrent Health calls.
type Component interface {
	// Name is a stable identifier used for logging and status reporting.
	Name() string

	// Start the component. Returns nil only when the component is ready
	// to serve. Returns an error if startup fails or ctx is cancelled.
	Start(ctx context.Context) error

	// Stop the component gracefully. Should complete within ctx's deadline.
	Stop(ctx context.Context) error

	// Health returns the current readiness/status of the component.
	// Must be safe to call concurrently with Start/Stop.
	Health(ctx context.Context) HealthStatus
}

// HealthStatus is the runtime state of a Component.
type HealthStatus struct {
	Ready   bool
	Message string
}

// RestartPolicy tells the supervisor how to react when a Component exits
// unexpectedly (not via a Stop call). The per-component watcher goroutine
// started by Run acts on it.
type RestartPolicy int

const (
	// RestartOnExit attempts to restart the Component up to its restart
	// budget within a sliding window before escalating.
	RestartOnExit RestartPolicy = iota

	// FatalOnExit halts the entire supervisor when this Component exits
	// unexpectedly. Used for components where an unclean exit risks data
	// corruption (Postgres mid-transaction is the canonical case).
	FatalOnExit
)

// Spec wraps a Component with its supervision policy.
type Spec struct {
	Component
	Policy RestartPolicy

	// MaxRestarts is the per-component restart budget within the sliding
	// window before the watcher escalates to fatal. 0 means the package
	// default (watchMaxRestarts).
	MaxRestarts int
}

// registration is one RegisterSpec call: the Spec plus the supervisor's own
// state for it. The supervisor tracks a component through its registration,
// held by pointer, so nothing depends on the component's own type being
// comparable.
type registration struct {
	Spec

	// stopping is the Stop in flight, nil when none; guarded by
	// Supervisor.mu.
	stopping *stopCall
}

// Supervisor manages the lifecycle of a set of Components.
type Supervisor struct {
	specs   []*registration
	mu      sync.Mutex
	started []*registration // for stop ordering

	// OnReady, when set, is called by Run exactly once, after Start has
	// brought every registered component up and before the health watchers
	// take over. It is the supervisor's "the whole stack is serving" signal
	// — the one moment it alone can name, since a component's own ready log
	// says nothing about the ones after it. Callers use it for the
	// operator-facing ready line. Set it before Run; it is not called when
	// Start fails.
	OnReady func()
}

// New returns a fresh, empty Supervisor.
func New() *Supervisor {
	return &Supervisor{}
}

// Register adds a Component with the default restart budget. Components
// start in the order they are registered and stop in reverse order.
func (s *Supervisor) Register(c Component, policy RestartPolicy) {
	s.RegisterSpec(Spec{Component: c, Policy: policy})
}

// RegisterSpec adds a Component with an explicit supervision Spec, for
// callers that need a non-default restart budget.
func (s *Supervisor) RegisterSpec(spec Spec) {
	s.specs = append(s.specs, &registration{Spec: spec})
}

// Start every registered component in order. On the first failure,
// already-started components are stopped in reverse order before the
// error is returned.
func (s *Supervisor) Start(ctx context.Context) error {
	for _, spec := range s.specs {
		log.Printf("supervisor: starting %s", spec.Name())
		if err := spec.Start(ctx); err != nil {
			log.Printf("supervisor: %s failed to start: %v", spec.Name(), err)
			_ = s.shutdown(ctx)
			return fmt.Errorf("start %s: %w", spec.Name(), err)
		}
		s.mu.Lock()
		s.started = append(s.started, spec)
		s.mu.Unlock()
		log.Printf("supervisor: %s ready", spec.Name())
	}
	return nil
}

// Stop every started component in reverse order. Best-effort: stop errors
// are logged but don't prevent the next component from being stopped. Each
// component's Stop is waited on for at most stopTimeout, so one that hangs
// does not keep the rest from stopping. The first error encountered (if any)
// is returned.
func (s *Supervisor) Stop(ctx context.Context) error {
	return s.shutdown(ctx)
}

func (s *Supervisor) shutdown(ctx context.Context) error {
	s.mu.Lock()
	started := s.started
	s.started = nil
	s.mu.Unlock()

	var firstErr error
	for i := len(started) - 1; i >= 0; i-- {
		c := started[i]
		log.Printf("supervisor: stopping %s", c.Name())
		if err := s.stop(ctx, c); err != nil {
			log.Printf("supervisor: %s stop error: %v", c.Name(), err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			log.Printf("supervisor: %s stopped", c.Name())
		}
	}
	return firstErr
}

// Health rolls up the current status of every started Component. The probes
// run at once, each under probeTimeout, so one hung probe neither stalls the
// rollup nor hides the others.
func (s *Supervisor) Health(ctx context.Context) map[string]HealthStatus {
	s.mu.Lock()
	started := append([]*registration(nil), s.started...)
	s.mu.Unlock()

	results := make([]HealthStatus, len(started))
	var wg sync.WaitGroup
	for i, c := range started {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = probe(ctx, c.Component)
		}()
	}
	wg.Wait()

	out := make(map[string]HealthStatus, len(started))
	for i, c := range started {
		out[c.Name()] = results[i]
	}
	return out
}

// probe runs one Health call under probeTimeout. A probe that has not
// answered by then is a miss, even if it ignores its context: the caller
// moves on and the late answer is dropped.
func probe(ctx context.Context, c Component) HealthStatus {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	done := make(chan HealthStatus, 1)
	go func() { done <- c.Health(pctx) }()
	select {
	case h := <-done:
		return h
	case <-pctx.Done():
		return HealthStatus{Ready: false, Message: fmt.Sprintf("health probe did not answer within %s", probeTimeout)}
	}
}

// errStopTimedOut marks a Stop that had not returned by stopTimeout.
var errStopTimedOut = errors.New("stop did not return")

// stopCall is one Stop in flight; err is set before done is closed.
type stopCall struct {
	done chan struct{}
	err  error
}

// stop calls r's Stop under stopTimeout and waits no longer than that, even
// for a Stop that ignores its context. The bound runs from now whether or
// not ctx is already cancelled: a shutdown still gives each component its
// time to stop. A Stop that times out is left running, so the caller must
// not Start r over it, and a later stop of r waits on that same call rather
// than calling Stop a second time.
func (s *Supervisor) stop(ctx context.Context, r *registration) error {
	s.mu.Lock()
	call := r.stopping
	if call == nil {
		call = &stopCall{done: make(chan struct{})}
		r.stopping = call
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
		go func() {
			defer cancel()
			call.err = r.Stop(sctx)
			s.mu.Lock()
			r.stopping = nil
			s.mu.Unlock()
			close(call.done)
		}()
	}
	s.mu.Unlock()

	timer := time.NewTimer(stopTimeout)
	defer timer.Stop()
	select {
	case <-call.done:
		return call.err
	case <-timer.C:
		return fmt.Errorf("%w within %s", errStopTimedOut, stopTimeout)
	}
}

// Watcher tuning. Conservative defaults: 5s polling, 2 consecutive misses
// before declaring exit (so a single transient blip doesn't trigger a
// restart cascade), 3 restarts in a rolling 5-minute window before escalating
// to fatal. Aggressive enough to catch real outages, lenient enough not to
// fight with normal startup jitter.
//
// probeTimeout bounds each Health call; stopTimeout bounds each Stop, the
// one before a restart's Start and each one in a shutdown.
//
// These are vars (not consts) so tests can override them via t.Cleanup.
var (
	probeTimeout           = 3 * time.Second
	stopTimeout            = 30 * time.Second
	watchInterval          = 5 * time.Second
	watchConsecutiveMisses = 2
	watchMaxRestarts       = 3
	watchRestartWindow     = 5 * time.Minute
)

// Run starts every registered component, then supervises them: each
// component is polled for health and restarted (or escalated to fatal-exit)
// per its RestartPolicy. Run blocks until ctx is cancelled OR a FatalOnExit
// component fails OR a RestartOnExit component exhausts its restart budget.
// Then Run stops every component and returns.
//
// This is the typical entry point for a long-running supervisor invocation.
// Calling Start + Stop directly remains valid for tests or for tighter
// external control of the lifecycle.
func (s *Supervisor) Run(ctx context.Context) error {
	began := time.Now()
	if err := s.Start(ctx); err != nil {
		return err
	}
	log.Printf("supervisor: all %d components ready in %s", len(s.specs), time.Since(began).Round(100*time.Millisecond))
	if s.OnReady != nil {
		s.OnReady()
	}

	runCtx, fatalCancel := context.WithCancel(ctx)
	defer fatalCancel()

	var wg sync.WaitGroup
	for _, spec := range s.specs {
		wg.Add(1)
		go func(spec *registration) {
			defer wg.Done()
			s.watch(runCtx, fatalCancel, spec)
		}(spec)
	}

	<-runCtx.Done()
	log.Println("supervisor: run cancelled; awaiting watchers")
	wg.Wait()

	if err := s.Stop(context.Background()); err != nil {
		return fmt.Errorf("supervisor stop: %w", err)
	}
	return nil
}

// watch polls a single Component for health on watchInterval. Two consecutive
// misses count as "unexpected exit"; on exit, the Component's RestartPolicy
// dictates whether the supervisor restarts it or escalates to fatal-cancel.
//
// Exits early on ctx cancellation (i.e., when Run is shutting down) — the
// caller's wg.Wait then knows it's safe to call Stop without races.
func (s *Supervisor) watch(ctx context.Context, fatalCancel context.CancelFunc, spec *registration) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	maxRestarts := spec.MaxRestarts
	if maxRestarts == 0 {
		maxRestarts = watchMaxRestarts
	}

	consecutiveMisses := 0
	var restartTimes []time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		h := probe(ctx, spec.Component)
		if h.Ready {
			consecutiveMisses = 0
			continue
		}
		consecutiveMisses++
		if consecutiveMisses < watchConsecutiveMisses {
			continue
		}

		log.Printf("supervisor: %s unhealthy after %d consecutive misses: %s",
			spec.Name(), consecutiveMisses, h.Message)

		switch spec.Policy {
		case FatalOnExit:
			log.Printf("supervisor: %s policy is FatalOnExit — terminating supervisor",
				spec.Name())
			fatalCancel()
			return
		case RestartOnExit:
			restartTimes = trimRestartHistory(restartTimes)
			if len(restartTimes) >= maxRestarts {
				log.Printf("supervisor: %s restarted %d times within %s — escalating to fatal",
					spec.Name(), maxRestarts, watchRestartWindow)
				fatalCancel()
				return
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("supervisor: restarting %s (attempt %d of %d in the last %s)",
				spec.Name(), len(restartTimes)+1, maxRestarts, watchRestartWindow)
			if err := s.stop(ctx, spec); err != nil {
				if errors.Is(err, errStopTimedOut) {
					// The old Stop may still be changing the component;
					// a Start now would race it.
					log.Printf("supervisor: %s %v; not restarting over it — escalating to fatal",
						spec.Name(), err)
					fatalCancel()
					return
				}
				// Start may still succeed (a process already gone); if it
				// fails, this line carries the cause.
				log.Printf("supervisor: %s stop before restart: %v", spec.Name(), err)
			}
			if ctx.Err() != nil {
				return
			}
			if err := spec.Start(ctx); err != nil {
				log.Printf("supervisor: %s restart failed: %v — escalating to fatal",
					spec.Name(), err)
				fatalCancel()
				return
			}
			restartTimes = append(restartTimes, time.Now())
			consecutiveMisses = 0
		}
	}
}

func trimRestartHistory(history []time.Time) []time.Time {
	cutoff := time.Now().Add(-watchRestartWindow)
	out := history[:0]
	for _, t := range history {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}
