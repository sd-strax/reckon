package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestClockBlock: the turn time is stated in UTC RFC 3339 and labelled as the
// engine's clock, not an observation, so the model cannot mistake it for
// telemetry.
func TestClockBlock(t *testing.T) {
	pdt := time.FixedZone("PDT", -7*3600)
	got := clockBlock(time.Date(2026, 10, 21, 13, 55, 0, 0, pdt))
	want := "[clock — this turn began at 2026-10-21T20:55:00Z; the engine's clock, not an observation]"
	if got != want {
		t.Errorf("clockBlock = %q\nwant %q", got, want)
	}
}

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
// With ordinals from the view, the block names predictions P<n> ahead of the
// id — the tracker's vocabulary, so analyst and agent point at the same test.
func TestHypothesisStateBlock_Ordinals(t *testing.T) {
	raw := `{"hypotheses":[
	  {"id":"x-hypothesis--a","handle":"H1 rdp","status":"OPEN","statement":"s1","predictions":[
	    {"id":"x-prediction--b","ordinal":3,"status":"UNTESTED","statement":"p","discriminates":["x-hypothesis--c"]}]},
	  {"id":"x-hypothesis--c","handle":"H2 admin","status":"OPEN","statement":"s2"}]}`
	b := hypothesisStateBlock(json.RawMessage(raw))
	for _, want := range []string{"untested P3 (x-prediction--b) (discriminates vs H2 admin)", "H1 rdp holds P3 (x-prediction--b) against H2 admin"} {
		if !strings.Contains(b, want) {
			t.Errorf("block lacks %q:\n%s", want, b)
		}
	}
}

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
	fixed := time.Date(2026, 10, 21, 20, 55, 0, 0, time.UTC)
	s, err := NewSession(context.Background(), Config{
		Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1",
		Clock: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if strings.Contains(s.System(), "x-hypothesis--1") || strings.Contains(s.System(), "2026-10-21T20:55") {
		t.Error("per-turn state leaked into the system prompt")
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
	// The clock rides every turn (the model has no other source of "now").
	clock := "[clock — this turn began at 2026-10-21T20:55:00Z"
	if !strings.Contains(u, clock) || !strings.Contains(res.Transcript, "[engine] "+clock) {
		t.Errorf("clock block missing from the user message or transcript:\n%s\n%s", u, res.Transcript)
	}
	// The user's own words come last, after the engine blocks.
	if i, j := strings.Index(u, "[reasoning state"), strings.Index(u, "and now?"); i < 0 || j < i {
		t.Errorf("analyst prompt should follow the engine blocks:\n%s", u)
	}
}
