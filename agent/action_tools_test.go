package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/internal/catalog"
)

// builtInCatalog is the built-in action catalog as GET /api/action-types
// serves it: sorted by action type, every type available, so every type gets
// a tool.
func builtInCatalog(t *testing.T) []ActionType {
	t.Helper()
	cat := defaultActionCatalog(t)
	var out []ActionType
	for _, at := range cat.ActionTypes() {
		d, _ := cat.Descriptor(at)
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var a ActionType
		if err := json.Unmarshal(raw, &a.Descriptor); err != nil {
			t.Fatal(err)
		}
		a.Status = "available"
		out = append(out, a)
	}
	return out
}

// actionToolsOf builds the action tools of a catalog, by name.
func actionToolsOf(t *testing.T, catalog []ActionType) map[string]tool {
	t.Helper()
	tools := actionTools(catalog)
	byName := map[string]tool{}
	for _, tl := range tools {
		byName[tl.def.Name] = tl
	}
	return byName
}

// TestActionToolName: an action type's tool is "request_" and the type with
// each "." written as "_".
func TestActionToolName(t *testing.T) {
	if got := actionToolName("ticket.create"); got != "request_ticket_create" {
		t.Errorf("actionToolName(ticket.create) = %q", got)
	}
}

// TestActionTools_BuiltInCatalog: every built-in type has a tool whose
// properties are its non-entity inputs and the shared fields, required as
// the descriptor requires them. No schema uses a union, a null type or a
// closed object, and no tool is strict (adr/0380).
func TestActionTools_BuiltInCatalog(t *testing.T) {
	catalog := builtInCatalog(t)
	tools := actionToolsOf(t, catalog)
	if len(tools) != len(catalog) {
		t.Fatalf("%d tools for %d types", len(tools), len(catalog))
	}
	for _, a := range catalog {
		tl, ok := tools[actionToolName(a.Descriptor.ActionType)]
		if !ok {
			t.Errorf("%s has no tool", a.Descriptor.ActionType)
			continue
		}
		props := tl.def.InputSchema["properties"].(map[string]any)
		want := []string{"targets", "rationale", "evidence_refs", "retry_of"}
		wantRequired := []string{"targets", "rationale", "evidence_refs"}
		for _, in := range a.Descriptor.Inputs {
			if in.Type == "entity" {
				continue
			}
			want = append(want, in.Name)
			if in.Required {
				wantRequired = append(wantRequired, in.Name)
			}
		}
		var got []string
		for name := range props {
			got = append(got, name)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s properties = %v; want %v", tl.def.Name, got, want)
		}
		if req := tl.def.InputSchema["required"]; !reflect.DeepEqual(req, wantRequired) {
			t.Errorf("%s required = %v; want %v", tl.def.Name, req, wantRequired)
		}
		raw, err := json.Marshal(tl.def)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{`"anyOf"`, `"null"`, `"additionalProperties"`, `"strict"`, `"parameters"`, `"action_type"`} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s carries %s: %s", tl.def.Name, banned, raw)
			}
		}
	}
}

// TestActionTools_EveryTypeHasATool: every type of the catalog has a tool,
// unavailable included (adr/0380). The degraded one says it is degraded, and
// the unavailable one says it cannot be carried out in this install.
func TestActionTools_EveryTypeHasATool(t *testing.T) {
	tools := actionToolsOf(t, exampleCatalog())
	if len(tools) != 3 {
		t.Fatalf("tools = %v; want host.isolate, ioc.block and ticket.create", tools)
	}
	isolate, block, ticket := tools["request_host_isolate"].def, tools["request_ioc_block"].def, tools["request_ticket_create"].def
	if strings.Contains(isolate.Description, "Degraded") || !strings.Contains(ticket.Description, "Degraded") {
		t.Errorf("degraded is not said of the degraded type alone:\n%s\n%s", isolate.Description, ticket.Description)
	}
	if !strings.Contains(block.Description, textActionToolUnavailable.Text) || strings.Contains(block.Description, textActionToolGuidance.Text) {
		t.Errorf("the unavailable type's description lacks the unavailable sentence or carries the shared guidance:\n%s", block.Description)
	}
	for _, d := range []ToolDef{isolate, ticket} {
		if strings.Contains(d.Description, textActionToolUnavailable.Text) {
			t.Errorf("%s says it is unavailable:\n%s", d.Name, d.Description)
		}
	}
	if !strings.HasPrefix(block.Description, "Block an indicator. Tier T2, BEST_EFFORT.") {
		t.Errorf("the unavailable type does not open with its intent, tier and reversibility:\n%s", block.Description)
	}
	requireLedgerText(t, "action tool", ticket.Description, "Open a ticket.", "T1", "IRREVERSIBLE")
	requireLedgerText(t, "unavailable action tool", block.Description, "Block an indicator.", "T2", "BEST_EFFORT")
}

// TestRequestAction_UnavailableReachesTheEngine: a call to an unavailable
// type's tool goes to the backend like any other, and the engine's refusal
// reaches the model as the tool's error, as it stands (adr/0380).
func TestRequestAction_UnavailableReachesTheEngine(t *testing.T) {
	f := newFakeBackend(t)
	const refusal = `{"error":"action type ioc.block is unavailable in this install and cannot be carried out"}`
	requests := 0
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/actions" {
			return false
		}
		requests++
		var body ActionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ActionType != "ioc.block" {
			t.Errorf("action_type = %q; want ioc.block", body.ActionType)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(refusal))
		return true
	}
	s := newToolSession(t, f)
	res, isErr := s.dispatchTool(context.Background(), "request_ioc_block", json.RawMessage(`{"targets":[{"entity_ref":"x","resolved_identifier":"1.2.3.4"}],"rationale":"r","evidence_refs":["e"]}`))
	if !isErr {
		t.Fatalf("the engine's refusal did not reach the model as an error: %s", res.Content)
	}
	if requests != 1 {
		t.Errorf("%d requests reached the backend; want 1", requests)
	}
	if !strings.Contains(string(res.Content), "unavailable in this install") {
		t.Errorf("result = %q; want the engine's refusal", res.Content)
	}
	if len(s.pendingActions) != 0 {
		t.Errorf("pending actions = %+v; want none", s.pendingActions)
	}
}

// TestActionShared_IsTheRequestFields (adr/0382): the fields every action tool
// carries are the ones the backend keeps input names off, so a descriptor it
// accepts never gives a tool one property for two things.
func TestActionShared_IsTheRequestFields(t *testing.T) {
	var got []string
	for name := range actionSharedFields {
		got = append(got, name)
	}
	want := catalog.ActionRequestFields()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("action tool shared fields = %v; catalog.ActionRequestFields = %v", got, want)
	}
}

// TestIntrinsicTools_AreTheBuiltinToolNames (adr/0382): the agent's built-in
// tools are the names the backend refuses a verb for.
func TestIntrinsicTools_AreTheBuiltinToolNames(t *testing.T) {
	var got []string
	for _, tl := range intrinsicTools() {
		got = append(got, tl.def.Name)
	}
	want := catalog.BuiltinToolNames()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("intrinsic tools = %v; catalog.BuiltinToolNames = %v", got, want)
	}
}

// TestResetContext_503KeepsTheActionTools: on a reset a fetch that answers 503
// on every attempt returns no catalog, and the session keeps the action tools
// and catalog it started with. Only at session start does a 503 mean no
// action tools.
func TestResetContext_503KeepsTheActionTools(t *testing.T) {
	f := newFakeBackend(t)
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{},
		InvestigationID: "inv-1", CatalogRetryPause: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	before := len(s.actionTypes)
	attempts := 0
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/action-types" {
			return false
		}
		attempts++
		http.Error(w, "off", http.StatusServiceUnavailable)
		return true
	}
	s.messages = []Message{{Role: RoleUser, Content: []ContentBlock{TextBlock("earlier")}}}
	if err := s.ResetContext(context.Background()); err != nil {
		t.Fatalf("ResetContext: %v", err)
	}
	if attempts != catalogAttempts {
		t.Errorf("attempts = %d; want %d", attempts, catalogAttempts)
	}
	if len(s.messages) != 0 {
		t.Errorf("messages not cleared: %d remain", len(s.messages))
	}
	if _, ok := s.toolsByName[isolateTool]; !ok {
		t.Error("the reset dropped the action tools on a 503")
	}
	if len(s.actionTypes) != before || before == 0 {
		t.Errorf("catalog = %d types; want the %d the session started with", len(s.actionTypes), before)
	}
}

// sentRequest dispatches one action tool call against the fake backend and
// returns the request the engine received.
func sentRequest(t *testing.T, f *fakeBackend, name, input string) ActionRequest {
	t.Helper()
	s := newToolSession(t, f)
	if res, isErr := s.dispatchTool(context.Background(), name, json.RawMessage(input)); isErr {
		t.Fatalf("refused: %s", res.Content)
	}
	calls := f.callsTo("/api/actions")
	if len(calls) != 1 {
		t.Fatalf("%d calls", len(calls))
	}
	var body ActionRequest
	if err := json.Unmarshal(calls[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// ticketBackend is the fake backend with the built-in ticket.create in its
// catalog.
func ticketBackend(t *testing.T) *fakeBackend {
	t.Helper()
	f := newFakeBackend(t)
	var ticket ActionType
	for _, a := range builtInCatalog(t) {
		if a.Descriptor.ActionType == "ticket.create" {
			ticket = a
		}
	}
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/action-types" {
			return false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"action_types": []ActionType{ticket}})
		return true
	}
	return f
}

// TestRequestAction_BuildsTheSameRequest: the tool's top-level inputs become
// parameters, the action type comes from the tool, and the shared fields ride
// the request beside them.
func TestRequestAction_BuildsTheSameRequest(t *testing.T) {
	body := sentRequest(t, ticketBackend(t), "request_ticket_create", `{"rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],"summary":"s","description":"d","retry_of":"act-1"}`)
	if body.ActionType != "ticket.create" || body.Rationale != "r" || body.RetryOf != "act-1" ||
		!slices.Equal(body.EvidenceRefs, []string{"e"}) || len(body.Targets) != 1 || body.InvestigationRef != "inv-1" {
		t.Errorf("request = %+v", body)
	}
	var params map[string]any
	if err := json.Unmarshal(body.Parameters, &params); err != nil {
		t.Fatalf("parameters = %s: %v", body.Parameters, err)
	}
	if !reflect.DeepEqual(params, map[string]any{"summary": "s", "description": "d"}) {
		t.Errorf("parameters = %s; want the type's inputs alone", body.Parameters)
	}
	if err := action.ValidateParameters(mustDescriptor(t, "ticket.create"), body.Parameters); err != nil {
		t.Errorf("the engine would refuse it: %v", err)
	}
}

// paramKeys is the sorted keys of a request's parameters.
func paramKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// TestRequestAction_NullRequiredReachesTheEngine: a null for a required input
// stays in the request, so the engine refuses it as missing. The nulls for
// declared optional inputs are dropped, and a null retry_of is no retry.
func TestRequestAction_NullRequiredReachesTheEngine(t *testing.T) {
	body := sentRequest(t, ticketBackend(t), "request_ticket_create", `{"rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],"summary":null,"description":"d",
		"issue_type":null,"assignee":null,"retry_of":null}`)
	if keys := paramKeys(t, body.Parameters); !slices.Equal(keys, []string{"description", "summary"}) {
		t.Errorf("parameters = %s; want the null optional inputs dropped and the null summary kept", body.Parameters)
	}
	if body.RetryOf != "" {
		t.Errorf("retry_of = %q; want none", body.RetryOf)
	}
	err := action.ValidateParameters(mustDescriptor(t, "ticket.create"), body.Parameters)
	if err == nil || !strings.Contains(err.Error(), "missing required parameter(s): summary") {
		t.Errorf("engine = %v; want summary refused as missing", err)
	}
}

// TestRequestAction_UndeclaredKeyIsSent: a key the type does not declare goes
// into the request as sent, so the engine refuses it as unknown.
func TestRequestAction_UndeclaredKeyIsSent(t *testing.T) {
	body := sentRequest(t, ticketBackend(t), "request_ticket_create", `{"rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],"summary":"s","title":"t"}`)
	if keys := paramKeys(t, body.Parameters); !slices.Equal(keys, []string{"summary", "title"}) {
		t.Errorf("parameters = %s; want the undeclared title kept", body.Parameters)
	}
	err := action.ValidateParameters(mustDescriptor(t, "ticket.create"), body.Parameters)
	if err == nil || !strings.Contains(err.Error(), `unknown parameter "title"`) {
		t.Errorf("engine = %v; want title refused as unknown", err)
	}
}

// TestRequestAction_UndeclaredKeyReachesTheEngine: a key the type does not
// declare is not dropped by the loop; the engine refuses it as unknown, and
// the refusal reaches the model as the tool's error.
func TestRequestAction_UndeclaredKeyReachesTheEngine(t *testing.T) {
	f := ticketBackend(t)
	ticket := mustDescriptor(t, "ticket.create")
	inner := f.override
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/actions" {
			return inner(w, r)
		}
		var body ActionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := action.ValidateParameters(ticket, body.Parameters); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return true
		}
		t.Errorf("the engine accepted parameters %s", body.Parameters)
		return true
	}
	s := newToolSession(t, f)
	res, isErr := s.dispatchTool(context.Background(), "request_ticket_create", json.RawMessage(`{"rationale":"r",
		"evidence_refs":["e"],"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],"summary":"s","parameters":{"summary":"s"}}`))
	if !isErr || !strings.Contains(string(res.Content), `unknown parameter \"parameters\"`) {
		t.Errorf("result = %s (error %v); want the engine's refusal of the undeclared key", res.Content, isErr)
	}
}

func mustDescriptor(t *testing.T, actionType string) action.ActionDescriptor {
	t.Helper()
	d, ok := defaultActionCatalog(t).Descriptor(actionType)
	if !ok {
		t.Fatalf("no descriptor for %s", actionType)
	}
	return d
}

// TestSession_CatalogFetch: the session start tries the catalog fetch up to
// three times. A 503 then a 200 gives the action tools; a 503 that survives
// the attempts means no action tools; a 500 that
// survives fails the start; any other 4xx fails at once, after one attempt.
func TestSession_CatalogFetch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		statuses  []int // the answers in order; past the end, the canned 200
		wantErr   bool
		wantTools bool
		attempts  int
	}{
		{"503 then 200", []int{503}, false, true, 2},
		{"three 503s", []int{503, 503, 503}, false, false, 3},
		{"three 500s", []int{500, 500, 500}, true, false, 3},
		{"403", []int{403}, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeBackend(t)
			attempts := 0
			f.override = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/api/action-types" {
					return false
				}
				attempts++
				if attempts > len(tc.statuses) {
					return false
				}
				http.Error(w, "no", tc.statuses[attempts-1])
				return true
			}
			s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{},
				InvestigationID: "inv-1", CatalogRetryPause: time.Nanosecond})
			if attempts != tc.attempts {
				t.Errorf("attempts = %d; want %d", attempts, tc.attempts)
			}
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "list action types") {
					t.Errorf("err = %v; want the session start to fail", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, hasAction := s.toolsByName[isolateTool]; hasAction != tc.wantTools {
				t.Errorf("action tool %v; want %v", hasAction, tc.wantTools)
			}
		})
	}
}

// TestActionTools_ListInput: an action input renders through the verb tools'
// scalar schema, so a list<string> input is an array of strings.
func TestActionTools_ListInput(t *testing.T) {
	var a ActionType
	a.Status = "available"
	a.Descriptor.ActionType = "x.act"
	a.Descriptor.Inputs = []ActionInput{{Name: "recipients", Type: "list<string>", Desc: "who"}}
	def := actionToolsOf(t, []ActionType{a})["request_x_act"].def
	got := def.InputSchema["properties"].(map[string]any)["recipients"]
	want := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "who"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recipients = %v; want %v", got, want)
	}
}

// TestOfferedTools: the session's exposed list is the tools it sends the
// model, by name and in order, each with the kind it was built as: a verb as
// a verb even when its name starts with "request_", an intrinsic tool, and an
// action tool with its action type, an unavailable type's included
// (adr/0380). A name the session never built is not in it.
func TestOfferedTools(t *testing.T) {
	f := newFakeBackend(t)
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/capabilities" {
			return false
		}
		_, _ = w.Write([]byte(`{"capabilities":[{"descriptor":{"verb":"request_logs","intent":"Fetch logs.","inputs":[],"output":"x"},"status":"available","bindings":1}]}`))
		return true
	}
	llm := &scriptedLLM{script: []CompleteResponse{{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock("done")}}}}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Turn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	sent := llm.requests[0].Tools
	offered := s.OfferedTools()
	if len(offered) != len(sent) {
		t.Fatalf("offered %d tools; the request sent %d", len(offered), len(sent))
	}
	byName := map[string]OfferedTool{}
	for i, o := range offered {
		if o.Name != sent[i].Name {
			t.Errorf("offered[%d] = %s; the request sent %s", i, o.Name, sent[i].Name)
		}
		byName[o.Name] = o
	}
	for name, want := range map[string]OfferedTool{
		"request_logs":  {Name: "request_logs", Kind: ToolKindVerb},
		ToolListActions: {Name: ToolListActions, Kind: ToolKindIntrinsic},
		isolateTool:     {Name: isolateTool, Kind: ToolKindAction, ActionType: "host.isolate"},
		// An unavailable type's tool is offered; the engine refuses its requests.
		"request_ioc_block": {Name: "request_ioc_block", Kind: ToolKindAction, ActionType: "ioc.block"},
	} {
		if got := byName[name]; got != want {
			t.Errorf("%s = %+v; want %+v", name, got, want)
		}
	}
	for _, name := range []string{"request_firewall_block_ip", "make_coffee"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s is in the list; the session never built it", name)
		}
	}
}

// TestResetContext_CatalogFailureKeepsTheActionTools: a catalog fetch that
// fails on a reset does not block it. The reset clears the context and the
// session keeps the action tools it had; a failure stops a session only at
// its start.
func TestResetContext_CatalogFailureKeepsTheActionTools(t *testing.T) {
	f := newFakeBackend(t)
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{},
		InvestigationID: "inv-1", CatalogRetryPause: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/action-types" {
			return false
		}
		http.Error(w, "down", http.StatusInternalServerError)
		return true
	}
	s.messages = []Message{{Role: RoleUser, Content: []ContentBlock{TextBlock("earlier")}}}
	if err := s.ResetContext(context.Background()); err != nil {
		t.Fatalf("ResetContext: %v", err)
	}
	if len(s.messages) != 0 {
		t.Errorf("messages not cleared: %d remain", len(s.messages))
	}
	if _, ok := s.toolsByName[isolateTool]; !ok {
		t.Error("the reset dropped the action tools the session had")
	}
}

// defaultActionCatalog is DefaultActionCatalog for a test: it fails the test
// when the catalog is refused.
func defaultActionCatalog(t testing.TB) *action.ActionCatalog {
	t.Helper()
	c, err := action.DefaultActionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
