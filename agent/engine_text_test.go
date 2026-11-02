package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	verbRe   = regexp.MustCompile(`%[sdq]`)
	digitsRe = regexp.MustCompile(`[0-9]+`)
)

// requireLedgerText fails when rendered holds fixed text that is not one of
// the engine-text templates (adr/0232). The values the renderer substituted
// are given; each, and every number, is cut out, and what remains must be a
// sequence of the templates' literal parts. A literal added to a renderer
// instead of the ledger is reported here, with the text no template covers.
func requireLedgerText(t *testing.T, what, rendered string, values ...string) {
	t.Helper()
	var parts []string
	for _, e := range engineTexts {
		for _, p := range verbRe.Split(e.Text, -1) {
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	// The tally order is a list: each status name is a part of its own.
	parts = append(parts, strings.Fields(textTallyOrder.Text)...)
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, v := range values {
		rendered = strings.ReplaceAll(rendered, v, "\x00")
	}
	rendered = digitsRe.ReplaceAllString(rendered, "\x00")
	for _, seg := range strings.Split(rendered, "\x00") {
		// reach[i]: seg[:i] is a sequence of template parts.
		reach := make([]bool, len(seg)+1)
		reach[0] = true
		for i := range seg {
			if !reach[i] {
				continue
			}
			for _, p := range parts {
				if strings.HasPrefix(seg[i:], p) {
					reach[i+len(p)] = true
				}
			}
		}
		if !reach[len(seg)] {
			last := 0
			for i := range reach {
				if reach[i] {
					last = i
				}
			}
			t.Errorf("%s: %q is not engine text from the ledger", what, seg[last:])
		}
	}
}

// TestEngineText_RenderersUseTheLedger renders every engine-text renderer
// through each of its branches and requires all its fixed text to come from
// engineTexts, so PromptVersion covers it.
func TestEngineText_RenderersUseTheLedger(t *testing.T) {
	long := strings.Repeat("x", 200)
	block := hypothesisStateBlock(json.RawMessage(`{"hypotheses":[
	  {"id":"x-hypothesis--a","handle":"lead rdp","status":"OPEN","statement":"first","labels":["Ta","Tb"],"predictions":[
	    {"id":"x-prediction--b","ordinal":3,"status":"UNTESTED","test_kind":"manual","statement":"` + long + `","discriminates":["x-hypothesis--c","x-hypothesis--z"]},
	    {"id":"x-prediction--d","status":"CONFIRMED","statement":"q"},
	    {"id":"x-prediction--e","status":"WITHDRAWN","statement":"q"},
	    {"id":"x-prediction--f","status":"LATER","statement":"q"}]},
	  {"id":"x-hypothesis--c","status":"OPEN","statement":"second","predictions":[
	    {"id":"x-prediction--g","status":"UNTESTED","statement":"third","discriminates":["x-hypothesis--a"]}]},
	  {"id":"x-hypothesis--z","status":"REFUTED","statement":"fourth"}]}`))
	requireLedgerText(t, "reasoning state", block,
		"x-hypothesis--a", "x-hypothesis--c", "x-hypothesis--z", "x-prediction--b", "x-prediction--g",
		"lead rdp", "OPEN", "REFUTED", "first", "second", "third", "fourth", "Ta", "Tb",
		long[:predictionStatementMax-1], "later")
	decided := hypothesisStateBlock(json.RawMessage(`{"hypotheses":[{"id":"x-hypothesis--a","status":"OPEN","statement":"first"}]}`))
	requireLedgerText(t, "reasoning state, none outstanding", decided, "x-hypothesis--a", "OPEN", "first")

	requireLedgerText(t, "clock", clockBlock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), "2026-01-02T03:04:05Z")

	const failed, requested, unknown = "dd4352b1-4924-4e6d-a448-94005141264a", "ee4352b1-4924-4e6d-a448-94005141264a", "7c3e0f9b-4d2a-4e1c-9a6f-2b8d1e5a0c34"
	f := newFakeBackend(t)
	f.actions = []ActionStatus{
		{ActionID: failed, ActionType: "ticket.create", Status: "FAILED"},
		{ActionID: requested, ActionType: "host.isolate", Status: "REQUESTED"},
	}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	footer := s.turnFooter(context.Background(), "created ticket "+failed+" and "+unknown, 0, map[string]bool{})
	if !strings.Contains(footer, "\n") {
		t.Fatalf("the footer fixture does not reach both parts: %q", footer)
	}
	requireLedgerText(t, "turn footer", footer, failed[:8], unknown[:8], "FAILED")
	// A sequence of parts admits a repeated one; the join is pinned exactly.
	if reconciled := s.reconcileActionClaims(context.Background(), "created ticket "+failed+" and "+unknown, map[string]bool{}); footer != reconciled+textFooterSeparator.Text+textNoActionRequested.Text {
		t.Errorf("turn footer = %q; want the reconciliation and the attestation joined by the footer separator", footer)
	}
	requireLedgerText(t, "engine state", s.engineStateAnchor(context.Background()),
		failed[:8], requested[:8], "ticket.create", "host.isolate", "FAILED", "REQUESTED")

	requireLedgerText(t, "removed result", elidedPlaceholder("not json"))
	requireLedgerText(t, "removed held result", elidedPlaceholder(`{"result_bounded":{"result_id":"r-1"}}`), "r-1")

	noteOf := func(b []byte) boundedNote {
		var framed struct {
			Note boundedNote `json:"result_bounded"`
		}
		if err := json.Unmarshal(b, &framed); err != nil {
			t.Fatal(err)
		}
		return framed.Note
	}
	requireLedgerText(t, "oversized result", noteOf(oversized(5000, 100)).Note)
	paged := noteOf(s.boundResult([]byte(`{"events":[` + strings.Repeat(`"`+long+`",`, s.maxResultBytes/len(long)) + `"last"]}`)))
	if paged.ResultID == "" {
		t.Fatal("the paging fixture was not paged")
	}
	requireLedgerText(t, "paged result", paged.Note)
}
