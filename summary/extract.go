// Package summary is the two-tier investigation summary (design/06 §3.2;
// adr/0103): the structured facts as a pure fold over the projections at any
// state, and the narrative as revisions with their authorship. The knowledge
// pipeline's post-conclusion extraction lives here too, so the one fold feeds
// the Summary lens, the handoff note, the export and the case-summaries corpus.
package summary

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/knowledge"
)

// ExtractorVersion stamps the structured extraction (06 §3.2) so a later
// extractor can be told apart from this one when summaries are re-derived.
const ExtractorVersion = "1"

var techniqueRe = regexp.MustCompile(`^T\d{4}(\.\d{3})?$`)

// Extract is the knowledge-summary extraction (06 §3.2): aggregate state →
// the recallable Summary, no I/O. Pure, so the shape is unit-testable without
// a database. Moved here from the post-conclusion pipeline (adr/0103).
func Extract(groupingID string, ic aggregate.InvestigationCurrent, hyps []aggregate.HypothesisCurrent, actions []aggregate.ActionCurrent, createdAt, concludedAt time.Time) knowledge.Summary {
	seedKind, seedValue := "", ""
	var entities []string
	if ic.Seed != nil {
		seedKind = ic.Seed.Type
		seedValue = SeedValueOf(ic.Seed)
		if ic.Seed.Type == aggregate.SeedEntity && ic.Seed.EntityIdentifier != "" {
			entities = append(entities, ic.Seed.EntityIdentifier)
		}
	}
	summaryActions := make([]knowledge.SummaryAction, 0, len(actions))
	for _, ac := range actions {
		summaryActions = append(summaryActions, knowledge.SummaryAction{ActionType: ac.ActionType, TargetCount: len(ac.Targets), Outcome: ac.Status})
	}
	var duration float64
	if !createdAt.IsZero() && !concludedAt.IsZero() && concludedAt.After(createdAt) {
		duration = concludedAt.Sub(createdAt).Hours()
	}
	sum := knowledge.Summary{
		InvestigationRef:   groupingID,
		Title:              ic.Title,
		ConclusionAt:       concludedAt,
		GeneratorModel:     "", // deterministic baseline; the client tier writes the narrative
		ExtractorVersion:   ExtractorVersion,
		SeedKind:           seedKind,
		SeedValue:          seedValue,
		Techniques:         Techniques(hyps),
		PrimaryEntities:    entities,
		ActionsTaken:       summaryActions,
		ConclusionOutcome:  ConclusionOutcome(ic),
		DurationHours:      duration,
		VerdictDisposition: ic.VerdictDisposition,
		VerdictRationale:   ic.VerdictRationale,
	}
	sum.SummaryText = RenderBody(sum)
	return sum
}

// SeedValueOf renders the seed's identifying value per kind (06 §3.2).
func SeedValueOf(s *aggregate.Seed) string {
	switch s.Type {
	case aggregate.SeedAlert:
		return s.AlertID
	case aggregate.SeedEntity:
		if s.EntityIdentifier != "" {
			return s.EntityIdentifier
		}
		return s.EntityRef
	case aggregate.SeedQuestion:
		return s.HypothesisStatement
	case aggregate.SeedCase:
		return s.CaseID
	default:
		return ""
	}
}

// Techniques pulls MITRE technique ids from hypothesis labels, sorted and
// deduplicated.
func Techniques(hyps []aggregate.HypothesisCurrent) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hyps {
		for _, label := range h.Labels {
			if techniqueRe.MatchString(label) && !seen[label] {
				seen[label] = true
				out = append(out, label)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ConclusionOutcome is the v0 classification (06 §3.2): a recorded verdict
// means the investigation reached a disposition ("succeeded"); its absence is
// "inconclusive".
func ConclusionOutcome(ic aggregate.InvestigationCurrent) string {
	if ic.VerdictDisposition != "" {
		return "succeeded"
	}
	return "inconclusive"
}

// RenderBody is the deterministic recallable body of a knowledge summary.
func RenderBody(sum knowledge.Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Investigation %q concluded %s.", sum.Title, sum.ConclusionOutcome)
	if sum.SeedKind != "" {
		fmt.Fprintf(&b, " Seeded from %s: %s.", sum.SeedKind, sum.SeedValue)
	}
	if sum.VerdictDisposition != "" {
		fmt.Fprintf(&b, " Verdict: %s.", sum.VerdictDisposition)
		if sum.VerdictRationale != "" {
			fmt.Fprintf(&b, " %s", sum.VerdictRationale)
		}
	}
	if len(sum.Techniques) > 0 {
		fmt.Fprintf(&b, " Techniques: %s.", strings.Join(sum.Techniques, ", "))
	}
	if len(sum.PrimaryEntities) > 0 {
		fmt.Fprintf(&b, " Entities: %s.", strings.Join(sum.PrimaryEntities, ", "))
	}
	if len(sum.ActionsTaken) > 0 {
		parts := make([]string, 0, len(sum.ActionsTaken))
		for _, a := range sum.ActionsTaken {
			parts = append(parts, fmt.Sprintf("%s (%d target(s), %s)", a.ActionType, a.TargetCount, a.Outcome))
		}
		fmt.Fprintf(&b, " Actions taken: %s.", strings.Join(parts, "; "))
	}
	if sum.DurationHours > 0 {
		fmt.Fprintf(&b, " Duration: %.1fh.", sum.DurationHours)
	}
	return b.String()
}
