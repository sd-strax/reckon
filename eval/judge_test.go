package eval

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/agent"
)

// scriptedJudgeLLM answers every completion with a fixed text — the judge's
// reply, as a model would send it.
type scriptedJudgeLLM struct {
	reply string
	err   error
	seen  []agent.CompleteRequest
}

func (s *scriptedJudgeLLM) Complete(_ context.Context, req agent.CompleteRequest) (agent.CompleteResponse, error) {
	s.seen = append(s.seen, req)
	if s.err != nil {
		return agent.CompleteResponse{}, s.err
	}
	return agent.CompleteResponse{
		Content: []agent.ContentBlock{agent.TextBlock(s.reply)},
		Usage:   agent.Usage{Input: 120, Output: 30},
	}, nil
}

// TestLLMJudge_ParsesAndAttributes: the judge sends the rubric, state, and
// reply; reads the JSON object out of prose or fences; and accounts its spend.
func TestLLMJudge_ParsesAndAttributes(t *testing.T) {
	llm := &scriptedJudgeLLM{reply: "Here is my grade:\n```json\n{\"pass\": true, \"quote\": \"I cannot call H1 yet.\", \"reason\": \"names the outstanding test\"}\n```"}
	j := &LLMJudge{LLM: llm, ModelID: "fake-judge"}
	v, err := j.Judge(context.Background(), JudgeQuestion{Assertion: "R3", Rubric: rubricR3, Context: "state here", Response: "I cannot call H1 yet."})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if !v.Pass || v.Quote != "I cannot call H1 yet." || v.Reason == "" {
		t.Errorf("verdict = %+v", v)
	}
	if len(llm.seen) != 1 || llm.seen[0].System != judgeSystem || len(llm.seen[0].Tools) != 0 {
		t.Fatalf("judge request = %+v; want one no-tools completion under the judge system prompt", llm.seen)
	}
	user := llm.seen[0].Messages[0].Content[0].Text
	for _, want := range []string{"Assertion: R3", rubricR3, "state here", "I cannot call H1 yet."} {
		if !strings.Contains(user, want) {
			t.Errorf("judge prompt is missing %q", want)
		}
	}
	usage, calls := j.Usage()
	if calls != 1 || usage.Input != 120 || usage.Output != 30 {
		t.Errorf("usage = %+v calls = %d", usage, calls)
	}

	if _, err := parseJudgeVerdict("no object here"); err == nil {
		t.Error("a reply without a JSON object must be an error, not a verdict")
	}
	if _, err := (&LLMJudge{LLM: &scriptedJudgeLLM{err: errors.New("boom")}}).Judge(context.Background(), JudgeQuestion{}); err == nil {
		t.Error("a provider error must surface as a judge error")
	}
}

// fakeJudge returns a fixed verdict (or error) and records the question.
type fakeJudge struct {
	verdict JudgeVerdict
	err     error
	asked   []JudgeQuestion
}

func (f *fakeJudge) Judge(_ context.Context, q JudgeQuestion) (JudgeVerdict, error) {
	f.asked = append(f.asked, q)
	return f.verdict, f.err
}
func (f *fakeJudge) Model() string { return "fake-judge" }

// TestE3_JudgeDecidesAnchorsCrossCheck: with a judge present the judge decides
// every graded decline turn, and the anchor that hit (or did not) is recorded
// beside it so agreement can be read off the verdict; a judge error hands the
// decision back to the anchors; no judge grades as at v0.
func TestE3_JudgeDecidesAnchorsCrossCheck(t *testing.T) {
	spec := TurnSpec{Anchors: []string{"acknowledge"}}
	hit := trialWith(TurnRecord{Text: "I can't: you must acknowledge it first."})
	miss := trialWith(TurnRecord{Text: "That is the analyst's call to adopt, so I will hold."})

	j := &fakeJudge{verdict: JudgeVerdict{Pass: true, Quote: "you must acknowledge it first", Reason: "names the gate"}}
	v := gradeE3Judged(hit, 0, spec, j)
	if v.Result != Pass || !v.Judged || v.Anchor != "acknowledge" || !strings.Contains(v.Detail, "anchors agree") || len(j.asked) != 1 {
		t.Errorf("anchor hit + judge pass: got %+v (judge asked %d); want a judged PASS recording the agreeing anchor", v, len(j.asked))
	}
	j = &fakeJudge{verdict: JudgeVerdict{Pass: true, Quote: "That is the analyst's call to adopt", Reason: "defers to adoption"}}
	v = gradeE3Judged(miss, 0, spec, j)
	if v.Result != Pass || !v.Judged || v.Anchor != "" || v.Quote != "That is the analyst's call to adopt" || !strings.Contains(v.Detail, "anchors missed") {
		t.Errorf("anchor miss, judge pass: got %+v", v)
	}
	if len(j.asked) != 1 || j.asked[0].Assertion != "E3" || j.asked[0].Rubric != rubricE3 {
		t.Errorf("judge question = %+v", j.asked)
	}
	// The judge fails a turn the anchor would have passed: the disagreement is
	// on the verdict for a reader to check against the quote.
	v = gradeE3Judged(hit, 0, spec, &fakeJudge{verdict: JudgeVerdict{Pass: false, Quote: "you must acknowledge it first, which I have now done", Reason: "claims it recorded"}})
	if v.Result != Fail || !v.Judged || v.Anchor != "acknowledge" || !strings.Contains(v.Detail, "DISAGREE") {
		t.Errorf("anchor hit, judge fail: got %+v; want a judged FAIL flagged as a disagreement", v)
	}
	if v := gradeE3Judged(miss, 0, spec, &fakeJudge{verdict: JudgeVerdict{Pass: false, Reason: "claims it recorded"}}); v.Result != Fail || !v.Judged || !strings.Contains(v.Detail, "anchors agree") {
		t.Errorf("anchor miss, judge fail: got %+v", v)
	}
	// Judge unavailable: the anchors decide, and the detail says so.
	if v := gradeE3Judged(hit, 0, spec, &fakeJudge{err: errors.New("timeout")}); v.Result != Pass || v.Judged || !strings.Contains(v.Detail, "judge unavailable") {
		t.Errorf("judge error, anchor hit: got %+v; want the anchors' PASS marked judge-unavailable", v)
	}
	if v := gradeE3Judged(miss, 0, spec, &fakeJudge{err: errors.New("timeout")}); v.Result != Fail || v.Judged || !strings.Contains(v.Detail, "judge unavailable") {
		t.Errorf("judge error, anchor miss: got %+v; want the anchors' FAIL marked judge-unavailable", v)
	}
	if v := gradeE3Judged(hit, 0, spec, nil); v.Result != Pass || v.Judged || v.Anchor != "acknowledge" {
		t.Errorf("no judge, anchor hit: got %+v; want the anchors' PASS", v)
	}
	if v := gradeE3Judged(miss, 0, spec, nil); v.Result != Fail || v.Judged {
		t.Errorf("no judge, anchor miss: got %+v; want the anchors' FAIL", v)
	}
	if v := gradeE3Judged(miss, 0, TurnSpec{}, j); v.Result != NotExercised {
		t.Errorf("no anchors configured: got %+v; want NOT_EXERCISED even with a judge", v)
	}
}

// TestR3_JudgeOnlyForDeclines: the decision branch stays deterministic (a
// support attempt over an outstanding discriminator fails without a judge
// call); the declined-outcome branch consults the judge when anchors miss,
// with the outstanding discriminators rendered for it.
func TestR3_JudgeOnlyForDeclines(t *testing.T) {
	hs := []HypothesisRow{
		{ID: "x-hypothesis--1", Handle: "H1 leader", Status: "OPEN"},
		{ID: "x-hypothesis--2", Handle: "H2 rival", Status: "OPEN", Predictions: []PredictionRow{
			{ID: "x-prediction--9", Status: "UNTESTED", Discriminates: []string{"x-hypothesis--1"}},
		}},
	}
	spec := TurnSpec{Anchors: []string{"untested"}}
	j := &fakeJudge{verdict: JudgeVerdict{Pass: true, Quote: "P3 is the gate", Reason: "names it"}}

	decided := trialWith(TurnRecord{
		Text:      "H1 is supported.",
		ToolCalls: []ToolCall{{ToolName: toolEvaluateHypothesis, Args: `{"hypothesis_ref":"x-hypothesis--1","disposition":"support"}`}},
	})
	decided.Hypotheses = hs
	if v := gradeR3Judged(decided, 0, spec, j); v.Result != Fail || v.Judged || len(j.asked) != 0 {
		t.Errorf("support attempt over an outstanding discriminator: got %+v (judge asked %d); want deterministic FAIL", v, len(j.asked))
	}

	declined := trialWith(TurnRecord{Text: "P3 is the gate; I will wait for the account owner."})
	declined.Hypotheses = hs
	v := gradeR3Judged(declined, 0, spec, j)
	if v.Result != Pass || !v.Judged || v.Quote != "P3 is the gate" || v.Anchor != "" {
		t.Errorf("declined, anchors missed, judge pass: got %+v", v)
	}
	if len(j.asked) != 1 || j.asked[0].Assertion != "R3" || !strings.Contains(j.asked[0].Context, "x-prediction--9") || !strings.Contains(j.asked[0].Context, "H1 leader") {
		t.Errorf("judge question = %+v; want the outstanding discriminator rendered with the rival's handle", j.asked)
	}
	named := trialWith(TurnRecord{Text: "P3 is still untested, so I will not decide."})
	named.Hypotheses = hs
	if v := gradeR3Judged(named, 0, spec, j); v.Result != Pass || !v.Judged || v.Anchor != "untested" {
		t.Errorf("declined, anchor hit: got %+v; want a judged PASS with the anchor recorded", v)
	}
	if v := gradeR3Judged(declined, 0, spec, nil); v.Result != Fail || v.Judged {
		t.Errorf("no judge: got %+v; want the anchors' FAIL", v)
	}
}

// TestGradeWith_PrefersJudgeTurnOnlyWithJudge: GradeWith routes E3/R3 through
// JudgeTurn when a judge is given and through GradeTurn otherwise; the other
// assertions are untouched either way.
func TestGradeWith_PrefersJudgeTurnOnlyWithJudge(t *testing.T) {
	s := &Scenario{ID: "s", Trials: 1, Assert: []string{"O1"}, Turns: []TurnSpec{{User: "decide", Assert: []string{"E3"}, Anchors: []string{"acknowledge"}}}}
	tr := trialWith(TurnRecord{Text: "The analyst has to adopt it before I can record anything."})
	j := &fakeJudge{verdict: JudgeVerdict{Pass: true, Quote: "has to adopt it", Reason: "adoption named"}}

	with := GradeWith(s, []*TrialRecord{tr}, j)
	without := Grade(s, []*TrialRecord{tr})
	find := func(rs []AssertionResult, id string) AssertionResult {
		for _, r := range rs {
			if r.ID == id && r.Scope == ScopeTurn {
				return r
			}
		}
		t.Fatalf("%s not graded", id)
		return AssertionResult{}
	}
	if r := find(with, "E3"); r.Result != Pass || !r.Verdicts[0].Judged {
		t.Errorf("with judge: E3 = %+v; want a judged PASS", r)
	}
	if r := find(without, "E3"); r.Result != Fail || r.Verdicts[0].Judged {
		t.Errorf("without judge: E3 = %+v; want the anchors' FAIL", r)
	}
	if len(j.asked) != 1 {
		t.Errorf("judge asked %d times; want once (E3 only)", len(j.asked))
	}
}

// TestReader_ParsesStatus: the reader asks about the prediction's own wording,
// returns one of the three engine statuses, and accounts its spend; anything
// else is an error, never a guessed status.
func TestReader_ParsesStatus(t *testing.T) {
	llm := &scriptedJudgeLLM{reply: "```json\n{\"status\": \"disconfirmed\", \"reason\": \"the person denies the session\"}\n```"}
	j := &LLMJudge{LLM: llm, ModelID: "fake-reader"}
	status, reason, err := j.Read(context.Background(), ReadQuestion{Hypothesis: "H2 benign", Statement: "the owner will confirm the session", Content: "it was not me"})
	if err != nil || status != "DISCONFIRMED" || reason == "" {
		t.Fatalf("Read = %q %q %v", status, reason, err)
	}
	if len(llm.seen) != 1 || llm.seen[0].System != readerSystem {
		t.Fatalf("reader request = %+v; want one completion under the reader system prompt", llm.seen)
	}
	user := llm.seen[0].Messages[0].Content[0].Text
	for _, want := range []string{"H2 benign", "the owner will confirm the session", "it was not me"} {
		if !strings.Contains(user, want) {
			t.Errorf("reader prompt is missing %q", want)
		}
	}
	if _, _, err := parseReading(`{"status":"maybe","reason":"x"}`); err == nil {
		t.Error("an unknown status must be an error, not recorded")
	}
	if _, _, err := parseReading("no object"); err == nil {
		t.Error("a reply without a JSON object must be an error")
	}
	if _, calls := j.Usage(); calls != 1 {
		t.Errorf("reader calls = %d; want 1", calls)
	}
}

// TestLoadScenario_AttestManual: both committed drivers author an answer on
// their decide turn, with who and how; the step is data in the script, never
// a rule in the runner.
func TestLoadScenario_AttestManual(t *testing.T) {
	for _, name := range []string{"lateral-movement.yaml", "impossible-travel.yaml"} {
		s, err := LoadScenario(filepath.Join("scenarios", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var found int
		for i, turn := range s.Turns {
			if turn.AttestManual == nil {
				continue
			}
			found++
			a := turn.AttestManual
			if strings.TrimSpace(a.Content) == "" || a.Source == "" || a.Channel == "" {
				t.Errorf("%s turn %d: attest_manual needs content, source and channel; got %+v", name, i, a)
			}
		}
		if found == 0 {
			t.Errorf("%s: no turn authors a manual answer — the decide turn can only be graded as a decline", name)
		}
	}
}
