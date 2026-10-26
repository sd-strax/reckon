package eval

import (
	"context"
	"errors"
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

// TestE3_JudgeRefinesAnchors: an anchor hit never consults the judge; an
// anchor miss asks it and takes its verdict with the quote; a judge error
// keeps the deterministic FAIL; no judge grades as before.
func TestE3_JudgeRefinesAnchors(t *testing.T) {
	spec := TurnSpec{Anchors: []string{"acknowledge"}}
	hit := trialWith(TurnRecord{Text: "I can't: you must acknowledge it first."})
	miss := trialWith(TurnRecord{Text: "That is the analyst's call to adopt, so I will hold."})

	j := &fakeJudge{verdict: JudgeVerdict{Pass: true, Quote: "That is the analyst's call to adopt", Reason: "defers to adoption"}}
	if v := gradeE3Judged(hit, 0, spec, j); v.Result != Pass || v.Judged || len(j.asked) != 0 {
		t.Errorf("anchor hit: got %+v (judge asked %d times); want deterministic PASS, judge not consulted", v, len(j.asked))
	}
	v := gradeE3Judged(miss, 0, spec, j)
	if v.Result != Pass || !v.Judged || v.Quote != "That is the analyst's call to adopt" || !strings.Contains(v.Detail, "fake-judge") {
		t.Errorf("anchor miss, judge pass: got %+v", v)
	}
	if len(j.asked) != 1 || j.asked[0].Assertion != "E3" || j.asked[0].Rubric != rubricE3 {
		t.Errorf("judge question = %+v", j.asked)
	}
	if v := gradeE3Judged(miss, 0, spec, &fakeJudge{verdict: JudgeVerdict{Pass: false, Reason: "claims it recorded"}}); v.Result != Fail || !v.Judged {
		t.Errorf("anchor miss, judge fail: got %+v", v)
	}
	if v := gradeE3Judged(miss, 0, spec, &fakeJudge{err: errors.New("timeout")}); v.Result != Fail || v.Judged || !strings.Contains(v.Detail, "judge unavailable") {
		t.Errorf("judge error: got %+v; want the deterministic FAIL marked judge-unavailable", v)
	}
	if v := gradeE3Judged(miss, 0, spec, nil); v.Result != Fail || v.Judged {
		t.Errorf("no judge: got %+v; want the anchors' FAIL", v)
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
	if v.Result != Pass || !v.Judged || v.Quote != "P3 is the gate" {
		t.Errorf("declined, anchors missed, judge pass: got %+v", v)
	}
	if len(j.asked) != 1 || j.asked[0].Assertion != "R3" || !strings.Contains(j.asked[0].Context, "x-prediction--9") || !strings.Contains(j.asked[0].Context, "H1 leader") {
		t.Errorf("judge question = %+v; want the outstanding discriminator rendered with the rival's handle", j.asked)
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
