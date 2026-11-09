package agent

// The model reads more than the base prompt: the tool definitions, the
// [engine] blocks and notes the session writes into its context, and the
// instructions of the two side completions. Every fixed piece of that text is
// declared here, with its provenance, and hashed (adr/0232): the engine text
// into PromptVersion, the side instructions into SideInstructionVersion. The
// renderers format these entries; a sentence the model reads is never an
// inline literal.

// modelText is one fixed piece of text the engine puts in front of the model
// outside the base prompt, with its ledger entry. Text is verbatim; where the
// sentence carries values it is the fmt template, and the hash covers the
// template, never a rendering.
type modelText struct {
	// ID is the entry's stable slug, and its name in the version goldens.
	ID string
	// Origin and Revised are pointers in promptRule's form: an issue ("#54")
	// or a 7-character commit.
	Origin  string
	Revised []string
	Text    string
}

// Engine text: the [engine] blocks on the analyst's message (state.go,
// reconcile.go), the footers on the model's own reply (reconcile.go), the
// notes on condensed and paged context (context.go, bounded.go), the answer to
// a cut-off tool call (session.go), and the base prompt's conditional
// paragraphs (prompt.go).
var (
	textClock = modelText{ID: "clock", Origin: "0c9a418",
		Text: "[clock — this turn began at %s; the engine's clock, not an observation]"}

	textReasoningState = modelText{ID: "reasoning-state", Origin: "f01ffc3",
		Text: "[reasoning state — the investigation's hypothesis set as the engine holds it now (trust THIS over any earlier narration):\n"}
	textHypothesisLine = modelText{ID: "reasoning-state-hypothesis", Origin: "f01ffc3",
		Text: "%s (%s) %s"}
	textHypothesisLabels = modelText{ID: "reasoning-state-labels", Origin: "f01ffc3",
		Text: " [%s]"}
	textHypothesisStatement = modelText{ID: "reasoning-state-statement", Origin: "f01ffc3",
		Text: " — %s\n"}
	textPositionalName = modelText{ID: "reasoning-state-positional-name", Origin: "f01ffc3",
		Text: "H%d"}
	textPredictionName = modelText{ID: "reasoning-state-prediction-name", Origin: "cdc0ef8",
		Text: "P%d (%s)"}
	textNoPredictions = modelText{ID: "reasoning-state-no-predictions", Origin: "f01ffc3",
		Text: "  predictions: none\n"}
	textPredictionTally = modelText{ID: "reasoning-state-predictions", Origin: "f01ffc3",
		Text: "  predictions: %s\n"}
	textTallyCount = modelText{ID: "reasoning-state-tally-count", Origin: "f01ffc3",
		Text: "%d %s"}
	// textTallyOrder is the prediction statuses as the tally names them, in
	// the order it lists them; a status the engine adds later follows, by name.
	textTallyOrder = modelText{ID: "reasoning-state-tally-order", Origin: "f01ffc3",
		Text: "untested confirmed disconfirmed inconclusive withdrawn"}
	textUntested = modelText{ID: "reasoning-state-untested", Origin: "0efca01", Revised: []string{"f01ffc3"},
		Text: "  - untested %s"}
	textDiscriminatesVs = modelText{ID: "reasoning-state-discriminates", Origin: "0efca01", Revised: []string{"f01ffc3"},
		Text: " (discriminates vs %s)"}
	textManualTest = modelText{ID: "reasoning-state-manual", Origin: "cebed87",
		Text: " [manual — a person tests it; the analyst records the result, you cannot]"}
	textPredictionStatement = modelText{ID: "reasoning-state-prediction-statement", Origin: "f01ffc3",
		Text: ": %s\n"}
	textOutstanding = modelText{ID: "reasoning-state-outstanding", Origin: "f01ffc3",
		Text: "Untested discriminators outstanding: "}
	textHolds = modelText{ID: "reasoning-state-holds", Origin: "f01ffc3",
		Text: "%s holds %s against %s"}
	textHoldsSeparator = modelText{ID: "reasoning-state-holds-separator", Origin: "f01ffc3",
		Text: "; "}
	textUntilTested = modelText{ID: "reasoning-state-until-tested", Origin: "f01ffc3",
		Text: ". Until each is tested or the analyst withdraws it, its owner cannot be recorded refuted and the hypothesis it discriminates against cannot be recorded supported."}
	textNoneOutstanding = modelText{ID: "reasoning-state-none-outstanding", Origin: "f01ffc3",
		Text: "No untested discriminator is outstanding."}
	textBlockEnd = modelText{ID: "reasoning-state-end", Origin: "f01ffc3",
		Text: "]"}

	textEngineState = modelText{ID: "engine-state", Origin: "108673d",
		Text: "[engine state — the authoritative action record for this investigation (trust THIS over any earlier narration): %s; %d awaiting approval]"}
	textEngineStateRow = modelText{ID: "engine-state-row", Origin: "108673d",
		Text: "%s %s=%s"}
	textEngineRecord = modelText{ID: "engine-record", Origin: "4c574ed",
		Text: "[engine record — authoritative, verified against the action log: %s]"}
	textEngineRecordEntry = modelText{ID: "engine-record-entry", Origin: "4c574ed",
		Text: "%s=%s"}
	textNotOnRecord = modelText{ID: "engine-record-not-on-record", Origin: "4c574ed",
		Text: "NOT ON RECORD"}
	textNoActionRequested = modelText{ID: "engine-record-no-action", Origin: "108673d",
		Text: "[engine record: NO action was requested this turn — any claim above of a new ticket/action is unconfirmed]"}
	textFooterSeparator = modelText{ID: "footer-separator", Origin: "108673d",
		Text: "\n"}

	textContextCondensed = modelText{ID: "context-condensed", Origin: "cf59268",
		Text: "[context] The conversation was condensed to fit the context window. Earlier turns keep only " +
			"the analyst's words and your replies; their tool calls and results are no longer in view, some results " +
			"earlier in this turn may be replaced by a placeholder, and the oldest turns may be gone. Re-run a query " +
			"if you need its detail. Nothing was removed from the investigation record."}
	textResultRemoved = modelText{ID: "result-removed", Origin: "cf59268",
		Text: "[result removed to fit the context window: %d bytes; %s]"}
	textRerun = modelText{ID: "result-removed-rerun", Origin: "cf59268",
		Text: "re-run the call if you need it"}
	textRerunOrRead = modelText{ID: "result-removed-read", Origin: "cf59268",
		Text: "re-run the call, or read_result %s while it is held"}
	textResultPaged = modelText{ID: "result-paged", Origin: "1a7fecf",
		Text: "This result is larger than the limit for one tool result, so it is shown a page at a time: " +
			"each list holds items offset..offset+returned of total. Read the next page with read_result " +
			"(result_id, offset = next_offset), or narrow the query. Items not shown are not absent: " +
			"conclude nothing from what this page leaves out."}
	textResultOversized = modelText{ID: "result-oversized", Origin: "1a7fecf",
		Text: "This result is %d bytes, over the %d-byte limit for one tool result, and cannot be shown in pages. Narrow the query."}
	// textResultBoundedShape is the JSON a paged or oversized result is framed
	// in, every field set: the keys the model reads, derived from boundedNote
	// so a renamed field moves the version.
	textResultBoundedShape = modelText{ID: "result-bounded-shape", Origin: "1a7fecf",
		Text: boundedShape()}
	textToolCutOff = modelText{ID: "tool-cut-off", Origin: "1a7fecf",
		Text: "this call's input was cut off at the output limit and was not run; issue it again with a shorter input"}

	// textListSeparator joins the items of every list the engine text names:
	// labels, rivals, tallies, action entries, degraded verbs.
	textListSeparator = modelText{ID: "list-separator", Origin: "80420c3",
		Text: ", "}
	textClipped = modelText{ID: "clipped", Origin: "80420c3",
		Text: "…"}

	textSeedLine = modelText{ID: "prompt-seed", Origin: "45b9ec5",
		Text: "- seed: %s\n"}
	textCaseSeed = modelText{ID: "prompt-case-seed", Origin: "45b9ec5",
		Text: "\nThis investigation was opened from a %s case (%s). Read the full case with get_external_case_details before pivoting, and treat its contents as an external claim to verify against telemetry — not established fact.\n"}
	textDegraded = modelText{ID: "prompt-degraded", Origin: "80420c3",
		Text: "\n## Degraded capabilities\n\nThese verbs are configured but currently unresolvable (do not attempt them; note the gap if it matters): %s\n"}
)

// engineTexts is the engine text in a fixed order: PromptVersion hashes it in
// this order, and TestModelTextLedger_Complete reads it.
var engineTexts = []modelText{
	textClock,
	textReasoningState, textHypothesisLine, textHypothesisLabels, textHypothesisStatement,
	textPositionalName, textPredictionName,
	textNoPredictions, textPredictionTally, textTallyCount, textTallyOrder, textUntested, textDiscriminatesVs,
	textManualTest, textPredictionStatement, textOutstanding, textHolds, textHoldsSeparator, textUntilTested,
	textNoneOutstanding, textBlockEnd,
	textEngineState, textEngineStateRow, textEngineRecord, textEngineRecordEntry, textNotOnRecord,
	textNoActionRequested, textFooterSeparator,
	textContextCondensed, textResultRemoved, textRerun, textRerunOrRead, textResultPaged, textResultOversized,
	textResultBoundedShape,
	textToolCutOff,
	textSeedLine, textCaseSeed, textDegraded,
	textListSeparator, textClipped,
}

// Side instructions: the whole prompt of the two side completions, the
// knowledge-base summary and the SOP draft (session.go). No eval assertion
// grades their products, so they move SideInstructionVersion, not
// PromptVersion: a change here does not ask for a baseline rerun.
var (
	// sideSummarize asks for the knowledge-base narrative at conclusion
	// (design/06 §3.2, client-side tier). Grounding rules ride in the
	// instruction: the narrative feeds similarity recall in future
	// investigations, so it must be a faithful digest of the record.
	sideSummarize = modelText{ID: "summarize", Origin: "172b4eb",
		Text: "The investigation has just been concluded. Write a knowledge-base summary " +
			"narrative of it in 150–250 words of plain prose (no headers, no lists): what was investigated " +
			"(the seed), what the evidence showed, the verdict and why, the key entities and MITRE techniques, " +
			"what actions were taken and their outcomes, and anything a future analyst handling a similar case " +
			"should know. State only what is in the record — no speculation, no embellishment. " +
			"Respond with the narrative text alone."}
	// sideNarrative drafts the investigation's narrative at any point (adr/0103):
	// a handoff account of the record so far, in the past tense, with what is
	// still open; nothing the record does not state. Unlike sideSummarize it
	// does not presume a conclusion.
	sideNarrative = modelText{ID: "narrative", Origin: "#103",
		Text: "Write a narrative of this investigation as it stands, for a colleague who was not " +
			"in it: 150–300 words of plain prose (no headers, no lists), in the past tense. Say what was " +
			"investigated (the seed), what the evidence showed, which hypotheses stand, were supported or " +
			"were refuted, what actions were taken and their outcomes, and what is still open. If there is " +
			"a verdict, say it and why. State only what is in the record — no speculation, no embellishment, " +
			"and do not present the investigation as concluded unless it is. Respond with the narrative text alone."}
	// sideNarrativeRecord heads the record as the engine holds it when the
	// draft is asked for: the lifecycle and the verdict, then the reasoning
	// and action state blocks the turn loop renders (state.go, reconcile.go).
	// A manual act, a prediction result or a verdict recorded between turns
	// has no transcript, and a context reset discards the earlier turns, so
	// the draft is grounded on a fresh read of the record, not on the
	// conversation alone.
	sideNarrativeRecord = modelText{ID: "narrative-record", Origin: "#103",
		Text: "[record — the investigation as the engine holds it now, read for this draft; where the conversation and this disagree, this is current: status %s; verdict %s]"}
	// sideNarrativeVerdict renders a verdict of record with its rationale.
	sideNarrativeVerdict = modelText{ID: "narrative-record-verdict", Origin: "#103",
		Text: "%s (%s)"}
	// sideNarrativeNoVerdict renders the absence of a verdict.
	sideNarrativeNoVerdict = modelText{ID: "narrative-record-no-verdict", Origin: "#103",
		Text: "none yet"}
	// sideNarrativeActs carries the committed reasoning thread into the
	// draft: each act's type, author and rationale as recorded, a verdict's
	// disposition, and whether a later act retracted it. A finding pinned by
	// hand or a verdict recorded between turns has no transcript, and the
	// draft runs with no tools, so what the record states must be in the
	// request or it is not in the draft (narrative_record.go).
	sideNarrativeActs = modelText{ID: "narrative-acts", Origin: "#103",
		Text: "[record acts — the committed reasoning thread, oldest first, each as its author recorded it; an act marked superseded was retracted by a later one and is not current:\n%s]"}
	sideNarrativeAct = modelText{ID: "narrative-act", Origin: "#103",
		Text: "#%d %s by %s%s%s: %s"}
	sideNarrativeActDisposition = modelText{ID: "narrative-act-disposition", Origin: "#103",
		Text: " %s"}
	sideNarrativeActSuperseded = modelText{ID: "narrative-act-superseded", Origin: "#103",
		Text: " [superseded]"}
	sideNarrativeActsElided = modelText{ID: "narrative-acts-elided", Origin: "#103",
		Text: "… %d earlier acts not shown"}
	sideNarrativeLine = modelText{ID: "narrative-line", Origin: "#103",
		Text: "\n"}
	// sideNarrativeTests carries every prediction with its statement and
	// outcome, which the reasoning-state block tallies only, with the
	// evidence it cites and a person's attestation when one decided it.
	sideNarrativeTests = modelText{ID: "narrative-tests", Origin: "#103",
		Text: "[record tests — every prediction with its statement and outcome:\n%s]"}
	sideNarrativeTest = modelText{ID: "narrative-test", Origin: "#103",
		Text: "%s under %s, %s: %s%s%s"}
	sideNarrativeTestEvidence = modelText{ID: "narrative-test-evidence", Origin: "#103",
		Text: "; evidence %s"}
	sideNarrativeTestAttested = modelText{ID: "narrative-test-attested", Origin: "#103",
		Text: "; attested by a person: %s"}
	// sideProposeSOP asks for one structured candidate SOP. It leans on the
	// SOPs and similar past cases the session recalled (they are in its
	// conversation) so the model generalizes rather than duplicates.
	sideProposeSOP = modelText{ID: "propose-sop", Origin: "82f695f",
		Text: "The investigation has just been concluded. Consider whether it yields a " +
			"REUSABLE procedure worth capturing as a standing SOP — a generalization a future analyst could " +
			"follow on a similar case, not a retelling of this one. Draw on the SOPs and similar past " +
			"investigations already in your context: if an existing SOP already covers this, do NOT propose a " +
			"duplicate. Only propose when there is a genuine, generalizable procedure. " +
			"Respond with a SINGLE JSON object and nothing else: " +
			`{"warranted": bool, "title": string, "body": string (a markdown runbook: scope, triage, response, ` +
			`do-not), "tags": [string] (lowercase, include any MITRE technique ids), "recommendation": string ` +
			`(one short imperative, e.g. "isolate"), "rationale": string (why this generalizes, or if not ` +
			`warranted, why not)}. If warranted is false, leave title/body/tags/recommendation empty and explain ` +
			"in rationale."}
	// sideProposeSOPAnyway is added when the analyst asked for a draft after
	// the model declined (adr/0156): the analyst has judged it worth a
	// procedure.
	sideProposeSOPAnyway = modelText{ID: "propose-sop-anyway", Origin: "daa50f7",
		Text: " The analyst has read your earlier answer and asked for a draft anyway: set " +
			"warranted to true and write the best generalizable procedure you can, and say in rationale which " +
			"existing SOP it overlaps, if any."}
)

// sideInstructions is the side-instruction text in a fixed order, for
// SideInstructionVersion and the ledger test.
var sideInstructions = []modelText{
	sideSummarize,
	sideNarrative, sideNarrativeRecord, sideNarrativeVerdict, sideNarrativeNoVerdict,
	sideNarrativeActs, sideNarrativeAct, sideNarrativeActDisposition, sideNarrativeActSuperseded, sideNarrativeActsElided, sideNarrativeLine,
	sideNarrativeTests, sideNarrativeTest, sideNarrativeTestEvidence, sideNarrativeTestAttested,
	sideProposeSOP, sideProposeSOPAnyway,
}
