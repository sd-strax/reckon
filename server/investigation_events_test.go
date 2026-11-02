package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
)

// getJSON reads one authenticated GET into out and returns the status.
func getJSON(t *testing.T, srv *httptest.Server, token, path string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// TestInvestigationEvents_MembershipAndCalls: an invocation made for an
// investigation returns a call id, its events are listed under the
// investigation (by entity, by category, around one of them), and the call
// resolves with what it produced and the verb's bindings (adr/0092). A call
// made for no investigation is listed under none.
func TestInvestigationEvents_MembershipAndCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE call_events, observed_data_events, capability_calls, ocsf_events, stix_edges`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	b := newTestBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	inv := activeInvestigation(t)
	token := mintToken(t, nil)

	resp, out := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{InvestigationRef: inv.String()})
	if resp.StatusCode != http.StatusOK || out.CallID == "" {
		t.Fatalf("invoke = %d, call_id %q; want 200 with a call id", resp.StatusCode, out.CallID)
	}
	if _, err := uuid.Parse(out.CallID); err != nil {
		t.Fatalf("call_id %q is not an id", out.CallID)
	}
	// A second call, for no investigation: recorded, listed nowhere.
	if resp, adhoc := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{}); resp.StatusCode != http.StatusOK || adhoc.CallID == out.CallID {
		t.Fatalf("ad-hoc invoke = %d, call_id %q", resp.StatusCode, adhoc.CallID)
	}
	if resp, _ := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{InvestigationRef: "not-an-id"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed investigation_ref = %d; want 400", resp.StatusCode)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	base := "/api/investigations/" + inv.String()

	var events InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events", &events); code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}
	if events.Total != len(out.Events) || len(events.Events) != len(out.Events) || events.Total == 0 {
		t.Fatalf("events = %d/%d; want the %d the call returned", len(events.Events), events.Total, len(out.Events))
	}
	row := events.Events[0]
	if row.CallID != out.CallID || row.Verb != "enumerate_logons" || row.Coverage != "COMPLETE" || row.ClassUID != 3002 {
		t.Errorf("row = %+v", row)
	}
	if len(row.EntityRefs) == 0 || len(row.ObservedDataRefs) == 0 {
		t.Errorf("row carries no derived refs: %+v", row)
	}

	var byEntity InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?entity="+row.EntityRefs[0], &byEntity); code != http.StatusOK || byEntity.Total == 0 {
		t.Errorf("by entity = %d, total %d", code, byEntity.Total)
	}
	var byCategory InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?category=network", &byCategory); code != http.StatusOK || byCategory.Total != 0 {
		t.Errorf("by category network = %d, total %d; the fixture has only logons", code, byCategory.Total)
	}
	var around InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?around="+row.ID+"&limit=3", &around); code != http.StatusOK || len(around.Events) == 0 || around.Events[0].ID != row.ID {
		t.Errorf("around = %d, %+v; want the anchor in its window", code, around.Events)
	}
	if code := getJSON(t, srv, token, base+"/events?around="+uuid.NewString(), nil); code != http.StatusNotFound {
		t.Errorf("around an unknown event = %d; want 404", code)
	}
	if code := getJSON(t, srv, token, base+"/events?limit=0", nil); code != http.StatusBadRequest {
		t.Errorf("limit=0 = %d; want 400", code)
	}

	var calls InvestigationCallsResponse
	if code := getJSON(t, srv, token, base+"/calls", &calls); code != http.StatusOK || len(calls.Calls) != 1 || calls.Calls[0].CallID != out.CallID {
		t.Fatalf("calls = %d, %+v; want the one call made for the investigation", code, calls.Calls)
	}
	var call CapabilityCallView
	if code := getJSON(t, srv, token, base+"/calls/"+out.CallID, &call); code != http.StatusOK {
		t.Fatalf("call = %d", code)
	}
	if call.Adapter != "fixture" || call.Operation != "replay" || call.Priority != 100 || call.BindingsAnswered != 1 || call.BindingsTotal != 1 {
		t.Errorf("call = %+v; want the fixture binding that answered", call)
	}
	if call.Produced == nil || len(call.Produced.EventIDs) != len(out.Events) || len(call.Produced.EntityRefs) == 0 {
		t.Errorf("produced = %+v", call.Produced)
	}
	if len(call.Bindings) != 1 || call.Bindings[0].Adapter != "fixture" || !call.Bindings[0].Enabled {
		t.Errorf("bindings = %+v; want the verb's one configured binding", call.Bindings)
	}
	if code := getJSON(t, srv, token, base+"/calls/"+uuid.NewString(), nil); code != http.StatusNotFound {
		t.Errorf("unknown call = %d; want 404", code)
	}
	if code := getJSON(t, srv, token, base+"/calls/nope", nil); code != http.StatusBadRequest {
		t.Errorf("malformed call id = %d; want 400", code)
	}
}

// TestInvestigationEvents_Unconfigured: without an evidence store the reads
// are a clean 503, like the citation-open route.
func TestInvestigationEvents_Unconfigured(t *testing.T) {
	b := &Backend{cfg: BackendConfig{}}
	for _, h := range []func(http.ResponseWriter, *http.Request, uuid.UUID){b.listInvestigationEvents, b.listInvestigationCalls, b.getInvestigationCall} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/x", nil), uuid.New())
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d; want 503", w.Code)
		}
	}
}

// TestVerbBindings_FromConfigFile: with a tenant config the bindings come from
// the file, so an installed-but-disabled adapter's binding is listed too.
func TestVerbBindings_FromConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cap.yaml"
	if err := os.WriteFile(path, []byte(`
tenant: t
adapters:
  fixture: {class: fixture, enabled: true, scenario: s}
  edr: {class: fixture, enabled: false, scenario: s}
bindings:
  enumerate_logons:
    - {adapter: edr, operation: logons, priority: 200}
    - {adapter: fixture, operation: replay, priority: 100}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Backend{cfg: BackendConfig{CapabilityConfigPath: path}}
	got := b.verbBindings("enumerate_logons")
	if len(got) != 2 || got[0].Adapter != "edr" || got[0].Enabled || got[1].Adapter != "fixture" || !got[1].Enabled {
		t.Errorf("bindings = %+v", got)
	}
	if got := b.verbBindings("unknown"); len(got) != 0 {
		t.Errorf("unknown verb bindings = %+v", got)
	}
}
