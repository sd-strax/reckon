package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSideCompletion_AfterFailedTurn: a failed turn leaves the history ending
// on the analyst's message; a side completion merges its instruction into it
// rather than sending two analyst messages in a row, and leaves the history
// as it was (adr/0234).
func TestSideCompletion_AfterFailedTurn(t *testing.T) {
	f := newFakeBackend(t)
	llm := &erroringLLM{err: errors.New("provider overloaded")}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := s.Turn(context.Background(), "what happened on H1?"); err == nil {
		t.Fatal("the turn should fail")
	}
	before := len(s.messages)

	llm.err = nil
	llm.script = []CompleteResponse{endTurn(`{"warranted":false,"rationale":"nothing new"}`)}
	if _, err := s.ProposeSOP(context.Background(), false); err != nil {
		t.Fatalf("ProposeSOP after a failed turn: %v", err)
	}
	req := llm.requests[len(llm.requests)-1]
	for i := 1; i < len(req.Messages); i++ {
		if req.Messages[i].Role == req.Messages[i-1].Role {
			t.Fatalf("messages %d and %d share role %q", i-1, i, req.Messages[i].Role)
		}
	}
	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != RoleUser || !strings.HasPrefix(lastMsg.Content[len(lastMsg.Content)-1].Text, "The investigation has just been concluded.") {
		t.Errorf("last message does not end with the instruction: %+v", lastMsg)
	}
	if len(s.messages) != before {
		t.Errorf("history %d -> %d messages; a side completion must not change it", before, len(s.messages))
	}
	if n := len(s.messages[before-1].Content); n != len(req.Messages[len(req.Messages)-1].Content)-1 {
		t.Errorf("the history's last message gained the instruction (%d blocks)", n)
	}
}

// TestSideCompletion_FitsContext: a side completion over a history past the
// window is compacted and retried on an overflow like a turn's call, and the
// model is told in the instruction; the analyst is not notified, there being
// no live turn (adr/0234).
func TestSideCompletion_FitsContext(t *testing.T) {
	f := newFakeBackend(t)
	llm := &overflowLLM{refuse: 1, limit: 50_000}
	notified := false
	s, err := NewSession(context.Background(), Config{
		Backend: f.client(), LLM: llm, InvestigationID: "inv-1",
		Hooks: Hooks{OnContextCompacted: func(string) { notified = true }},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for i := range 8 {
		s.addTurn(fmt.Sprintf("q%d", i), 20_000)
	}
	resp, err := s.sideCompletion(context.Background(), summarizeInstruction)
	if err != nil {
		t.Fatalf("side completion after an overflow: %v", err)
	}
	if len(llm.sizes) != 2 || llm.sizes[1] >= llm.sizes[0] {
		t.Errorf("request sizes %v; want a smaller retry", llm.sizes)
	}
	if resp.Content[0].Text != "ok" {
		t.Errorf("reply = %+v", resp.Content)
	}
	if notified {
		t.Error("the analyst hook fired for a side completion")
	}
	assertWellFormed(t, s)
}

// TestSummarize_RetriesNotFoundByStatus: the not-yet-written baseline is
// recognised by its 404 status, not by the text of the error.
func TestSummarize_RetriesNotFoundByStatus(t *testing.T) {
	prev := summaryRetryDelay
	summaryRetryDelay = 0
	t.Cleanup(func() { summaryRetryDelay = prev })

	for name, tc := range map[string]struct {
		status    int
		body      string
		wantCalls int
	}{
		"404 is retried":              {http.StatusNotFound, "no summary", 2},
		"a 500 naming 404 is not":     {http.StatusInternalServerError, "upstream said 404", 1},
		"a 409 is not retried either": {http.StatusConflict, "conflict", 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend(t)
			calls := 0
			f.override = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/api/knowledge/summary_narrative" {
					return false
				}
				calls++
				if calls == 1 {
					http.Error(w, tc.body, tc.status)
					return true
				}
				return false
			}
			llm := &scriptedLLM{script: []CompleteResponse{endTurn("A narrative of the case.")}}
			s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
			if err != nil {
				t.Fatalf("NewSession: %v", err)
			}
			_, err = s.SummarizeConcluded(context.Background())
			if calls != tc.wantCalls {
				t.Errorf("submit calls = %d; want %d", calls, tc.wantCalls)
			}
			if (err == nil) != (tc.wantCalls == 2) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

// TestAnthropic_ToolChoiceNone: a side completion's request keeps the tools
// and sends tool_choice none; a turn's sends no tool_choice.
func TestAnthropic_ToolChoiceNone(t *testing.T) {
	var got []map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		got = append(got, body)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`))
	}))
	defer srv.Close()

	a := &Anthropic{APIKey: "k", BaseURL: srv.URL}
	tools := []ToolDef{{Name: "x", Description: "d", InputSchema: map[string]any{"type": "object"}}}
	msgs := []Message{{Role: RoleUser, Content: []ContentBlock{TextBlock("hi")}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, none := range []bool{true, false} {
		if _, err := a.Complete(ctx, CompleteRequest{Messages: msgs, Tools: tools, MaxTokens: 16, ToolChoiceNone: none}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	if string(got[0]["tool_choice"]) != `{"type":"none"}` || got[0]["tools"] == nil {
		t.Errorf("side request: tool_choice %s, tools present %v", got[0]["tool_choice"], got[0]["tools"] != nil)
	}
	if _, ok := got[1]["tool_choice"]; ok {
		t.Errorf("turn request carried tool_choice %s", got[1]["tool_choice"])
	}
}
