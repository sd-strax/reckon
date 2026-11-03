package eval

import (
	"context"
	"strings"
	"testing"
)

// TestEstimatedCost prices a run per-model, billing the cache counters at their
// own rates (Anthropic reports them disjoint from Input).
func TestEstimatedCost(t *testing.T) {
	r := &Report{
		Attribution: Attribution{Model: "claude-sonnet-4-6"},
		Usage:       Usage{Input: 100_000, Output: 20_000, CacheWrite: 40_000, CacheRead: 500_000},
	}
	cost, ok := r.EstimatedCost()
	if !ok {
		t.Fatal("expected pricing for claude-sonnet-4-6")
	}
	// 0.1M*$3 + 0.02M*$15 + 0.04M*$3.75 + 0.5M*$0.30 = 0.30+0.30+0.15+0.15 = 0.90
	if cost < 0.899 || cost > 0.901 {
		t.Errorf("cost = %.4f; want ~0.90", cost)
	}

	// An unknown model prices nothing (tokens-only readout).
	unknown := &Report{Attribution: Attribution{Model: "gpt-nope"}, Usage: Usage{Input: 1}}
	if _, ok := unknown.EstimatedCost(); ok {
		t.Error("unknown model should have no pricing")
	}
}

// TestUsageLine surfaces tokens, cost, and the caching saving.
func TestUsageLine(t *testing.T) {
	r := &Report{
		Attribution: Attribution{Model: "claude-sonnet-4-6"},
		Usage:       Usage{Input: 100_000, Output: 20_000, CacheWrite: 0, CacheRead: 500_000},
	}
	line := r.usageLine()
	if !strings.Contains(line, "$") || !strings.Contains(line, "cache-read") {
		t.Errorf("usage line missing cost/cache detail: %q", line)
	}
	if !strings.Contains(line, "caching saved") {
		t.Errorf("usage line should show the caching saving when cache reads occurred: %q", line)
	}

	// Unknown model: tokens, but an explicit no-pricing note instead of a bogus $.
	unknown := &Report{Attribution: Attribution{Model: "gpt-nope"}, Usage: Usage{Input: 1000, Output: 100}}
	if l := unknown.usageLine(); !strings.Contains(l, "no pricing") {
		t.Errorf("unknown model should say no pricing: %q", l)
	}
}

// TestUsageAccumulates: Usage.Add sums each field.
func TestUsageAccumulates(t *testing.T) {
	var u Usage
	u.Add(Usage{Input: 1, Output: 2, CacheWrite: 3, CacheRead: 4})
	u.Add(Usage{Input: 10, Output: 20, CacheWrite: 30, CacheRead: 40})
	if u != (Usage{Input: 11, Output: 22, CacheWrite: 33, CacheRead: 44}) {
		t.Errorf("accumulated usage = %+v", u)
	}
}

// TestRegressions_RelocatedTurnKey: a turn-scoped assertion that moved to a
// different turn (scenario re-layout) is still compared against its baseline
// entry when the id is asserted at exactly one turn on each side; an id
// asserted at several turns stays exact-key only; exact keys win outright.
func TestRegressions_RelocatedTurnKey(t *testing.T) {
	r := &Report{Results: []AssertionResult{
		{ID: "E2", Severity: Should, Scope: ScopeTurn, Turn: 9, Result: Fail, Passed: 1, Exercised: 3},
		{ID: "O2", Severity: Should, Scope: ScopeTurn, Turn: 0, Result: Pass, Passed: 3, Exercised: 3},
		{ID: "O2", Severity: Should, Scope: ScopeTurn, Turn: 9, Result: Fail, Passed: 0, Exercised: 3},
		{ID: "H4", Severity: Must, Scope: ScopeTurn, Turn: 7, Result: Fail, Passed: 2, Exercised: 3},
		{ID: "G1", Severity: Must, Scope: ScopeTrial, Result: Pass, Passed: 3, Exercised: 3},
		// Exactly one trial's worth (3/3 → 2/3) is within tolerance — and must
		// be decided in integers: 1 - 1/3 and 2/3 differ in floating point (#18).
		{ID: "H6", Severity: Should, Scope: ScopeTrial, Result: Fail, Passed: 2, Exercised: 3},
	}}
	base := &BaselineSummary{Results: map[string]RateRef{
		"E2@turn7": {Severity: Should, Result: Pass, Passed: 3, Exercised: 3},
		"O2@turn7": {Severity: Should, Result: Pass, Passed: 3, Exercised: 3},
		"H4@turn7": {Severity: Must, Result: Pass, Passed: 3, Exercised: 3},
		"G1":       {Severity: Must, Result: Pass, Passed: 3, Exercised: 3},
		"H6":       {Severity: Should, Result: Pass, Passed: 3, Exercised: 3},
	}}
	got := r.Regressions(base)
	want := []string{
		"E2@turn9 (baseline E2@turn7): SHOULD rate dropped 3/3 -> 1/3 (tolerance: one trial)",
		"H4@turn7: MUST regressed PASS -> FAIL",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Regressions =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Asserted at several turns in the BASELINE: no pairing either.
	base.Results["E2@turn3"] = RateRef{Severity: Should, Result: Pass, Passed: 3, Exercised: 3}
	for _, reg := range r.Regressions(base) {
		if strings.HasPrefix(reg, "E2") {
			t.Errorf("E2 should not pair against a baseline that asserts it at two turns: %s", reg)
		}
	}
}

// TestVersionSkew: the run names each condition it does not share with the
// baseline. One the baseline never recorded is said to be unrecorded, not
// taken as agreement; one the run lacks says nothing; a baseline without a
// judge rubric was graded by anchors, which is no finding either.
func TestVersionSkew(t *testing.T) {
	r := &Report{Attribution: Attribution{
		PromptVersion: "f5df72f6b87f19000cece63d", SideInstructionVersion: "a1bd6f383f4e", JudgeRubric: "caa20d15db77",
		ReaderVersion: "e11a1ab74e69", ReaderModel: "reader-b", ToolSetHash: "0123456789abcdef",
	}}
	check := func(what string, base *BaselineSummary, want ...string) {
		t.Helper()
		if got := r.VersionSkew(base); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: VersionSkew =\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	// A baseline accepted before adr/0232: prompt and judge only.
	check("pre-existing baseline", &BaselineSummary{Attribution: Attribution{PromptVersion: "a870ddbf8038fe4ee2049f35", JudgeRubric: "v1"}},
		"prompt f5df72f6b87f against baseline a870ddbf8038",
		"side instructions a1bd6f383f4e against baseline unrecorded",
		"judge rubric caa20d15db77 against baseline v1",
		"reader e11a1ab74e69 against baseline unrecorded",
		"reader model reader-b against baseline unrecorded",
		"tool set 0123456789ab against baseline unrecorded")
	differs := r.Attribution
	differs.ReaderModel, differs.ToolSetHash, differs.JudgeRubric = "reader-a", "fedcba9876543210", ""
	check("baseline under other conditions", &BaselineSummary{Attribution: differs},
		"reader model reader-b against baseline reader-a",
		"tool set 0123456789ab against baseline fedcba987654")
	check("same conditions", &BaselineSummary{Attribution: r.Attribution})
	check("no baseline", nil)
	noReader := &Report{Attribution: Attribution{PromptVersion: r.Attribution.PromptVersion}}
	if got := noReader.VersionSkew(&BaselineSummary{Attribution: Attribution{PromptVersion: r.Attribution.PromptVersion}}); got != nil {
		t.Errorf("a run without a reader or tool set reports %v", got)
	}
	if s := r.Summary(); !strings.Contains(s, "prompt f5df72f6b87f  side a1bd6f383f4e  catalogue") ||
		!strings.Contains(s, "tools 0123456789ab") || !strings.Contains(s, "reader reader-b (e11a1ab74e69)") {
		t.Errorf("summary does not carry the versions:\n%s", s)
	}
}

// TestStampReader: a reader that read an answer attributes the run with its
// model, version and spend; one that read nothing leaves no trace.
func TestStampReader(t *testing.T) {
	quiet := &Report{}
	stampReader(quiet, &LLMJudge{LLM: &scriptedJudgeLLM{}, ModelID: "reader-model"})
	if quiet.Attribution.ReaderModel != "" || quiet.Attribution.ReaderVersion != "" || quiet.ReaderCalls != 0 {
		t.Errorf("a reader that read nothing was stamped: %+v", quiet.Attribution)
	}
	stampReader(quiet, nil)

	j := &LLMJudge{LLM: &scriptedJudgeLLM{reply: `{"status":"CONFIRMED","reason":"r"}`}, ModelID: "reader-model"}
	if _, _, err := j.Read(context.Background(), ReadQuestion{Hypothesis: "h", Statement: "s", Content: "c"}); err != nil {
		t.Fatal(err)
	}
	r := &Report{}
	stampReader(r, j)
	if r.Attribution.ReaderModel != "reader-model" || r.Attribution.ReaderVersion != ReaderVersion || r.ReaderCalls != 1 {
		t.Errorf("stamped %+v, %d calls", r.Attribution, r.ReaderCalls)
	}
	if !strings.Contains(r.Summary(), "reader: 1 answer(s) read by reader-model") {
		t.Errorf("summary does not report the reader:\n%s", r.Summary())
	}
}
