package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/action"
)

// builtInCatalog is the built-in action catalog as GET /api/action-types
// serves it: sorted by action type, every type available, so every type gets
// a parameters shape.
func builtInCatalog(t *testing.T) []ActionType {
	t.Helper()
	cat := action.DefaultActionCatalog()
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

func requestActionTool(t *testing.T, actionTypes []ActionType) ToolDef {
	t.Helper()
	for _, tl := range intrinsicTools(actionTypes) {
		if tl.def.Name == ToolRequestAction {
			return tl.def
		}
	}
	t.Fatal("no request_action tool")
	return ToolDef{}
}

// TestRequestActionTool_BuiltInCatalogFitsStrict: the built-in catalog is sent
// strict, inside the API's limits (adr/0376). It uses 12 union parameters:
// ten nullable inputs, retry_of and parameters. A catalog change that crosses
// a limit turns strict mode off without an error, so this test is the alarm.
func TestRequestActionTool_BuiltInCatalogFitsStrict(t *testing.T) {
	def := requestActionTool(t, builtInCatalog(t))
	if !def.Strict {
		t.Fatal("request_action is not strict for the built-in catalog")
	}
	if unions := strictUnions(def.InputSchema); unions != 12 {
		t.Errorf("unions = %d; want 12", unions)
	}
	want := []string{"action_type", "targets", "rationale", "evidence_refs", "parameters", "retry_of"}
	if got := def.InputSchema["required"]; !reflect.DeepEqual(got, want) {
		t.Errorf("required = %v; want %v", got, want)
	}
	retry := def.InputSchema["properties"].(map[string]any)["retry_of"].(map[string]any)
	if !reflect.DeepEqual(retry["type"], []string{"string", "null"}) {
		t.Errorf("retry_of type = %v; want string or null", retry["type"])
	}
}

// TestRequestActionTool_NoCatalogIsNotStrict: with no catalog the loop knows no
// parameter keys, so parameters stays an open object and the tool is not
// strict.
func TestRequestActionTool_NoCatalogIsNotStrict(t *testing.T) {
	if def := requestActionTool(t, nil); def.Strict {
		t.Error("request_action is strict with no catalog")
	}
	for _, tl := range intrinsicTools(exampleCatalog()) {
		if tl.def.Strict != (tl.def.Name == ToolRequestAction) {
			t.Errorf("%s: strict = %v; only request_action is strict", tl.def.Name, tl.def.Strict)
		}
	}
}

// TestRequestActionTool_PastTheLimitIsNotStrict: a catalog past the union limit
// is sent with the same schema and no strict flag, because the API would
// refuse every request of the session.
func TestRequestActionTool_PastTheLimitIsNotStrict(t *testing.T) {
	var cat []ActionType
	for i := range strictUnionLimit {
		var a ActionType
		a.Status = "available"
		a.Descriptor.ActionType = fmt.Sprintf("x.act%02d", i)
		a.Descriptor.Inputs = []ActionInput{{Name: fmt.Sprintf("p%02d", i), Type: "string"}}
		cat = append(cat, a)
	}
	def := requestActionTool(t, cat)
	if def.Strict {
		t.Fatal("request_action is strict past the union limit")
	}
	if unions := strictUnions(def.InputSchema); unions <= strictUnionLimit {
		t.Fatalf("the catalog is not past the limit: %d unions", unions)
	}
	// The schema is the strict one all the same: closed and with every key
	// required, so building it checked it.
	if def.InputSchema["additionalProperties"] != false {
		t.Error("the fallback schema is not the strict schema")
	}
	checkStrict(ToolRequestAction, def.InputSchema)
}

// TestParametersSchema_OneShapePerType: each requestable type has one closed
// shape that requires all its inputs, listed in required in descriptor order,
// the optional ones nullable. Types with no non-entity inputs share one empty shape, placed
// last. An unavailable type has no shape.
func TestParametersSchema_OneShapePerType(t *testing.T) {
	schema := parametersSchema(exampleCatalog())
	shapes, ok := schema["anyOf"].([]any)
	if !ok || len(shapes) != 2 {
		t.Fatalf("parameters = %+v; want two shapes", schema)
	}
	isolate := shapes[0].(map[string]any)
	if !reflect.DeepEqual(isolate["required"], []string{"reason"}) || isolate["additionalProperties"] != false {
		t.Errorf("host.isolate shape = %+v", isolate)
	}
	if d, _ := isolate["description"].(string); !strings.Contains(d, "host.isolate") {
		t.Errorf("shape description %q does not name its action type", d)
	}
	ticket := shapes[1].(map[string]any)
	if !reflect.DeepEqual(ticket["required"], []string{"summary", "reason", "priority"}) {
		t.Errorf("ticket.create required = %v; want descriptor order in required", ticket["required"])
	}
	props := ticket["properties"].(map[string]any)
	if got := props["summary"].(map[string]any); got["type"] != "string" || got["description"] != "headline" {
		t.Errorf("summary = %+v; want a plain string with the descriptor's description", got)
	}
	if got := props["priority"].(map[string]any)["type"]; !reflect.DeepEqual(got, []string{"integer", "null"}) {
		t.Errorf("priority type = %v; want integer or null", got)
	}

	// Entity-only types share one empty closed shape, placed last.
	var host, acct ActionType
	host.Status, acct.Status = "available", "available"
	host.Descriptor.ActionType, acct.Descriptor.ActionType = "host.isolate", "account.disable"
	host.Descriptor.Inputs = []ActionInput{{Name: "host", Type: "entity", Required: true}}
	acct.Descriptor.Inputs = []ActionInput{{Name: "account", Type: "entity", Required: true}}
	cat := []ActionType{exampleCatalog()[1], host, acct}
	shapes = parametersSchema(cat)["anyOf"].([]any)
	if len(shapes) != 2 {
		t.Fatalf("shapes = %d; want ticket.create and one shared empty shape", len(shapes))
	}
	empty := shapes[1].(map[string]any)
	if len(empty["properties"].(map[string]any)) != 0 || empty["additionalProperties"] != false {
		t.Errorf("empty shape = %+v", empty)
	}
	if d, _ := empty["description"].(string); !strings.Contains(d, "host.isolate") || !strings.Contains(d, "account.disable") {
		t.Errorf("empty shape description %q does not name its action types", d)
	}

	// One shape alone is the parameters schema itself, without anyOf.
	single := parametersSchema(exampleCatalog()[1:2])
	if _, ok := single["anyOf"]; ok || single["additionalProperties"] != false {
		t.Errorf("a single shape = %+v; want the closed object itself", single)
	}
}

// TestInputSchema_StrictRefusesAnOptionalKey: a strict tool has no optional key
// and no open object, in a field or in an override.
func TestInputSchema_StrictRefusesAnOptionalKey(t *testing.T) {
	type optional struct {
		A string `json:"a" required:"true" desc:"d"`
		B string `json:"b" desc:"d"`
	}
	type nullable struct {
		A string `json:"a" required:"true" desc:"d"`
		B string `json:"b" required:"true" nullable:"true" desc:"d"`
		C any    `json:"c" required:"true"`
	}
	for name, build := range map[string]func(){
		"optional field": func() { inputSchema[optional](nil, true) },
		"open override": func() {
			inputSchema[nullable](map[string]any{"c": map[string]any{"type": "object"}}, true)
		},
		"open override with a type list": func() {
			inputSchema[nullable](map[string]any{"c": map[string]any{"type": []any{"object", "null"}}}, true)
		},
		"optional key in an override": func() {
			inputSchema[nullable](map[string]any{"c": map[string]any{"anyOf": []any{map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"k": map[string]any{"type": "string"}}, "required": []string{},
			}}}}, true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("accepted")
				}
			}()
			build()
		})
	}

	s := inputSchema[nullable](map[string]any{"c": map[string]any{"type": "string"}}, true)
	if s["additionalProperties"] != false {
		t.Errorf("strict object not closed: %+v", s)
	}
	if got := s["properties"].(map[string]any)["b"].(map[string]any)["type"]; !reflect.DeepEqual(got, []string{"string", "null"}) {
		t.Errorf("nullable b type = %v", got)
	}
	if _, ok := inputSchema[optional](nil, false)["additionalProperties"]; ok {
		t.Error("a tool that is not strict got a closed object")
	}
}

// TestRequestAction_DropsNullParameters: a key the model wrote as null is
// absent from the request the engine receives, and a null retry_of is no
// retry (adr/0376).
func TestRequestAction_DropsNullParameters(t *testing.T) {
	f := newFakeBackend(t)
	s := newToolSession(t, f)
	input := `{"action_type":"ticket.create","rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],
		"parameters":{"summary":"s","description":"d","issue_type":null,"assignee":null,"priority":null},
		"retry_of":null}`
	if res, isErr := s.dispatchTool(context.Background(), ToolRequestAction, json.RawMessage(input)); isErr {
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
	var params map[string]any
	if err := json.Unmarshal(body.Parameters, &params); err != nil {
		t.Fatalf("parameters = %s: %v", body.Parameters, err)
	}
	if !reflect.DeepEqual(params, map[string]any{"summary": "s", "description": "d"}) {
		t.Errorf("parameters = %s; want the null keys dropped", body.Parameters)
	}
	if body.RetryOf != "" {
		t.Errorf("retry_of = %q; want none", body.RetryOf)
	}
}

// TestRequestAction_StringParametersReachTheEngine: the loop does not turn a
// string into an object. The string goes to the engine as sent, and the
// engine's refusal reaches the model as the tool's error.
func TestRequestAction_StringParametersReachTheEngine(t *testing.T) {
	f := newFakeBackend(t)
	ticket, _ := action.DefaultActionCatalog().Descriptor("ticket.create")
	var received []json.RawMessage
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/actions" {
			return false
		}
		var body ActionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		received = append(received, body.Parameters)
		if err := action.ValidateParameters(ticket, body.Parameters); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return true
		}
		t.Errorf("the engine accepted parameters %s", body.Parameters)
		return true
	}
	s := newToolSession(t, f)
	const sent = `"{\"summary\":\"s\"}"`
	input := `{"action_type":"ticket.create","rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x","resolved_identifier":"SEC"}],"parameters":` + sent + `,"retry_of":null}`
	res, isErr := s.dispatchTool(context.Background(), ToolRequestAction, json.RawMessage(input))
	if !isErr || !strings.Contains(string(res.Content), "parameters is not a JSON object") {
		t.Errorf("result = %s (error %v); want the engine's refusal", res.Content, isErr)
	}
	if len(received) != 1 || string(received[0]) != sent {
		t.Errorf("the engine received %s; want the string as sent", received)
	}
}
