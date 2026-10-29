package agent

import (
	"context"
	"strings"
	"testing"
)

// TestSystemPrompt_ActionHonesty guards the load-bearing anti-fabrication rules:
// the agent must not claim an action exists without a returned action_id, and
// must re-call request_action for each create request. These prevent the model
// narrating tickets/isolations it never actually filed — a trust-critical
// property for an IR surface. If this test fails, a prompt edit removed the
// guarantee; restore it rather than deleting the test.
// TestPromptVersion: the version is a function of the prompt code and the
// framing dial only — never of the investigation or the environment (#20: the
// harness once hashed the assembled prompt, investigation id included, and
// minted a new "version" on every run).
func TestPromptVersion(t *testing.T) {
	a := PromptVersion("")
	if a != PromptVersion(framingLeadingNull) {
		t.Error("the empty dial must version as the default (leading_null)")
	}
	if a == PromptVersion(framingLeadingOnly) {
		t.Error("the two framing dials render different instructions and must version differently")
	}
	if len(a) != 64 {
		t.Errorf("want a hex sha256, got %q", a)
	}
	// Two sessions over different investigations on the same code share it.
	f := newFakeBackend(t)
	s1, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s2, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-2"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s1.PromptVersion() != s2.PromptVersion() || s1.PromptVersion() != a {
		t.Errorf("sessions on the same code must share the prompt version: %s %s %s", s1.PromptVersion(), s2.PromptVersion(), a)
	}
	if s1.HypothesisFraming() != framingLeadingNull {
		t.Errorf("framing attribution = %q; want the defaulted dial", s1.HypothesisFraming())
	}
}

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

// TestSystemPrompt_AnswerShape guards the two rules #137 added (adr/0137): a
// findings report carries one shape, and containment ahead of the verdict is
// recommended only on named evidence of harm in progress. It also pins that
// the prompt no longer promises a [knowledge] block the loop stopped sending
// (adr/0129).
func TestSystemPrompt_AnswerShape(t *testing.T) {
	p := systemPrompt(Investigation{Title: "t"}, nil)
	for _, want := range []string{
		"a one-sentence headline that states the answer",
		"Findings",
		"Not covered",
		"Hypotheses",
		"Next steps",
		"Containment before the verdict",
		"harm under way",
		"never placed in your context",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt is missing: %q", want)
		}
	}
	if strings.Contains(p, "[knowledge]") {
		t.Error("the prompt still promises a [knowledge] block; knowledge reaches the agent only through tools (adr/0129)")
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
