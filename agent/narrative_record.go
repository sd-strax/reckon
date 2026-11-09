package agent

// The narrative draft's grounding (adr/0103). The draft is of the record,
// and it runs with no tools, so what the record states has to be in the
// request. The turn loop's blocks (state.go, reconcile.go) carry the
// hypothesis set as statuses and tallies and the action record as statuses;
// the two blocks here carry the content behind them: the committed reasoning
// thread, each act as its author recorded it, and every prediction with its
// statement, outcome and evidence. A finding pinned by hand, a verdict or a
// manual test result recorded between turns reaches the draft this way.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bounds on the blocks: a handoff account is drafted from the recent record,
// and one act's rationale or one statement need not ride whole.
const (
	narrativeActsMax        = 40
	narrativeActSummaryMax  = 400
	narrativeStatementMax   = 280
	narrativeAttestationMax = 300
)

// narrativeActsBlock renders the thread, oldest first, without the
// context-reset markers (they say nothing about the case): sequence, type,
// actor kind, a verdict's disposition, a superseded mark, and the rationale.
// The oldest acts beyond the bound are elided with their count. Empty when
// the thread holds no act.
func narrativeActsBlock(thread []ThreadEntry) string {
	acts := make([]ThreadEntry, 0, len(thread))
	for _, e := range thread {
		if e.InterpretationType == interpretationContextReset {
			continue
		}
		acts = append(acts, e)
	}
	if len(acts) == 0 {
		return ""
	}
	rows := make([]string, 0, len(acts)+1)
	if n := len(acts) - narrativeActsMax; n > 0 {
		rows = append(rows, fmt.Sprintf(sideNarrativeActsElided.Text, n))
		acts = acts[n:]
	}
	for _, e := range acts {
		disposition, superseded := "", ""
		if e.Disposition != "" {
			disposition = fmt.Sprintf(sideNarrativeActDisposition.Text, e.Disposition)
		}
		if e.Superseded {
			superseded = sideNarrativeActSuperseded.Text
		}
		rows = append(rows, fmt.Sprintf(sideNarrativeAct.Text, e.SequenceNo, e.InterpretationType, e.Actor.Kind, disposition, superseded,
			clipRunes(strings.TrimSpace(e.Summary), narrativeActSummaryMax)))
	}
	return fmt.Sprintf(sideNarrativeActs.Text, strings.Join(rows, sideNarrativeLine.Text))
}

// narrativeTestsBlock renders every prediction under its hypothesis with its
// status and statement, the evidence refs its outcome cites, and a person's
// attestation when one decided it. Empty when no prediction exists.
func narrativeTestsBlock(raw json.RawMessage) string {
	var lines []string
	for i, h := range hypothesisRows(raw) {
		name := hypothesisLabel(h, i)
		for _, p := range h.Predictions {
			evidence, attested := "", ""
			if len(p.TestResultRefs) > 0 {
				evidence = fmt.Sprintf(sideNarrativeTestEvidence.Text, strings.Join(p.TestResultRefs, textListSeparator.Text))
			}
			if p.Attestation != nil && strings.TrimSpace(p.Attestation.Content) != "" {
				attested = fmt.Sprintf(sideNarrativeTestAttested.Text, clipRunes(strings.TrimSpace(p.Attestation.Content), narrativeAttestationMax))
			}
			lines = append(lines, fmt.Sprintf(sideNarrativeTest.Text, predictionLabel(p), name, p.Status,
				clipRunes(strings.TrimSpace(p.Statement), narrativeStatementMax), evidence, attested))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf(sideNarrativeTests.Text, strings.Join(lines, sideNarrativeLine.Text))
}

// hypothesisRows decodes the hypotheses view as hypothesisStateBlock does:
// the wrapped list the route serves, or a bare array. Nil when neither.
func hypothesisRows(raw json.RawMessage) []hypothesisRow {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wrapped struct {
		Hypotheses []hypothesisRow `json:"hypotheses"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		return wrapped.Hypotheses
	}
	var rows []hypothesisRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	return rows
}
