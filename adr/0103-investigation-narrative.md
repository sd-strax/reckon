# 0103 — The investigation's narrative is a revision on the record, drafted on demand and edited by a person, each with its author

Issue: #103 · PR: #316 · Status: accepted

## Context

An investigation had no written account of itself until it concluded. At
conclusion the workbench ran one model completion over the session and
posted the text into the past-case corpus as the knowledge summary's
narrative (design/06 §3.2 as amended). For an open investigation the Summary
lens showed a gap line where "What happened" should be, the handoff note was
a list of facts stitched in the page, and the conclude dialog asked for a
summary from nothing. Nothing recorded who wrote a narrative: the knowledge
summary stamps the model's name, and a person's edit of it had no form.

The use is the handoff: an investigation in progress has to be explained to
someone who was not in it. The account must say what the record shows so far
and who wrote it, since a model's draft and a person's statement are
different claims.

Two constraints already decided bound the answer. The backend holds no model
key, so a draft is the client's act on the analyst's own key (design/06
§3.2). And every act on the investigation records a human principal with the
AI as a delegate, never the other way (design/02 §7); the narrative's
authorship is that same question.

The implementation sheet (`workbench-react.md §8.7`) sketched a table of
narratives with its own author columns, and a server-side read of the
structured facts. Both were weighed against the record as it is.

## Decision

- **A narrative is a revision on the investigation aggregate.**
  `RecordNarrative{Body, Model}` appends one domain event,
  `investigation.narrative_recorded`, with payload `{body, model?}`. Like a
  rename it pairs no interpretation: it is an account of the record, not a
  reasoning act, and it never enters the chronicle. The authorship is the
  event's actor, the attribution every act already carries and the HTTP
  layer derives from the token alone: `AI_DELEGATED` with the person as
  principal for a draft, `HUMAN` for an edit. Every record is a new
  revision; the model's draft stays on the record under a person's edit.
  - Guards at the command: the body is non-blank and at most 20 000 runes;
    `Model` is refused on a human actor, so an edit can never be dressed as
    a draft. `RecordNarrative` is on the AI allowlist. A concluded
    investigation still narrates (a closed case is handed on too); archived
    and discarded are refused by the terminal guard as everything is.
  - `foldState` lists the event as metadata. `InvestigationCurrentProjector`
    advances the cursor for it, so `last_event_sequence` says how far the
    record had gone when the narrative was written.
- **`NarrativeProjector` keeps `investigation_narratives`** (migration 0030):
  one row per revision in the append transaction, the revision number the
  count so far plus one, with the actor's kind, principal, name and the
  model. `Reset` truncates; replay rebuilds it. `ReadModel.Narrative` answers
  the current revision, who drafted the first, and the history newest first,
  each revision with its body; `ErrNoRecord` when none.
- **Two routes beside the investigation's.** `GET
  /api/investigations/{id}/narrative` (read roles; 404 when none) and `POST
  /api/investigations/{id}/narrative` with `{body, model?}` (analyst role),
  the actor from the token as on every write. Both answer `NarrativeView`:
  `investigation_ref, revision, body, sequence_no, occurred_at, author,
  drafted_by, revisions`. The investigation detail carries `narrative` so
  the Summary lens renders from the snapshot; list rows do not. A failed
  narrative read fails the detail: an absence and a failure must not look
  alike, or an edit over the failure would drop the text on the record.
- **The draft is the model's act through the sidecar.**
  `Session.DraftNarrative` runs one no-tools completion over the session's
  own conversation under a new side instruction, `sideNarrative` (plain
  prose in the past tense, the record so far, what stands, was supported or
  refuted, what was done, what is open; nothing the record does not state;
  not presented as concluded unless it is), then posts the text and the
  model's name through the agent token.
  - **The draft is of the record, not of the conversation.** A hypothesis
    framed by hand, a prediction result or a verdict recorded between turns
    has no transcript, and a context reset discards the earlier turns. So
    the draft reads the record now, the investigation's lifecycle and
    verdict, the hypothesis set and the action record, and the request
    carries them ahead of the instruction as a turn's `[engine]` blocks do
    (the clock, a record header `sideNarrativeRecord`, then the
    reasoning-state and engine-state blocks the turn loop already renders).
    A failed read fails the draft: a draft over a partial record would state
    less than the record holds, stamped as current.
  - **Emptiness is judged against the record.** With no conversation and
    nothing recorded beyond the seed (no act on the thread but a context
    reset marker, no hypothesis, no action) it refuses rather than narrate
    the seed line alone; the lens offers no draft in the same case. An
    investigation worked by hand, or one just after a context reset, drafts
    from the record's state alone.
  - The sidecar method is `draftNarrative {session_id}` → `{revision,
    chars}`, holding the session like a turn. The side instruction and the
    record header join the ledger ([0232](0232-model-facing-text.md));
    `SideInstructionVersion` moves from `a1bd6f38…` to `ba2bfd37…` and the
    component golden gains `side/narrative` and `side/narrative-record`
    with its verdict renderings. No baseline rerun: no eval assertion grades
    the side products.
- **The analyst's revision is a plain write on the human token.** The
  workbench posts `narrative.edit {body}`, the host calls the route with the
  person's token, and the record attributes the revision to them.
- **The Summary lens shows the account and says who wrote it.** "What
  happened" renders the body with "drafted by reckon (model) · edited by
  <name> · revision N · when", "redrafted by reckon (model)" with the current
  revision's model when a later draft replaced the text, or "drafted by
  <name>" for a human-first narrative, and "the record has moved M events
  since" when it has. A failed draft's note goes when the investigation is
  re-read. It
  offers **Draft narrative** (then **Draft again**) and **Edit** (an editor
  on the lens, saved as the analyst's revision; cancel and an unchanged save
  record nothing). The editor holds the only copy of the analyst's text, so
  it stays open, read-only, from the press until the record carries the
  revision and the re-read shows it; a refused save (the bound, a failed
  request) says why and keeps the text editable. While a draft runs the lens
  says so and disables both; a failed draft says why. The gap line is gone. The conclude prompt is prefilled with
  the current body. The handoff note leads with the narrative when one
  exists and keeps the facts beneath.
- **The knowledge summary pipeline is unchanged.** At conclusion the
  workbench still asks the model for the past-case narrative as before.

## Alternatives rejected

- **A table with its own author columns, written by the route.** A second,
  weaker copy of the attribution the aggregate already records, enforced by
  nothing, with the narrative absent from the record's own history. The
  investigation aggregate is the one event-sourced thing, and a narrative is
  content of the investigation.
- **A paired interpretation, or a new interpretation type.** It would put
  the narrative in the chronicle as a reasoning act and widen the closed set
  of reasoning types the spec fixes; an account of the record is not a step
  in it.
- **Overwriting in place.** The record must keep the model's draft under a
  person's edit, or the authorship line is a claim with nothing behind it.
- **The structured facts read server-side.** The page derives them from the
  snapshot today and nothing else needs them; the item's value is the
  draft, its authorship and the prefill.
- **Feeding the narrative to the knowledge summary at conclude.** A reviewed
  narrative would save a model call, but the knowledge summary's narrative
  is stamped with a model, and a human-edited text is not one; the two
  records would need a shared notion of authorship first.

## Consequences

- An analyst can hand on an open investigation in writing, with a draft one
  press away and the record saying who wrote what; concluding starts from
  that text.
- One new event type and projection, two routes, one sidecar method and one
  side instruction. `SideInstructionVersion` moves; the goldens are updated
  with it.
- A draft costs one model call on the analyst's key, like the knowledge
  summary and the SOP draft; nothing runs without a press.
- Narrative events do not appear on the thread or the Story lens; the
  Summary lens is where the account lives. The knowledge summary pipeline
  still drafts its own narrative at conclusion; folding the two is a
  separate decision.
- design/ui/00-backend-binding gap 18, design/ui/08 §8.4 and
  workbench-react §8.7 note what is built and what the sheet said
  differently.
