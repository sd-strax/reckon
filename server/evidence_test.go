package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sd-strax/reckon/capability"
)

// TestEvidence_PersistAndOpen: a capability invocation persists its telemetry
// + normalized objects (03 §4.13 eager promotion), and every ref in the
// returned envelope opens via GET /api/evidence/{ref} — the citation-open
// contract (design/ui 02 §2.8): no ref leaves the building unless it can be
// opened later.
func TestEvidence_PersistAndOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE ocsf_events, stix_edges`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	b := newTestBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)

	token := mintToken(t, nil)
	resp, out := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invoke = %d", resp.StatusCode)
	}
	if out.Coverage != string(capability.CoverageComplete) {
		t.Fatalf("coverage = %q (notes: %v)", out.Coverage, out.DegradationNotes)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	open := func(ref string) (int, EvidenceView) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/evidence/"+ref, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var v EvidenceView
		_ = json.NewDecoder(r.Body).Decode(&v)
		return r.StatusCode, v
	}

	// Every ref class in the envelope opens: raw telemetry, observed-data,
	// and the entity SCOs.
	code, ev := open(out.OcsfEventRefs[0])
	if code != http.StatusOK || ev.Kind != "ocsf" || ev.ClassUID != 3002 || len(ev.Payload) == 0 {
		t.Errorf("open ocsf ref = %d %+v; want a raw telemetry record", code, ev)
	}
	code, ev = open(out.ObservedDataRefs[0])
	if code != http.StatusOK || ev.Kind != "stix" || ev.Type != "observed-data" {
		t.Errorf("open observed-data ref = %d %+v", code, ev)
	}
	code, ev = open(out.EntityRefs[0])
	if code != http.StatusOK || ev.Kind != "stix" || len(ev.Payload) == 0 {
		t.Errorf("open entity ref = %d %+v", code, ev)
	}

	// Determinism makes re-invocation idempotent on the interpretation layer:
	// the same entities upsert to the same ids (no duplicate objects).
	var stixCount int
	if err := testDB.QueryRow(`SELECT count(*) FROM stix_objects WHERE type NOT IN ('x-hypothesis','x-prediction')`).Scan(&stixCount); err != nil {
		t.Fatal(err)
	}
	if resp, _ := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-invoke = %d", resp.StatusCode)
	}
	var after int
	if err := testDB.QueryRow(`SELECT count(*) FROM stix_objects WHERE type NOT IN ('x-hypothesis','x-prediction')`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != stixCount {
		t.Errorf("stix objects grew %d → %d on re-invocation; deterministic ids must dedupe", stixCount, after)
	}

	// An unknown ref is an honest 404, never an empty 200.
	if code, _ := open("observed-data--00000000-0000-0000-0000-00000000dead"); code != http.StatusNotFound {
		t.Errorf("unknown ref = %d; want 404", code)
	}
}

// TestEvidence_NullObservationOpens: a complete, empty answer persists its
// x-null-observation (adr/0061) and the citation opens like any other.
func TestEvidence_NullObservationOpens(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	b := newTestBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)

	token := mintToken(t, nil)
	// The test fixture answers get_network_connections for H1 only: asking
	// about H2 is a complete, empty answer.
	resp, out := postCapability(t, b, token, "get_network_connections", CapabilityInvokeBody{Entity: map[string]any{"host": map[string]any{"hostname": "H2"}}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invoke = %d", resp.StatusCode)
	}
	if out.Coverage != string(capability.CoverageComplete) || out.NullObservation == nil || len(out.ObservedDataRefs) != 1 {
		t.Fatalf("coverage=%q null=%v refs=%v (notes: %v); want COMPLETE with one citeable null observation", out.Coverage, out.NullObservation != nil, out.ObservedDataRefs, out.DegradationNotes)
	}
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/evidence/"+out.ObservedDataRefs[0], nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("open null observation = %d", res.StatusCode)
	}
	var ev EvidenceView
	if err := json.NewDecoder(res.Body).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "stix" || ev.Type != "x-null-observation" {
		t.Errorf("opened %s as kind=%q type=%q", out.ObservedDataRefs[0], ev.Kind, ev.Type)
	}
}
