package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxRationaleRunes mirrors the aggregate's rationale bound (01: "terse by
// design"). The loop clips before sending; full detail lives in the transcript.
const maxRationaleRunes = 500

// defaultMaxToolRounds bounds the model↔tool loop within one analyst turn. A
// full pivot rarely needs more than a handful; the bound stops a looping model
// from burning the tool budget unattended.
const defaultMaxToolRounds = 16

// defaultMaxTokens is the per-completion output budget.
const defaultMaxTokens = 4096

// Hooks lets the surface render progress as the turn executes. All fields are
// optional.
type Hooks struct {
	OnText       func(text string)                        // model text as each round completes
	OnToolCall   func(name string, input json.RawMessage) // a tool is about to dispatch
	OnToolResult func(name string, content string, isError bool)

	// OnTextDelta receives text fragments as the model generates them (E.4).
	// When set and the LLM implements StreamingLLM, completions stream and a
	// streamed completion's text arrives ONLY here — OnText is not called for
	// it (the deltas were the delivery; a surface that got both would render
	// the text twice). OnText still fires when the LLM cannot stream, so a
	// surface sets both and renders whichever arrives.
	OnTextDelta func(delta string)

	// OnRoundStart fires as each model↔tool round begins (1-based) — the
	// step marker surfaces group tool calls under (design/ui binding §2.6).
	OnRoundStart func(round int)

	// OnContextCompacted fires when the history was condensed to fit the
	// model's context window (adr/0230), with a one-line account of what the
	// model no longer sees.
	OnContextCompacted func(note string)
}

// Config assembles a session.
type Config struct {
	Backend         *Client
	LLM             LLM
	InvestigationID string
	MaxToolRounds   int
	MaxTokens       int
	// MaxToolResultBytes bounds one tool result fed back to the model; a
	// larger one is shown a page at a time (adr/0209). Zero means
	// DefaultMaxToolResultBytes.
	MaxToolResultBytes int
	// ContextWindowTokens is the model's context window, which the session
	// keeps the history within (adr/0230). Zero means
	// DefaultContextWindowTokens.
	ContextWindowTokens int
	Hooks               Hooks
	// Clock is the engine's notion of now, stamped on every turn as a [clock]
	// block (state.go) and used wherever a tool result pre-judges time for the
	// model (list_actions' expired flag). nil means time.Now; tests fix it.
	Clock func() time.Time
}

// Session is one analyst's interactive loop over one investigation. Not safe
// for concurrent turns — the conversation is ordered.
type Session struct {
	backend         *Client
	llm             LLM
	investigationID string
	maxRounds       int
	maxTokens       int
	maxResultBytes  int
	contextTokens   int
	hooks           Hooks
	now             func() time.Time

	system   string
	tools    []ToolDef
	messages []Message
	// results holds large tool results the model reads in pages (adr/0209).
	results resultStore
	// turns marks each analyst turn in messages; tokensPerByte calibrates the
	// size estimate from reported usage; fixedBytes is the system prompt and
	// tool definitions (adr/0230).
	turns         []turnSpan
	tokensPerByte float64
	fixedBytes    int

	// held is turns whose commit never reached the backend, oldest first;
	// they are committed before the next one (adr/0231). commitBackoff is the
	// wait before each retry of a commit.
	held          []InterpretationRequest
	commitBackoff []time.Duration

	// Standing context (refreshContext is the only writer) and the hypothesis
	// set as last read (refreshHypotheses, every turn). hyps never enters the
	// system prompt: it is rendered per turn as a [reasoning state] block on
	// the user message (state.go) so the cached prefix stays byte-stable.
	investigation Investigation
	caps          []Capability
	hyps          json.RawMessage

	pendingActions []ActionResponse // actions proposed this turn, for the surface to offer approval
	// consulted accumulates the turn's recall_sops retrievals (by sop_id,
	// keeping the best score) — the knowledge provenance the commit carries.
	consulted map[string]ConsultedSOP
	// consultedSimilar accumulates the turn's recall_similar_investigations
	// retrievals (by investigation_ref, best score) — case-knowledge provenance.
	consultedSimilar map[string]ConsultedSimilarInvestigation
	// turnsSinceAnchor counts turns since the engine-state anchor last rode a
	// user message; at anchorEveryTurns the next turn re-grounds the model on
	// the authoritative action record (countering narrative drift in long
	// conversations). ResetContext sets it to fire immediately.
	turnsSinceAnchor int
	// seenIDs is the set of full-UUID tokens the engine has produced this session
	// (observed-data refs from read verbs, entity ids, ids embedded in external
	// data a tool returned, the investigation id). The ground-truth footer uses
	// it to avoid flagging a legitimately-cited non-action id as "NOT ON RECORD":
	// an id the engine itself produced is not a fabrication (agent-reliability.md
	// §3; the runtime counterpart of the eval's G4 ground truth).
	seenIDs map[string]bool
}

// TurnResult is what one analyst turn produced.
type TurnResult struct {
	// Text is the model's final response text.
	Text string
	// InterpretationID is the committed turn-summary reasoning act.
	InterpretationID string
	// PendingActions are ALL of the investigation's actions still awaiting the
	// analyst (not just this turn's proposals) — refreshed from the backend at
	// turn end, so an approval missed on an earlier turn is re-offered rather
	// than stranded. The surface offers approval on each.
	PendingActions []ActionStatus
	// ToolRounds is how many model→tool rounds the turn took.
	ToolRounds int
	// Transcript is the turn's committed transcript — the exact bytes that were
	// content-hashed into the side store (10 §1.1): Turn fails when the commit
	// fails, so a returned Transcript with a non-empty InterpretationID IS the
	// persisted record, byte for byte. The eval harness grades this (10 §5)
	// instead of scraping hook output.
	Transcript string
	// ToolCalls is the turn's committed tool-call log — the same slice that rode
	// the commit transaction (05 §3.4).
	ToolCalls []ToolCall
	// Usage is the token accounting summed over every model call this turn made
	// (a turn spans one call per tool round). Surfaces feed it to a cost readout;
	// the eval harness aggregates it into the run report.
	Usage Usage
}

// NewSession assembles the session: investigation context and the live tool
// set are fetched once (05 §3.4 steps 2–4); the system prompt embeds both.
func NewSession(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.Backend == nil || cfg.LLM == nil {
		return nil, fmt.Errorf("agent: Backend and LLM are required")
	}
	s := &Session{
		backend:         cfg.Backend,
		llm:             cfg.LLM,
		investigationID: cfg.InvestigationID,
		maxRounds:       cfg.MaxToolRounds,
		maxTokens:       cfg.MaxTokens,
		maxResultBytes:  cfg.MaxToolResultBytes,
		contextTokens:   cfg.ContextWindowTokens,
		tokensPerByte:   defaultTokensPerByte,
		commitBackoff:   defaultCommitBackoff,
		hooks:           cfg.Hooks,
		now:             cfg.Clock,
		// The investigation id is always a legit engine id — never a fabrication
		// even when the model quotes it back (e.g. from a ticket body).
		seenIDs: map[string]bool{strings.ToLower(cfg.InvestigationID): true},
	}
	if s.maxRounds <= 0 {
		s.maxRounds = defaultMaxToolRounds
	}
	if s.maxTokens <= 0 {
		s.maxTokens = defaultMaxTokens
	}
	if s.maxResultBytes <= 0 {
		s.maxResultBytes = DefaultMaxToolResultBytes
	}
	if s.contextTokens <= 0 {
		s.contextTokens = DefaultContextWindowTokens
	}
	if s.now == nil {
		s.now = time.Now
	}

	if err := s.refreshContext(ctx); err != nil {
		return nil, err
	}

	// Rehydrate the conversation (05 §3.4; the workbench's cold-restore): a
	// reconnecting session must be able to CONTINUE, not just re-read. The
	// committed transcripts are the message history — replay each turn's
	// analyst question and the model's own prose as a user/assistant pair, so
	// the model resumes with genuine memory of the exchange. Tool detail is
	// deliberately NOT replayed: collapsing to text keeps the reconstruction
	// robust (no dangling tool_use the provider would reject) and the model
	// re-queries tools for fresh data when it needs them. Best-effort — a
	// rehydration failure leaves a fresh session (context still rides the
	// system prompt), never blocks the session.
	s.rehydrate(ctx)
	return s, nil
}

// refreshContext (re)fetches the investigation, capabilities, hypotheses, and
// action catalog and rebuilds the tool set + system prompt from them. Shared by
// NewSession and ResetContext so a reset re-grounds on exactly what a fresh
// session would see.
func (s *Session) refreshContext(ctx context.Context) error {
	inv, err := s.backend.GetInvestigation(ctx, s.investigationID)
	if err != nil {
		return fmt.Errorf("agent: load investigation: %w", err)
	}
	caps, err := s.backend.ListCapabilities(ctx)
	if err != nil {
		return fmt.Errorf("agent: list capabilities: %w", err)
	}
	// Hypotheses are optional context — a fresh investigation has none.
	hyps, err := s.backend.ListHypotheses(ctx, s.investigationID)
	if err != nil {
		hyps = nil
	}
	// The write-side action catalog shapes request_action so the model uses real
	// action types. Optional: a 503 (action layer off) leaves request_action
	// generic, exactly as before.
	actionTypes, err := s.backend.ListActionTypes(ctx)
	if err != nil {
		actionTypes = nil
	}

	s.tools = buildTools(caps, actionTypes)
	s.investigation, s.caps, s.hyps = inv, caps, hyps
	s.system = systemPrompt(inv, caps)
	return nil
}

// refreshHypotheses re-reads the investigation's hypothesis set for this turn's
// [reasoning state] block. A failure keeps the last known set.
func (s *Session) refreshHypotheses(ctx context.Context) {
	hyps, err := s.backend.ListHypotheses(ctx, s.investigationID)
	if err != nil {
		return
	}
	s.hyps = hyps
}

// contextResetRationale marks a context reset in the reasoning thread. It makes
// the reset DURABLE: rehydrate refuses to replay transcripts from before the
// newest marker, so a sidecar respawn cannot re-import the discarded narration.
// Written by ResetContext, matched by rehydrate — keep them in lockstep.
const contextResetRationale = "context reset: the analyst rebuilt the agent's working conversation from the engine record; earlier narration is not replayed"

// ResetContext is the analyst's context reset: it discards the model's working
// conversation — including its own past narration, the vector by which a
// confabulated claim compounds turn over turn ("narrative poisoning") — and
// re-grounds on the engine record alone. The durable investigation state
// (events, actions, hypotheses, chronicle) is untouched: it IS the source of
// truth this rebuilds from, so no investigative state is lost. The next turn
// opens with the engine-state anchor so the model's first post-reset context is
// authoritative fact, not remembered prose. The reset itself is committed to
// the thread (attributed, auditable) and honored by future rehydration.
func (s *Session) ResetContext(ctx context.Context) error {
	if err := s.refreshContext(ctx); err != nil {
		return err
	}
	s.messages = nil
	s.turns = nil
	s.results.clear()                     // the model no longer holds the result ids
	s.turnsSinceAnchor = anchorEveryTurns // anchor immediately on the next turn

	// Turns still held from a failed commit go on the thread before the
	// marker, so rehydration treats them as reset away (adr/0231).
	if err := s.flushHeld(ctx); err != nil {
		return fmt.Errorf("context reset applied to the live session, but an earlier turn could not be recorded before it: %w", err)
	}

	// Durability: without the marker, the next sidecar respawn would rehydrate
	// the discarded narration straight back. The reset is itself a recorded,
	// attributed act on the thread.
	if _, err := s.backend.RecordInterpretation(ctx, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: "other",
		Rationale:          contextResetRationale,
	}); err != nil {
		return fmt.Errorf("context reset applied to the live session, but recording it failed (a sidecar respawn would replay the old narration): %w", err)
	}
	return nil
}

// maxRehydratedTurns bounds how many prior turns seed the conversation on
// reconnect — recent context matters most, and the whole history need not
// ride every resumed turn's prompt.
const maxRehydratedTurns = 20

// rehydrate seeds s.messages from the committed thread so a reconnected
// session continues the conversation. Best-effort and side-effect-free on
// failure. It honors the newest context-reset marker: transcripts from before
// it are NOT replayed — an analyst's reset survives sidecar respawns instead of
// silently re-importing the narration they discarded.
func (s *Session) rehydrate(ctx context.Context) {
	thread, err := s.backend.Thread(ctx, s.investigationID)
	if err != nil {
		return
	}
	var resetAfter int64 = -1
	for _, e := range thread {
		if strings.HasPrefix(e.Summary, "context reset:") && e.SequenceNo > resetAfter {
			resetAfter = e.SequenceNo
		}
	}
	var withTranscript []ThreadEntry
	for _, e := range thread {
		if e.HasTranscript && e.SequenceNo > resetAfter {
			withTranscript = append(withTranscript, e)
		}
	}
	if len(withTranscript) > maxRehydratedTurns {
		withTranscript = withTranscript[len(withTranscript)-maxRehydratedTurns:]
	}
	var msgs []Message
	var turns []turnSpan
	for _, e := range withTranscript {
		body, terr := s.backend.Transcript(ctx, e.InterpretationID)
		if terr != nil {
			continue
		}
		userMsg, assistantMsg := parseTranscriptTurn(body)
		// A rehydrated turn is already its words and prose: a whole pair, or
		// nothing, so the history alternates and every turn starts on the
		// analyst.
		if userMsg == "" || assistantMsg == "" {
			continue
		}
		turns = append(turns, turnSpan{start: len(msgs), analyst: userMsg, collapsed: true})
		msgs = append(msgs,
			Message{Role: RoleUser, Content: []ContentBlock{TextBlock(userMsg)}},
			Message{Role: RoleAssistant, Content: []ContentBlock{TextBlock(assistantMsg)}})
	}
	if len(msgs) > 0 {
		s.messages, s.turns = msgs, turns
	}
}

// parseTranscriptTurn extracts the analyst question and the model's joined
// prose from one line-framed committed transcript (session.go's own format:
// [user]/[assistant]/[tool_use]/[tool_result] records, content newlines
// escaped). Tool lines are dropped — the conversational thread is what
// continuation needs; tool detail is re-fetchable and lives in the transcript.
func parseTranscriptTurn(body string) (userMsg, assistantMsg string) {
	var user strings.Builder
	var assistant strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, "[user] "); ok {
			if user.Len() > 0 {
				user.WriteByte('\n')
			}
			user.WriteString(unsanitizeTranscript(rest))
		} else if rest, ok := strings.CutPrefix(line, "[assistant] "); ok {
			if assistant.Len() > 0 {
				assistant.WriteByte('\n')
			}
			assistant.WriteString(unsanitizeTranscript(rest))
		}
	}
	return strings.TrimSpace(user.String()), strings.TrimSpace(assistant.String())
}

// unsanitizeTranscript reverses sanitizeTranscript's newline escaping so a
// rehydrated message reads as the model wrote it.
func unsanitizeTranscript(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	return strings.NewReplacer("\\n", "\n", "\\r", "\r").Replace(s)
}

// Tools exposes the assembled tool set (for surfaces that display it).
func (s *Session) Tools() []ToolDef { return s.tools }

// System exposes the assembled system prompt. The eval harness hashes it as the
// prompt-version identifier for run attribution (09 §4.2, 10 §1.4) until the
// product stamps a version itself.
func (s *Session) System() string { return s.system }

// PromptVersion is the base-prompt version this session runs under (see
// PromptVersion): the same on every investigation and install that shares
// the prompt code and the framing dial.
func (s *Session) PromptVersion() string {
	return PromptVersion(s.investigation.HypothesisFraming)
}

// HypothesisFraming is the framing dial the session's prompt was rendered
// under, defaulted (framingOrDefault) — attribution for the prompt version.
func (s *Session) HypothesisFraming() string {
	return framingOrDefault(s.investigation.HypothesisFraming)
}

// Turn runs one analyst turn: the model reasons and dispatches tools until it
// ends its turn (or the round budget is spent), then the whole turn — full
// transcript bytes plus the tool-call log — is committed to the reasoning
// thread as one interpretation (05 §3.4). A turn that ends early, on a model
// error or a cancel, is committed too, with a line saying how it ended
// (adr/0231). The turn is returned even when the model call or the commit
// fails; the error reports both. A commit that cannot reach the backend is
// held and committed before the next turn's.
func (s *Session) Turn(ctx context.Context, userMsg string) (*TurnResult, error) {
	s.pendingActions = nil
	s.consulted = map[string]ConsultedSOP{}
	s.consultedSimilar = map[string]ConsultedSimilarInvestigation{}
	// The reasoning state must be current at every turn: between turns the
	// analyst adopts with a restatement, frames a rival by hand, adds or
	// withdraws predictions — and the competing-set contract asks the model to
	// read EVERY live hypothesis (01 §x-hypothesis). One GET; a failure keeps
	// the last known set rather than failing the turn.
	s.refreshHypotheses(ctx)
	turnID := uuid.NewString()

	var transcript strings.Builder
	fmt.Fprintf(&transcript, "[user] %s\n", sanitizeTranscript(userMsg))

	var toolLog []ToolCall
	var turnUsage Usage
	var finalText strings.Builder

	// Engine state rides the user message, never the system prompt (state.go):
	// the turn clock, the hypothesis set as the engine holds it now, and — every
	// anchorEveryTurns and immediately after a ResetContext — the authoritative
	// action record, so the model re-grounds on fact rather than its own
	// accumulated narration. Each is deterministic: it states only what the
	// engine holds.
	var userContent []ContentBlock
	engine := func(block string) {
		userContent = append(userContent, TextBlock(block))
		fmt.Fprintf(&transcript, "[engine] %s\n", sanitizeTranscript(block))
	}
	engine(clockBlock(s.now()))
	if hb := hypothesisStateBlock(s.hyps); hb != "" {
		engine(hb)
	}
	s.turnsSinceAnchor++
	if s.turnsSinceAnchor >= anchorEveryTurns {
		if anchor := s.engineStateAnchor(ctx); anchor != "" {
			engine(anchor)
			s.turnsSinceAnchor = 0
		}
	}
	userContent = append(userContent, TextBlock(userMsg))

	// A turn aborted by a model-call failure leaves the history ending in a
	// user message (the tool results, or the aborted turn's own text). Merge
	// rather than append a sibling — consecutive same-role messages violate the
	// provider contract.
	// The merged turn joins the aborted one's span (adr/0230).
	if n := len(s.messages); n > 0 && s.messages[n-1].Role == RoleUser {
		s.messages[n-1].Content = append(s.messages[n-1].Content, userContent...)
		if t := len(s.turns); t > 0 {
			s.turns[t-1].analyst += "\n" + userMsg
		} else {
			s.turns = append(s.turns, turnSpan{start: 0, analyst: userMsg})
		}
	} else {
		s.turns = append(s.turns, turnSpan{start: len(s.messages), analyst: userMsg})
		s.messages = append(s.messages, Message{Role: RoleUser, Content: userContent})
	}

	rounds := 0
	var turnErr error
	for {
		if s.hooks.OnRoundStart != nil {
			s.hooks.OnRoundStart(rounds + 1)
		}
		resp, streamed, err := s.completeFitted(ctx, &transcript)
		if err != nil {
			// The turn ends here but is still closed and committed below: tool
			// calls earlier in it may already have recorded hypotheses or
			// requested actions, and the record must say why (adr/0231).
			turnErr = fmt.Errorf("agent: model call: %w", err)
			ended := "model call failed: " + err.Error()
			if ctx.Err() != nil {
				ended = "cancelled by the analyst"
			}
			fmt.Fprintf(&transcript, "[engine] turn ended early: %s\n", sanitizeTranscript(ended))
			break
		}
		turnUsage.Add(resp.Usage)
		// History must stay serializable: a call whose input is not a JSON
		// object (a max_tokens cut-off mid-input) enters it as {} and never runs.
		assistant, cutOff := sealToolInputs(resp.Content)
		s.messages = append(s.messages, Message{Role: RoleAssistant, Content: assistant})

		var toolUses []ContentBlock
		var cutResults []ContentBlock
		for _, blk := range assistant {
			switch blk.Type {
			case BlockText:
				if blk.Text != "" {
					fmt.Fprintf(&transcript, "[assistant] %s\n", sanitizeTranscript(blk.Text))
					finalText.WriteString(blk.Text)
					// A streamed completion's text already reached the surface as
					// deltas (Hooks contract) — firing OnText too would double it.
					if !streamed && s.hooks.OnText != nil {
						s.hooks.OnText(blk.Text)
					}
				}
			case BlockToolUse:
				if n, cut := cutOff[blk.ToolUseID]; cut {
					fmt.Fprintf(&transcript, "[tool_use %s id=%s] input cut off after %d bytes; not run\n",
						sanitizeTranscript(blk.ToolName), sanitizeTranscript(blk.ToolUseID), n)
					cutResults = append(cutResults, abandonedResults(&transcript, []ContentBlock{blk}, cutOffReason)...)
					continue
				}
				toolUses = append(toolUses, blk)
			}
		}

		if resp.StopReason != StopToolUse || len(toolUses) == 0 {
			// A stop that still carried tool calls (a max_tokens cut-off
			// mid-tool_use) must not leave them dangling: every tool_use has to be
			// answered by a tool_result, or the NEXT turn's model call — this
			// session's message history is reused across turns — is a contract
			// violation the provider rejects. Close them off.
			s.answerToolUses(append(cutResults,
				abandonedResults(&transcript, toolUses, "turn ended before this tool was dispatched")...))
			break
		}
		rounds++
		if rounds > s.maxRounds {
			fmt.Fprintf(&transcript, "[loop] tool budget exhausted after %d rounds\n", s.maxRounds)
			s.answerToolUses(append(cutResults,
				abandonedResults(&transcript, toolUses, "tool budget exhausted; tool not dispatched")...))
			break
		}

		results := cutResults
		for _, tu := range toolUses {
			fmt.Fprintf(&transcript, "[tool_use %s id=%s] %s\n",
				sanitizeTranscript(tu.ToolName), sanitizeTranscript(tu.ToolUseID), sanitizeTranscript(string(tu.Input)))
			toolLog = append(toolLog, ToolCall{CallID: tu.ToolUseID, ToolName: tu.ToolName, Args: tu.Input})
			if s.hooks.OnToolCall != nil {
				s.hooks.OnToolCall(tu.ToolName, tu.Input)
			}

			content, isErr := s.dispatchTool(ctx, tu.ToolName, tu.Input)
			if !isErr {
				s.rememberIDs(content)
			}
			if tu.ToolName == ToolRecallSOPs && !isErr {
				s.trackConsulted(content)
			}
			if (tu.ToolName == ToolRecallSimilar || tu.ToolName == ToolPriorInvestigations) && !isErr {
				s.trackConsultedSimilar(content)
			}
			fmt.Fprintf(&transcript, "[tool_result %s id=%s error=%v] %s\n",
				sanitizeTranscript(tu.ToolName), sanitizeTranscript(tu.ToolUseID), isErr, sanitizeTranscript(content))
			if s.hooks.OnToolResult != nil {
				s.hooks.OnToolResult(tu.ToolName, content, isErr)
			}
			results = append(results, ContentBlock{
				Type:      BlockToolResult,
				ToolUseID: tu.ToolUseID,
				Content:   content,
				IsError:   isErr,
			})
		}
		s.messages = append(s.messages, Message{Role: RoleUser, Content: results})
	}

	// The turn's closing work runs even when the turn was cancelled: a cancel
	// stops the model, not the record (adr/0231).
	closing, cancelClosing := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancelClosing()

	// Deterministic ground-truth backstop (the prompt honesty rules have proven
	// insufficient): state the real status of every action_id the model cited,
	// and attest when a creation claim had no matching request_action — so a
	// fabricated id or narrated-but-never-made call is corrected IN PLACE:
	// visible to the analyst, committed to the transcript, and folded into the
	// assistant message so the correction is in the model's own history.
	if gt := s.turnFooter(closing, finalText.String(), len(s.pendingActions), s.seenIDs); gt != "" {
		fmt.Fprintf(&transcript, "[assistant] %s\n", sanitizeTranscript(gt))
		finalText.WriteString("\n\n")
		finalText.WriteString(gt)
		if s.hooks.OnText != nil {
			s.hooks.OnText("\n\n" + gt)
		}
		if n := len(s.messages); n > 0 && s.messages[n-1].Role == RoleAssistant {
			s.messages[n-1].Content = append(s.messages[n-1].Content, TextBlock(gt))
		}
	}

	// PendingActions rides even a failed turn: an action the model already
	// requested is pending server-side, and the surface must offer it.
	res := &TurnResult{
		Text:           finalText.String(),
		PendingActions: s.refreshPending(closing),
		ToolRounds:     rounds,
		Transcript:     transcript.String(),
		ToolCalls:      toolLog,
		Usage:          turnUsage,
	}

	// Commit the turn to the reasoning thread: the transcript bytes are hashed
	// into the content-addressed side store and the tool-call log rides the same
	// transaction (05 §3.4). The rationale is the terse summary; the transcript
	// is the record.
	rationale := clipRunes(strings.TrimSpace(finalText.String()), maxRationaleRunes)
	switch {
	case rationale != "":
	case turnErr != nil:
		rationale = "(turn ended early — see transcript)"
	default:
		rationale = "(no final text — see transcript)"
	}
	id, err := s.commitTurn(closing, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: "other",
		Rationale:          rationale,
		Transcript:         &Transcript{TurnID: turnID, Body: transcript.String()},
		ToolCalls:          toolLog,
		ConsultedSOPs:      s.consultedList(finalText.String()),
		ConsultedSimilar:   s.consultedSimilarList(finalText.String()),
	})
	res.InterpretationID = id
	return res, errors.Join(turnErr, err)
}

// summarizeInstruction asks the model for the knowledge-base narrative at
// conclusion (design/06 §3.2, client-side tier). Grounding rules ride in the
// instruction: the narrative feeds similarity recall in FUTURE investigations,
// so it must be a faithful digest of the record, not a story.
const summarizeInstruction = "The investigation has just been concluded. Write a knowledge-base summary " +
	"narrative of it in 150–250 words of plain prose (no headers, no lists): what was investigated " +
	"(the seed), what the evidence showed, the verdict and why, the key entities and MITRE techniques, " +
	"what actions were taken and their outcomes, and anything a future analyst handling a similar case " +
	"should know. State only what is in the record — no speculation, no embellishment. " +
	"Respond with the narrative text alone."

// SummarizeConcluded writes the concluded investigation's knowledge-summary
// narrative — the client-side tier of the two-tier summary (design/06 §3.2):
// the server wrote the structured baseline deterministically at conclusion;
// this runs one BYOK model completion over the session's own conversation (the
// richest available context — the model lived the investigation) and submits
// the prose, which the server merges as a revision. The model key never
// leaves this process. The exchange is deliberately NOT appended to the
// conversation — it is a side product, not a turn. A fresh session (sidecar
// respawn) rehydrates from the committed thread first. Returns the narrative.
func (s *Session) SummarizeConcluded(ctx context.Context) (string, error) {
	if len(s.messages) == 0 {
		s.rehydrate(ctx)
	}
	msgs := append(append([]Message{}, s.messages...),
		Message{Role: RoleUser, Content: []ContentBlock{TextBlock(summarizeInstruction)}})
	resp, err := s.llm.Complete(ctx, CompleteRequest{
		System:    s.system,
		Messages:  msgs,
		MaxTokens: s.maxTokens,
		// No tools: this is a single narrative completion, not an agentic turn.
	})
	if err != nil {
		return "", fmt.Errorf("agent: summarize concluded: %w", err)
	}
	var narrative strings.Builder
	for _, blk := range resp.Content {
		if blk.Type == BlockText {
			narrative.WriteString(blk.Text)
		}
	}
	text := strings.TrimSpace(narrative.String())
	if text == "" {
		return "", fmt.Errorf("agent: summarize concluded: the model returned no text")
	}
	model := ""
	if m, ok := s.llm.(interface{ ModelName() string }); ok {
		model = m.ModelName()
	}
	// The structured baseline is written by the post-conclusion pipeline, which
	// may still be in flight moments after conclude — retry once on not-found
	// before giving up (the narrative is an enhancement; the baseline stands).
	err = s.backend.SubmitSummaryNarrative(ctx, s.investigationID, text, model)
	if err != nil && strings.Contains(err.Error(), "404") {
		select {
		case <-time.After(summaryRetryDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		err = s.backend.SubmitSummaryNarrative(ctx, s.investigationID, text, model)
	}
	if err != nil {
		return "", fmt.Errorf("agent: submit summary narrative: %w", err)
	}
	return text, nil
}

// summaryRetryDelay is the single not-found retry backoff in
// SummarizeConcluded — long enough for the post-conclusion pipeline's
// structured write, short enough not to hang the conclude UX.
var summaryRetryDelay = 3 * time.Second

// SOPCandidate is a draft standing procedure the model proposes from a concluded
// investigation — a GENERALIZATION over this case plus the SOPs and similar past
// cases the session consulted, not a retelling of the one investigation. It is
// NOT persisted here: the analyst reviews it in the workbench and decides to add
// it to the SOP library or discard it (the compounding loop, closed by a human).
// Warranted=false means the model judged no new procedure worth capturing (e.g.
// an existing SOP already covers it); Rationale says why either way.
type SOPCandidate struct {
	Warranted      bool     `json:"warranted"`
	Title          string   `json:"title"`
	Body           string   `json:"body"` // markdown runbook prose
	Tags           []string `json:"tags"`
	Recommendation string   `json:"recommendation"`
	Rationale      string   `json:"rationale"`
}

// proposeSOPInstruction asks for one structured candidate SOP. It leans on the
// SOPs + similar past cases the session recalled (they are in its
// conversation) so the model generalizes rather than duplicates.
const proposeSOPInstruction = "The investigation has just been concluded. Consider whether it yields a " +
	"REUSABLE procedure worth capturing as a standing SOP — a generalization a future analyst could " +
	"follow on a similar case, not a retelling of this one. Draw on the SOPs and similar past " +
	"investigations already in your context: if an existing SOP already covers this, do NOT propose a " +
	"duplicate. Only propose when there is a genuine, generalizable procedure. " +
	"Respond with a SINGLE JSON object and nothing else: " +
	`{"warranted": bool, "title": string, "body": string (a markdown runbook: scope, triage, response, ` +
	`do-not), "tags": [string] (lowercase, include any MITRE technique ids), "recommendation": string ` +
	`(one short imperative, e.g. "isolate"), "rationale": string (why this generalizes, or if not ` +
	`warranted, why not)}. If warranted is false, leave title/body/tags/recommendation empty and explain ` +
	"in rationale."

// proposeSOPAnyway is added when the analyst asked for a draft after the model
// declined (adr/0156): the analyst has judged it worth a procedure.
const proposeSOPAnyway = " The analyst has read your earlier answer and asked for a draft anyway: set " +
	"warranted to true and write the best generalizable procedure you can, and say in rationale which " +
	"existing SOP it overlaps, if any."

// ProposeSOP runs one BYOK model completion over the session's own conversation
// and returns a candidate SOP generalized from the concluded investigation — the
// client tier of candidate-SOP generation (design/06: the compounding loop). It
// is the twin of SummarizeConcluded: no tools, the model key never leaves this
// process, the exchange is NOT appended to the conversation (a side product, not
// a turn), and a fresh session (sidecar respawn) rehydrates from the committed
// thread first. Unlike the summary, nothing is written here — the candidate goes
// back to the workbench for the analyst to accept (create the SOP) or discard.
func (s *Session) ProposeSOP(ctx context.Context, force bool) (SOPCandidate, error) {
	if len(s.messages) == 0 {
		s.rehydrate(ctx)
	}
	instruction := proposeSOPInstruction
	if force {
		instruction += proposeSOPAnyway
	}
	msgs := append(append([]Message{}, s.messages...),
		Message{Role: RoleUser, Content: []ContentBlock{TextBlock(instruction)}})
	resp, err := s.llm.Complete(ctx, CompleteRequest{
		System:    s.system,
		Messages:  msgs,
		MaxTokens: s.maxTokens,
		// No tools: a single structured completion, not an agentic turn.
	})
	if err != nil {
		return SOPCandidate{}, fmt.Errorf("agent: propose sop: %w", err)
	}
	var out strings.Builder
	for _, blk := range resp.Content {
		if blk.Type == BlockText {
			out.WriteString(blk.Text)
		}
	}
	cand, err := parseSOPCandidate(out.String())
	if err != nil {
		return SOPCandidate{}, fmt.Errorf("agent: propose sop: %w", err)
	}
	return cand, nil
}

// parseSOPCandidate extracts the JSON object from the model's reply, tolerating
// surrounding prose or ```json fences by taking the first '{' through the last
// '}'. A body that isn't valid JSON is an error the caller softens (candidate
// generation is best-effort, like the summary narrative).
func parseSOPCandidate(text string) (SOPCandidate, error) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return SOPCandidate{}, fmt.Errorf("no JSON object in model reply")
	}
	var cand SOPCandidate
	if err := json.Unmarshal([]byte(text[start:end+1]), &cand); err != nil {
		return SOPCandidate{}, fmt.Errorf("parse candidate JSON: %w", err)
	}
	return cand, nil
}

// complete runs one model call, streaming when both sides can (the surface
// registered OnTextDelta and the provider implements StreamingLLM). streamed
// reports whether deltas carried the text, so the caller skips OnText for it.
func (s *Session) complete(ctx context.Context) (resp CompleteResponse, streamed bool, err error) {
	req := CompleteRequest{
		System:    s.system,
		Messages:  s.messages,
		Tools:     s.tools,
		MaxTokens: s.maxTokens,
	}
	if s.hooks.OnTextDelta != nil {
		if sl, ok := s.llm.(StreamingLLM); ok {
			resp, err = sl.CompleteStream(ctx, req, s.hooks.OnTextDelta)
			return resp, true, err
		}
	}
	resp, err = s.llm.Complete(ctx, req)
	return resp, false, err
}

// trackConsulted accumulates one recall_sops result's retrievals into the
// turn's knowledge provenance (dedup by sop_id, best score kept).
func (s *Session) trackConsulted(content string) {
	var res struct {
		Results []struct {
			SOPID string  `json:"sop_id"`
			Title string  `json:"title"`
			Score float64 `json:"score"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(content), &res) != nil {
		return
	}
	for _, r := range res.Results {
		if r.SOPID == "" {
			continue
		}
		if prev, ok := s.consulted[r.SOPID]; !ok || r.Score > prev.RetrievalScore {
			s.consulted[r.SOPID] = ConsultedSOP{SOPID: r.SOPID, Title: r.Title, RetrievalScore: r.Score}
		}
	}
}

// consultedList finalizes the turn's knowledge provenance. Used is decided
// CONSERVATIVELY: only when the model's own final text references the SOP (by
// title or id) — retrieval alone is "consulted, not applied", and overclaiming
// "followed the SOP" would be fabricated provenance (01 schema; the transcript
// is deliberately not searched: it contains the retrieval results themselves,
// which would mark everything used).
func (s *Session) consultedList(finalText string) []ConsultedSOP {
	if len(s.consulted) == 0 {
		return nil
	}
	lower := strings.ToLower(finalText)
	out := make([]ConsultedSOP, 0, len(s.consulted))
	for _, c := range s.consulted {
		c.Used = (c.Title != "" && strings.Contains(lower, strings.ToLower(c.Title))) ||
			strings.Contains(lower, strings.ToLower(c.SOPID))
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RetrievalScore > out[j].RetrievalScore })
	return out
}

// trackConsultedSimilar accumulates one recall_similar_investigations result's
// retrievals into the turn's case-knowledge provenance (dedup by
// investigation_ref, best score kept).
func (s *Session) trackConsultedSimilar(content string) {
	type hit struct {
		InvestigationRef string  `json:"investigation_ref"`
		Title            string  `json:"title"`
		Score            float64 `json:"score"`
		Band             string  `json:"band"`
	}
	var res struct {
		Results []hit `json:"results"`
		// prior_investigations answers with the exact matches here (adr/0163):
		// consulted the same way, with no similarity score.
		Investigations []hit `json:"investigations"`
	}
	if json.Unmarshal([]byte(content), &res) != nil {
		return
	}
	for _, r := range append(res.Results, res.Investigations...) {
		if r.InvestigationRef == "" {
			continue
		}
		if prev, ok := s.consultedSimilar[r.InvestigationRef]; !ok || r.Score > prev.RetrievalScore {
			s.consultedSimilar[r.InvestigationRef] = ConsultedSimilarInvestigation{
				InvestigationRef: r.InvestigationRef, Title: r.Title, RetrievalScore: r.Score, Band: r.Band,
			}
		}
	}
}

// consultedSimilarList finalizes the turn's case-knowledge provenance, with the
// same conservative Used rule as consultedList: only when the model's own final
// text references the prior investigation (by title or ref).
func (s *Session) consultedSimilarList(finalText string) []ConsultedSimilarInvestigation {
	if len(s.consultedSimilar) == 0 {
		return nil
	}
	lower := strings.ToLower(finalText)
	out := make([]ConsultedSimilarInvestigation, 0, len(s.consultedSimilar))
	for _, c := range s.consultedSimilar {
		c.Used = (c.Title != "" && strings.Contains(lower, strings.ToLower(c.Title))) ||
			strings.Contains(lower, strings.ToLower(c.InvestigationRef))
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RetrievalScore > out[j].RetrievalScore })
	return out
}

// refreshPending returns the investigation's actions still awaiting approval,
// straight from the backend — the authoritative queue, covering actions from
// earlier turns (or earlier sessions) whose approval offer was missed. When the
// list endpoint fails, it degrades to this turn's request responses so a
// just-proposed action is still offered.
func (s *Session) refreshPending(ctx context.Context) []ActionStatus {
	if acts, err := s.backend.ListActions(ctx, s.investigationID); err == nil {
		var out []ActionStatus
		for _, a := range acts {
			if a.Pending() {
				out = append(out, a)
			}
		}
		return out
	}
	var out []ActionStatus
	for _, r := range s.pendingActions {
		if r.Status == "PENDING_MANUAL" || r.Status == "PENDING_TWO_PARTY" {
			out = append(out, ActionStatus{ActionID: r.ActionID, Tier: r.Tier, Status: r.Status})
		}
	}
	return out
}

// cutOffReason answers a tool call whose input did not arrive whole.
const cutOffReason = "this call's input was cut off at the output limit and was not run; " +
	"issue it again with a shorter input"

// sealToolInputs returns content with every tool_use input that is not a JSON
// object replaced by {}, and the byte length each such input arrived with,
// keyed by tool_use id. A stream stopped on max_tokens mid-tool_use delivers
// the input JSON unfinished; kept as received it would fail to marshal on every
// later model call, since the session reuses its history across turns. The
// input is never repaired: closing the brackets would invent arguments the
// model did not finish (half a target list), so a sealed call is not run.
// An empty input is a call with no arguments, not a cut-off.
func sealToolInputs(content []ContentBlock) ([]ContentBlock, map[string]int) {
	var cutOff map[string]int
	var out []ContentBlock
	for i, blk := range content {
		if blk.Type != BlockToolUse || isJSONObject(blk.Input) {
			continue
		}
		if out == nil {
			out = slices.Clone(content)
			cutOff = map[string]int{}
		}
		if len(blk.Input) > 0 {
			cutOff[blk.ToolUseID] = len(blk.Input)
		}
		out[i].Input = json.RawMessage(`{}`)
	}
	if out == nil {
		return content, nil
	}
	return out, cutOff
}

// isJSONObject reports whether raw is a well-formed JSON object.
func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

// abandonedResults closes off tool calls the turn accepted but will not
// dispatch (the round budget is spent, the model stopped mid-tool_use on
// max_tokens, or the input was cut off). The Messages API requires every
// tool_use to be answered by a tool_result; because the session reuses its
// message history across turns, a dangling tool_use would make every
// subsequent turn's model call fail. Synthetic is_error results keep the
// conversation valid and tell the model why the call did not run.
func abandonedResults(transcript *strings.Builder, toolUses []ContentBlock, reason string) []ContentBlock {
	results := make([]ContentBlock, 0, len(toolUses))
	for _, tu := range toolUses {
		fmt.Fprintf(transcript, "[tool_result %s id=%s error=true] %s\n",
			sanitizeTranscript(tu.ToolName), sanitizeTranscript(tu.ToolUseID), reason)
		results = append(results, ContentBlock{
			Type:      BlockToolResult,
			ToolUseID: tu.ToolUseID,
			Content:   reason,
			IsError:   true,
		})
	}
	return results
}

// answerToolUses appends the tool results that answer the last assistant
// message, as one user message.
func (s *Session) answerToolUses(results []ContentBlock) {
	if len(results) == 0 {
		return
	}
	s.messages = append(s.messages, Message{Role: RoleUser, Content: results})
}

// sanitizeTranscript neutralizes line breaks in model- or tool-supplied text
// before it is interpolated into the line-framed transcript. The transcript is
// the content-hashed audit record; without this, model output (or an injected
// tool result) could embed a literal newline followed by a fake "[tool_result
// ...]" / "[tool_use ...]" line and masquerade as reckon's own framing. The
// structured tool-call log is built from the real dispatch, not this text, so it
// is unaffected either way — this closes the human-readable-record gap.
func sanitizeTranscript(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\n", "\\n", "\r", "\\r").Replace(s)
}

// clipRunes truncates s to at most n runes.
func clipRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
