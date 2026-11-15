package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
)

// buildUnreadCaseCapability serves a queue of INC1, a case record with no
// usable time, and INC2, a readable one, and a detail read of INC1. A record
// with no time is refused by the normalizer (adr/0357), so INC1 comes back
// and cannot be read.
func buildUnreadCaseCapability(t *testing.T) (*capability.Resolver, *capability.Catalog) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, verb, params, num, timeFields string) {
		t.Helper()
		body := fmt.Sprintf(`{
		  "fixture_meta":{"scenario":"s","matches":{"verb":%q,"params":%s},"delay_ms":0},
		  "ocsf":{"class_uid":2005,"class_name":"Incident Finding",%s"status":"New",
		          "finding_info_list":[{"uid":%q,"title":"t","desc":"body"}],"unmapped":{"sor":"servicenow"}}
		}`, verb, params, timeFields, num)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("q1.json", "query_external_cases", `{"queue":"SOC"}`, "INC1", "")
	write("q2.json", "query_external_cases", `{"queue":"SOC"}`, "INC2", `"time":1776696060000,"time_dt":"2026-04-20T14:41:00Z",`)
	write("d1.json", "get_external_case_details", `{"case_id":"INC1"}`, "INC1", "")
	tc := capability.TenantConfig{
		Tenant:   "t",
		Adapters: map[string]capability.AdapterSpec{"fixture": {Class: capability.ClassFixture, Enabled: true, Scenario: "s"}},
		Bindings: map[string][]capability.BindingSpec{
			"query_external_cases":      {{Adapter: "fixture", Operation: "replay", Priority: 100, Params: map[string]any{"queue": "${queue?}"}}},
			"get_external_case_details": {{Adapter: "fixture", Operation: "replay", Priority: 100, Params: map[string]any{"case_id": "${case_id}"}}},
		},
	}
	res, cat, err := capability.BuildResolver(tc, root, uuid.New(), true)
	if err != nil {
		t.Fatal(err)
	}
	return res, cat
}

// A case record that came back but could not be read is never reported as
// absent (adr/0357): the detail read is a 502 naming why, the case seed
// refuses with the same message, and the queue lists the readable cases with
// a count of the unread ones and their reasons. The malformed record itself is
// still refused.
func TestCases_UnreadRecordIsNotAbsence(t *testing.T) {
	res, cat := buildUnreadCaseCapability(t)
	b := &Backend{cfg: BackendConfig{CapabilityResolver: res, CapabilityCatalog: cat}}

	rec := httptest.NewRecorder()
	b.getCase(rec, httptest.NewRequest("GET", "/api/cases/INC1", nil), "INC1")
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "could not be read") || strings.Contains(rec.Body.String(), "no case matches") {
		t.Errorf("detail of an unreadable INC1: %d %s; want 502 could not be read", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), capability.ErrNoEventTime.Error()) {
		t.Errorf("detail does not give the reason: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	b.listCases(rec, httptest.NewRequest("GET", "/api/cases?queue=SOC", nil))
	if rec.Code != 200 {
		t.Fatalf("queue: %d %s", rec.Code, rec.Body.String())
	}
	var list CaseListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Cases) != 1 || list.Cases[0].Number != "INC2" {
		t.Errorf("queue cases = %+v; want INC2, the readable one", list.Cases)
	}
	if list.Unread == nil || list.Unread.Count != 1 || len(list.Unread.Reasons) != 1 || !strings.Contains(list.Unread.Reasons[0], capability.ErrNoEventTime.Error()) {
		t.Errorf("queue unread = %+v; want one unread record with its reason", list.Unread)
	}

	b.cfg.Evidence = &fakeEvidence{}
	_, status, err := b.resolveCaseSeed(httptest.NewRequest("POST", "/api/investigations", nil), "INC1", "")
	if status != 502 || err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("case seed on an unreadable INC1: %d %v; want 502 could not be read", status, err)
	}
}
