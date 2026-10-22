package agent

import (
	"strings"
	"testing"
)

// TestSystemPrompt_ActionHonesty guards the load-bearing anti-fabrication rules:
// the agent must not claim an action exists without a returned action_id, and
// must re-call request_action for each create request. These prevent the model
// narrating tickets/isolations it never actually filed — a trust-critical
// property for an IR surface. If this test fails, a prompt edit removed the
// guarantee; restore it rather than deleting the test.
// TestSystemPrompt_TimeframeHonesty guards the rule the eval grades as E2: the
// model has no live feed, the present is the per-turn [clock] block, and dated
// evidence is never presented as live. Without the rule the behaviour is a
// model disposition, not a contract (Sonnet 4.6 lost it under v0.9, #13).
func TestSystemPrompt_TimeframeHonesty(t *testing.T) {
	p := systemPrompt(Investigation{Title: "t"}, nil)
	for _, want := range []string{
		"You have no live feed",
		"[clock] block",
		"Never present dated evidence as live",
		"not evidence that activity is ongoing",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt is missing the timeframe-honesty rule: %q", want)
		}
	}
}

func TestSystemPrompt_ActionHonesty(t *testing.T) {
	p := systemPrompt(Investigation{Title: "t"}, nil)
	for _, want := range []string{
		"An action exists ONLY if a request_action call returned an action_id",
		"do not narrate a ticket",
		"MUST call request_action again",
		"parameters is a JSON object",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt is missing the action-honesty rule: %q", want)
		}
	}
}
