package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ask outside an investigation (adr/0111; design/ui 09 §9.2): one bounded loop
// of a knowledge-only model over the two recall tools, answering an analyst's
// question from the retrieved text alone, citing what it used, writing nothing
// anywhere — no interpretation, no thread entry, no side-store row.

// askPreamble opens the Ask prompt. It states what the assistant is, not what
// any scenario looks like (the fixture-vocabulary guard covers it).
const askPreamble = "You are reckon's knowledge assistant: you answer a human SOC analyst's question from the organization's recorded knowledge — its procedures and its concluded investigations — outside any investigation. Nothing you say is recorded anywhere.\n"

// askLedger is the Ask prompt, rendered like the base prompt (adr/0057): every
// rule carries its provenance, its principle, and the mechanism that also
// enforces it. The eval has no Ask scenario at v0; the loop is driven by a unit
// test against a scripted model, and the wording awaits acceptance under #111.
var askLedger = []promptSection{
	{
		Title: "Answering from the knowledge base",
		Rules: []promptRule{
			{
				ID:                "ask-knowledge-only",
				Origin:            "#111",
				Why:               "An answer outside an investigation carries no evidence of its own; its only ground is the text the organization recorded, so anything not in that text is not an answer.",
				Enforced:          "sidecar: the Ask loop offers only the two recall tools; a citation to an item it did not retrieve is dropped",
				PendingValidation: "#111",
				Text:              "Answer only from the text the recall tools return. Do not supply the substance of an answer from general knowledge. When the retrieved text does not cover the question, say that it is not covered rather than filling the gap.",
			},
			{
				ID:                "ask-cite",
				Origin:            "#111",
				Why:               "A reader must be able to open what the answer rests on; an uncited sentence is a claim without a source.",
				Enforced:          "sidecar: [n] markers resolve to the retrieved items; others are removed",
				PendingValidation: "#111",
				Text:              "Cite each statement with the [n] marker of the retrieved item it comes from, using the n the tool result gave. Never cite an item you did not retrieve.",
			},
			{
				ID:                "ask-no-investigation",
				Origin:            "#111",
				Why:               "Reading the knowledge base is not investigating: no observation, no record, no action is warranted or possible here.",
				Enforced:          "sidecar: no record_* or capability tools are offered; no session exists",
				PendingValidation: "#111",
				Text:              "You are outside any investigation: observe nothing, record nothing, and propose no actions. If the question needs live data, say that it needs an investigation.",
			},
			{
				ID:                "ask-brief",
				Origin:            "#111",
				Why:               "The analyst is deciding what to read next; the answer points at the knowledge, it does not replace it.",
				PendingValidation: "#111",
				Text:              "Answer briefly, in the analyst's terms, quoting a procedure's own words where the wording matters, and name which procedure or past case to open.",
			},
		},
	},
}

// askPrompt renders the Ask system prompt from its ledger.
func askPrompt() string {
	var b strings.Builder
	b.WriteString(askPreamble)
	for _, sec := range askLedger {
		b.WriteString("\n## ")
		b.WriteString(sec.Title)
		b.WriteString("\n\n")
		for _, r := range sec.Rules {
			b.WriteString("- ")
			b.WriteString(r.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// AskPromptVersion is the content hash of the Ask prompt, pinned by
// TestPrompt_Frozen like the base prompt's versions.
func AskPromptVersion() string { return hashText(askPrompt()) }

// AskConfig assembles one Ask: the backend client (its tokens), the model, and
// the optional delta sink the surface streams through.
type AskConfig struct {
	Backend       *Client
	LLM           LLM
	MaxTokens     int
	MaxToolRounds int
	OnTextDelta   func(string)
}

// AskCitation is one retrieved item the answer cited: its marker, what it is
// and where it is.
type AskCitation struct {
	N        int             `json:"n"`
	Kind     string          `json:"kind"` // sop | case
	Ref      string          `json:"ref"`
	Title    string          `json:"title"`
	Location json.RawMessage `json:"location,omitempty"`
}

// AskResult is the answer: the text with only resolvable markers kept, the
// citations in marker order, the model, and the recall tools the loop ran.
type AskResult struct {
	Text      string        `json:"text"`
	Citations []AskCitation `json:"citations"`
	Model     string        `json:"model"`
	ToolsUsed []string      `json:"tools_used"`
}

// askItem is one retrieved item numbered for citation.
type askItem struct {
	N        int             `json:"n"`
	Kind     string          `json:"kind"`
	Ref      string          `json:"ref"`
	Title    string          `json:"title"`
	Excerpt  string          `json:"excerpt,omitempty"`
	Score    float64         `json:"score,omitempty"`
	Band     string          `json:"band,omitempty"`
	Location json.RawMessage `json:"location,omitempty"`
}

var askMarkerRe = regexp.MustCompile(`\[(\d{1,3})\]`)

// Ask runs one bounded loop: the model may call recall_sops and
// recall_similar_investigations, each result re-rendered with a citation
// number per item, then answers. A marker to an item the loop did not retrieve
// is dropped from the text, so "answered from the knowledge base only" is a
// checked fact, not a prompt's promise. Nothing is written anywhere.
func Ask(ctx context.Context, cfg AskConfig, question string) (AskResult, error) {
	if cfg.Backend == nil || cfg.LLM == nil {
		return AskResult{}, fmt.Errorf("agent: Ask: Backend and LLM are required")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return AskResult{}, fmt.Errorf("agent: Ask: the question is empty")
	}
	maxRounds := cfg.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 4
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	var tools []ToolDef
	for _, td := range intrinsicTools(nil) {
		if td.Name == ToolRecallSOPs || td.Name == ToolRecallSimilar {
			tools = append(tools, td)
		}
	}
	msgs := []Message{{Role: RoleUser, Content: []ContentBlock{TextBlock(question)}}}
	var items []askItem
	seenRef := map[string]int{}
	toolsUsed := []string{}
	usedSet := map[string]bool{}
	var final strings.Builder

	complete := func() (CompleteResponse, error) {
		req := CompleteRequest{System: askPrompt(), Messages: msgs, Tools: tools, MaxTokens: maxTokens}
		if cfg.OnTextDelta != nil {
			if sl, ok := cfg.LLM.(StreamingLLM); ok {
				return sl.CompleteStream(ctx, req, cfg.OnTextDelta)
			}
		}
		return cfg.LLM.Complete(ctx, req)
	}
	for round := 0; ; round++ {
		resp, err := complete()
		if err != nil {
			return AskResult{}, fmt.Errorf("agent: Ask: model call: %w", err)
		}
		msgs = append(msgs, Message{Role: RoleAssistant, Content: resp.Content})
		var uses []ContentBlock
		for _, blk := range resp.Content {
			switch blk.Type {
			case BlockText:
				final.WriteString(blk.Text)
			case BlockToolUse:
				uses = append(uses, blk)
			}
		}
		if resp.StopReason != StopToolUse || len(uses) == 0 || round >= maxRounds {
			break
		}
		var results []ContentBlock
		for _, tu := range uses {
			content, isErr := askDispatch(ctx, cfg.Backend, tu.ToolName, tu.Input, &items, seenRef)
			if !usedSet[tu.ToolName] {
				usedSet[tu.ToolName] = true
				toolsUsed = append(toolsUsed, tu.ToolName)
			}
			results = append(results, ContentBlock{Type: BlockToolResult, ToolUseID: tu.ToolUseID, Content: content, IsError: isErr})
		}
		msgs = append(msgs, Message{Role: RoleUser, Content: results})
	}

	// Resolve markers: keep those naming a retrieved item, drop the rest.
	byN := map[int]askItem{}
	for _, it := range items {
		byN[it.N] = it
	}
	cited := map[int]bool{}
	text := askMarkerRe.ReplaceAllStringFunc(final.String(), func(m string) string {
		n, _ := strconv.Atoi(strings.Trim(m, "[]"))
		if _, ok := byN[n]; !ok {
			return ""
		}
		cited[n] = true
		return m
	})
	ns := make([]int, 0, len(cited))
	for n := range cited {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	citations := make([]AskCitation, 0, len(ns))
	for _, n := range ns {
		it := byN[n]
		citations = append(citations, AskCitation{N: n, Kind: it.Kind, Ref: it.Ref, Title: it.Title, Location: it.Location})
	}
	model := ""
	if m, ok := cfg.LLM.(interface{ ModelName() string }); ok {
		model = m.ModelName()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return AskResult{}, fmt.Errorf("agent: Ask: the model returned no text")
	}
	return AskResult{Text: text, Citations: citations, Model: model, ToolsUsed: toolsUsed}, nil
}

// askDispatch runs one recall and re-renders its results with citation numbers
// (one per distinct item across the whole Ask), so the model cites by n.
func askDispatch(ctx context.Context, backend *Client, name string, input json.RawMessage, items *[]askItem, seen map[string]int) (string, bool) {
	var in struct {
		Query string   `json:"query"`
		Tags  []string `json:"tags"`
	}
	if err := json.Unmarshal(input, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return "bad input: query is required", true
	}
	body := map[string]any{"query": in.Query}
	if len(in.Tags) > 0 {
		body["tags"] = in.Tags
	}
	var raw []byte
	var err error
	switch name {
	case ToolRecallSOPs:
		raw, err = backend.RecallSOPs(ctx, body)
	case ToolRecallSimilar:
		raw, err = backend.RecallSimilar(ctx, body)
	default:
		return "unknown tool " + name + ": only the recall tools exist here", true
	}
	if err != nil {
		return "recall failed: " + err.Error(), true
	}
	var res struct {
		Coverage string `json:"coverage"`
		Results  []struct {
			SOPID            string          `json:"sop_id"`
			InvestigationRef string          `json:"investigation_ref"`
			Title            string          `json:"title"`
			Excerpt          string          `json:"excerpt"`
			Score            float64         `json:"score"`
			Band             string          `json:"band"`
			Location         json.RawMessage `json:"location"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return "recall returned an unreadable result", true
	}
	numbered := make([]askItem, 0, len(res.Results))
	for _, r := range res.Results {
		kind, ref := CitationKindFor(name), r.SOPID
		if name == ToolRecallSimilar {
			ref = r.InvestigationRef
		}
		if ref == "" {
			continue
		}
		key := kind + ":" + ref
		n, ok := seen[key]
		if !ok {
			n = len(*items) + 1
			seen[key] = n
			*items = append(*items, askItem{N: n, Kind: kind, Ref: ref, Title: r.Title, Excerpt: r.Excerpt, Score: r.Score, Band: r.Band, Location: r.Location})
		}
		numbered = append(numbered, askItem{N: n, Kind: kind, Ref: ref, Title: r.Title, Excerpt: r.Excerpt, Score: r.Score, Band: r.Band, Location: r.Location})
	}
	out, _ := json.Marshal(map[string]any{"coverage": res.Coverage, "results": numbered})
	if len(out) > maxToolResultBytes {
		out = append(out[:maxToolResultBytes], []byte(`… [truncated]`)...)
	}
	return string(out), false
}

// CitationKindFor maps a recall tool to the citation kind of its results.
func CitationKindFor(tool string) string {
	if tool == ToolRecallSimilar {
		return "case"
	}
	return "sop"
}
