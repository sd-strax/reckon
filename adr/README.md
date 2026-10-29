# Architectural decision records

An issue says what is wrong or what is wanted. It does not say how. The how —
the design, the alternatives weighed, and why one won — lives here, one record
per decision, committed in the PR that implements it.

## Naming

`NNNN-component.md`, where `NNNN` is the GitHub issue number zero-padded to
four digits and `component` is a short slug for the part of the system the
decision mostly touches. `0030-cases-pane.md` is the record for issue #30.

A decision that spans several issues takes the number of the issue that
prompted it and lists the others in its header. A decision with no issue
(rare — a refactor nobody asked for in words) takes the PR number instead,
prefixed `pr`.

## Shape

Keep it short. A record is read by someone who wants to know why the code is
the way it is, months later, without the issue thread.

```markdown
# NNNN — Title (a sentence naming the decision)

Issue: #NNNN · PR: #MMMM · Status: accepted | superseded by NNNN

## Context
What was wrong, and the constraints that bound the answer (specs, invariants,
principles already decided).

## Decision
What was done, stated as facts about the system.

## Alternatives rejected
Each with the one reason it lost.

## Consequences
What this makes easier, harder, or now needs doing.
```

Reference specs by file and section (`design/14-case-seed.md §4.2`), the same
way the specs reference each other. A record states architectural merit only:
it belongs to a public repository.

## Index

| ADR | Decision |
|---|---|
| [0030](0030-cases-pane.md) | The case queue is a read-only view with a non-persisting read surface; starting an investigation is the only act on it |
| [0032](0032-case-note.md) | The "opened in reckon" note on a seed case is a `ticket.comment` action in the new investigation, offered under `cases.note_on_seed` (confirm by default) |
| [0034](0034-start-detach.md) | `reckon start --detach` returns when the stack is ready; foreground stays the default; `scripts/demo-reseed.sh` composes the lifecycle |
| [0047](0047-hypotheses-panel.md) | The hypothesis tracker shows the whole competing set in full, names predictions `P<n>` from the server view, and shows the decide guard where it bites |
| [0051](0051-decide-guard.md) | Decide-against-rivals is an aggregate gate for AI delegates (the human override is a visible act, never a flag); scoped ⚡ commands say where they end; predictions name themselves |
| [0054](0054-manual-predictions.md) | A prediction declares whether a tool or a person tests it; a person's answer is recorded by the analyst as a STIX `note` the outcome cites; the AI can propose such a test, never record its result |
| [0057](0057-prompt-ledger.md) | The base prompt is rendered from a ledger (id, origin, principle, enforcing mechanism, grading assertion, validated scenarios), its version is pinned by a frozen test, and a fixture-derived guard refuses scenario vocabulary in the prompt and tool descriptions |
| [0058](0058-second-scenario.md) | The eval corpus has two scenarios of different shape (host-rooted lateral movement; identity-rooted impossible travel); the stack is wired for one fixture scenario at a time (`demo seed --scenario`, `demo scenario`), the driver declares it and the runner checks it; fixture matchers accept a list of alternatives |
| [0059](0059-judge-graded-honesty.md) | E3/R3 keep the anchors as the deterministic first pass and ask a BYOK judge model, against a versioned rubric, only when a declined-outcome turn matches no anchor; the verdict quotes the sentence relied on and the run records the judge |
| [0060](0060-prompt-revision.md) | Five prompt rules restated as principles with no scenario vocabulary and no re-legislation of engine gates; the revision reviewed by a party who did not write it against a fixed rubric; a revised rule is `PendingValidation` in the ledger until both scenarios' baselines accept it |
| [0061](0061-null-observation.md) | Absence is citeable: a COMPLETE, empty capability answer mints an `x-null-observation` (verb, subject, parameters, window, sources; UUIDv5 over the same) whose id rides `observed_data_refs`; a PARTIAL empty answer mints nothing |
| [0069](0069-driver-answers-manual-tests.md) | The eval driver answers a manual test with a per-scenario authored answer through the Record result path; a reader model decides which way it points for each prediction's wording and the reading is in the trial record; graders untouched |
| [0075](0075-test-button.md) | A prediction row's test is a scoped ⚡ turn (`⚡ Test P<n>`), the per-row member of the ⚡ family; the workbench maps the server's prediction ordinal so analyst and agent share one name |
| [0079](0079-investigation-layout.md) | The investigation document is organized as four lenses (Summary, Story, Reasoning, Response) with the default following the derived lifecycle; the conversation is a companion column; every citation opens in a typed inspector, never an editor tab; the webview moves to React |
| [0084](0084-knowledge-view.md) | Institutional knowledge gets its own view (procedures, past cases, candidates, markdown import, natural-language Search and Ask), shows no investigation state, and is curated inside each investigation by an include section; markdown with frontmatter is the import format until other formats are added |
| [0077](0077-act-line-and-author-name.md) | A chronicle act line is one line (ellipsized text, shrinking citation chips); the actor carries the identity provider's display name and an attestation names its author by it, keyed still by principal id |
| [0137](0137-answer-shape.md) | A findings report has one shape (headline, Findings, Not covered, Hypotheses, numbered Next steps) and containment ahead of the verdict is recommended only on a named observation of harm in progress; the stale `[knowledge]` sentence goes; O3 and A4 grade the two rules (catalogue v0.13) |
| [0152](0152-action-lineage.md) | Each action has at most one re-request and one live reversal, checked by the aggregate in the request transaction; the original card shows its successor in place of the button; a fresh request with no lineage is never limited |
| [0163](0163-recall-by-entity.md) | A case summary records its seed entity and every action target as `entity:` tags; an exact `prior_investigations` lookup answers "investigated this before?" from the seed alone; a recall emptied by an unknown filter tag reports `EMPTY_UNDER_FILTER` |
| [0164](0164-demo-fixture-fidelity.md) | Demo fixtures answer the account and window asked for (a null matcher alternative matches an absent param; the incident-day logons are time-scoped); both surfaced accounts can be contained; a failed action records the activity's own reason and the card says what it means for the target |
| [0123](0123-action-names.md) | Every action descriptor declares a human Name and a Category (containment, eradication, recovery, notification, case_record), pinned by the freeze test and served on action rows; surfaces title and group by them instead of guessing from the id |
| [0127](0127-edit-before-approve.md) | A REQUESTED action can be edited by a human before approval — same action, new parameters, `action.edited` with the prior parameters on the record, Gate 2 re-evaluated (never to auto-approve), targets fixed; `ticket.create` gains a vendor-neutral `priority` |
| [0161](0161-discard-draft.md) | A draft can be discarded by a human with a reason (draft→discarded, terminal, refused while a request awaits a decision); it leaves the work queue and never counts as a case's investigation; nothing is deleted |
| [0129](0129-knowledge-through-tools.md) | Knowledge reaches the agent only through its recall tools: no implicit retrieval, no injection dial, no include checkbox; the rail is a library (separate lists, search with Clear, import, "ask reckon to use this"); inspectors show only what the agent received |
| [0156](0156-sop-draft-step.md) | Concluding no longer drafts a candidate SOP; the concluded Summary offers "Draft SOP from this case" and shows every outcome (drafting, declined with reason and Draft anyway, failed, ready with Add/Discard on the panel); the automatic case summary shows whether its narrative was written |
| [0139](0139-attestation-reference.md) | reckon records references to artifacts, never the artifacts: an attestation may name where it happened (as written or as its SHA-256) and where its evidence is kept (system, location, access, optional content SHA-256), as STIX external references on the note; links carrying a credential are refused; no upload |
| [0245](0245-workbench-test-seam.md) | The workbench host is tested against a hand-written `vscode` fake that vitest resolves `vscode` to, typed against `@types/vscode` so drift fails `tsc`; panels and webviews are recorders the test drives; no editor in the test loop |
