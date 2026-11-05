package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
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
	if _, err := testDB.Exec(`TRUNCATE ai_tool_calls, call_events, observed_data_events, capability_calls, ocsf_events, stix_edges`); err != nil {
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
	// The row reads as its sentence (adr/0090), the entities bound to the
	// refs the record derived from it.
	if row.View.Category != "auth" || len(row.View.Sentence) == 0 || len(row.View.Fields) == 0 {
		t.Errorf("row view = %+v; want the authentication sentence and fields", row.View)
	}
	bound := 0
	for _, tok := range row.View.Sentence {
		if tok.T == "entity" && slices.Contains(row.EntityRefs, tok.Ref) {
			bound++
		}
	}
	if bound == 0 {
		t.Errorf("no sentence slot bound to a derived ref: %+v / %v", row.View.Sentence, row.EntityRefs)
	}

	var byEntity InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?entity="+row.EntityRefs[0], &byEntity); code != http.StatusOK || byEntity.Total == 0 {
		t.Errorf("by entity = %d, total %d", code, byEntity.Total)
	}

	// Grouped (adr/0095): the whole record folded by the default key, the
	// scenario's nightly logons one group over seven days; pivoted to an
	// entity, its slot leaves the key.
	var grouped InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?group_by=default", &grouped); code != http.StatusOK {
		t.Fatalf("grouped = %d", code)
	}
	if grouped.FoldedOver != grouped.Total || grouped.NextCursor != "" || len(grouped.Groups) == 0 {
		t.Errorf("grouped = folded %d of %d, cursor %q, %d groups", grouped.FoldedOver, grouped.Total, grouped.NextCursor, len(grouped.Groups))
	}
	if len(grouped.Keys) != 1 || grouped.Keys[0].ClassUID != 3002 || !slices.Equal(grouped.Keys[0].Key, []string{"user", "logon_type", "src", "dst"}) || len(grouped.Keys[0].Slots) == 0 {
		t.Errorf("grouped keys = %+v", grouped.Keys)
	}
	if grouped.Total >= 7 {
		nightly := false
		for _, g := range grouped.Groups {
			if g.Count == 7 && g.Days == 7 && len(g.EventIDs) == 7 && len(g.Key) == 4 {
				nightly = true
			}
		}
		if !nightly {
			t.Errorf("no nightly group of 7 over 7 days: %+v", grouped.Groups)
		}
	}
	var pivoted InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?group_by=default&entity="+row.EntityRefs[0], &pivoted); code != http.StatusOK || len(pivoted.Groups) == 0 {
		t.Errorf("pivoted = %d, %d groups", code, len(pivoted.Groups))
	}
	for _, g := range pivoted.Groups {
		for _, p := range g.Key {
			if p.Ref == row.EntityRefs[0] {
				t.Errorf("pivoted entity left in the key: %+v", g.Key)
			}
		}
	}
	for _, bad := range []string{"group_by=3002:nope", "group_by=default&cursor=x", "group_by=default&around=" + row.ID} {
		if code := getJSON(t, srv, token, base+"/events?"+bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d; want 400", bad, code)
		}
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
	// Newest first, one at a time: the cursor reaches the oldest and then stops.
	var newest InvestigationEventsResponse
	if code := getJSON(t, srv, token, base+"/events?order=newest&limit=1", &newest); code != http.StatusOK || len(newest.Events) != 1 || newest.Total != len(out.Events) || (newest.NextCursor == "") != (newest.Total == 1) {
		t.Fatalf("newest = %d, %d of %d, cursor %q; want one row and a cursor exactly when more remain", code, len(newest.Events), newest.Total, newest.NextCursor)
	}
	if newest.Events[0].ID != events.Events[len(events.Events)-1].ID {
		t.Errorf("newest first = %s; want the last of the oldest-first list", newest.Events[0].ID)
	}
	seen := map[string]bool{newest.Events[0].ID: true}
	for cursor := newest.NextCursor; cursor != ""; {
		var p InvestigationEventsResponse
		if code := getJSON(t, srv, token, base+"/events?order=newest&limit=1&cursor="+url.QueryEscape(cursor), &p); code != http.StatusOK || len(p.Events) != 1 {
			t.Fatalf("page after %s = %d, %d events", cursor, code, len(p.Events))
		}
		if seen[p.Events[0].ID] {
			t.Fatalf("event %s paged twice", p.Events[0].ID)
		}
		seen[p.Events[0].ID] = true
		cursor = p.NextCursor
	}
	if len(seen) != events.Total {
		t.Errorf("paged %d events; want all %d", len(seen), events.Total)
	}
	if code := getJSON(t, srv, token, base+"/events?order=sideways", nil); code != http.StatusBadRequest {
		t.Errorf("order=sideways = %d; want 400", code)
	}
	if code := getJSON(t, srv, token, base+"/events?cursor=yesterday", nil); code != http.StatusBadRequest {
		t.Errorf("cursor=yesterday = %d; want 400", code)
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
	if len(call.Outcomes) != 1 || !call.Outcomes[0].Answered || call.Outcomes[0].Adapter != "fixture" || strings.Contains(string(call.Outcomes[0].Params), `"verb"`) {
		t.Errorf("outcomes = %+v; want the one binding, answered, without the injected verb", call.Outcomes)
	}
	if strings.Contains(string(call.ParamsSent), `"verb"`) {
		t.Errorf("params_sent = %s carries the injected verb", call.ParamsSent)
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
