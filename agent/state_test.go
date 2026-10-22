package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const hypothesesFixture = `{"hypotheses":[
  {"id":"x-hypothesis--1","handle":"H1 rdp-intrusion","status":"OPEN","statement":"An attacker moved laterally over RDP.","labels":["T1021.001"],
   "predictions":[
     {"id":"x-prediction--a","statement":"PowerShell parent is an RDP session","status":"CONFIRMED","discriminates":["x-hypothesis--2"]},
     {"id":"x-prediction--b","statement":"Outbound C2 follows the logon","status":"UNTESTED","discriminates":["x-hypothesis--2"]}
   ]},
  {"id":"x-hypothesis--2","handle":"H2 legit-admin","status":"OPEN","statement":"An admin used svc_backup for maintenance.",
   "predictions":[
     {"id":"x-prediction--c","statement":"A change ticket covers the window","status":"UNTESTED","discriminates":["x-hypothesis--1"]}
   ]},
  {"id":"x-hypothesis--3","status":"REFUTED","statement":"Scanner noise.","predictions":[]}
]}`

// TestHypothesisStateBlock: the per-turn block names every hypothesis, tallies
// its predictions, lists the untested discriminators by rival handle, and ends
// with the decide-contract summary. The same state renders identically twice.
func TestHypothesisStateBlock(t *testing.T) {
	got := hypothesisStateBlock(json.RawMessage(hypothesesFixture))
	for _, want := range []string{
		"[reasoning state",
		"H1 rdp-intrusion (x-hypothesis--1) OPEN [T1021.001] — An attacker moved laterally over RDP.",
		"predictions: 1 untested, 1 confirmed",
		"untested x-prediction--b (discriminates vs H2 legit-admin)",
		"H2 legit-admin (x-hypothesis--2) OPEN — An admin used svc_backup for maintenance.",
		"untested x-prediction--c (discriminates vs H1 rdp-intrusion)",
		"H3 (x-hypothesis--3) REFUTED — Scanner noise.",
		"predictions: none",
		"H1 rdp-intrusion holds x-prediction--b against H2 legit-admin; H2 legit-admin holds x-prediction--c against H1 rdp-intrusion",
		"cannot be recorded supported",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	if again := hypothesisStateBlock(json.RawMessage(hypothesesFixture)); again != got {
		t.Error("block is not deterministic for the same state")
	}
	for _, empty := range []string{"", "null", "[]", `{"hypotheses":[]}`} {
		if b := hypothesisStateBlock(json.RawMessage(empty)); b != "" {
			t.Errorf("hypothesisStateBlock(%q) = %q; want empty", empty, b)
		}
	}
	decided := strings.ReplaceAll(hypothesesFixture, `"UNTESTED"`, `"DISCONFIRMED"`)
	if b := hypothesisStateBlock(json.RawMessage(decided)); !strings.Contains(b, "No untested discriminator is outstanding.") {
		t.Errorf("all-tested set should say nothing is outstanding:\n%s", b)
	}
}

// TestTurn_StateRidesTheMessageNotThePrompt: the hypothesis set reaches the
// model as a [reasoning state] block on the turn's user message, the system
// prompt never carries it and does not change between turns (the prompt-cache
// prefix), and the transcript records the block as an [engine] line.
func TestTurn_StateRidesTheMessageNotThePrompt(t *testing.T) {
	f := newFakeBackend(t)
	f.hypotheses = json.RawMessage(hypothesesFixture)
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if strings.Contains(s.System(), "x-hypothesis--1") {
		t.Error("hypothesis set leaked into the system prompt")
	}
	before := s.System()
	res, err := s.Turn(context.Background(), "where are we?")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if s.System() != before {
		t.Error("a turn rewrote the system prompt")
	}
	u := lastUserMessage(t, s, "and now?")
	if !strings.Contains(u, "[reasoning state") || !strings.Contains(u, "x-prediction--c") {
		t.Errorf("reasoning state not on the user message:\n%s", u)
	}
	if !strings.Contains(res.Transcript, "[engine] [reasoning state") {
		t.Errorf("transcript does not record the block:\n%s", res.Transcript)
	}
	// The user's own words come last, after the engine blocks.
	if i, j := strings.Index(u, "[reasoning state"), strings.Index(u, "and now?"); i < 0 || j < i {
		t.Errorf("analyst prompt should follow the engine blocks:\n%s", u)
	}
}
