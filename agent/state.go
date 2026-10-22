package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Per-turn engine state blocks.
//
// The system prompt is the standing contract (who the agent is, the rules, the
// tool vocabulary) and is byte-stable for the life of a session: the prompt
// cache matches the request prefix in the order tools → system → messages, so
// any change to the system prompt invalidates every cached message after it and
// the whole history is rewritten at the cache-write premium. State that moves
// between turns — the hypothesis set, the action record, the knowledge the
// analyst curated — therefore rides the user message of the turn it is observed
// on, as an [engine] block logged in the transcript. The model sees current
// state every turn; earlier turns keep the state they reasoned against; the
// record shows what the model was told and when.

// clockBlock states the time this turn began. The model has no clock: every
// tool result is a timestamped observation, and the only "now" it would
// otherwise ever see is the one inside a list_actions result, turns earlier.
// "Is this still happening?" is a comparison between the latest observation
// held and the present — so the engine supplies the present on every turn,
// and the model never has to remember one or infer it from narrative. The
// prompt's timeframe rule (prompt.go, "Output discipline") names this block.
func clockBlock(now time.Time) string {
	return fmt.Sprintf("[clock — this turn began at %s; the engine's clock, not an observation]",
		now.UTC().Format(time.RFC3339))
}

// hypothesisRow mirrors the fields of the server's HypothesisView the block
// renders. Decoded locally so the agent package keeps no dependency on server.
type hypothesisRow struct {
	ID          string          `json:"id"`
	Statement   string          `json:"statement"`
	Handle      string          `json:"handle"`
	Status      string          `json:"status"`
	Labels      []string        `json:"labels"`
	Predictions []predictionRow `json:"predictions"`
}

type predictionRow struct {
	ID            string   `json:"id"`
	Statement     string   `json:"statement"`
	Status        string   `json:"status"`
	Ordinal       int      `json:"ordinal"`
	Discriminates []string `json:"discriminates"`
	TestKind      string   `json:"test_kind"` // "manual" when a person tests it (adr/0054)
}

// predictionLabel is how the block names a prediction: its ordinal (P3) when
// the view supplied one — the same short name the tracker shows the analyst,
// so "test P3" means the same thing on both sides — followed by the id the
// tools take. Without an ordinal, the id alone.
func predictionLabel(p predictionRow) string {
	if p.Ordinal > 0 {
		return fmt.Sprintf("P%d (%s)", p.Ordinal, p.ID)
	}
	return p.ID
}

// predictionStatementMax bounds a prediction statement in the block; the
// hypothesis statement is kept whole because the model reasons against it.
const predictionStatementMax = 140

// hypothesisStateBlock renders the investigation's hypothesis set as the
// engine holds it now: every hypothesis with handle, id, status, statement and
// a prediction tally, then the discriminators still untested — the one fact
// the decide contract turns on (01 §x-hypothesis: a rival holding an untested
// discriminator is undecided, so neither it nor the leader can be called).
// Empty when the investigation has no hypotheses.
func hypothesisStateBlock(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// GET /api/investigations/{id}/hypotheses wraps the list; accept a bare
	// array too so the block is robust to either.
	var wrapped struct {
		Hypotheses []hypothesisRow `json:"hypotheses"`
	}
	rows := wrapped.Hypotheses
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		rows = wrapped.Hypotheses
	} else if err := json.Unmarshal(raw, &rows); err != nil {
		return ""
	}
	if len(rows) == 0 {
		return ""
	}
	nameOf := make(map[string]string, len(rows))
	for i, h := range rows {
		nameOf[h.ID] = hypothesisLabel(h, i)
	}

	var b strings.Builder
	b.WriteString("[reasoning state — the investigation's hypothesis set as the engine holds it now (trust THIS over any earlier narration):\n")
	type outstanding struct {
		owner, rival, pred string
	}
	var open []outstanding
	for _, h := range rows {
		fmt.Fprintf(&b, "%s (%s) %s", nameOf[h.ID], h.ID, h.Status)
		if len(h.Labels) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(h.Labels, ", "))
		}
		fmt.Fprintf(&b, " — %s\n", strings.TrimSpace(h.Statement))
		if len(h.Predictions) == 0 {
			b.WriteString("  predictions: none\n")
			continue
		}
		tally := map[string]int{}
		for _, p := range h.Predictions {
			tally[p.Status]++
		}
		b.WriteString("  predictions: " + tallyString(tally) + "\n")
		for _, p := range h.Predictions {
			if p.Status != "UNTESTED" {
				continue
			}
			rivals := make([]string, 0, len(p.Discriminates))
			for _, d := range p.Discriminates {
				name := nameOf[d]
				if name == "" {
					name = d
				}
				rivals = append(rivals, name)
				if isLiveHypothesis(h.Status) {
					open = append(open, outstanding{owner: nameOf[h.ID], rival: name, pred: predictionLabel(p)})
				}
			}
			fmt.Fprintf(&b, "  - untested %s", predictionLabel(p))
			if len(rivals) > 0 {
				fmt.Fprintf(&b, " (discriminates vs %s)", strings.Join(rivals, ", "))
			}
			if p.TestKind == "manual" {
				// A person tests it (adr/0054): the model cannot run it and the
				// engine refuses its result from the delegate — say so where the
				// model reads the test, not only in the standing rules.
				b.WriteString(" [manual — a person tests it; the analyst records the result, you cannot]")
			}
			fmt.Fprintf(&b, ": %s\n", clipRunes(strings.TrimSpace(p.Statement), predictionStatementMax))
		}
	}
	if len(open) > 0 {
		b.WriteString("Untested discriminators outstanding: ")
		parts := make([]string, 0, len(open))
		for _, o := range open {
			parts = append(parts, fmt.Sprintf("%s holds %s against %s", o.owner, o.pred, o.rival))
		}
		b.WriteString(strings.Join(parts, "; "))
		b.WriteString(". Until each is tested or the analyst withdraws it, its owner cannot be recorded refuted and the hypothesis it discriminates against cannot be recorded supported.")
	} else {
		b.WriteString("No untested discriminator is outstanding.")
	}
	b.WriteString("]")
	return b.String()
}

// hypothesisLabel is the short name the block uses for a hypothesis: its handle
// when it has one, else a positional H<n> so rivals can still be referred to.
func hypothesisLabel(h hypothesisRow, i int) string {
	if strings.TrimSpace(h.Handle) != "" {
		return strings.TrimSpace(h.Handle)
	}
	return fmt.Sprintf("H%d", i+1)
}

// isLiveHypothesis reports whether a hypothesis is still undecided — the only
// state in which an untested discriminator constrains a decision.
func isLiveHypothesis(status string) bool {
	return status == "PROPOSED" || status == "OPEN"
}

// tallyString renders a prediction status tally in a fixed order so the block
// is stable for the same state.
func tallyString(tally map[string]int) string {
	order := []string{"UNTESTED", "CONFIRMED", "DISCONFIRMED", "INCONCLUSIVE", "WITHDRAWN"}
	parts := make([]string, 0, len(order))
	for _, st := range order {
		if n := tally[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(st)))
			delete(tally, st)
		}
	}
	rest := make([]string, 0, len(tally)) // anything the engine adds later
	for st := range tally {
		rest = append(rest, st)
	}
	sort.Strings(rest)
	for _, st := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", tally[st], strings.ToLower(st)))
	}
	return strings.Join(parts, ", ")
}
