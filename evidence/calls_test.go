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
			{Adapter: "siem", Operation: "search", Priority: 50, Params: map[string]any{"q": "logon H1", capability.ParamVerb: "enumerate_logons"}, Answered: true},
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
		t.Errorf("params_sent = %s; want the parameters without the injected verb", got.ParamsSent)
	}
	// Every outcome is kept, in the order walked, each without the injected verb.
	if len(got.Bindings) != 2 || got.Bindings[0].Adapter != "edr" || got.Bindings[0].Skipped != "adapter unhealthy" || got.Bindings[0].Answered ||
		got.Bindings[1].Adapter != "siem" || !got.Bindings[1].Answered || got.Bindings[1].Priority != 50 {
		t.Errorf("binding outcomes = %+v", got.Bindings)
	}
	if string(got.Bindings[0].Params) != `{"host": "H1"}` && string(got.Bindings[0].Params) != `{"host":"H1"}` {
		t.Errorf("skipped binding params = %s", got.Bindings[0].Params)
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

// TestInvestigationEvents_ExtensionCategory: an extension's class carries the
// extension's number above the category (201002, the Windows extension's
// Registry Value Activity, is category 1), and the category filter lists and
// counts it with the core classes of that category.
func TestInvestigationEvents_ExtensionCategory(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := uuid.New()
	host := stixID("x-host")
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	reg, regNorm := retrieved(201002, "Registry Value Activity", base.Add(time.Minute), host)
	proc, procNorm := retrieved(1007, "Process Activity", base.Add(2*time.Minute), host)
	logon, logonNorm := retrieved(3002, "Authentication", base.Add(3*time.Minute), host)
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "get_registry_changes", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete,
			Events:     []capability.OcsfEvent{reg, proc, logon},
			Normalized: []capability.NormalizationResult{regNorm, procNorm, logonNorm}})

	got, err := s.InvestigationEvents(ctx, inv, EventFilter{Category: "process"})
	if err != nil {
		t.Fatalf("InvestigationEvents: %v", err)
	}
	if got.Total != 2 || len(got.Rows) != 2 || got.Rows[0].ID != reg.ID || got.Rows[1].ID != proc.ID {
		t.Fatalf("category 1 = %d rows, total %d; want the registry event and the process event", len(got.Rows), got.Total)
	}
	// Extension 2 is not category 2: the registry event is no finding.
	if det, err := s.InvestigationEvents(ctx, inv, EventFilter{Category: "detection"}); err != nil || det.Total != 0 {
		t.Errorf("category 2 = %d, %v; want none", det.Total, err)
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
	// The first event's objects are persisted too, so its row can carry them.
	norm[0].SCOs = []capability.SCO{
		{ID: host1, Type: identity.TypeHost, Properties: map[string]any{"hostname": "H1"}},
		{ID: svc, Type: identity.TypeUserAccount, Properties: map[string]any{"account_login": "svc"}},
	}
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
	if first.CallID != call.ID || first.Verb != "enumerate_logons" || first.Coverage != "COMPLETE" || len(first.Payload) == 0 {
		t.Errorf("row = %+v; want the call on it and the payload (adr/0090)", first)
	}
	if len(first.ObservedDataRefs) != 1 || len(first.EntityRefs) != 2 {
		t.Errorf("derived refs = %v %v", first.ObservedDataRefs, first.EntityRefs)
	}
	// The row carries the objects its refs name, in ref order, as the record
	// holds them; an event whose objects were never persisted carries none.
	if len(first.Entities) != 2 || first.Entities[0].Ref != first.EntityRefs[0] || first.Entities[1].Ref != first.EntityRefs[1] || len(first.Entities[0].Payload) == 0 {
		t.Errorf("entities = %+v for refs %v", first.Entities, first.EntityRefs)
	}
	// The second event's observed-data names host1 alone, and the record holds
	// that object (ids are deterministic, so one persisted host serves every
	// event that names it); svc it never named.
	if len(all.Rows[1].Entities) != 1 || all.Rows[1].Entities[0].Ref != string(host1) {
		t.Errorf("second row entities = %+v; want the host alone", all.Rows[1].Entities)
	}
	if ents, err := s.EventEntities(ctx, first.ID); err != nil || len(ents) != 2 {
		t.Errorf("EventEntities = %d, %v; want 2", len(ents), err)
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
	if err != nil || len(page.Rows) != 2 || page.Total != 7 || page.NextCursor == nil || page.NextCursor.ID != page.Rows[1].ID {
		t.Errorf("page = %d rows, total %d, next %v, %v; want 2 of 7 with a cursor on the second", len(page.Rows), page.Total, page.NextCursor, err)
	}
	// The next page continues past the cursor; the last page has none.
	rest, err := s.InvestigationEvents(ctx, inv, EventFilter{Limit: 5, Cursor: page.NextCursor})
	if err != nil || len(rest.Rows) != 5 || rest.Total != 7 || rest.NextCursor != nil || rest.Rows[0].Time.Minute() != 3 {
		t.Errorf("rest = %v, total %d, next %v, %v; want minutes 3..7 and no cursor", times(rest.Rows), rest.Total, rest.NextCursor, err)
	}
	// Newest first: the page starts at the latest event and the cursor walks back.
	newest, err := s.InvestigationEvents(ctx, inv, EventFilter{Limit: 3, Newest: true})
	if err != nil || len(newest.Rows) != 3 || newest.Rows[0].Time.Minute() != 7 || newest.Rows[2].Time.Minute() != 5 || newest.NextCursor == nil {
		t.Errorf("newest = %v, next %v, %v; want 7, 6, 5 with a cursor", times(newest.Rows), newest.NextCursor, err)
	}
	older, err := s.InvestigationEvents(ctx, inv, EventFilter{Limit: 10, Newest: true, Cursor: newest.NextCursor})
	if err != nil || len(older.Rows) != 4 || older.Rows[0].Time.Minute() != 4 || older.NextCursor != nil {
		t.Errorf("older = %v, next %v, %v; want 4, 3, 2, 1", times(older.Rows), older.NextCursor, err)
	}

	// The same event retrieved again by a later call is one row, under the
	// call that first brought it in.
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "enumerate_logons", StartedAt: base.Add(time.Minute)},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: events[:2], Normalized: norm[:2]})
	again, err := s.InvestigationEvents(ctx, inv, EventFilter{})
	if err != nil || again.Total != 7 || len(again.Rows) != 7 || again.Rows[0].CallID != call.ID || len(again.Rows[0].ObservedDataRefs) != 1 {
		t.Errorf("after a second retrieval = %d rows, total %d, first under %s with refs %v, %v; want 7 once, under the first call", len(again.Rows), again.Total, again.Rows[0].CallID, again.Rows[0].ObservedDataRefs, err)
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
	// An event of another investigation is not an anchor here, however real.
	if _, err := s.InvestigationEvents(ctx, inv, EventFilter{Around: foreign.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("around another investigation's event: %v; want ErrNotFound", err)
	}
	// An anchor naming no entity has nothing "on the same entity" around it.
	lone, lnr := retrieved(3002, "Authentication", base.Add(8*time.Minute))
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "enumerate_logons", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: []capability.OcsfEvent{lone}, Normalized: []capability.NormalizationResult{lnr}})
	if empty, err := s.InvestigationEvents(ctx, inv, EventFilter{Around: lone.ID}); err != nil || len(empty.Rows) != 0 || empty.Total != 0 {
		t.Errorf("around an event with no entity = %v, %v; want an empty window", times(empty.Rows), err)
	}
}

func times(rows []EventRow) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Time.Minute())
	}
	return out
}
