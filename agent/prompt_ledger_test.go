package agent

import (
	"strings"
	"testing"
)

// TestPrompt_Frozen pins the base prompt's version for each framing dial
// (adr/0057). The prompt is the soft layer of the agent's behavior and every
// change to its words moves the version the eval baselines are accepted under
// (design/10 §4.4), so a change must be deliberate: update the golden here, the
// ledger entry of the rule that moved, and rerun the per-model baselines. A
// refactor that keeps the bytes (the ledger rendering, for one) leaves this
// test green — that is the point of pinning the hash rather than the text.
func TestPrompt_Frozen(t *testing.T) {
	golden := map[string]string{
		framingLeadingNull: "c7b63b5bf7a6699572284715cedd268d878462f63954721b25966ad860551658",
		framingLeadingOnly: "4c25f9e10c6659abb483b3f154acdc2d907d64b8e6ef4ad8ed151a3b84faf9c9",
	}
	for dial, want := range golden {
		if got := PromptVersion(dial); got != want {
			t.Errorf("prompt version under %s = %s; want %s\n"+
				"  the base prompt changed. If that is deliberate: update this golden, the promptLedger entry of the rule that moved (Since/Why/Validated), and rerun the per-model eval baselines (make eval; make eval-accept) — design/10 §4.4.",
				dial, got, want)
		}
	}
}

// TestPromptLedger_Complete refuses a rule without provenance: every rendered
// bullet must carry a stable ID, what introduced it, the principle it encodes
// (stated without scenario vocabulary), and at least one scenario its wording
// was graded against. A rule that exists only because one transcript went
// wrong cannot hide here as prose.
func TestPromptLedger_Complete(t *testing.T) {
	seen := map[string]bool{}
	for _, sec := range promptLedger {
		if sec.Title == "" || len(sec.Rules) == 0 {
			t.Errorf("ledger section %q is empty", sec.Title)
		}
		for _, r := range sec.Rules {
			switch {
			case r.ID == "":
				t.Errorf("rule %q has no ID", r.Text)
			case seen[r.ID]:
				t.Errorf("rule ID %q is used twice", r.ID)
			}
			seen[r.ID] = true
			if r.Since == "" {
				t.Errorf("rule %s: Since is empty — name the commit, issue, or PR that introduced it", r.ID)
			}
			if strings.TrimSpace(r.Why) == "" {
				t.Errorf("rule %s: Why is empty — state the principle, or the rule is a patch", r.ID)
			}
			if len(r.Validated) == 0 {
				t.Errorf("rule %s: Validated is empty — a rule is graded against at least one scenario before it ships", r.ID)
			}
			if r.Text == "" {
				t.Errorf("rule %s: Text is empty", r.ID)
			}
			if strings.HasPrefix(r.Text, "- ") || strings.HasSuffix(r.Text, "\n") {
				t.Errorf("rule %s: Text carries its own bullet or newline; the renderer adds those", r.ID)
			}
		}
	}
	// Every rule renders: the prompt under each dial contains each rule's text
	// for that dial, so the ledger and the prompt cannot drift apart.
	for _, dial := range []string{framingLeadingNull, framingLeadingOnly} {
		p := systemPrompt(Investigation{HypothesisFraming: dial}, nil)
		for _, sec := range promptLedger {
			for _, r := range sec.Rules {
				if !strings.Contains(p, "- "+r.render(dial)+"\n") {
					t.Errorf("rule %s is in the ledger but not rendered under %s", r.ID, dial)
				}
			}
		}
	}
}
