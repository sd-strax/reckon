package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// turnCommits is the turn commits the backend accepted, in order: the
// interpretations that carry a transcript.
func (f *fakeBackend) turnCommits(t *testing.T) []InterpretationRequest {
	t.Helper()
	var out []InterpretationRequest
	for _, c := range f.callsTo("/api/interpretations") {
		var req InterpretationRequest
		if err := json.Unmarshal(c.Body, &req); err != nil {
			t.Fatalf("decode interpretation: %v", err)
		}
		if req.Transcript != nil {
			out = append(out, req)
		}
	}
	return out
}

// failCommits makes the backend answer status to the next n turn commits (all
// of them while n < 0), counting every attempt it refused.
func (f *fakeBackend) failCommits(status, n int) *int {
	refused := 0
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/interpretations" || r.Method != http.MethodPost || n == 0 {
			return false
		}
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		var req InterpretationRequest
		if json.Unmarshal(body, &req) != nil || req.Transcript == nil {
			return false
		}
		if n > 0 {
			n--
		}
		refused++
		http.Error(w, "unavailable", status)
		return true
	}
	return &refused
}

func commitSession(t *testing.T, f *fakeBackend, llm LLM) *Session {
	t.Helper()
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.commitBackoff = []time.Duration{0, 0}
	return s
}

func endTurn(text string) CompleteResponse {
	return CompleteResponse{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock(text)}}
}

// TestTurn_ModelFailureCommitsTurn: a model call that fails after the turn
// requested an action still commits the turn, with its tool call and a line
// saying how it ended, so the action has its reasoning on the record
// (adr/0231).
func TestTurn_ModelFailureCommitsTurn(t *testing.T) {
	f := newFakeBackend(t)
	llm := &erroringLLM{
		scriptedLLM: scriptedLLM{script: []CompleteResponse{
			{StopReason: StopToolUse, Content: []ContentBlock{
				toolUse("t1", ToolRequestAction, `{"action_type":"host.isolate","targets":[{"entity_ref":"x-host--1","resolved_identifier":"H1"}],"rationale":"contain"}`),
			}},
		}},
		err: errors.New("provider overloaded"),
	}
	s := commitSession(t, f, llm)

	res, err := s.Turn(context.Background(), "contain H1")
	if err == nil || !strings.Contains(err.Error(), "provider overloaded") {
		t.Fatalf("Turn error = %v; want the model failure", err)
	}
	commits := f.turnCommits(t)
	if len(commits) != 1 {
		t.Fatalf("turn commits = %d; want 1", len(commits))
	}
	body := commits[0].Transcript.Body
	for _, want := range []string{"[user] contain H1", "[tool_use request_action id=t1]", "[engine] turn ended early: model call failed: provider overloaded"} {
		if !strings.Contains(body, want) {
			t.Errorf("transcript lacks %q:\n%s", want, body)
		}
	}
	if commits[0].Rationale != "(turn ended early — see transcript)" {
		t.Errorf("rationale = %q", commits[0].Rationale)
	}
	if len(commits[0].ToolCalls) != 1 {
		t.Errorf("tool calls = %+v; want the request_action", commits[0].ToolCalls)
	}
	if res.InterpretationID == "" || res.Transcript != body {
		t.Errorf("result: interpretation %q, transcript matches commit %v", res.InterpretationID, res.Transcript == body)
	}
	if len(res.PendingActions) != 1 {
		t.Errorf("pending actions = %+v; want the requested isolate", res.PendingActions)
	}
}

// cancellingLLM replays its script, then cancels the turn the way an analyst's
// Cancel does and fails with the context's error.
type cancellingLLM struct {
	scriptedLLM
	cancel context.CancelFunc
}

func (c *cancellingLLM) Complete(ctx context.Context, req CompleteRequest) (CompleteResponse, error) {
	if len(c.script) == 0 {
		c.cancel()
		return CompleteResponse{}, ctx.Err()
	}
	return c.scriptedLLM.Complete(ctx, req)
}

// TestTurn_CancelCommitsTurn: a cancel stops the model, not the record. The
// closing work runs on a context the cancel does not reach (adr/0231).
func TestTurn_CancelCommitsTurn(t *testing.T) {
	f := newFakeBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llm := &cancellingLLM{
		scriptedLLM: scriptedLLM{script: []CompleteResponse{
			{StopReason: StopToolUse, Content: []ContentBlock{
				toolUse("t1", ToolRequestAction, `{"action_type":"host.isolate","targets":[{"entity_ref":"x-host--1","resolved_identifier":"H1"}],"rationale":"contain"}`),
			}},
		}},
		cancel: cancel,
	}
	s := commitSession(t, f, llm)

	res, err := s.Turn(ctx, "contain H1")
	if err != nil || !res.Stopped {
		t.Fatalf("Turn = %+v, %v; want a stopped turn and no error", res, err)
	}
	commits := f.turnCommits(t)
	if len(commits) != 1 || !strings.Contains(commits[0].Transcript.Body, "[engine] turn ended early: cancelled by the analyst") {
		t.Fatalf("turn commits = %+v; want one ending in the cancel line", commits)
	}
	if res.InterpretationID == "" || len(res.PendingActions) != 1 {
		t.Errorf("result: interpretation %q, pending %+v; want both despite the cancel", res.InterpretationID, res.PendingActions)
	}
}

// TestTurn_CancelLetsDispatchedToolFinish (adr/0239): a cancel arriving as a
// write tool is dispatched does not cut it off. Its real result enters the
// history, the tool after it never runs and is answered as such, and no further
// model call is made.
func TestTurn_CancelLetsDispatchedToolFinish(t *testing.T) {
	f := newFakeBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	action := `{"action_type":"host.isolate","targets":[{"entity_ref":"x-host--1","resolved_identifier":"H1"}],"rationale":"contain"}`
	llm := &scriptedLLM{script: []CompleteResponse{
		{StopReason: StopToolUse, Content: []ContentBlock{
			toolUse("t1", ToolRequestAction, action),
			toolUse("t2", ToolRequestAction, action),
		}},
	}}
	s := commitSession(t, f, llm)
	s.hooks.OnToolCall = func(string, json.RawMessage) { cancel() }

	res, err := s.Turn(ctx, "contain H1")
	if err != nil || !res.Stopped {
		t.Fatalf("Turn = %+v, %v; want a stopped turn and no error", res, err)
	}
	if got := len(f.callsTo("/api/actions")); got != 1 {
		t.Errorf("actions requested = %d; want the dispatched one only", got)
	}
	if len(res.ToolCalls) != 1 {
		t.Errorf("tool calls = %+v; want the dispatched one only", res.ToolCalls)
	}
	last := s.messages[len(s.messages)-1]
	if last.Role != RoleUser || len(last.Content) != 2 {
		t.Fatalf("last message = %+v; want both tool_uses answered", last)
	}
	if done := last.Content[0]; done.IsError {
		t.Errorf("dispatched tool's result = %+v; want its real result", done)
	}
	if skipped := last.Content[1]; !skipped.IsError || !strings.Contains(skipped.Content, "not dispatched") {
		t.Errorf("undispatched tool's result = %+v; want it answered as not run", skipped)
	}
	if !strings.Contains(res.Transcript, "[engine] turn ended early: cancelled by the analyst") {
		t.Errorf("transcript lacks the cancel line:\n%s", res.Transcript)
	}
}

// TestTurn_TimeLimitRecordedAsSuch (adr/0239): the record says a turn timed
// out, not that the analyst stopped it.
func TestTurn_TimeLimitRecordedAsSuch(t *testing.T) {
	f := newFakeBackend(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cancel(ErrTurnTimedOut)
	s := commitSession(t, f, &scriptedLLM{})

	res, err := s.Turn(ctx, "go")
	if err != nil || !res.Stopped {
		t.Fatalf("Turn = %+v, %v; want a stopped turn and no error", res, err)
	}
	if !strings.Contains(res.Transcript, "[engine] turn ended early: timed out") {
		t.Errorf("transcript:\n%s", res.Transcript)
	}
}

// TestTurn_CommitRetried: a commit that fails in transit is retried with the
// same turn id, and the turn is recorded (adr/0231).
func TestTurn_CommitRetried(t *testing.T) {
	f := newFakeBackend(t)
	refused := f.failCommits(http.StatusServiceUnavailable, 1)
	s := commitSession(t, f, &scriptedLLM{script: []CompleteResponse{endTurn("done")}})

	res, err := s.Turn(context.Background(), "go")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if *refused != 1 || len(f.turnCommits(t)) != 1 || res.InterpretationID == "" {
		t.Errorf("refused %d, recorded %d, id %q; want 1, 1, set", *refused, len(f.turnCommits(t)), res.InterpretationID)
	}
	if len(s.held) != 0 {
		t.Errorf("held = %d; want 0", len(s.held))
	}
}

// TestTurn_CommitHeldUntilNextTurn: a commit that keeps failing in transit is
// held, reported as such, and recorded before the next turn's own commit,
// in order (adr/0231).
func TestTurn_CommitHeldUntilNextTurn(t *testing.T) {
	f := newFakeBackend(t)
	refused := f.failCommits(http.StatusBadGateway, -1)
	llm := &scriptedLLM{script: []CompleteResponse{endTurn("first"), endTurn("second")}}
	s := commitSession(t, f, llm)

	res, err := s.Turn(context.Background(), "one")
	if !errors.Is(err, errTurnHeld) {
		t.Fatalf("Turn error = %v; want the turn held", err)
	}
	if *refused != 3 || res.InterpretationID != "" || len(s.held) != 1 {
		t.Fatalf("refused %d, id %q, held %d; want 3 attempts, no id, 1 held", *refused, res.InterpretationID, len(s.held))
	}

	f.override = nil
	if _, err := s.Turn(context.Background(), "two"); err != nil {
		t.Fatalf("next turn: %v", err)
	}
	commits := f.turnCommits(t)
	if len(commits) != 2 {
		t.Fatalf("turn commits = %d; want the held turn and this one", len(commits))
	}
	if !strings.Contains(commits[0].Transcript.Body, "[user] one") || !strings.Contains(commits[1].Transcript.Body, "[user] two") {
		t.Errorf("commits out of order:\n%s\n---\n%s", commits[0].Transcript.Body, commits[1].Transcript.Body)
	}
	if len(s.held) != 0 {
		t.Errorf("held = %d after the flush; want 0", len(s.held))
	}
}

// TestTurn_CommitRejectedNotRetried: a commit the backend refuses is reported
// once, neither retried nor held: it would not succeed later.
func TestTurn_CommitRejectedNotRetried(t *testing.T) {
	f := newFakeBackend(t)
	refused := f.failCommits(http.StatusConflict, -1)
	s := commitSession(t, f, &scriptedLLM{script: []CompleteResponse{endTurn("done")}})

	_, err := s.Turn(context.Background(), "go")
	if err == nil || errors.Is(err, errTurnHeld) {
		t.Fatalf("Turn error = %v; want a rejection, not a hold", err)
	}
	if *refused != 1 || len(s.held) != 0 {
		t.Errorf("refused %d, held %d; want 1, 0", *refused, len(s.held))
	}
}

// TestResetContext_RecordsHeldTurnsFirst: a held turn goes on the thread
// before the reset marker, so rehydration treats it as reset away.
func TestResetContext_RecordsHeldTurnsFirst(t *testing.T) {
	f := newFakeBackend(t)
	f.failCommits(http.StatusServiceUnavailable, -1)
	s := commitSession(t, f, &scriptedLLM{script: []CompleteResponse{endTurn("done")}})
	if _, err := s.Turn(context.Background(), "go"); !errors.Is(err, errTurnHeld) {
		t.Fatalf("Turn error = %v; want the turn held", err)
	}

	f.override = nil
	if err := s.ResetContext(context.Background()); err != nil {
		t.Fatalf("ResetContext: %v", err)
	}
	var order []string
	for _, c := range f.callsTo("/api/interpretations") {
		var req InterpretationRequest
		_ = json.Unmarshal(c.Body, &req)
		if req.Transcript != nil {
			order = append(order, "turn")
		} else if req.Rationale == contextResetRationale {
			order = append(order, "reset")
		}
	}
	if strings.Join(order, ",") != "turn,reset" {
		t.Errorf("thread order = %v; want the held turn before the reset", order)
	}
}
