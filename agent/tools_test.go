package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const toolsGolden = "testdata/intrinsic_tools.golden.json"

func defsOf(tools []tool) []ToolDef {
	defs := make([]ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, t.def)
	}
	return defs
}

// TestIntrinsicTools_Golden pins every byte of the intrinsic definitions the
// model is shown, with and without an action catalog. The golden was captured
// from the hand-written schemas the generator replaced (adr/0233).
func TestIntrinsicTools_Golden(t *testing.T) {
	got, err := json.MarshalIndent(map[string][]ToolDef{
		"no_catalog": defsOf(intrinsicTools(nil)),
		"catalog":    defsOf(intrinsicTools(exampleCatalog())),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(toolsGolden)
	if err != nil {
		t.Fatal(err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Errorf("the tool definitions no longer match %s: what the model is shown changed", toolsGolden)
	}
}

func TestInputSchema_RefusesWhatItCannotShow(t *testing.T) {
	type undescribed struct {
		Field string `json:"field"`
	}
	type described struct {
		Field string `json:"field" desc:"d"`
	}
	for name, build := range map[string]func(){
		"undescribed field":      func() { inputSchema[undescribed](nil) },
		"override of no field":   func() { inputSchema[described](map[string]any{"feild": 1}) },
		"unsupported field type": func() { valueSchema(reflect.TypeFor[float64]()) },
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
}

// TestInputSchema_Required lists required fields in field order.
func TestInputSchema_Required(t *testing.T) {
	type in struct {
		B string `json:"b" required:"true" desc:"d"`
		A string `json:"a" required:"true" desc:"d"`
		C string `json:"c" desc:"d"`
	}
	if got := inputSchema[in](nil)["required"]; !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("required = %v", got)
	}
}

func newToolSession(t *testing.T, f *fakeBackend) *Session {
	t.Helper()
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s
}

// TestBuildTools_ListAndMapAreOneSet: what the model is shown and what a call
// can reach are the same set, in both directions.
func TestBuildTools_ListAndMapAreOneSet(t *testing.T) {
	s := newToolSession(t, newFakeBackend(t))
	var shown, reachable []string
	for _, d := range s.tools {
		shown = append(shown, d.Name)
	}
	for name := range s.toolsByName {
		reachable = append(reachable, name)
	}
	slices.Sort(shown)
	slices.Sort(reachable)
	if !slices.Equal(shown, reachable) {
		t.Errorf("shown %v, reachable %v", shown, reachable)
	}
}

func TestBuildTools_RefusesASharedName(t *testing.T) {
	var verb Capability
	verb.Status = "available"
	verb.Descriptor.Verb = ToolListActions
	_, _, err := buildTools([]Capability{verb}, nil)
	if err == nil || !strings.Contains(err.Error(), ToolListActions) {
		t.Fatalf("err = %v; want a refusal naming the tool", err)
	}
}

// TestBuildTools_OrderIsTheSessions: the backend's capability order does not
// reach the list the prompt cache matches on (adr/0232). Every order of the
// same capabilities gives the same bytes: verbs by name, then the intrinsics.
func TestBuildTools_OrderIsTheSessions(t *testing.T) {
	var caps []Capability
	for _, v := range []string{"verb_c", "verb_a", "verb_b"} {
		var c Capability
		c.Status = "available"
		c.Descriptor.Verb = v
		caps = append(caps, c)
	}
	render := func(caps []Capability) string {
		defs, _, err := buildTools(caps, exampleCatalog())
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(defs)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	want := render(caps)
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		shuffled := []Capability{caps[order[0]], caps[order[1]], caps[order[2]]}
		if got := render(shuffled); got != want {
			t.Errorf("order %v gives a different tool list", order)
		}
	}
	defs, _, _ := buildTools(caps, nil)
	var names []string
	for _, d := range defs[:3] {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"verb_a", "verb_b", "verb_c"}) {
		t.Errorf("verbs in %v; want them by name", names)
	}
	if defs[3].Name != ToolRecallSOPs || defs[len(defs)-1].Name != ToolReadResult {
		t.Errorf("intrinsics are not after the verbs in declared order: %s … %s", defs[3].Name, defs[len(defs)-1].Name)
	}
}

// TestDispatch_RefusesWhatWasNotOffered: a misspelled intrinsic and verbs the
// session trimmed (degraded, unavailable) are all told so by the agent, and the
// backend never hears of them.
func TestDispatch_RefusesWhatWasNotOffered(t *testing.T) {
	f := newFakeBackend(t)
	s := newToolSession(t, f)
	for _, name := range []string{"recall_sop", "RECALL_SOPS", "get_process_ancestry", "search_alerts", "list_capabilities"} {
		res, isErr := s.dispatchTool(context.Background(), name, json.RawMessage(`{}`))
		if !isErr {
			t.Errorf("%s was dispatched", name)
		}
		want := fmt.Sprintf("unknown tool %q: not one of the tools defined for this session", name)
		if string(res.Content) != want {
			t.Errorf("%s: content = %q", name, res.Content)
		}
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c.Path, "/api/capability/") {
			t.Errorf("reached the backend: %s", c.Path)
		}
	}
}

func TestDispatch_OfferedVerbReachesTheBackend(t *testing.T) {
	f := newFakeBackend(t)
	s := newToolSession(t, f)
	if _, isErr := s.dispatchTool(context.Background(), "enumerate_logons", json.RawMessage(`{"entity":{"host":{"hostname":"h"}}}`)); isErr {
		t.Fatal("an offered verb was refused")
	}
	if n := len(f.callsTo("/api/capability/enumerate_logons")); n != 1 {
		t.Errorf("%d calls; want 1", n)
	}
}

// TestRequestAction_ModelCannotDeclareCriticality: asset_criticality reaches
// Gate 2 policy and the model is never offered it. What it sends anyway stops
// at the decoder.
func TestRequestAction_ModelCannotDeclareCriticality(t *testing.T) {
	f := newFakeBackend(t)
	s := newToolSession(t, f)
	input := `{"action_type":"host.isolate","rationale":"r","evidence_refs":["e"],
		"targets":[{"entity_ref":"x-host--1","resolved_identifier":"H","asset_criticality":"low"}]}`
	if res, isErr := s.dispatchTool(context.Background(), ToolRequestAction, json.RawMessage(input)); isErr {
		t.Fatalf("refused: %s", res.Content)
	}
	calls := f.callsTo("/api/actions")
	if len(calls) != 1 {
		t.Fatalf("%d calls", len(calls))
	}
	if strings.Contains(string(calls[0].Body), "asset_criticality") {
		t.Errorf("the model's value reached the backend: %s", calls[0].Body)
	}
	if !strings.Contains(string(calls[0].Body), `"resolved_identifier":"H"`) {
		t.Errorf("the target was lost: %s", calls[0].Body)
	}
}

// bigRecall is a recall answer too large for one page of the given limit.
func bigRecall(t *testing.T, n int, item func(i int) map[string]any) []byte {
	t.Helper()
	results := make([]map[string]any, 0, n)
	for i := range n {
		results = append(results, item(i))
	}
	raw, err := json.Marshal(map[string]any{"results": results, "coverage": "COMPLETE"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestRecall_ConsultedIsWhatTheModelWasShown: a recall over the page limit
// records the hits on the page, not the hits held back. The record says what
// was put in front of the model.
func TestRecall_ConsultedIsWhatTheModelWasShown(t *testing.T) {
	const total = 40
	f := newFakeBackend(t)
	raw := bigRecall(t, total, func(i int) map[string]any {
		return map[string]any{"sop_id": fmt.Sprintf("sop-%d", i), "title": "T", "score": 0.5, "body": strings.Repeat("x", 300)}
	})
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/knowledge/recall_sops" {
			return false
		}
		_, _ = w.Write(raw)
		return true
	}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1", MaxToolResultBytes: 4 << 10})
	if err != nil {
		t.Fatal(err)
	}
	res, isErr := s.dispatchTool(context.Background(), ToolRecallSOPs, json.RawMessage(`{"query":"q"}`))
	if isErr {
		t.Fatalf("refused: %s", res.Content)
	}
	if n := len(res.Consulted); n == 0 || n >= total {
		t.Fatalf("consulted %d of %d; want the page's hits only", n, total)
	}
	for _, c := range res.Consulted {
		if !strings.Contains(string(res.Content), fmt.Sprintf("%q", c.SOPID)) {
			t.Errorf("%s is consulted but not on the page", c.SOPID)
		}
	}
}

// TestRecordConsulted_KeepsBestScore: the same hit from two calls of a turn is
// one entry at its best score, whichever came first.
func TestRecordConsulted_KeepsBestScore(t *testing.T) {
	s := &Session{consulted: map[consultedKey]ConsultedSOP{}, consultedSimilar: map[string]ConsultedSimilarInvestigation{}}
	s.recordConsulted([]ConsultedSOP{{SOPID: "a", RetrievalScore: 0.4}}, []ConsultedSimilarInvestigation{{InvestigationRef: "c", RetrievalScore: 0.9}})
	s.recordConsulted([]ConsultedSOP{{SOPID: "a", RetrievalScore: 0.8}}, []ConsultedSimilarInvestigation{{InvestigationRef: "c", RetrievalScore: 0.95}})
	s.recordConsulted([]ConsultedSOP{{SOPID: "a", RetrievalScore: 0.2}}, []ConsultedSimilarInvestigation{{InvestigationRef: "c", RetrievalScore: 0.2}})
	if got := s.consulted[consultedKey{id: "a"}].RetrievalScore; got != 0.8 {
		t.Errorf("sop score = %v", got)
	}
	if got := s.consultedSimilar["c"].RetrievalScore; got != 0.95 {
		t.Errorf("case score = %v", got)
	}
}

func TestConsultedCases_BothShapes(t *testing.T) {
	got := consultedCases([]byte(`{"results":[{"investigation_ref":"a","title":"A","score":0.9,"band":"RELATED"},{"title":"no ref"}],
		"investigations":[{"investigation_ref":"b","title":"B"}]}`))
	if len(got) != 2 || got[0].InvestigationRef != "a" || got[0].Band != "RELATED" || got[1].InvestigationRef != "b" {
		t.Errorf("got %+v", got)
	}
}
