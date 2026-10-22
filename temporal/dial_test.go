package temporal

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shrinkDialTiming makes the retry loop fast enough for a unit test and
// restores production values at cleanup.
func shrinkDialTiming(t *testing.T) {
	t.Helper()
	window, attempt, interval := dialReadyWindow, dialAttemptTimeout, dialRetryInterval
	dialReadyWindow, dialAttemptTimeout, dialRetryInterval = 2*time.Second, 200*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { dialReadyWindow, dialAttemptTimeout, dialRetryInterval = window, attempt, interval })
}

// TestDial_RetriesWhileFrontendStalls: a listener that accepts TCP but never
// speaks gRPC looks exactly like the stall that took the stack down — a plain
// TCP health probe passes while GetSystemInfo times out. Dial must keep
// trying until the caller's deadline instead of failing on the first attempt.
// Fast — no dev server.
func TestDial_RetriesWhileFrontendStalls(t *testing.T) {
	shrinkDialTiming(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			defer conn.Close() // hold it open, silently
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	start := time.Now()
	c, err := Dial(ctx, ln.Addr().String(), "")
	if err == nil {
		c.Close()
		t.Fatal("Dial succeeded against a listener that never speaks gRPC")
	}
	if !strings.Contains(err.Error(), "gave up after") {
		t.Errorf("error should report the retry budget was exhausted; got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Errorf("Dial gave up after %s — before the caller's deadline", elapsed)
	}
	if n := accepted.Load(); n < 2 {
		t.Errorf("expected ≥2 connection attempts before giving up; saw %d", n)
	}
}

// TestDial_BoundedWithoutCallerDeadline: the supervisor's Run context has no
// deadline, so Dial must impose its own window rather than spin forever
// against an address nothing listens on. Fast — no dev server.
func TestDial_BoundedWithoutCallerDeadline(t *testing.T) {
	shrinkDialTiming(t)
	dialReadyWindow = 500 * time.Millisecond

	start := time.Now()
	c, err := Dial(context.Background(), "127.0.0.1:1", "default")
	if err == nil {
		c.Close()
		t.Fatal("Dial succeeded against a closed port")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Dial without a caller deadline ran %s; the internal window should have bounded it", elapsed)
	}
}
