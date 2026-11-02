# 0092 — A capability call is an addressable record, and an event belongs to an investigation through the call that retrieved it

Issue: #92, #93 · PR: #297 · Status: accepted

## Context

A capability invocation persisted its events into `ocsf_events` and its
objects into `stix_objects` (03 §4.13, adr/0218), and nothing else. The row
said what the event was and when, not which investigation asked for it or
which call brought it in. The only link from an investigation to an event was
a prediction's citation, so the Story timeline could show the events the agent
chose to cite and no others, the inspector's "around it" strip had nothing to
read, and an entity's events in an investigation could not be listed
(`design/ui/00-backend-binding.md §6` item 13). The call itself left no
record: a committed turn kept the model's `tool_use` id, the tool name and
the arguments, but not which binding answered, with what parameters after
templating, with what coverage, or what it produced (item 20). The contracts
in `implementation/workbench-react.md §8.2` and §8.9 had already fixed the
tables, the paths and the JSON; what remained was how membership is passed in,
what a call outside an investigation is, how the turn's record picks up the
id, and the events that already exist.

## Decision

- **The call is the unit of membership.** `capability_calls` holds one row
  per invocation (verb, every binding the resolver walked with the parameters
  as rendered and whether it answered or why it was skipped, the first that
  answered as the summary columns, coverage, bindings answered and applicable,
  the absence object, the degradation notes, start and duration),
  `call_events` joins it to every event it returned, and
  `observed_data_events` is the derived-from edge from each observed-data to
  the event it was normalized from, persisted. The `verb` parameter the
  resolver injects for the adapter is not stored as a parameter; it is the
  call's verb. An event is a member of an investigation when the call that
  retrieved it names the investigation. `evidence.Store.Persist` writes all of
  it in the transaction that writes the events, so no event is left without
  its membership.
- **Membership rides the invoke body.** `POST /api/capability/{verb}` takes
  `investigation_ref`; the agent loop sends its session's investigation and
  the workbench its document's. A call made for no investigation (the case
  seed's read, the workbench's own checks) is recorded with a null investigation:
  it keeps its id, so an id in an envelope is never a dangling one, and it is
  listed under none.
- **The engine mints the id** before the resolver runs, returns it in the
  envelope as `call_id`, and the sidecar carries it on the live tool-result
  note. The turn's tool-call record gains `capability_call_id`, on the wire
  and on `ai_tool_calls`; `call_id` there stays the model's `tool_use` id,
  so the two never collide. The agent stamps it only from a capability verb's
  envelope and only when it parses as an id, and the aggregate accepts it only
  as an id, so a stray `call_id` on another tool's payload never fails the
  commit.
- **The reads live on `evidence.Store`**, behind the server's `evidenceStore`
  interface: `GET /investigations/{id}/events` in event time, one row per
  event under the earliest call that retrieved it, oldest or newest first
  (`order`) and paged by a `(time, id)` cursor (`cursor`, from the previous
  page's `next_cursor`), filtered by `entity` (through the observed-data that
  name it), by `category` (the thousands digit of `class_uid`: process 1,
  detection 2, auth 3, network 4), or `around` one event (`limit` on each side,
  on the anchor's host entities, else its entities; an anchor that is not a
  member of the investigation is a 404, and one naming no entity has an empty
  window), with the unpaged `total`; `GET .../calls`; and
  `GET .../calls/{call_id}` with what the call produced and every binding
  configured for the verb, the tenant config file being the authority so an
  installed-but-disabled adapter's binding is listed too. Payloads are not
  served here; `GET /api/evidence/{ref}` does that.
- **The resolver records what each binding did** (`CapabilityResult.Bindings`:
  adapter, operation, priority, the rendered parameters, answered or the
  reason skipped) and sets the event id on each normalization result, so the
  store writes the edge the normalizer always implied.
- **The workbench** lists the newest page of retrieved events on the Story
  timeline with the record's total, each naming the call that retrieved it,
  with the cited refs that carry an event time joined in; an opened event
  shows the retrieved events around it and the call under Provenance; a
  Capability call inspector (Call · Result · Raw) opens from a timeline row, a
  tool row and the strip, its bindings badged from the stored outcomes. On a
  record without membership the cited events alone remain; a failed read says
  so, since an empty result means the read succeeded and held nothing.
- **No backfill.** Events persisted before the migration have no call and no
  membership; they keep appearing through citations only.
- `EventRow` carries no `view` and no `relationships`: the per-class sentence,
  the curated fields and the attack path's relations are #90's (§8.1), and
  the rows gain them there.

## Alternatives rejected

- **Membership as a column on `ocsf_events`.** Records the investigation but
  not the call; the call would have been a second thing to invent for #93, and
  an event retrieved twice would need two rows or a list.
- **Infer membership from citations for existing events.** A guess presented
  as a record; stating the gap is cheaper and honest.
- **Reuse `call_id` for the engine's id on the tool-call record.** The model's
  id already lives under that name on the interpretation event and the wire.
- **Skip the row for a call outside an investigation.** An envelope would then
  carry an id that resolves nowhere.
- **A separate entity-membership table.** The observed-data already name the
  entities an event involves; the persisted derived-from edge reaches them
  with no second source of truth.

## Consequences

- The Story lens shows the whole retrieved record, so the context that makes a
  cited event unusual is visible without the agent citing it.
- `Persist` takes the call; a caller with no investigation passes one with a
  null investigation. The invoke handler gains a 400 for a malformed
  `investigation_ref`.
- The tables grow with every call; nothing prunes them yet, as nothing prunes
  `ocsf_events`.
- Server tests that truncate `ocsf_events` truncate `call_events`,
  `observed_data_events` and `capability_calls` first.
