package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ctxSession is a bare session for compaction tests: one token per byte, so
// sizes read directly as tokens.
func ctxSession(window int) *Session {
	return &Session{system: "sys", contextTokens: window, tokensPerByte: 1}
}

// addTurn appends one complete turn: the analyst's message, a tool call, a
// result of resultBytes, and the model's answer.
func (s *Session) addTurn(analyst string, resultBytes int) {
	id := fmt.Sprintf("tu%d", len(s.turns))
	s.turns = append(s.turns, turnSpan{start: len(s.messages), analyst: analyst})
	s.messages = append(s.messages,
		Message{Role: RoleUser, Content: []ContentBlock{TextBlock("[clock] now"), TextBlock(analyst)}},
		Message{Role: RoleAssistant, Content: []ContentBlock{TextBlock("looking"), toolUse(id, "enumerate_logons", `{}`)}},
		Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockToolResult, ToolUseID: id, Content: strings.Repeat("r", resultBytes)}}},
		Message{Role: RoleAssistant, Content: []ContentBlock{TextBlock("answer to " + analyst)}},
	)
}

// startTurn opens the current turn with the analyst's message.
func (s *Session) startTurn(analyst string) {
	s.turns = append(s.turns, turnSpan{start: len(s.messages), analyst: analyst})
	s.messages = append(s.messages, Message{Role: RoleUser, Content: []ContentBlock{TextBlock(analyst)}})
}

// addRound appends one tool round to the current turn.
func (s *Session) addRound(resultBytes int) {
	id := fmt.Sprintf("r%d", len(s.messages))
	s.messages = append(s.messages,
		Message{Role: RoleAssistant, Content: []ContentBlock{toolUse(id, "enumerate_logons", `{}`)}},
		Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockToolResult, ToolUseID: id, Content: strings.Repeat("r", resultBytes)}}},
	)
}

// assertWellFormed fails unless roles alternate starting on the analyst,
// every tool_use is answered, and every turn starts on a user message.
func assertWellFormed(t *testing.T, s *Session) {
	t.Helper()
	for i, m := range s.messages {
		want := RoleUser
		if i%2 == 1 {
			want = RoleAssistant
		}
		if m.Role != want {
			t.Fatalf("message %d has role %s; roles must alternate from the analyst", i, m.Role)
		}
	}
	assertToolUsesAnswered(t, s.messages)
	for i, tr := range s.turns {
		if tr.start >= len(s.messages) || s.messages[tr.start].Role != RoleUser {
			t.Fatalf("turn %d starts at %d, not on a user message", i, tr.start)
		}
	}
}

func budgetOf(s *Session) (high, low int) {
	b := s.contextTokens - s.maxTokens
	return b * 3 / 4, b / 2
}

func TestFitContext_UnderThresholdIsUntouched(t *testing.T) {
	s := ctxSession(1_000_000)
	s.addTurn("q1", 2000)
	s.startTurn("q2")
	before := len(s.messages)
	if c := s.fitContext(false); c.any() || len(s.messages) != before {
		t.Errorf("compacted under the threshold: %+v", c)
	}
}

// TestFitContext_CollapsesOldestTurnsFirst: earlier turns are reduced to the
// analyst's words and the model's prose, oldest first, and only as many as
// needed; the current turn is not touched.
func TestFitContext_CollapsesOldestTurnsFirst(t *testing.T) {
	s := ctxSession(0)
	for i := range 6 {
		s.addTurn(fmt.Sprintf("q%d", i), 3000)
	}
	s.startTurn("now")
	s.addRound(1000)
	// Window such that the history is over the high mark, and collapsing
	// about half the earlier turns brings it under the low mark.
	est := s.estimateTokens()
	s.contextTokens = est * 6 / 5
	c := s.fitContext(false)
	high, low := budgetOf(s)
	if est <= high {
		t.Fatalf("test setup: estimate %d not over high %d", est, high)
	}
	if c.collapsed == 0 || c.elided != 0 || c.dropped != 0 {
		t.Fatalf("compaction = %+v; want collapse alone", c)
	}
	if got := s.estimateTokens(); got > low {
		t.Errorf("estimate %d still over low %d", got, low)
	}
	if c.collapsed == 6 {
		t.Error("collapsed every earlier turn; collapsing should stop once under the low mark")
	}
	for i := range 6 {
		if s.turns[i].collapsed != (i < c.collapsed) {
			t.Fatalf("turn %d collapsed=%v; collapse must go oldest first", i, s.turns[i].collapsed)
		}
	}
	first := s.messages[s.turns[0].start : s.turns[0].start+2]
	if first[0].Content[0].Text != "q0" || first[1].Content[0].Text != "looking\nanswer to q0" {
		t.Errorf("collapsed turn = %+v; want the analyst's words and the model's prose", first)
	}
	cur := s.turns[len(s.turns)-1]
	if got := s.messages[len(s.messages)-1].Content[0].Content; len(got) != 1000 {
		t.Errorf("current turn's result was changed (%d bytes)", len(got))
	}
	if s.messages[cur.start].Content[0].Text != "now" {
		t.Error("current turn's message moved or changed")
	}
	assertWellFormed(t, s)
}

// TestFitContext_LongTurnElidesItsOwnOlderResults: one turn over the window
// by itself replaces its older results with a placeholder, keeping the round
// the model is about to read.
func TestFitContext_LongTurnElidesItsOwnOlderResults(t *testing.T) {
	s := ctxSession(0)
	s.startTurn("dig")
	for range 6 {
		s.addRound(4000)
	}
	s.contextTokens = s.estimateTokens() * 6 / 5
	c := s.fitContext(false)
	if c.elided == 0 || c.collapsed != 0 || c.dropped != 0 {
		t.Fatalf("compaction = %+v; want elision alone", c)
	}
	last := s.messages[len(s.messages)-1].Content[0].Content
	if len(last) != 4000 {
		t.Error("the latest round's result was removed")
	}
	first := s.messages[2].Content[0].Content
	if !strings.HasPrefix(first, "[result removed to fit the context window: 4000 bytes;") {
		t.Errorf("oldest result = %.80q; want the placeholder", first)
	}
	assertWellFormed(t, s)
}

// TestFitContext_DropsOldestWhenProseIsTooBig: when collapsed turns are still
// too big, the oldest are dropped; the current turn always stays.
func TestFitContext_DropsOldestWhenProseIsTooBig(t *testing.T) {
	s := ctxSession(0)
	for i := range 4 {
		s.addTurn(strings.Repeat(fmt.Sprint(i), 3000), 10)
	}
	s.startTurn("now")
	s.contextTokens = s.estimateTokens() * 6 / 5
	c := s.fitContext(false)
	if c.dropped == 0 {
		t.Fatalf("compaction = %+v; want the oldest turns dropped", c)
	}
	if got := s.turns[len(s.turns)-1].analyst; got != "now" {
		t.Errorf("the current turn was dropped (last turn is %q)", got)
	}
	if !strings.HasPrefix(s.messages[0].Content[0].Text, fmt.Sprint(c.dropped)) {
		t.Errorf("first remaining turn is not the oldest kept: %.10q", s.messages[0].Content[0].Text)
	}
	assertWellFormed(t, s)
}

func TestElidedPlaceholder_NamesHeldResult(t *testing.T) {
	got := elidedPlaceholder(`{"events":[],"result_bounded":{"result_id":"res-3"}}` + strings.Repeat(" ", 600))
	if !strings.Contains(got, "read_result res-3") {
		t.Errorf("placeholder = %q; want the way back to the held result", got)
	}
}

func TestStatusError_PromptTooLong(t *testing.T) {
	err := statusError(http.StatusBadRequest, "invalid_request_error: prompt is too long: 215000 tokens > 200000 maximum")
	var ov *ContextOverflowError
	if !errors.As(err, &ov) || ov.Tokens != 215000 || ov.Limit != 200000 {
		t.Fatalf("got %#v; want a ContextOverflowError with 215000 > 200000", err)
	}
	if !errors.As(statusError(http.StatusBadRequest, "invalid_request_error: prompt is too long"), &ov) {
		t.Error("an overflow without numbers is still an overflow")
	}
	if errors.As(statusError(http.StatusBadRequest, "invalid_request_error: messages: roles must alternate"), &ov) {
		t.Error("another 400 is not an overflow")
	}
	if errors.As(statusError(http.StatusTooManyRequests, "prompt is too long"), &ov) {
		t.Error("only a 400 is an overflow")
	}
}

// overflowLLM refuses its first `refuse` calls as over the window, then
// answers; it records each request's size.
type overflowLLM struct {
	refuse int
	limit  int
	sizes  []int
}

func (o *overflowLLM) Complete(_ context.Context, req CompleteRequest) (CompleteResponse, error) {
	o.sizes = append(o.sizes, historyBytes(req.Messages))
	if len(o.sizes) <= o.refuse {
		return CompleteResponse{}, &ContextOverflowError{Tokens: o.limit + 1, Limit: o.limit, Err: errors.New("anthropic: 400 prompt is too long")}
	}
	return CompleteResponse{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock("ok")}}, nil
}

// TestSession_OverflowCompactsAndRetries (#230): a refusal as over the window
// adopts the reported limit, compacts, retries once and the turn succeeds; the
// analyst and the record are told.
func TestSession_OverflowCompactsAndRetries(t *testing.T) {
	f := newFakeBackend(t)
	llm := &overflowLLM{refuse: 1, limit: 50_000}
	var notes []string
	s, err := NewSession(context.Background(), Config{
		Backend: f.client(), LLM: llm, InvestigationID: "inv-1",
		Hooks: Hooks{OnContextCompacted: func(n string) { notes = append(notes, n) }},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for i := range 8 {
		s.addTurn(fmt.Sprintf("q%d", i), 20_000)
	}
	res, err := s.Turn(context.Background(), "go on")
	if err != nil {
		t.Fatalf("turn after an overflow: %v", err)
	}
	if s.contextTokens != 50_000 {
		t.Errorf("window = %d; want the provider's 50000", s.contextTokens)
	}
	if len(llm.sizes) != 2 || llm.sizes[1] >= llm.sizes[0] {
		t.Errorf("request sizes %v; want a smaller retry", llm.sizes)
	}
	if len(notes) == 0 || !strings.HasPrefix(notes[0], "Context condensed to fit the model's window:") {
		t.Errorf("analyst notes = %q", notes)
	}
	if !strings.Contains(res.Transcript, "[engine] Context condensed") {
		t.Error("the transcript does not record the compaction")
	}
	assertWellFormed(t, s)
}

// TestSession_RepeatedOverflowFails: a second refusal in a row is returned,
// not retried forever.
func TestSession_RepeatedOverflowFails(t *testing.T) {
	f := newFakeBackend(t)
	llm := &overflowLLM{refuse: 5, limit: 50_000}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	_, err = s.Turn(context.Background(), "go")
	var ov *ContextOverflowError
	if !errors.As(err, &ov) || len(llm.sizes) != 2 {
		t.Errorf("err = %v after %d calls; want the overflow after one retry", err, len(llm.sizes))
	}
}

// TestSession_ModelIsToldWhatItNoLongerSees: the request after a compaction
// carries the context note on its last message.
func TestSession_ModelIsToldWhatItNoLongerSees(t *testing.T) {
	f := newFakeBackend(t)
	llm := &scriptedLLM{}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for i := range 6 {
		s.addTurn(fmt.Sprintf("q%d", i), 30_000)
	}
	s.contextTokens = s.estimateTokens() + s.maxTokens // over the high mark
	if _, err := s.Turn(context.Background(), "next"); err != nil {
		t.Fatalf("turn: %v", err)
	}
	req := llm.requests[0]
	last := req.Messages[len(req.Messages)-1].Content
	if got := last[len(last)-1].Text; got != modelNote {
		t.Errorf("last block of the request = %.60q; want the context note", got)
	}
}

// TestSession_OverflowThroughAnthropic drives the real adapter: the API
// refuses the request as too long, and the retry fits.
func TestSession_OverflowThroughAnthropic(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens > 60000 maximum"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":1}}`))
	}))
	defer srv.Close()

	f := newFakeBackend(t)
	s, err := NewSession(context.Background(), Config{
		Backend: f.client(), InvestigationID: "inv-1",
		LLM: &Anthropic{APIKey: "k", BaseURL: srv.URL, RetryBaseDelay: time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for i := range 6 {
		s.addTurn(fmt.Sprintf("q%d", i), 30_000)
	}
	if _, err := s.Turn(context.Background(), "go"); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if calls != 2 || s.contextTokens != 60_000 {
		t.Errorf("calls=%d window=%d; want a retry under the API's 60000", calls, s.contextTokens)
	}
}

// failingLLM fails its first call, then answers.
type failingLLM struct{ calls int }

func (f *failingLLM) Complete(context.Context, CompleteRequest) (CompleteResponse, error) {
	f.calls++
	if f.calls == 1 {
		return CompleteResponse{}, errors.New("provider down")
	}
	return CompleteResponse{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock("ok")}}, nil
}

// TestSession_TurnAfterFailureSharesItsSpan: the next turn merges into a
// failed turn's dangling message, so the two are one span carrying both
// analyst messages, and collapsing it keeps both.
func TestSession_TurnAfterFailureSharesItsSpan(t *testing.T) {
	f := newFakeBackend(t)
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &failingLLM{}, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := s.Turn(context.Background(), "first"); err == nil {
		t.Fatal("turn 1 should fail")
	}
	if _, err := s.Turn(context.Background(), "second"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if len(s.turns) != 1 || s.turns[0].analyst != "first\nsecond" {
		t.Fatalf("turns = %+v; want one span with both messages", s.turns)
	}
	s.startTurn("third")
	s.collapseTurn(0)
	if s.messages[0].Content[0].Text != "first\nsecond" || s.messages[1].Content[0].Text != "ok" {
		t.Errorf("collapsed = %+v", s.messages[:2])
	}
	assertWellFormed(t, s)
}
