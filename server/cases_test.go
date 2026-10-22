package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
)

// buildQueueCapability returns a fixture-backed resolver + catalog serving a
// two-case queue (matched on the `queue` input) and a detail read per case,
// with policies.case_queue set — the shape adr/0030-cases-pane.md reads.
func buildQueueCapability(t *testing.T) (*capability.Resolver, *capability.Catalog) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const tmpl = `{
	  "fixture_meta":{"scenario":"s","matches":{"verb":%q,"params":%s},"delay_ms":0},
	  "ocsf":{"class_uid":2005,"class_name":"Incident Finding","time":"2026-04-20T14:41:00Z",
	          "status":"New","priority":"2 - High","assignment_group":"SOC Tier 2",
	          "finding_info":{"uid":%q,"title":%q,"desc":"body"},
	          "unmapped":{"link":"https://x/nav_to.do","sor":"servicenow"}}
	}`
	write := func(name, verb, params, num, title string) {
		t.Helper()
		body := []byte(fmt.Sprintf(tmpl, verb, params, num, title))
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("q1.json", "query_external_cases", `{"queue":"SOC Tier 2"}`, "INC1", "First")
	write("q2.json", "query_external_cases", `{"queue":"SOC Tier 2"}`, "INC2", "Second")
	write("d1.json", "get_external_case_details", `{"case_id":"INC1"}`, "INC1", "First")

	tc := capability.TenantConfig{
		Tenant:   "t",
		Adapters: map[string]capability.AdapterSpec{"fixture": {Class: capability.ClassFixture, Enabled: true, Scenario: "s"}},
		Bindings: map[string][]capability.BindingSpec{
			"query_external_cases":      {{Adapter: "fixture", Operation: "replay", Priority: 100, Params: map[string]any{"filter": "${filter?}", "queue": "${queue?}"}}},
			"get_external_case_details": {{Adapter: "fixture", Operation: "replay", Priority: 100, Params: map[string]any{"case_id": "${case_id}"}}},
		},
		Policies: map[string]any{capability.PolicyCaseQueue: "SOC Tier 2"},
	}
	res, cat, err := capability.BuildResolver(tc, root, uuid.New(), true)
	if err != nil {
		t.Fatalf("BuildResolver: %v", err)
	}
	return res, cat
}

func TestCasesQueueAndDetail(t *testing.T) {
	res, cat := buildQueueCapability(t)
	// Handler nil: no investigation lookup, no persistence path at all — the
	// read surface must work without touching the aggregate store.
	b := &Backend{cfg: BackendConfig{CapabilityResolver: res, CapabilityCatalog: cat}}

	// The queue, defaulting to the tenant policy.
	rec := httptest.NewRecorder()
	b.listCases(rec, httptest.NewRequest("GET", "/api/cases", nil))
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list CaseListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Queue != "SOC Tier 2" || len(list.Cases) != 2 {
		t.Fatalf("list = queue %q, %d cases; want the configured queue with 2", list.Queue, len(list.Cases))
	}
	c := list.Cases[0]
	if c.Number != "INC1" || c.Title != "First" || c.Priority != "2 - High" || c.Status != "New" || c.AssignmentGroup != "SOC Tier 2" {
		t.Errorf("row = %+v; extensions not flattened", c)
	}
	if c.Source == "" || c.OpenedAt == nil || c.Investigation != nil {
		t.Errorf("row = %+v; want source + opened_at, no investigation", c)
	}

	// An explicit queue nobody is in: COMPLETE and empty, not an error.
	rec = httptest.NewRecorder()
	b.listCases(rec, httptest.NewRequest("GET", "/api/cases?queue=Other", nil))
	if rec.Code != 200 {
		t.Fatalf("list other queue: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Queue != "Other" || len(list.Cases) != 0 {
		t.Errorf("other queue = %q/%d; want Other/0", list.Queue, len(list.Cases))
	}

	// Detail: exactly one; unknown number is a 404 (COMPLETE + empty).
	rec = httptest.NewRecorder()
	b.getCase(rec, httptest.NewRequest("GET", "/api/cases/INC1", nil), "INC1")
	if rec.Code != 200 {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	var one CaseView
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Number != "INC1" || one.Summary != "body" {
		t.Errorf("detail = %+v; want INC1 with its body", one)
	}
	rec = httptest.NewRecorder()
	b.getCase(rec, httptest.NewRequest("GET", "/api/cases/INC9", nil), "INC9")
	if rec.Code != 404 {
		t.Errorf("unknown case: %d; want 404", rec.Code)
	}
}

func TestCasesWithoutSoR(t *testing.T) {
	// Verbs known to the catalog, no binding → UNAVAILABLE_TENANT → 503, the
	// same answer the seed flow gives (14 §2).
	res, cat := buildCaseCapability(t, false)
	b := &Backend{cfg: BackendConfig{CapabilityResolver: res, CapabilityCatalog: cat}}
	rec := httptest.NewRecorder()
	b.listCases(rec, httptest.NewRequest("GET", "/api/cases", nil))
	if rec.Code != 503 {
		t.Errorf("no SoR: %d; want 503", rec.Code)
	}
	// No capability layer at all.
	rec = httptest.NewRecorder()
	(&Backend{}).listCases(rec, httptest.NewRequest("GET", "/api/cases", nil))
	if rec.Code != 503 {
		t.Errorf("nil resolver: %d; want 503", rec.Code)
	}
}
