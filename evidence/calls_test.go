package evidence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// retrieved builds one event with its observed-data naming refs, as the
// resolver's ingest would: the derived-from edge is the EventID on the result.
func retrieved(classUID int, className string, at time.Time, refs ...identity.STIXID) (capability.OcsfEvent, capability.NormalizationResult) {
	id := uuid.Must(uuid.NewV7())
	evt := capability.OcsfEvent{ID: id, ClassUID: classUID, ClassName: className, Time: at, RecordedAt: at, SourceTool: "fixture:test", Payload: map[string]any{"class_uid": classUID}}
	nr := capability.NormalizationResult{EventID: id, ObservedData: []capability.ObservedData{{ID: stixID("observed-data"), FirstObserved: at, ObjectRefs: refs}}}
	return evt, nr
}

func persistCall(t *testing.T, s *Store, call Call, res capability.CapabilityResult) {
	t.Helper()
	if err := s.Persist(context.Background(), call, res); err != nil {
		t.Fatalf("Persist: %v", err)
	}
}

// TestPersist_RecordsTheCall: the call row carries the binding that answered,
// the parameters as sent, the counts and the timing; every event it returned is
// joined to it, and the one made outside an investigation is under none.
func TestPersist_RecordsTheCall(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := uuid.New()
	evt, nr := retrieved(3002, "Authentication", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), stixID("user-account"), stixID("x-host"))
	call := Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "enumerate_logons", StartedAt: time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC), Duration: 1500 * time.Millisecond}
	persistCall(t, s, call, capability.CapabilityResult{
		Coverage: capability.CoverageComplete,
		Bindings: []capability.BindingOutcome{
			{Adapter: "edr", Operation: "logons", Priority: 100, Params: map[string]any{"host": "H1"}, Skipped: "adapter unhealthy"},
			{Adapter: "siem", Operation: "search", Priority: 50, Params: map[string]any{"q": "logon H1"}, Answered: true},
		},
		DegradationNotes: []string{"binding enumerate_logons/edr: adapter unhealthy"},
		Events:           []capability.OcsfEvent{evt},
		Normalized:       []capability.NormalizationResult{nr},
	})

	got, err := s.Call(ctx, inv, call.ID)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.Verb != "enumerate_logons" || got.Adapter != "siem" || got.Operation != "search" || got.Priority != 50 {
		t.Errorf("binding that answered = %s %s/%s p%d", got.Verb, got.Adapter, got.Operation, got.Priority)
	}
	if string(got.ParamsSent) != `{"q": "logon H1"}` && string(got.ParamsSent) != `{"q":"logon H1"}` {
		t.Errorf("params_sent = %s", got.ParamsSent)
	}
	if got.Coverage != "COMPLETE" || got.BindingsAnswered != 1 || got.BindingsTotal != 2 || got.DurationMS != 1500 {
		t.Errorf("coverage=%s answered=%d total=%d duration=%d", got.Coverage, got.BindingsAnswered, got.BindingsTotal, got.DurationMS)
	}
	if len(got.DegradationNotes) != 1 || got.InvestigationID == nil || *got.InvestigationID != inv {
		t.Errorf("notes=%v investigation=%v", got.DegradationNotes, got.InvestigationID)
	}
	if len(got.Produced.EventIDs) != 1 || got.Produced.EventIDs[0] != evt.ID {
		t.Errorf("produced events = %v", got.Produced.EventIDs)
	}
	if len(got.Produced.ObservedDataRefs) != 1 || len(got.Produced.EntityRefs) != 2 {
		t.Errorf("produced refs = %v %v", got.Produced.ObservedDataRefs, got.Produced.EntityRefs)
	}

	// Listed under its investigation, and under no other.
	calls, err := s.Calls(ctx, inv)
	if err != nil || len(calls) != 1 || calls[0].ID != call.ID {
		t.Errorf("Calls = %+v, %v", calls, err)
	}
	if _, err := s.Call(ctx, uuid.New(), call.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("call under another investigation: %v; want ErrNotFound", err)
	}

	// A call outside any investigation keeps its row and is listed nowhere.
	adhoc := aCall()
	persistCall(t, s, adhoc, capability.CapabilityResult{Coverage: capability.CoverageUnavailableTenant})
	if n := rowCount(t, "capability_calls"); n != 2 {
		t.Errorf("capability_calls = %d; want 2", n)
	}
	if calls, _ := s.Calls(ctx, inv); len(calls) != 1 {
		t.Errorf("the ad-hoc call was listed under an investigation")
	}
}

// TestPersist_CallIsInTheTransaction: a call row without an id fails the whole
// write, so no event is left without its membership.
func TestPersist_CallIsInTheTransaction(t *testing.T) {
	s := newStore(t)
	evt, nr := retrieved(3002, "Authentication", time.Now(), stixID("x-host"))
	err := s.Persist(context.Background(), Call{Verb: "v"}, capability.CapabilityResult{Events: []capability.OcsfEvent{evt}, Normalized: []capability.NormalizationResult{nr}})
	if err == nil {
		t.Fatal("Persist accepted a call without an id")
	}
	if n := rowCount(t, "ocsf_events"); n != 0 {
		t.Errorf("ocsf_events = %d after a failed write; want 0", n)
	}
}

// TestInvestigationEvents: the events of one investigation in event time, by
// entity and by category, and the window around one of them on its host.
func TestInvestigationEvents(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv, other := uuid.New(), uuid.New()
	host1, host2, svc := stixID("x-host"), stixID("x-host"), stixID("user-account")
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	// Five logons on host1 (svc on three of them), one process on host1, one
	// logon on host2; another investigation's logon must never show.
	var events []capability.OcsfEvent
	var norm []capability.NormalizationResult
	add := func(classUID int, name string, minute int, refs ...identity.STIXID) uuid.UUID {
		evt, nr := retrieved(classUID, name, base.Add(time.Duration(minute)*time.Minute), refs...)
		events = append(events, evt)
		norm = append(norm, nr)
		return evt.ID
	}
	add(3002, "Authentication", 1, host1, svc)
	add(3002, "Authentication", 2, host1)
	anchor := add(3002, "Authentication", 3, host1, svc)
	add(3002, "Authentication", 4, host1)
	add(3002, "Authentication", 5, host1, svc)
	proc := add(1007, "Process Activity", 6, host1)
	add(3002, "Authentication", 7, host2)
	call := Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "enumerate_logons", StartedAt: base}
	persistCall(t, s, call, capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: events, Normalized: norm,
		Bindings: []capability.BindingOutcome{{Adapter: "fixture", Operation: "replay", Params: map[string]any{}, Answered: true}}})
	foreign, fnr := retrieved(3002, "Authentication", base.Add(time.Hour), host1)
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &other, Verb: "enumerate_logons", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: []capability.OcsfEvent{foreign}, Normalized: []capability.NormalizationResult{fnr}})

	all, err := s.InvestigationEvents(ctx, inv, EventFilter{})
	if err != nil {
		t.Fatalf("InvestigationEvents: %v", err)
	}
	if all.Total != 7 || len(all.Rows) != 7 {
		t.Fatalf("all = %d rows, total %d; want 7", len(all.Rows), all.Total)
	}
	for i := 1; i < len(all.Rows); i++ {
		if all.Rows[i].Time.Before(all.Rows[i-1].Time) {
			t.Errorf("rows not in event time at %d", i)
		}
	}
	first := all.Rows[0]
	if first.CallID != call.ID || first.Verb != "enumerate_logons" || first.Coverage != "COMPLETE" || len(first.Payload) != 0 {
		t.Errorf("row = %+v; want the call on it and no payload", first)
	}
	if len(first.ObservedDataRefs) != 1 || len(first.EntityRefs) != 2 {
		t.Errorf("derived refs = %v %v", first.ObservedDataRefs, first.EntityRefs)
	}

	bySvc, err := s.InvestigationEvents(ctx, inv, EventFilter{Entity: string(svc)})
	if err != nil || bySvc.Total != 3 || len(bySvc.Rows) != 3 {
		t.Errorf("by entity = %d/%d, %v; want 3", len(bySvc.Rows), bySvc.Total, err)
	}
	byProc, err := s.InvestigationEvents(ctx, inv, EventFilter{Category: "process"})
	if err != nil || byProc.Total != 1 || byProc.Rows[0].ID != proc {
		t.Errorf("by category = %+v, %v; want the one process event", byProc, err)
	}
	if none, _ := s.InvestigationEvents(ctx, inv, EventFilter{Category: "weather"}); none.Total != 0 || len(none.Rows) != 0 {
		t.Errorf("unknown category matched %d", none.Total)
	}
	page, err := s.InvestigationEvents(ctx, inv, EventFilter{Limit: 2})
	if err != nil || len(page.Rows) != 2 || page.Total != 7 {
		t.Errorf("page = %d rows, total %d, %v; want 2 of 7", len(page.Rows), page.Total, err)
	}

	// Around the anchor on host1: one each side, the anchor in the middle, the
	// host2 logon excluded, the other investigation excluded.
	around, err := s.InvestigationEvents(ctx, inv, EventFilter{Around: anchor, Limit: 1})
	if err != nil {
		t.Fatalf("around: %v", err)
	}
	if len(around.Rows) != 3 || around.Rows[1].ID != anchor || around.Rows[0].Time.Minute() != 2 || around.Rows[2].Time.Minute() != 4 {
		t.Errorf("around = %v", times(around.Rows))
	}
	wide, _ := s.InvestigationEvents(ctx, inv, EventFilter{Around: anchor, Limit: 10})
	if len(wide.Rows) != 6 {
		t.Errorf("wide window = %d rows; want the 6 on host1", len(wide.Rows))
	}
	if _, err := s.InvestigationEvents(ctx, inv, EventFilter{Around: uuid.New()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("around an unknown event: %v; want ErrNotFound", err)
	}
}

func times(rows []EventRow) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Time.Minute())
	}
	return out
}
