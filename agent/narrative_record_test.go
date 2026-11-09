package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestNarrativeActsBlock: the thread rides the draft as recorded, oldest
// first, without reset markers; beyond the bound the oldest acts are elided
// with their count, and a long rationale is clipped.
func TestNarrativeActsBlock(t *testing.T) {
	if narrativeActsBlock(nil) != "" || narrativeActsBlock([]ThreadEntry{{InterpretationType: interpretationContextReset}}) != "" {
		t.Fatal("a thread without an act rendered a block")
	}
	thread := []ThreadEntry{{SequenceNo: 1, InterpretationType: interpretationContextReset, Summary: "reset"}}
	for i := 2; i <= narrativeActsMax+6; i++ {
		thread = append(thread, ThreadEntry{SequenceNo: int64(i), InterpretationType: "hypothesis", Summary: fmt.Sprintf("act %d", i), Actor: ThreadActor{Kind: "AI_DELEGATED"}})
	}
	thread = append(thread, ThreadEntry{SequenceNo: 99, InterpretationType: "evidence-pin", Summary: strings.Repeat("x", narrativeActSummaryMax+50), Actor: ThreadActor{Kind: "HUMAN"}})
	got := narrativeActsBlock(thread)
	lines := strings.Split(got, "\n")
	// Header, the elision line, narrativeActsMax acts (the pin is the last).
	if len(lines) != narrativeActsMax+2 {
		t.Fatalf("%d lines; want header + elision + %d acts:\n%s", len(lines), narrativeActsMax, got)
	}
	if lines[1] != "… 6 earlier acts not shown" {
		t.Errorf("elision line = %q", lines[1])
	}
	if strings.Contains(got, "act 2:") || strings.Contains(got, "act 7:") || !strings.Contains(got, "#8 hypothesis by AI_DELEGATED: act 8") {
		t.Errorf("the oldest acts beyond the bound must go, the rest stay:\n%s", got)
	}
	if strings.Contains(got, "reset") {
		t.Error("the reset marker rode as an act")
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "#99 evidence-pin by HUMAN: ") || len([]rune(last)) > narrativeActSummaryMax+40 || !strings.HasSuffix(last, textClipped.Text+"]") {
		t.Errorf("the long rationale was not clipped: %d runes", len([]rune(last)))
	}
}

// TestNarrativeTestsBlock: every prediction with its statement and outcome,
// under its hypothesis's handle (or positional name), with the evidence refs
// and the attestation when the outcome has them; empty with no prediction.
func TestNarrativeTestsBlock(t *testing.T) {
	if narrativeTestsBlock(nil) != "" || narrativeTestsBlock(json.RawMessage(`{"hypotheses":[{"id":"h","statement":"s","predictions":[]}]}`)) != "" {
		t.Fatal("no prediction rendered a block")
	}
	raw := json.RawMessage(`{"hypotheses":[
		{"id":"h-1","handle":"H1","statement":"a","status":"OPEN","predictions":[
			{"id":"p-1","ordinal":1,"statement":"a second logon follows","status":"CONFIRMED","test_result_refs":["note--77","observed-data--9"],"attestation":{"content":"denied by phone"}},
			{"id":"p-2","ordinal":2,"statement":"no badge entry","status":"UNTESTED"}]},
		{"id":"h-2","statement":"b","status":"REFUTED","predictions":[
			{"id":"p-3","statement":"a scanner signature","status":"DISCONFIRMED"}]}]}`)
	got := narrativeTestsBlock(raw)
	for _, want := range []string{
		"P1 (p-1) under H1, CONFIRMED: a second logon follows; evidence note--77, observed-data--9; attested by a person: denied by phone",
		"P2 (p-2) under H1, UNTESTED: no badge entry\n",
		"p-3 under H2, DISCONFIRMED: a scanner signature]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block lacks %q:\n%s", want, got)
		}
	}
}
