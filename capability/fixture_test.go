package capability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeScenario writes named fixture JSON files into a temp
// <root>/<scenario>/ directory and returns the root.
func writeScenario(t *testing.T, scenario string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, scenario)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// twoLogonsPlusOther is a small scenario: two SUCCESS logons on WIN-FILE01, one
// on a different host, and one process-ancestry fixture for another verb.
var twoLogonsPlusOther = map[string]string{
	"logon-1.json": `{
      "fixture_meta": {"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"WIN-FILE01","outcome":"SUCCESS"}},"delay_ms":0},
      "ocsf": {"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:32:11Z","dst_endpoint":{"hostname":"WIN-FILE01"}}
    }`,
	"logon-2.json": `{
      "fixture_meta": {"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"WIN-FILE01","outcome":"SUCCESS"}},"delay_ms":0},
      "ocsf": {"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:35:02Z","dst_endpoint":{"hostname":"WIN-FILE01"}}
    }`,
	"logon-other.json": `{
      "fixture_meta": {"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"OTHER-HOST","outcome":"SUCCESS"}},"delay_ms":0},
      "ocsf": {"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:33:00Z","dst_endpoint":{"hostname":"OTHER-HOST"}}
    }`,
	"ancestry.json": `{
      "fixture_meta": {"scenario":"s","matches":{"verb":"get_process_ancestry","params":{"process.pid":900}},"delay_ms":0},
      "ocsf": {"class_uid":1007,"class_name":"Process Activity","time":"2026-04-20T14:30:00Z"}
    }`,
	"asset-criticality.json": `{"WIN-FILE01":"high"}`,
}

func newTestFixture(t *testing.T, files map[string]string) *FixtureAdapter {
	root := writeScenario(t, "s", files)
	a := NewFixtureAdapter(root, "s")
	a.SetApplyDelays(false)
	return a
}

// TestFixtureMatchAndOrder: an enumerate_logons replay against WIN-FILE01
// returns exactly the two matching logons, in chronological order, and ignores
// the other-host fixture, the different-verb fixture, and the sidecar file.
func TestFixtureMatchAndOrder(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "enumerate_logons",
		"target":  map[string]any{"hostname": "WIN-FILE01"},
		"outcome": "SUCCESS",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("got %d events; want 2", len(resp.Events))
	}
	if resp.SourceTool != "fixture:s" {
		t.Errorf("source_tool = %q; want fixture:s", resp.SourceTool)
	}
	if !resp.Events[0].Time.Before(resp.Events[1].Time) {
		t.Errorf("events not chronological: %v then %v", resp.Events[0].Time, resp.Events[1].Time)
	}
	for _, e := range resp.Events {
		if e.ClassUID != 3002 {
			t.Errorf("event class_uid = %d; want 3002", e.ClassUID)
		}
	}
}

// TestFixtureDottedNumericParam: a dotted path into nested params with a numeric
// matcher value matches across the JSON number/int boundary.
func TestFixtureDottedNumericParam(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "get_process_ancestry",
		"process": map[string]any{"pid": 900},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.Events) != 1 || resp.Events[0].ClassUID != 1007 {
		t.Fatalf("got %d events (first class %v); want 1 of class 1007", len(resp.Events), classOf(resp.Events))
	}
}

// TestFixtureNoMatch: a verb with no matching fixture returns zero events with
// COMPLETE-style success (no error) — evidence-of-absence, not a failure.
func TestFixtureNoMatch(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "enumerate_logons",
		"target":  map[string]any{"hostname": "UNKNOWN"},
		"outcome": "SUCCESS",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Errorf("got %d events; want 0", len(resp.Events))
	}
}

// TestFixtureWildcard: a "*" matcher value matches any incoming value.
func TestFixtureWildcard(t *testing.T) {
	a := newTestFixture(t, map[string]string{
		"any.json": `{
          "fixture_meta":{"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"*"}},"delay_ms":0},
          "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:32:11Z"}
        }`,
	})
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "enumerate_logons",
		"target":  map[string]any{"hostname": "ANYTHING"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Errorf("wildcard matcher returned %d events; want 1", len(resp.Events))
	}
}

// TestFixtureToOcsfEvents: conversion mints ids, stamps recordedAt, and carries
// the source tool.
func TestFixtureToOcsfEvents(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "enumerate_logons",
		"target":  map[string]any{"hostname": "WIN-FILE01"},
		"outcome": "SUCCESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	events := resp.ToOcsfEvents(now)
	if len(events) != 2 {
		t.Fatalf("got %d OcsfEvents; want 2", len(events))
	}
	seen := map[string]bool{}
	for _, e := range events {
		if e.ID.String() == "" || seen[e.ID.String()] {
			t.Errorf("event id missing or duplicated: %v", e.ID)
		}
		seen[e.ID.String()] = true
		if !e.RecordedAt.Equal(now) {
			t.Errorf("recordedAt = %v; want %v", e.RecordedAt, now)
		}
		if e.SourceTool != "fixture:s" {
			t.Errorf("source_tool = %q; want fixture:s", e.SourceTool)
		}
	}
}

// TestFixtureUnsupportedOperation: a non-replay operation is a FALLTHROUGH so
// the resolver moves to the next binding.
func TestFixtureUnsupportedOperation(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	_, err := a.Invoke(context.Background(), "dispatch", nil)
	assertErrorClass(t, err, ErrFallthrough)
}

// TestFixtureMissingScenario: a missing scenario directory makes the adapter
// unhealthy and Invoke returns an UNHEALTHY error.
func TestFixtureMissingScenario(t *testing.T) {
	a := NewFixtureAdapter(t.TempDir(), "does-not-exist")
	a.SetApplyDelays(false)
	if h := a.Health(); h.Healthy {
		t.Error("missing scenario reported healthy")
	}
	_, err := a.Invoke(context.Background(), "replay", map[string]any{ParamVerb: "enumerate_logons"})
	assertErrorClass(t, err, ErrUnhealthy)
}

// TestFixtureRecoversAfterFailedLoad: a load failure is re-tested on the next
// call (§6.2 UNHEALTHY semantics), and the recovery load must not duplicate
// fixtures.
func TestFixtureRecoversAfterFailedLoad(t *testing.T) {
	root := t.TempDir()
	a := NewFixtureAdapter(root, "late")
	a.SetApplyDelays(false)

	// Scenario dir doesn't exist yet: unhealthy.
	if h := a.Health(); h.Healthy {
		t.Fatal("expected unhealthy before scenario exists")
	}

	// Scenario appears (e.g. operator drops fixtures in); adapter recovers.
	dir := filepath.Join(root, "late")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := `{
      "fixture_meta":{"scenario":"late","matches":{"verb":"enumerate_logons","params":{}},"delay_ms":0},
      "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:32:11Z"}
    }`
	if err := os.WriteFile(filepath.Join(dir, "e1.json"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	if h := a.Health(); !h.Healthy {
		t.Fatalf("expected recovery after scenario appears: %s", h.Message)
	}
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{ParamVerb: "enumerate_logons"})
	if err != nil {
		t.Fatalf("invoke after recovery: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Errorf("got %d events after recovery; want exactly 1 (no duplication)", len(resp.Events))
	}
}

// TestFixtureHealth: a loaded scenario reports healthy with a fixture count.
func TestFixtureHealth(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	h := a.Health()
	if !h.Healthy {
		t.Fatalf("healthy=false: %s", h.Message)
	}
}

func classOf(events []OcsfPayload) any {
	if len(events) == 0 {
		return "none"
	}
	return events[0].ClassUID
}

func assertErrorClass(t *testing.T, err error, want ErrorClass) {
	t.Helper()
	var ae *AdapterError
	if !errors.As(err, &ae) {
		t.Fatalf("error is %T (%v); want *AdapterError", err, err)
	}
	if ae.Class != want {
		t.Errorf("error class = %q; want %q", ae.Class, want)
	}
}

// TestFixtureTimeScoped: a time_scoped fixture replays only inside the window
// the binding passed (reserved window.from/to params); open bounds and
// unscoped fixtures are unaffected. This is how a scenario carries a baseline
// that a 30-day question reaches and a 24-hour question does not.
func TestFixtureTimeScoped(t *testing.T) {
	a := newTestFixture(t, map[string]string{
		"incident.json": `{
          "fixture_meta":{"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"*"}},"delay_ms":0},
          "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-04-20T14:32:47Z"}
        }`,
		"baseline.json": `{
          "fixture_meta":{"scenario":"s","matches":{"verb":"enumerate_logons","params":{"target.hostname":"*"}},"delay_ms":0,"time_scoped":true},
          "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-04-13T02:00:10Z"}
        }`,
	})
	invoke := func(window map[string]any) int {
		t.Helper()
		params := map[string]any{ParamVerb: "enumerate_logons", "target": map[string]any{"hostname": "WIN-FILE01"}}
		if window != nil {
			params["window"] = window
		}
		resp, err := a.Invoke(context.Background(), "replay", params)
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		return len(resp.Events)
	}
	if n := invoke(nil); n != 2 {
		t.Errorf("no window: %d events; want 2 (scoped fixtures replay when unbounded)", n)
	}
	if n := invoke(map[string]any{"from": "0001-01-01T00:00:00Z", "to": "0001-01-01T00:00:00Z"}); n != 2 {
		t.Errorf("zero window: %d events; want 2 (zero bounds are open)", n)
	}
	if n := invoke(map[string]any{"from": "2026-04-20T00:00:00Z", "to": "2026-04-21T00:00:00Z"}); n != 1 {
		t.Errorf("24h window: %d events; want 1 (baseline out of window; incident unscoped)", n)
	}
	if n := invoke(map[string]any{"from": "2026-03-21T00:00:00Z", "to": "2026-04-21T00:00:00Z"}); n != 2 {
		t.Errorf("30d window: %d events; want 2", n)
	}
}

// TestFixtureAnyOf: a matches.param given as a list matches any one of its
// values and nothing else (§9.1) — the mechanism that lets one scenario hold
// successful and failed logons that `outcome` tells apart, while `ALL`
// reaches both.
func TestFixtureAnyOf(t *testing.T) {
	a := newTestFixture(t, map[string]string{
		"ok.json": `{
          "fixture_meta":{"scenario":"s","matches":{"verb":"enumerate_logons","params":{"outcome":["SUCCESS","ALL"]}},"delay_ms":0},
          "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-06-11T03:21:08Z","status":"SUCCESS"}
        }`,
		"fail.json": `{
          "fixture_meta":{"scenario":"s","matches":{"verb":"enumerate_logons","params":{"outcome":["FAILURE","ALL"]}},"delay_ms":0},
          "ocsf":{"class_uid":3002,"class_name":"Authentication","time":"2026-06-11T03:14:02Z","status":"FAILURE"}
        }`,
	})
	for outcome, want := range map[string]int{"SUCCESS": 1, "FAILURE": 1, "ALL": 2, "OTHER": 0} {
		resp, err := a.Invoke(context.Background(), "replay", map[string]any{ParamVerb: "enumerate_logons", "outcome": outcome})
		if err != nil {
			t.Fatalf("invoke %s: %v", outcome, err)
		}
		if len(resp.Events) != want {
			t.Errorf("outcome=%s returned %d events; want %d", outcome, len(resp.Events), want)
		}
	}
}
