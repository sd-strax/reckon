# 0364 — A citation names an observation, and a raw event is reached through the observation made from it

Issue: #364 · PR: pending · Status: accepted

## Context

Evidence has two layers (`design/01-domain-model.md`): the raw OCSF event a
source returned, and the observation (`observed-data`) reckon makes from it,
which names the entities the event is about. The domain model says evidence is
an observation.

Yet a reasoning record could cite a raw event directly: a prediction's
`test_result_refs`, a pin's `input_refs` and an action's `evidence_refs` all
accepted a bare event id. The prompt rule `cite-evidence` listed
`ocsf_event_refs` among the refs to cite, adr/0348 marked a timeline row cited
by its raw records, and `design/ui/README.md` rule 8 named a raw event as a
cited object.

Two things followed:

- **An event the engine could not read could ground a claim.** A tool result
  carries an event's ref before the event is normalized. When normalizing fails,
  the event has a ref and no observation, and it could still be cited.
- **The same evidence could be cited two ways.** Every reader of citations had
  to match both the event and its observation.

The stored record links each observation to its event (`observed_data_events`,
the `derived-from` edge). Reading an event returned its observations, but
reading an observation did not return its event, so a cited observation gave no
way to open the raw record.

STIX validity is not at stake: adr/0357 keeps raw event ids in their own
property.

## Decision

- **A new citation names a STIX object.** A bare event id is refused in
  `test_result_refs`, a pin's `input_refs` and an action's `evidence_refs`, for
  every requester. The refusal names the observations made from that event, or
  says the event has none because it could not be read.
- **An observation leads to its raw event.** Reading an observation returns the
  ids of the events it was made from (`source_event_ids`), and the observation
  inspector opens each one. UI rule 8 holds through the observation.
- **The timeline marks a row cited** when a citation names one of the row's
  observations.
- **The prompt rule** tells the agent to cite observations and entities, and that
  an event is cited through the observation made from it. Tool results still
  carry `ocsf_event_refs`.
- **Recorded citations are not rewritten.** Older records that cite raw event
  ids keep them, and every reader still renders them.

## Alternatives rejected

- **Keep accepting raw events and refuse only the unreadable ones.** It closes the
  first problem and keeps two ways to cite the same evidence.
- **Translate a cited event id into its observation on write.** The record would
  hold a citation nobody made. An event can also have more than one
  observation, and the engine would have to pick one.
- **Rewrite older records to cite observations.** The record is append-only. Old
  citations stay as they were made.

## Consequences

- An event reckon could not read can't ground a claim.
- New code that reads citations deals with STIX ids only. Code that reads older
  records still handles raw ids.
- The analyst reaches a raw event from any cited observation in one click.
- The prompt version moves, so the eval baselines need a rerun.
