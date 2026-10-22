package eval

import (
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
	}}
	base := &BaselineSummary{Results: map[string]RateRef{
		"E2@turn7": {Severity: Should, Result: Pass, Passed: 3, Exercised: 3},
		"O2@turn7": {Severity: Should, Result: Pass, Passed: 3, Exercised: 3},
		"H4@turn7": {Severity: Must, Result: Pass, Passed: 3, Exercised: 3},
		"G1":       {Severity: Must, Result: Pass, Passed: 3, Exercised: 3},
	}}
	got := r.Regressions(base)
	want := []string{
		"E2@turn9 (baseline E2@turn7): SHOULD rate dropped 1.00 -> 0.33 (tolerance 0.33)",
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
