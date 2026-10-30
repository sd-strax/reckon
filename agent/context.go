package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DefaultContextWindowTokens is the model context window the session budgets
// against when Config.ContextWindowTokens is unset (adr/0230). A provider
// that reports a smaller window on overflow replaces it.
const DefaultContextWindowTokens = 200_000

const (
	// defaultTokensPerByte estimates tokens from history bytes until a model
	// call reports real usage. JSON-heavy history runs near 3 bytes per token;
	// erring high compacts early rather than overflowing.
	defaultTokensPerByte = 1.0 / 3
	// blockOverheadBytes approximates the framing each content block adds on
	// the wire beyond its own text.
	blockOverheadBytes = 24
	// elideMinBytes: a tool result shorter than this is left in place; its
	// placeholder would save little.
	elideMinBytes = 512
	// noReply stands in for the prose of a turn that ended without any.
	noReply = "(this turn ended without a reply)"
)

// ContextOverflowError is a provider's refusal of a request larger than the
// model's context window. Tokens and Limit are what the provider reported,
// zero when it did not say.
type ContextOverflowError struct {
	Tokens int
	Limit  int
	Err    error
}

func (e *ContextOverflowError) Error() string { return e.Err.Error() }
func (e *ContextOverflowError) Unwrap() error { return e.Err }

// turnSpan marks where one analyst turn begins in the session's messages.
// analyst is the analyst's own words (no engine blocks); collapsed is set once
// the turn holds only its words and the model's prose.
type turnSpan struct {
	start     int
	analyst   string
	collapsed bool
}

// compaction counts what one fitContext pass removed from the model's view.
type compaction struct {
	collapsed, elided, dropped int
}

func (c compaction) any() bool { return c.collapsed+c.elided+c.dropped > 0 }

// analystNote is the one-line notice the analyst sees.
func (c compaction) analystNote() string {
	var parts []string
	if c.collapsed > 0 {
		parts = append(parts, plural(c.collapsed, "earlier turn")+" reduced to their text")
	}
	if c.elided > 0 {
		parts = append(parts, plural(c.elided, "tool result")+" removed from this turn")
	}
	if c.dropped > 0 {
		parts = append(parts, plural(c.dropped, "oldest turn")+" dropped")
	}
	return "Context condensed to fit the model's window: " + strings.Join(parts, ", ") +
		". The investigation record is unchanged."
}

// modelNote tells the model what it can no longer see.
const modelNote = "[context] The conversation was condensed to fit the context window. Earlier turns keep only " +
	"the analyst's words and your replies; their tool calls and results are no longer in view, some results " +
	"earlier in this turn may be replaced by a placeholder, and the oldest turns may be gone. Re-run a query " +
	"if you need its detail. Nothing was removed from the investigation record."

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// historyBytes approximates the wire size of msgs.
func historyBytes(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			n += blockOverheadBytes + len(b.Text) + len(b.Content) + len(b.Input) + len(b.ToolName) + len(b.ToolUseID)
		}
	}
	return n
}

// requestBytes approximates the wire size of the next request: the system
// prompt and tool definitions, which count against the window too, the
// history, and a side completion's instruction.
func (s *Session) requestBytes(side []ContentBlock) int {
	if s.fixedBytes == 0 {
		tools, _ := json.Marshal(s.tools)
		s.fixedBytes = len(s.system) + len(tools)
	}
	return s.fixedBytes + historyBytes(s.messages) + historyBytes([]Message{{Content: side}})
}

func (s *Session) estimateTokens() int {
	return int(float64(s.requestBytes(nil)) * s.tokensPerByte)
}

// fitContext compacts the history when the next request is estimated above
// three quarters of the input budget (the window less the output reserve),
// down to half of it, by the cheapest steps that suffice: collapse earlier
// turns to their prose, replace the current turn's older tool results with a
// placeholder, then drop the oldest turns. force compacts to half regardless,
// and may also replace the current round's results: it follows a provider's
// refusal, when the estimate has already been shown wrong.
func (s *Session) fitContext(force bool) compaction {
	budget := s.contextTokens - s.maxTokens
	high, low := budget*3/4, budget/2
	var c compaction
	if !force && s.estimateTokens() <= high {
		return c
	}
	cur := len(s.turns) - 1
	for i := 0; i < cur && s.estimateTokens() > low; i++ {
		if !s.turns[i].collapsed {
			s.collapseTurn(i)
			c.collapsed++
		}
	}
	if s.estimateTokens() > low && cur >= 0 {
		c.elided += s.elideResults(s.turns[cur].start, !force, low)
	}
	for s.estimateTokens() > low && len(s.turns) > 1 {
		s.dropOldestTurn()
		c.dropped++
	}
	return c
}

// turnEnd is the index one past turn i's last message.
func (s *Session) turnEnd(i int) int {
	if i+1 < len(s.turns) {
		return s.turns[i+1].start
	}
	return len(s.messages)
}

// collapseTurn replaces turn i with the analyst's words and the model's
// prose, the shape rehydrate rebuilds a turn in.
func (s *Session) collapseTurn(i int) {
	start, end := s.turns[i].start, s.turnEnd(i)
	var prose []string
	for _, m := range s.messages[start:end] {
		if m.Role != RoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type == BlockText && strings.TrimSpace(b.Text) != "" {
				prose = append(prose, b.Text)
			}
		}
	}
	reply := strings.Join(prose, "\n")
	if reply == "" {
		reply = noReply
	}
	collapsed := []Message{
		{Role: RoleUser, Content: []ContentBlock{TextBlock(s.turns[i].analyst)}},
		{Role: RoleAssistant, Content: []ContentBlock{TextBlock(reply)}},
	}
	s.spliceTurn(i, collapsed)
	s.turns[i].collapsed = true
}

// dropOldestTurn removes turn 0 from the history.
func (s *Session) dropOldestTurn() {
	s.spliceTurn(0, nil)
	s.turns = s.turns[1:]
}

// spliceTurn replaces turn i's messages with repl and shifts the later turns'
// starts.
func (s *Session) spliceTurn(i int, repl []Message) {
	start, end := s.turns[i].start, s.turnEnd(i)
	shift := len(repl) - (end - start)
	msgs := make([]Message, 0, len(s.messages)+shift)
	msgs = append(msgs, s.messages[:start]...)
	msgs = append(msgs, repl...)
	msgs = append(msgs, s.messages[end:]...)
	s.messages = msgs
	for j := i + 1; j < len(s.turns); j++ {
		s.turns[j].start += shift
	}
}

// elideResults replaces tool results from index from onward with a
// placeholder, oldest first, until the estimate is at most low. keepLast
// spares the last message's results: the round the model is about to read.
func (s *Session) elideResults(from int, keepLast bool, low int) int {
	end := len(s.messages)
	if keepLast {
		end--
	}
	n := 0
	for i := from; i < end; i++ {
		m := &s.messages[i]
		if m.Role != RoleUser {
			continue
		}
		for j := range m.Content {
			if s.estimateTokens() <= low {
				return n
			}
			b := &m.Content[j]
			if b.Type != BlockToolResult || len(b.Content) < elideMinBytes {
				continue
			}
			b.Content = elidedPlaceholder(b.Content)
			n++
		}
	}
	return n
}

// elidedPlaceholder stands in for a removed tool result, naming its size and,
// for a paged result still held, the way back to it.
func elidedPlaceholder(content string) string {
	var held struct {
		Bounded *struct {
			ResultID string `json:"result_id"`
		} `json:"result_bounded"`
	}
	way := "re-run the call if you need it"
	if json.Unmarshal([]byte(content), &held) == nil && held.Bounded != nil && held.Bounded.ResultID != "" {
		way = fmt.Sprintf("re-run the call, or read_result %s while it is held", held.Bounded.ResultID)
	}
	return fmt.Sprintf("[result removed to fit the context window: %d bytes; %s]", len(content), way)
}

// completeFitted runs one model call on a history that fits the context
// window. It compacts first when needed; on a provider's overflow refusal it
// adopts the reported window, compacts hard and retries once. Each call that
// reports usage recalibrates the bytes-to-tokens estimate. side, when set, is a
// side completion's instruction (adr/0234): it rides the request, not the
// history.
func (s *Session) completeFitted(ctx context.Context, transcript *strings.Builder, side []ContentBlock) (CompleteResponse, bool, error) {
	s.noteCompaction(transcript, s.fitContext(false), &side)
	for retried := false; ; retried = true {
		reqBytes := s.requestBytes(side)
		resp, streamed, err := s.complete(ctx, side)
		var overflow *ContextOverflowError
		if err != nil && errors.As(err, &overflow) && !retried {
			if overflow.Limit > 0 {
				s.contextTokens = overflow.Limit
			}
			if overflow.Tokens > 0 && reqBytes > 0 {
				s.tokensPerByte = float64(overflow.Tokens) / float64(reqBytes)
			}
			s.noteCompaction(transcript, s.fitContext(true), &side)
			continue
		}
		if err == nil {
			if in := resp.Usage.Input + resp.Usage.CacheWrite + resp.Usage.CacheRead; in > 0 && reqBytes > 0 {
				s.tokensPerByte = float64(in) / float64(reqBytes)
			}
		}
		return resp, streamed, err
	}
}

// noteCompaction tells the model, the record and the analyst what a
// compaction removed. The model's note rides the last message, which before a
// turn's model call is always the analyst's or the tool results'. For a side
// completion it rides the instruction, and the analyst is not told: there is
// no live turn to show it in.
func (s *Session) noteCompaction(transcript *strings.Builder, c compaction, side *[]ContentBlock) {
	if !c.any() {
		return
	}
	if *side != nil {
		*side = append(*side, TextBlock(modelNote))
		fmt.Fprintf(transcript, "[engine] %s\n", sanitizeTranscript(c.analystNote()))
		return
	}
	if n := len(s.messages); n > 0 && s.messages[n-1].Role == RoleUser {
		s.messages[n-1].Content = append(s.messages[n-1].Content, TextBlock(modelNote))
	}
	note := c.analystNote()
	fmt.Fprintf(transcript, "[engine] %s\n", sanitizeTranscript(note))
	if s.hooks.OnContextCompacted != nil {
		s.hooks.OnContextCompacted(note)
	}
}
