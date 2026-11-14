package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/evidence"
)

// buildCaseCapability returns a fixture-backed resolver + catalog serving
// get_external_case_details as a class-2005 Incident Finding — the read a case
// seed roots on (14-case-seed.md §3). An unbound variant (bind=false) exercises
// the fail-closed unavailable path.
func buildCaseCapability(t *testing.T, bind bool) (*capability.Resolver, *capability.Catalog) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := `{
	  "fixture_meta":{"scenario":"s","matches":{"verb":"get_external_case_details","params":{}},"delay_ms":0},
	  "ocsf":{"class_uid":2005,"class_name":"Incident Finding","time":1786193200000,"time_dt":"2026-08-08T12:46:40Z",
	          "status":"New","priority":"4 - Low",
	          "finding_info_list":[{"uid":"INC0010001","title":"Reimage request: WIN-FILE01","desc":"Reimage WIN-FILE01"}],
	          "src_url":"https://x/nav_to.do","unmapped":{"sor":"servicenow"}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "case.json"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := capability.TenantConfig{
		Tenant:   "t",
		Adapters: map[string]capability.AdapterSpec{"fixture": {Class: capability.ClassFixture, Enabled: true, Scenario: "s"}},
	}
	if bind {
		tc.Bindings = map[string][]capability.BindingSpec{
			"get_external_case_details": {{Adapter: "fixture", Operation: "replay", Priority: 100}},
		}
	}
	res, cat, err := capability.BuildResolver(tc, root, uuid.New(), true)
	if err != nil {
		t.Fatalf("BuildResolver: %v", err)
	}
	return res, cat
}

func TestResolveCaseSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("fixture-backed resolver; skipped in short mode")
	}
	req := httptest.NewRequest("POST", "/api/investigations", nil)

	// Happy path: exactly one case → a valid CaseSeed, persisted with the call
	// that read it (adr/0092). Handler nil: the fail-closed logic is what this
	// exercises.
	if _, err := testDB.Exec(`TRUNCATE ai_tool_calls, call_events, observed_data_events, capability_calls, ocsf_events, stix_edges`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	res, cat := buildCaseCapability(t, true)
	b := &Backend{cfg: BackendConfig{CapabilityResolver: res, CapabilityCatalog: cat, Evidence: evidence.NewStore(testDB)}}
	got, status, err := b.resolveCaseSeed(req, "INC0010001", "")
	if err != nil || status != 200 {
		t.Fatalf("happy path: status=%d err=%v", status, err)
	}
	if got.Seed.Type != aggregate.SeedCase || got.Seed.CaseID != "INC0010001" {
		t.Errorf("seed = %+v; want a case seed for INC0010001", got.Seed)
	}
	if got.Seed.Source == "" || got.Seed.CaseRef == "" {
		t.Errorf("seed missing source/case_ref: %+v", got.Seed)
	}
	if got.Display != "Reimage request: WIN-FILE01" {
		t.Errorf("display (→ investigation title) = %q; want the case title", got.Display)
	}
	var calls int
	if err := testDB.QueryRow(`SELECT count(*) FROM capability_calls WHERE verb = 'get_external_case_details' AND investigation_id IS NULL`).Scan(&calls); err != nil || calls != 1 {
		t.Errorf("recorded case reads = %d, %v; want one under no investigation", calls, err)
	}

	// Fail closed — no evidence store: the read is refused before it runs, so
	// no case object and no call go unpersisted.
	bNoStore := &Backend{cfg: BackendConfig{CapabilityResolver: res, CapabilityCatalog: cat}}
	if _, status, err := bNoStore.resolveCaseSeed(req, "INC0010001", ""); err == nil || status != 503 {
		t.Errorf("no store: status=%d err=%v; want 503 and an error", status, err)
	}

	// Fail closed — no case SoR configured (verb known via DefaultCatalog, no
	// binding) → UNAVAILABLE_TENANT → 503, never a create.
	resNo, catNo := buildCaseCapability(t, false)
	bNo := &Backend{cfg: BackendConfig{CapabilityResolver: resNo, CapabilityCatalog: catNo, Evidence: evidence.NewStore(testDB)}}
	if _, status, err := bNo.resolveCaseSeed(req, "INC0010001", ""); err == nil || status != 503 {
		t.Errorf("no-SoR: status=%d err=%v; want 503 and an error", status, err)
	}

	// Fail closed — no capability layer at all.
	bNil := &Backend{}
	if _, status, err := bNil.resolveCaseSeed(req, "INC0010001", ""); err == nil || status != 503 {
		t.Errorf("nil resolver: status=%d err=%v; want 503 and an error", status, err)
	}

	// Empty case id → 400.
	if _, status, err := b.resolveCaseSeed(req, "  ", ""); err == nil || status != 400 {
		t.Errorf("empty id: status=%d err=%v; want 400", status, err)
	}
}
