package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
)

// adr/0092: a call made for an investigation is a record — the invoke
// envelope names it, the investigation's events list under it with their
// view and joins, the call opens whole, and an event names the call that
// retrieved it. An ad-hoc call (no investigation) is listed nowhere.
func TestCalls_RecordedListedAndOpened(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE ocsf_events, stix_edges, capability_calls, call_events, observed_data_events`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	b := newTestBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	b.cfg.TenantNamespace = testNamespace.String()
	token := mintToken(t, nil)
	invID := uuid.New()

	resp, out := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{InvestigationRef: invID.String()})
	if resp.StatusCode != http.StatusOK || out.Coverage != string(capability.CoverageComplete) || out.CallID == "" {
		t.Fatalf("invoke = %d coverage=%q call_id=%q", resp.StatusCode, out.Coverage, out.CallID)
	}
	// An ad-hoc call persists its telemetry but joins no investigation.
	if resp2, adhoc := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{}); resp2.StatusCode != http.StatusOK || adhoc.CallID == "" {
		t.Fatalf("ad-hoc invoke = %d call_id=%q", resp2.StatusCode, adhoc.CallID)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	get := func(path string, into any) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(into)
		return r.StatusCode
	}

	var ev EventsResponse
	if code := get("/api/investigations/"+invID.String()+"/events", &ev); code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}
	if len(ev.Events) != len(out.Events) || ev.Total != len(out.Events) {
		t.Fatalf("events listed = %d (total %d); want the %d the call returned, once each", len(ev.Events), ev.Total, len(out.Events))
	}
	first := ev.Events[0]
	if first.CallID != out.CallID || first.Verb != "enumerate_logons" || first.Coverage != "COMPLETE" {
		t.Errorf("row call facts = %+v", first)
	}
	if first.View == nil || len(first.View.Sentence) == 0 || first.View.Category != "auth" {
		t.Errorf("row view = %+v", first.View)
	}
	if len(first.ObservedDataRefs) == 0 || len(first.EntityRefs) == 0 {
		t.Errorf("row joins: observed=%v entities=%v", first.ObservedDataRefs, first.EntityRefs)
	}
	for i := 1; i < len(ev.Events); i++ {
		if ev.Events[i].Time.Before(ev.Events[i-1].Time) {
			t.Errorf("events not in event time order at %d", i)
		}
	}

	// Selectors: by entity, by category, the window around one event.
	var byEntity EventsResponse
	get("/api/investigations/"+invID.String()+"/events?entity="+first.EntityRefs[0], &byEntity)
	if len(byEntity.Events) == 0 || len(byEntity.Events) > len(ev.Events) {
		t.Errorf("entity filter = %d rows", len(byEntity.Events))
	}
	for _, e := range byEntity.Events {
		if !containsRef(e.EntityRefs, first.EntityRefs[0]) {
			t.Errorf("entity filter leaked %s", e.ID)
		}
	}
	var byCategory EventsResponse
	get("/api/investigations/"+invID.String()+"/events?category=network", &byCategory)
	if len(byCategory.Events) != 0 {
		t.Errorf("category=network listed %d authentication events", len(byCategory.Events))
	}
	var around EventsResponse
	get("/api/investigations/"+invID.String()+"/events?around="+first.ID+"&limit=2", &around)
	if len(around.Events) == 0 || len(around.Events) > 3 {
		t.Errorf("around window = %d rows", len(around.Events))
	}
	found := false
	for _, e := range around.Events {
		if e.ID == first.ID {
			found = true
		}
	}
	if !found {
		t.Error("around window omits the anchor")
	}

	// The call lists and opens whole; only the investigation's call is listed.
	var calls struct {
		Calls []CallView `json:"calls"`
	}
	get("/api/investigations/"+invID.String()+"/calls", &calls)
	if len(calls.Calls) != 1 || calls.Calls[0].ID != out.CallID || calls.Calls[0].EventCount != len(out.Events) {
		t.Fatalf("calls = %+v", calls.Calls)
	}
	var detail CallDetail
	if code := get("/api/investigations/"+invID.String()+"/calls/"+out.CallID, &detail); code != http.StatusOK {
		t.Fatalf("call detail = %d", code)
	}
	if detail.Verb != "enumerate_logons" || detail.Coverage != "COMPLETE" || detail.BindingsAnswered != 1 || detail.BindingsTotal != 1 || detail.Adapter != "fixture" {
		t.Errorf("call = %+v", detail.CallView)
	}
	if len(detail.Attempts) != 1 || !detail.Attempts[0].Answered || detail.Attempts[0].Params == nil {
		t.Errorf("attempts = %+v", detail.Attempts)
	}
	if len(detail.Produced.Events) != len(out.Events) || len(detail.Produced.ObservedDataRefs) == 0 || len(detail.Produced.EntityRefs) == 0 {
		t.Errorf("produced = %+v", detail.Produced)
	}
	if len(detail.OtherBindings) != 1 || !detail.OtherBindings[0].Healthy {
		t.Errorf("other bindings = %+v", detail.OtherBindings)
	}
	var missing CallDetail
	if code := get("/api/investigations/"+uuid.NewString()+"/calls/"+out.CallID, &missing); code != http.StatusNotFound {
		t.Errorf("a call under another investigation = %d; want 404", code)
	}

	// An event names the call that retrieved it.
	var evidence EvidenceView
	get("/api/evidence/"+first.ID, &evidence)
	if evidence.CallID != out.CallID {
		t.Errorf("evidence call_id = %q; want %s", evidence.CallID, out.CallID)
	}
}

// adr/0095: the baseline is a read over the investigation's events for one
// entity — the nightly logons are a pattern with its count, days and hour,
// the interactive logon is the deviation naming the fields that differ, and
// an entity with nothing repeating answers 204.
func TestBaseline_Endpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE ocsf_events, stix_edges, capability_calls, call_events, observed_data_events`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	b := newTestBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	b.cfg.TenantNamespace = testNamespace.String()
	token := mintToken(t, nil)
	invID := uuid.New()
	if resp, out := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{InvestigationRef: invID.String()}); resp.StatusCode != http.StatusOK || len(out.Events) != 4 {
		t.Fatalf("invoke = %d events=%d; want 4", resp.StatusCode, len(out.Events))
	}
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	get := func(path string, into any) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if into != nil {
			_ = json.NewDecoder(r.Body).Decode(into)
		}
		return r.StatusCode
	}
	var ev EventsResponse
	get("/api/investigations/"+invID.String()+"/events", &ev)
	var host string
	for _, e := range ev.Events {
		for _, r := range e.EntityRefs {
			if len(r) > 7 && r[:7] == "x-host-" {
				host = r
			}
		}
		if e.Pattern == nil || e.Pattern["destination"] != "H1" {
			t.Errorf("row pattern = %v", e.Pattern)
		}
	}
	if host == "" {
		t.Fatal("no host entity among the events")
	}
	var base EntityBaselineResponse
	if code := get("/api/investigations/"+invID.String()+"/entities/"+host+"/baseline", &base); code != http.StatusOK {
		t.Fatalf("baseline = %d", code)
	}
	if base.Entity != host || base.EventsConsidered != 4 || len(base.Patterns) != 1 || len(base.Deviations) != 1 {
		t.Fatalf("baseline = %+v", base)
	}
	p := base.Patterns[0]
	if p.Count != 3 || p.Days != 3 || p.TypicalHour != 2 || p.Key["logon_type"] != "Network" {
		t.Errorf("pattern = %+v", p)
	}
	if d := base.Deviations[0]; d.Key["logon_type"] != "RemoteInteractive" || len(d.Differs) != 2 {
		t.Errorf("deviation = %+v", d)
	}
	// An entity the record holds no pattern for: 204, nothing to show.
	if code := get("/api/investigations/"+invID.String()+"/entities/x-host--00000000-0000-4000-8000-000000000000/baseline", nil); code != http.StatusNoContent {
		t.Errorf("no pattern = %d; want 204", code)
	}
}

// adr/0097: a turn that consulted a procedure commits the excerpt the model
// saw to the side store, keyed by its hash, and the investigation's
// retrievals read back with the turn, the revision and the location.
func TestRetrievals_Endpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE knowledge_retrievals, knowledge_retrieval_uses`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	b := newTestBackend(t)
	invID := activeInvestigation(t)
	token := mintToken(t, nil)
	sopID := uuid.New()
	resp, out := postInterpretation(t, b, token, RecordInterpretationBody{
		InvestigationRef:   invID.String(),
		InterpretationType: "pivot",
		Rationale:          "consulted the runbook",
		ConsultedSOPs: []ConsultedSOPBody{{
			SOPID: sopID.String(), Title: "Host Isolation Runbook", RetrievalScore: 0.81, Used: true,
			Excerpt: "Isolate first, preserve volatile evidence.", RevisionHash: "rev-aaaa",
			Location: json.RawMessage(`{"heading_path":["Host Isolation Runbook","Preconditions"],"offset":42}`), Band: "RELATED",
		}},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("record: %d", resp.StatusCode)
	}
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/investigations/"+invID.String()+"/retrievals?sop="+sopID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var got struct {
		Retrievals []RetrievalView `json:"retrievals"`
	}
	_ = json.NewDecoder(r.Body).Decode(&got)
	if r.StatusCode != http.StatusOK || len(got.Retrievals) != 1 {
		t.Fatalf("retrievals = %d %+v", r.StatusCode, got.Retrievals)
	}
	v := got.Retrievals[0]
	if v.InterpretationID != out.InterpretationID || v.SOPID != sopID.String() || v.Excerpt != "Isolate first, preserve volatile evidence." || v.RevisionHash != "rev-aaaa" || v.Band != "RELATED" {
		t.Errorf("retrieval = %+v", v)
	}
	if v.ExcerptHash == "" || !strings.Contains(string(v.Location), "Preconditions") || v.ChunkIndex != 1 || v.ChunkCount != 1 {
		t.Errorf("retrieval provenance = %+v", v)
	}
	if v.MatchesCurrent != nil {
		t.Error("no knowledge store in this backend: matches_current must be absent, not guessed")
	}
	// The event kept the hashes only; the bytes live in the side store.
	var excerptHash, revisionHash string
	if err := testDB.QueryRow(`SELECT payload -> 'consulted_sops' -> 0 ->> 'excerpt_hash', payload -> 'consulted_sops' -> 0 ->> 'revision_hash' FROM events WHERE aggregate_id = $1 AND event_type = 'interpretation.recorded' ORDER BY sequence_no DESC LIMIT 1`, invID).Scan(&excerptHash, &revisionHash); err != nil {
		t.Fatalf("event: %v", err)
	}
	if excerptHash != v.ExcerptHash || revisionHash != "rev-aaaa" {
		t.Errorf("event hashes = %q %q; want %q rev-aaaa", excerptHash, revisionHash, v.ExcerptHash)
	}
	var n int
	_ = testDB.QueryRow(`SELECT count(*) FROM events WHERE aggregate_id = $1 AND payload::text LIKE '%preserve volatile evidence%'`, invID).Scan(&n)
	if n != 0 {
		t.Error("the excerpt's bytes leaked into the event log")
	}
}
