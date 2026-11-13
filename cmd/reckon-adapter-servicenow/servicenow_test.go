package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/adapterplugin"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// snowMock is a stateful ServiceNow Table API double: it stores incidents by
// sys_id, indexes correlation_id and number for the two query paths the adapter
// uses, and counts POSTs so a test can prove idempotent-create dedup.
type snowMock struct {
	mu         sync.Mutex
	byCorr     map[string]*mockRec
	byNumber   map[string]*mockRec
	bySysID    map[string]*mockRec
	seq        int
	postCount  int
	createBody map[string]any // last POST body, for assertion
	patchBody  map[string]any // last PATCH body, for assertion
}

type mockRec struct {
	SysID string
	Num   string
	State string
}

func newSNowMock() *snowMock {
	return &snowMock{
		byCorr: map[string]*mockRec{}, byNumber: map[string]*mockRec{}, bySysID: map[string]*mockRec{},
	}
}

// server wires the mock onto an httptest server. Basic auth is enforced
// (admin:secret) so the auth header is exercised.
func (m *snowMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"User Not Authenticated"},"status":"failure"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			m.handleQuery(w, r)
		case http.MethodPost:
			m.handleCreate(w, r)
		case http.MethodPatch:
			m.handlePatch(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(srv.Close)
	return srv
}

func (m *snowMock) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("sysparm_query")
	m.mu.Lock()
	defer m.mu.Unlock()
	var rec *mockRec
	if v, ok := strings.CutPrefix(q, "correlation_id="); ok {
		rec = m.byCorr[v]
	} else if v, ok := strings.CutPrefix(q, "number="); ok {
		rec = m.byNumber[v]
	}
	if rec == nil {
		_, _ = w.Write([]byte(`{"result":[]}`))
		return
	}
	_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":%q,"number":%q,"state":%q}]}`, rec.SysID, rec.Num, rec.State)
}

func (m *snowMock) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postCount++
	m.createBody = body
	m.seq++
	rec := &mockRec{
		SysID: fmt.Sprintf("%032x", m.seq),
		Num:   fmt.Sprintf("INC%07d", m.seq),
		State: "1",
	}
	m.bySysID[rec.SysID] = rec
	m.byNumber[rec.Num] = rec
	if corr, ok := body["correlation_id"].(string); ok && corr != "" {
		m.byCorr[corr] = rec
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, `{"result":{"sys_id":%q,"number":%q,"state":%q}}`, rec.SysID, rec.Num, rec.State)
}

func (m *snowMock) handlePatch(w http.ResponseWriter, r *http.Request) {
	sysID := strings.TrimPrefix(r.URL.Path, "/api/now/table/incident/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patchBody = body
	rec := m.bySysID[sysID]
	if rec == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Record not found"},"status":"failure"}`))
		return
	}
	if s, ok := body["state"].(string); ok {
		rec.State = s
	}
	_, _ = fmt.Fprintf(w, `{"result":{"sys_id":%q,"number":%q,"state":%q}}`, rec.SysID, rec.Num, rec.State)
}

// configuredBridge builds a bridge pointing at baseURL with the mock's creds.
func configuredBridge(t *testing.T, baseURL string) *bridge {
	t.Helper()
	b := newBridge()
	cfg, _ := json.Marshal(adapterplugin.ConfigureParams{Config: map[string]any{
		"instance_url": baseURL, "username": "admin", "password": "secret",
	}})
	if _, werr := b.configure(cfg); werr != nil {
		t.Fatalf("configure: %v", werr)
	}
	return b
}

func dispatch(t *testing.T, b *bridge, op string, params map[string]any, idem string) action.WriteResult {
	t.Helper()
	raw, _ := json.Marshal(adapterplugin.DispatchParams{Operation: op, Params: params, IdempotencyKey: idem})
	res, werr := b.dispatch(context.Background(), raw)
	if werr != nil {
		t.Fatalf("dispatch %s wire error: %v", op, werr)
	}
	wr, ok := res.(action.WriteResult)
	if !ok {
		t.Fatalf("dispatch result type %T", res)
	}
	return wr
}

// TestCreateIncidentIdempotent: the first create opens INC…001; a second create
// with the same idempotency key dedups via correlation_id and returns the same
// number WITHOUT a second POST (08 §6a — a retry never opens a duplicate).
func TestCreateIncidentIdempotent(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	params := map[string]any{"assignment_group": "SOC-Tier3", "short_description": "Contain WIN-FILE01"}

	first := dispatch(t, b, "create_incident", params, "idem-A")
	if first.FinalOutcome != action.OutcomeSucceeded || first.PerTargetResults["SOC-Tier3"] != action.TargetOK {
		t.Fatalf("first create = %+v, want SUCCEEDED/OK", first)
	}
	if first.RawResponseRef != "INC0000001" {
		t.Errorf("created number = %q, want INC0000001", first.RawResponseRef)
	}

	second := dispatch(t, b, "create_incident", params, "idem-A")
	if second.RawResponseRef != "INC0000001" {
		t.Errorf("dedup number = %q, want the original INC0000001", second.RawResponseRef)
	}
	if m.postCount != 1 {
		t.Errorf("POST count = %d, want 1 (the retry must dedup, not re-create)", m.postCount)
	}
}

// TestProtocolParamsNeverReachServiceNow: the resolver injects action_type and
// resolved_identifier into dispatch params (protocol metadata); neither may be
// written as an incident field on create or PATCH.
func TestProtocolParamsNeverReachServiceNow(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	created := dispatch(t, b, "create_incident", map[string]any{
		"assignment_group": "SOC", "short_description": "x",
		"action_type": "ticket.create", "resolved_identifier": "SOC", // as injected by the resolver
	}, "k1")
	for _, k := range []string{"action_type", "resolved_identifier"} {
		if _, leaked := m.createBody[k]; leaked {
			t.Errorf("create body leaked protocol param %q: %+v", k, m.createBody)
		}
	}

	_ = dispatch(t, b, "add_comment", map[string]any{
		"ticket": created.RawResponseRef, "work_notes": "n",
		"action_type": "ticket.comment", "resolved_identifier": created.RawResponseRef,
	}, "k2")
	for _, k := range []string{"action_type", "resolved_identifier", "ticket"} {
		if _, leaked := m.patchBody[k]; leaked {
			t.Errorf("PATCH body leaked protocol param %q: %+v", k, m.patchBody)
		}
	}
}

// TestCommentResolvesNumber: ticket.comment targets a display number; the
// adapter resolves it to a sys_id (number→sys_id GET) then PATCHes work_notes.
func TestCommentResolvesNumber(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	created := dispatch(t, b, "create_incident", map[string]any{"assignment_group": "SOC", "short_description": "x"}, "k1")
	num := created.RawResponseRef // INC0000001

	res := dispatch(t, b, "add_comment", map[string]any{"ticket": num, "work_notes": "reimage scheduled"}, "k2")
	if res.FinalOutcome != action.OutcomeSucceeded {
		t.Fatalf("comment = %+v, want SUCCEEDED", res)
	}
	if m.patchBody["work_notes"] != "reimage scheduled" {
		t.Errorf("PATCH body = %+v, want work_notes set", m.patchBody)
	}
	if _, addressed := m.patchBody["ticket"]; addressed {
		t.Error("the addressing key 'ticket' leaked into the PATCH body")
	}
}

// TestCloseSetsStateAndCode: ticket.close stamps state 7 and a default close
// code even when the caller supplied only notes.
func TestCloseSetsStateAndCode(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	created := dispatch(t, b, "create_incident", map[string]any{"assignment_group": "SOC", "short_description": "x"}, "k1")

	res := dispatch(t, b, "close_incident", map[string]any{"ticket": created.RawResponseRef, "close_notes": "resolved"}, "k2")
	if res.FinalOutcome != action.OutcomeSucceeded {
		t.Fatalf("close = %+v, want SUCCEEDED", res)
	}
	if m.patchBody["state"] != stateClosed {
		t.Errorf("close state = %v, want %s", m.patchBody["state"], stateClosed)
	}
	if m.patchBody["close_code"] != defaultCloseCode {
		t.Errorf("close_code = %v, want the default", m.patchBody["close_code"])
	}
	if m.patchBody["close_notes"] != "resolved" {
		t.Errorf("close_notes = %v, want 'resolved'", m.patchBody["close_notes"])
	}
}

// TestTransitionAcceptsSysID: a 32-hex sys_id is used directly — no number
// lookup — and set_state writes the target state.
func TestTransitionAcceptsSysID(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	_ = dispatch(t, b, "create_incident", map[string]any{"assignment_group": "SOC", "short_description": "x"}, "k1")
	sysID := fmt.Sprintf("%032x", 1) // the first created record's sys_id

	res := dispatch(t, b, "set_state", map[string]any{"ticket": sysID, "state": "2"}, "k2")
	if res.FinalOutcome != action.OutcomeSucceeded {
		t.Fatalf("transition = %+v, want SUCCEEDED", res)
	}
	if m.patchBody["state"] != "2" {
		t.Errorf("state = %v, want 2", m.patchBody["state"])
	}
}

// TestCommentTargetNotFound: an unknown incident number resolves to nothing →
// FAIL, FATAL (08 §6c: target-not-found is definitive, never retried).
func TestCommentTargetNotFound(t *testing.T) {
	m := newSNowMock()
	b := configuredBridge(t, m.server(t).URL)
	res := dispatch(t, b, "add_comment", map[string]any{"ticket": "INC9999999", "work_notes": "x"}, "k")
	if res.FinalOutcome != action.OutcomeFailed || res.PerTargetResults["INC9999999"] != action.TargetFail {
		t.Fatalf("result = %+v, want FAILED/FAIL", res)
	}
	if res.ErrorClass != action.WriteFatal {
		t.Errorf("class = %s, want FATAL", res.ErrorClass)
	}
}

// TestAuthFailureIsFatal: bad credentials → 401 on the create → FAIL, FATAL.
func TestAuthFailureIsFatal(t *testing.T) {
	m := newSNowMock()
	b := newBridge()
	cfg, _ := json.Marshal(adapterplugin.ConfigureParams{Config: map[string]any{
		"instance_url": m.server(t).URL, "username": "admin", "password": "WRONG",
	}})
	if _, werr := b.configure(cfg); werr != nil {
		t.Fatalf("configure: %v", werr)
	}
	res := dispatch(t, b, "create_incident", map[string]any{"assignment_group": "SOC", "short_description": "x"}, "k")
	if res.FinalOutcome != action.OutcomeFailed || res.ErrorClass != action.WriteFatal {
		t.Fatalf("result = %+v, want FAILED/FATAL on 401", res)
	}
}

// TestTransportAmbiguityUnknown: the instance is unreachable → per-target
// UNKNOWN, retryable — never an inferred failure (08 §6c).
func TestTransportAmbiguityUnknown(t *testing.T) {
	m := newSNowMock()
	srv := m.server(t)
	url := srv.URL
	srv.Close() // unreachable
	b := configuredBridge(t, url)
	res := dispatch(t, b, "create_incident", map[string]any{"assignment_group": "SOC", "short_description": "x"}, "k")
	if res.PerTargetResults["SOC"] != action.TargetUnknown || res.ErrorClass != action.WriteRetryable {
		t.Fatalf("result = %+v, want UNKNOWN/RETRYABLE", res)
	}
}

// --- read side (03 §2.9) ----------------------------------------------------

// caseServer is a read-only Table API double serving the case fields the read
// verbs fetch. It returns a fixed record on the get-by-sys_id path and a fixed
// list on the query path, resolving INC0010042→its sys_id for the number path.
func caseServer(t *testing.T) *httptest.Server {
	t.Helper()
	const sysID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rec := func(num, short string) string {
		return fmt.Sprintf(`{"sys_id":%q,"number":%q,"short_description":%q,"description":"full body",`+
			`"state":"In Progress","priority":"2 - High","assignment_group":"SOC-Tier3","opened_at":"2026-04-20 14:32:11"}`,
			sysID, num, short)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"User Not Authenticated"},"status":"failure"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// get-by-sys_id: /api/now/table/incident/<sysid>
		if id := strings.TrimPrefix(r.URL.Path, "/api/now/table/incident/"); id != r.URL.Path && id != "" {
			_, _ = fmt.Fprintf(w, `{"result":%s}`, rec("INC0010042", "Suspicious RDP"))
			return
		}
		// number-resolution (findOne) returns the minimal record shape.
		q := r.URL.Query().Get("sysparm_query")
		if v, ok := strings.CutPrefix(q, "number="); ok {
			_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":%q,"number":%q,"state":"In Progress"}]}`, sysID, v)
			return
		}
		// query_incidents: return a two-record list.
		_, _ = fmt.Fprintf(w, `{"result":[%s,%s]}`, rec("INC0010042", "Suspicious RDP"), rec("INC0010043", "Odd logon"))
	}
	srv := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(srv.Close)
	return srv
}

// TestGetIncidentByID: a sys_id is fetched directly (no number lookup) and the
// full case fields come back as a map.
func TestGetIncidentByID(t *testing.T) {
	srv := caseServer(t)
	c := newSnowClient(srv.URL, "admin", "secret", "incident")
	rec, status, err := c.getIncidentByID(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("getIncidentByID: %v (status %d)", err, status)
	}
	if rec["short_description"] != "Suspicious RDP" {
		t.Errorf("short_description = %v, want 'Suspicious RDP'", rec["short_description"])
	}
	if rec["state"] != "In Progress" {
		t.Errorf("state = %v, want display label 'In Progress'", rec["state"])
	}
}

// TestQueryIncidents: the query path returns the full list with case fields.
func TestQueryIncidents(t *testing.T) {
	srv := caseServer(t)
	c := newSnowClient(srv.URL, "admin", "secret", "incident")
	recs, status, err := c.queryIncidents(context.Background(), "short_descriptionLIKErdp", 50)
	if err != nil {
		t.Fatalf("queryIncidents: %v (status %d)", err, status)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0]["number"] != "INC0010042" || recs[0]["assignment_group"] != "SOC-Tier3" {
		t.Errorf("first record = %+v, want number/assignment_group set", recs[0])
	}
}

// assertOCSF is check 3 of adr/0356 for this adapter: the event holds only
// fields OCSF defines for its class at the version it declares, each in the
// shape and scalar type the schema gives it.
func assertOCSF(t *testing.T, classUID int, raw map[string]any) {
	t.Helper()
	if got := raw["class_uid"]; got != classUID {
		t.Errorf("sent as class %d, and the event says %v", classUID, got)
	}
	if meta, _ := raw["metadata"].(map[string]any); meta["version"] != ocsfVersion {
		t.Errorf("metadata.version = %v; want %s", meta["version"], ocsfVersion)
	}
	problems := ocsfschema.ValidateEvent(raw)
	if _, ok := raw["time"]; !ok {
		// OCSF requires a time. A record with no opened_at gave none, and
		// the bridge does not make one up (ocsf.SetTime).
		problems = ocsfschema.WithoutRequired(problems, "time")
	}
	for _, p := range problems {
		t.Errorf("class %d: %s", classUID, p)
	}
}

// firstFinding is the first entry of an event's finding_info_list[].
func firstFinding(t *testing.T, raw map[string]any) map[string]any {
	t.Helper()
	list, _ := raw["finding_info_list"].([]any)
	if len(list) != 1 {
		t.Fatalf("finding_info_list = %v; want one entry", raw["finding_info_list"])
	}
	fi, _ := list[0].(map[string]any)
	return fi
}

// TestCaseToOcsf: a Table API record shapes into an OCSF Incident Finding
// (2005) with the fields the engine's incident normalizer reads (adr/0356):
// the case in finding_info_list[], the group in assignee_group, the link
// back in src_url, built from the sys_id, and the system of record's name in
// unmapped.sor.
func TestCaseToOcsf(t *testing.T) {
	b := configuredBridge(t, "https://acme.service-now.com")
	p := b.caseToOcsf(map[string]any{
		"number": "INC0010042", "sys_id": "sys123", "short_description": "Suspicious RDP",
		"description": "full body", "state": "In Progress", "priority": "2 - High",
		"assignment_group": "SOC-Tier3", "opened_at": "2026-04-20 14:32:11",
	})
	if p.ClassUID != 2005 {
		t.Errorf("class_uid = %d, want 2005", p.ClassUID)
	}
	fi := firstFinding(t, p.Raw)
	if fi["title"] != "Suspicious RDP" || fi["uid"] != "INC0010042" || fi["desc"] != "full body" {
		t.Errorf("finding_info_list[0] = %+v, want uid/title/desc set", fi)
	}
	if p.Time.IsZero() {
		t.Error("opened_at did not parse into Time")
	}
	// The event's own time is OCSF's epoch milliseconds, with the same
	// instant as an RFC 3339 string beside it (adr/0356).
	if p.Raw["time"] != int64(1776695531000) || p.Raw["time_dt"] != "2026-04-20T14:32:11Z" {
		t.Errorf("time = %v, time_dt = %v; want 1776695531000 and 2026-04-20T14:32:11Z", p.Raw["time"], p.Raw["time_dt"])
	}
	if group, _ := p.Raw["assignee_group"].(map[string]any); group["name"] != "SOC-Tier3" || len(group) != 1 {
		t.Errorf("assignee_group = %v; want the group by name", p.Raw["assignee_group"])
	}
	if link, _ := p.Raw["src_url"].(string); link != "https://acme.service-now.com/nav_to.do?uri=incident.do?sys_id=sys123" {
		t.Errorf("src_url = %v, want the sys_id deep-link", p.Raw["src_url"])
	}
	if p.Raw["status"] != "In Progress" || p.Raw["priority"] != "2 - High" {
		t.Errorf("status %v, priority %v", p.Raw["status"], p.Raw["priority"])
	}
	um, _ := p.Raw["unmapped"].(map[string]any)
	if um["sor"] != "servicenow" || um["sys_id"] != "sys123" || len(um) != 2 {
		t.Errorf("unmapped = %v; want the system of record and the sys_id", um)
	}
	for _, k := range []string{"finding_info", "assignment_group"} {
		if _, has := p.Raw[k]; has {
			t.Errorf("the event carries %s, which OCSF does not define", k)
		}
	}
	assertOCSF(t, p.ClassUID, p.Raw)

	// The engine's incident normalizer reads every field.
	res, err := capability.NewRegistry(identity.NewResolver(uuid.MustParse("33333333-3333-4333-8333-333333333333"))).
		Normalize(capability.OcsfEvent{ID: uuid.Must(uuid.NewV7()), ClassUID: p.ClassUID, Time: p.Time, SourceTool: "servicenow", Payload: p.Raw})
	if err != nil {
		t.Fatal(err)
	}
	ext := res.ObservedData[0].Extensions
	for k, want := range map[string]any{
		"case_number": "INC0010042", "title": "Suspicious RDP", "summary": "full body", "status": "In Progress", "priority": "2 - High",
		"assignment_group": "SOC-Tier3", "link": "https://acme.service-now.com/nav_to.do?uri=incident.do?sys_id=sys123", "sor": "servicenow",
	} {
		if ext[k] != want {
			t.Errorf("the engine reads %s = %v; want %v", k, ext[k], want)
		}
	}

	// A record with only a number leaves the empty fields out.
	p = b.caseToOcsf(map[string]any{"number": "INC1"})
	for _, k := range []string{"status", "priority", "assignee_group", "src_url", "time", "time_dt"} {
		if _, has := p.Raw[k]; has {
			t.Errorf("an empty %s was written: %v", k, p.Raw[k])
		}
	}
	assertOCSF(t, p.ClassUID, p.Raw)
}

// TestInvokeGetIncident: the invoke routing shapes one case into a class-2005
// event tagged with the servicenow source_tool.
func TestInvokeGetIncident(t *testing.T) {
	srv := caseServer(t)
	b := configuredBridge(t, srv.URL)
	raw, _ := json.Marshal(adapterplugin.InvokeParams{
		Operation: "get_incident", Params: map[string]any{"case_id": "INC0010042"},
	})
	res, werr := b.invoke(context.Background(), raw)
	if werr != nil {
		t.Fatalf("invoke get_incident: %v", werr)
	}
	ir, ok := res.(invokeResult)
	if !ok {
		t.Fatalf("invoke result type %T", res)
	}
	if ir.SourceTool != "servicenow" || len(ir.Events) != 1 {
		t.Fatalf("result = %+v, want servicenow/1 event", ir)
	}
	if ir.Events[0].ClassUID != 2005 {
		t.Errorf("event class_uid = %d, want 2005", ir.Events[0].ClassUID)
	}
	assertOCSF(t, ir.Events[0].ClassUID, ir.Events[0].Raw)
}

// TestInvokeQueryIncidents: the query path returns all matching cases as events.
func TestInvokeQueryIncidents(t *testing.T) {
	srv := caseServer(t)
	b := configuredBridge(t, srv.URL)
	raw, _ := json.Marshal(adapterplugin.InvokeParams{
		Operation: "query_incidents", Params: map[string]any{"filter": "rdp", "limit": float64(10)},
	})
	res, werr := b.invoke(context.Background(), raw)
	if werr != nil {
		t.Fatalf("invoke query_incidents: %v", werr)
	}
	ir := res.(invokeResult)
	if len(ir.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(ir.Events))
	}
	for _, e := range ir.Events {
		assertOCSF(t, e.ClassUID, e.Raw)
	}
}

// TestInvokeQueryIncidentsQueue: the queue input (14 §4.2) becomes an
// assignment-group clause by display name, alongside the free-text clause.
func TestInvokeQueryIncidentsQueue(t *testing.T) {
	var gotQuery string
	h := func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("sysparm_query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"result":[{"sys_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","number":"INC1","short_description":"a","state":"New","assignment_group":"SOC Tier 2","opened_at":"2026-04-20 14:32:11"}]}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(srv.Close)

	b := configuredBridge(t, srv.URL)
	raw, _ := json.Marshal(adapterplugin.InvokeParams{
		Operation: "query_incidents", Params: map[string]any{"filter": "rdp", "queue": "SOC Tier 2"},
	})
	if _, werr := b.invoke(context.Background(), raw); werr != nil {
		t.Fatalf("invoke: %v", werr)
	}
	if gotQuery != "short_descriptionLIKErdp^assignment_group.name=SOC Tier 2" {
		t.Errorf("sysparm_query = %q; want the text clause and the group-name clause", gotQuery)
	}
}

// TestInvokeQueryIncidentsStatusFilter: the status filter runs client-side on
// the display label (ServiceNow's encoded query matches the stored numeric
// state, not the label), case-insensitively.
func TestInvokeQueryIncidentsStatusFilter(t *testing.T) {
	h := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"result":[`+
			`{"sys_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","number":"INC1","short_description":"a","state":"In Progress","opened_at":"2026-04-20 14:32:11"},`+
			`{"sys_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","number":"INC2","short_description":"b","state":"New","opened_at":"2026-04-20 14:32:11"}]}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(srv.Close)

	b := configuredBridge(t, srv.URL)
	raw, _ := json.Marshal(adapterplugin.InvokeParams{
		Operation: "query_incidents", Params: map[string]any{"status": "new"},
	})
	res, werr := b.invoke(context.Background(), raw)
	if werr != nil {
		t.Fatalf("invoke: %v", werr)
	}
	ir := res.(invokeResult)
	if len(ir.Events) != 1 {
		t.Fatalf("status filter kept %d events, want 1 (the New case)", len(ir.Events))
	}
	fi := firstFinding(t, ir.Events[0].Raw)
	if fi["uid"] != "INC2" {
		t.Errorf("kept case uid = %v, want INC2 (the one whose label matched 'new')", fi["uid"])
	}
}

// TestInvokeUnknownOperation: an unknown read op is FALLTHROUGH, not a crash.
func TestInvokeUnknownOperation(t *testing.T) {
	b := configuredBridge(t, "https://acme.service-now.com")
	raw, _ := json.Marshal(adapterplugin.InvokeParams{Operation: "delete_everything"})
	_, werr := b.invoke(context.Background(), raw)
	if werr == nil {
		t.Fatal("unknown read op should error")
	}
}

// TestUrgencyFor: the engine's vendor-neutral priority becomes a ServiceNow
// urgency label (adr/0127); anything else is the low default.
func TestUrgencyFor(t *testing.T) {
	for in, want := range map[string]string{"critical": "1 - High", "High": "1 - High", "medium": "2 - Medium", "low": "3 - Low", "": "3 - Low", "urgent!": "3 - Low"} {
		if got := urgencyFor(in); got != want {
			t.Errorf("urgencyFor(%q) = %q; want %q", in, got, want)
		}
	}
}
