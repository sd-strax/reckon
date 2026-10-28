package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAsk: the loop offers only the recall tools, numbers what they return,
// keeps the markers that resolve and drops the rest, reports which tools ran,
// and writes nothing to the backend (adr/0111).
func TestAsk(t *testing.T) {
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/knowledge/recall") {
			writes = append(writes, r.Method+" "+r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/knowledge/recall_sops":
			_, _ = w.Write([]byte(`{"coverage":"COMPLETE","results":[{"sop_id":"sop-1","title":"Containment procedure","excerpt":"Isolate the host first.","score":0.9,"location":{"heading_path":["Steps"]}}]}`))
		case "/api/knowledge/recall_similar_investigations":
			_, _ = w.Write([]byte(`{"coverage":"COMPLETE","results":[{"investigation_ref":"grouping--past","title":"A prior case","excerpt":"Was isolated.","score":0.8,"band":"RELATED"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	backend := NewClient(srv.URL, staticToken("t"), staticToken("t"))
	llm := &scriptedLLM{script: []CompleteResponse{
		{StopReason: StopToolUse, Content: []ContentBlock{
			{Type: BlockToolUse, ToolUseID: "u1", ToolName: ToolRecallSOPs, Input: json.RawMessage(`{"query":"containment"}`)},
			{Type: BlockToolUse, ToolUseID: "u2", ToolName: ToolRecallSimilar, Input: json.RawMessage(`{"query":"containment"}`)},
		}},
		{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock("Isolate the host first [1]; a prior case did the same [2]. Unrelated claim [7].")}},
	}}
	res, err := Ask(context.Background(), AskConfig{Backend: backend, LLM: llm}, "what do we do first?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Isolate the host first [1]; a prior case did the same [2]. Unrelated claim ." {
		t.Errorf("text = %q; want the unresolvable marker dropped", res.Text)
	}
	if len(res.Citations) != 2 || res.Citations[0].Kind != "sop" || res.Citations[0].Ref != "sop-1" || res.Citations[1].Kind != "case" || res.Citations[1].Ref != "grouping--past" {
		t.Errorf("citations = %+v", res.Citations)
	}
	if string(res.Citations[0].Location) != `{"heading_path":["Steps"]}` {
		t.Errorf("location = %s", res.Citations[0].Location)
	}
	if len(res.ToolsUsed) != 2 || res.ToolsUsed[0] != ToolRecallSOPs || res.ToolsUsed[1] != ToolRecallSimilar {
		t.Errorf("tools used = %v", res.ToolsUsed)
	}
	if len(writes) != 0 {
		t.Errorf("Ask wrote to the backend: %v", writes)
	}
	// The model was offered only the recall tools, under the Ask prompt.
	req := llm.requests[0]
	if len(req.Tools) != 2 || req.System != askPrompt() {
		t.Errorf("offered %d tools under %q", len(req.Tools), req.System[:40])
	}
	// The tool result carried the citation numbers.
	tr := llm.requests[1].Messages[2].Content[0].Content
	if !strings.Contains(tr, `"n":1`) || !strings.Contains(tr, `"kind":"sop"`) {
		t.Errorf("tool result = %s", tr)
	}
}

// TestAsk_EmptyAnswer: a model that returns no text is an error, never a blank
// answer presented as one.
func TestAsk_EmptyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()
	backend := NewClient(srv.URL, staticToken("t"), staticToken("t"))
	llm := &scriptedLLM{script: []CompleteResponse{{StopReason: StopEndTurn}}}
	if _, err := Ask(context.Background(), AskConfig{Backend: backend, LLM: llm}, "anything?"); err == nil {
		t.Error("want an error for an empty answer")
	}
}
