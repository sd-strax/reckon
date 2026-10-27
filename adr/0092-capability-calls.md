# 0092 — A capability call is an addressable record that joins its events to the investigation at ingest; the investigation's events are a read over those joins

Issue: #92 (also #93) · PR: #94 · Status: accepted

## Context

Two gaps in `design/ui/00-backend-binding.md §6` shared a cause. Gap 13: the Story timeline and
the evidence inspector's "around it" need the events retrieved for one investigation, by entity
and in event time, but an event was persisted with no record of which investigation retrieved
it, so the only events a surface could show were the ones a prediction happened to cite. Gap 20:
a committed turn records its tool calls, but a capability invocation left no addressable record,
so the timeline, the observed-data bundle and the turn's tool-call rows could not open the call
that produced their evidence, and nothing said which binding answered, with what parameters,
how long it took, or what the other bindings for the verb were.

Constraints: the read surface is pure I/O and normalization (`03`), so membership must be a fact
the invoke endpoint records, never something inferred from citations later; "every citation
opens" (`design/ui/README` 8) extends to the call; a read over persisted tables, never a new
capability call (`03 §6.3`); the normalizer's derived-from edge (observed-data → event) was
computed but not persisted.

## Decision

- **The unit of membership is the capability call.** `CapabilityInvokeBody` gains
  `investigation_ref`; the invoke endpoint mints a `call_id` and writes, in the same transaction
  as the telemetry, `capability_calls` (verb, the binding that answered, the parameters sent
  exactly as templated, coverage, bindings answered of total, the null-observation ref, start,
  duration, degradation notes, every binding attempt with its outcome), `call_events` (every
  event the call returned) and `observed_data_events` (the derived-from edge, persisted). The
  envelope returns `call_id`. A call with no `investigation_ref` is recorded but joins no
  investigation and is listed nowhere: the case picker's and the seed's reads stay out of the
  record.
- **The resolver records what it did.** `CapabilityResult` carries `Attempts` (per binding:
  parameters, answered, note), `BindingsAnswered`, `BindingsTotal`, `StartedAt`, `Duration`;
  `Resolver.VerbBindings(verb)` lists a verb's configured bindings with presence and health.
- **The turn's tool-call row names its call.** The agent's `InvokeInput` carries the
  investigation; the session reads `call_id` from the envelope into the tool log
  (`capability_call_id`, distinct from the model's `call_id` tool-use id), the side store
  persists it on `ai_tool_calls`, and the sidecar's tool-result notice carries it to the
  surface, so a live or reconstructed tool row opens its call.
- **`GET /api/investigations/{id}/events`** lists the events the investigation's calls
  retrieved, ordered by event time, each with its call, verb, coverage, per-class view
  (`adr/0090`), observed-data refs, entity refs and the normalizer relationships among those
  entities; `?entity=` keeps events whose observed-data cite the ref, `?category=` filters by
  the view's category, `?around=<event>&limit=` returns the window on the same host entity.
  **`GET …/calls`** and **`…/calls/{call_id}`** list and open calls; the detail carries what
  was sent, what came back, what it produced (as event rows), the verb's other bindings and
  the installed-but-disabled adapters that could serve it. `GET /api/evidence/{ref}` on an
  event carries `call_id`.
- **In the document**: the snapshot carries `events`; the Story timeline rows are the
  investigation's events (the cited-refs strip stands in when the read fails); the attack path
  is derived from the events cited by CONFIRMED predictions of SUPPORTED hypotheses (impl
  §7.6); the evidence inspector shows "Produced by" and "Around it" from the snapshot; the
  Call inspector (Call · Result · Raw) is the bundle view, grouped by source with repetitions
  collapsed; a tool row's "open call" opens it.

## Alternatives rejected

- **Inferring membership from citations** (events a prediction cites): the timeline would show
  only what the reasoning already named, which is the gap, not its answer.
- **Membership on the event row** (`ocsf_events.aggregate_id`): the same event retrieved by two
  investigations, or twice in one, has one row; the call is the thing that happened per
  investigation, so it is the join.
- **Deriving "around it" by a second capability call**: an invocation is outbound I/O with a
  record of its own; a neighbourhood is a read over what was already retrieved.
- **One id for the model's tool use and the capability call**: they are different things (a
  tool use may be an intrinsic tool; a call may be made outside a turn); both are kept, named
  apart.

## Consequences

- Migration `0025`; events persisted before it have no call and list nowhere until retrieved
  again, which the timeline says.
- Gap 14 (baselines) has its row shape: a baseline is a collapsed run of event rows.
- The eval baselines are expected to stand: the agent's envelope gains a field and the call
  carries the investigation; no prompt change.
