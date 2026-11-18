# 0406 — The engine derives a target's identifier from the stored entity, by a rule the binding declares, and freezes the binding with it

Issue: #406 · PR: pending · Status: proposed

## Context

A target is a `TargetSpec` (`04 §3.1`): an `entity_ref`, the STIX id of the
thing acted on, and a `resolved_identifier`, the string the tool receives.
`04 §8.1` says the identifier is resolved at request time and frozen into
the action. The freeze keeps the approved string the sent string, keeps
telemetry from changing the target after approval, and puts the exact
string in the audit record.

`04 §8.1` doesn't say who resolves. In the code, no one does. The requester
writes `resolved_identifier`, and the engine passes it to the tool. Every
write binding sends `${target.resolved_identifier}` as it came. So a
request can name one host and send another host's name. Two runs can send
different strings for the same host, and the approver can't tell which one
the tool needs.

The requester can't know the right form, because tools for the same action
take different strings, and the binding is chosen at dispatch, after
approval. A request-time resolution can't know which binding it is
resolving for. `PlannedBinding` already runs dispatch's selection before
approval, to preview the adapter, but the choice isn't kept. Dispatch picks
again later.

Read bindings already name the entity field they need in their template
(`${entity.host.hostname}`). The caller supplies those values. That is right
for a read: it's often how an entity is first found, and a wrong value costs
a wrong answer, not a change in the world.

#354 makes every typed action's target carry an `entity_ref` of a declared
type. This decision builds on that.

Some tools need a vendor's own id, such as a Falcon device id. An entity
keeps vendor ids in `source_ids`, keyed by the source that returned them.
The read adapters label a source `<vendor>:<tool>`, so the key names the
read tool, and one vendor's id can sit under several keys. A tool only ever
takes its own vendor's id. No vendor's tool takes another vendor's id.

## Decision

- **A binding for an action with target types declares its identifier
  rule.** The binding gets `identifier`, a map from STIX type to a template
  over the stored entity's properties, under the root `target.entity`:

  ```yaml
  - action_type: ioc.block
    adapter: edr
    operation: add_ioc
    identifier:
      ipv4-addr: "${target.entity.value}"
      domain-name: "${target.entity.value}"
      file: "${target.entity.hashes.SHA-256}"
    params:
      value: "${target.resolved_identifier}"
  ```

  The rule names either a field that means the same thing everywhere, such
  as a hostname, a login, an indicator's value or a file's hash, or a
  vendor's own id.
- **A vendor's own id is read through `vendor_ids`.** A source label is
  `<vendor>:<tool>`, or a bare `<vendor>` for a source with one tool. When
  the engine loads a target entity, it adds `vendor_ids` beside its
  properties. For each vendor, that's the id when every id under that
  vendor's labels is the same. A vendor whose ids disagree is left out, and
  a refusal names the conflict. For a host, the ids are the ones its
  identity view has seen across every record naming it or its alias set
  (`SeenAs.Agents`), with contested aliases cut as identity cuts them.
  A stored object keeps only its first source's properties, so for other
  types the ids are the stored `source_ids`. A containment binding that
  takes a Falcon device id declares:

  ```yaml
  identifier:
    reckon-host: "${target.entity.vendor_ids.falcon}"
  ```

  The label convention becomes the contract for source labels.
- **Config load checks the rule.** A binding for a type with target types
  must have an `identifier`. Each key must be one of the action type's
  target types. A binding for a type with none (ticket, notify) must not
  have one. `target.entity` is valid only inside `identifier`, never in
  `params`, so dispatch never reads the entity.
- **The engine picks the binding at request time and derives the
  identifier.** After `BuildRequestCommand`, and before Gate 2, the engine
  loads each target's entity from the evidence by `entity_ref`. It walks
  the bindings by priority, with the same usable-and-scope rule as
  dispatch. A binding applies when it has a rule for every target's type,
  every rule renders, and its params render. The first that applies is
  chosen. Each target's `resolved_identifier` is set to its rendered rule.
  A value the requester sent is not used.
- **The binding is frozen with the identifier.** The action records the
  chosen binding's adapter and operation. Dispatch uses that binding only.
  If it is no longer configured, enabled or in scope, dispatch fails with
  nothing sent, as a missing binding fails today. The adapter preview and
  the dispatched record read the frozen binding.
- **No applicable binding means refusal.** If a target's entity isn't in
  the evidence, or no usable binding can render for it, the request is
  refused with a 422 that names the reason, such as the missing field.
  Nothing is recorded. Falling to a lower-priority binding here is not the
  write side's fall-through: nothing has been sent.
- **Every request derives afresh.** A re-request and a reversal copy
  targets, then derive their identifiers and pick their binding again. A
  reversal derives for the inverse type's bindings.
- **An edit keeps the frozen binding.** An edit changes parameters only. It
  is refused if the frozen binding's params no longer render with the new
  parameters.
- **A type with no target types is unchanged in what it sends.** Its
  `resolved_identifier`, a destination, is still the requester's. Its
  binding is frozen like any other.
- **The agent's action tools ask only for the entity.** The target schema
  of an entity action's tool drops `resolved_identifier`.

## Alternatives rejected

- **Re-picking the binding at dispatch and deriving there.** The adapter is
  always current, but the approver approves a host, not the string sent,
  and `04 §8.1`'s guarantees go.
- **Deriving for every usable binding and freezing them all.** Dispatch
  could then fall back without re-resolving, but the approver sees several
  candidate strings and the record holds strings that were never sent.
- **A per-type default rule** (a host's hostname, an account's login). An
  unstated default is the same guess this removes.
- **Letting `target.entity` into `params`.** Dispatch would render from the
  entity after approval, and the approved record would no longer hold what
  is sent.
- **Asking a model which stored field fits the tool.** It would infer from
  field names, give different answers on different runs, and read fields
  that can carry attacker-written text.
- **Naming a vendor id by its `source_ids` key.** The key names the read
  tool, so it changes with whichever tool saw the entity.
- **Tagging every stored id with an id space** (vendor, id kind, tenant).
  A tool only takes its own vendor's id, so the vendor is enough. Tenants
  are a later version's concern.
- **Asking the write adapter for the identifier at request time.** It needs
  a new adapter call, and a template in the binding says the same thing.
- **A template transform taking the vendor as an argument.** Transforms
  take no arguments today. A path under `vendor_ids` needs no syntax
  change.
- **Picking one when a vendor's entries disagree.** Any choice would be a
  guess about which source is right.
- **Following alias links to a strong entity's base fields.** A weak host
  has no hostname, and finding the strong one is identity resolution. A
  weak target is refused when its binding's rule names a field it doesn't
  have. Vendor ids already come through the alias set, because the host's
  identity view reads them that way.
- **Reading vendor ids from the stored host's `source_ids`.** It holds only
  the first source's id, so a Falcon id reported after another source saw
  the host would be missed.

## Consequences

- A request for an entity action names only the entity. The string sent is
  the engine's, derived the same way on every run, and the approver sees it.
- A target must be an entity in the evidence, so a request for something
  reckon has never seen is refused.
- Containment through the CrowdStrike response adapter takes its Falcon
  device id from the target host. A host no Falcon source reported is
  refused. So is a host Falcon reported under two device ids, as after a
  sensor reinstall.
- A new read adapter must label its sources `<vendor>:<tool>` or
  `<vendor>`, or its ids can't be named by a write binding.
- Two tenants of one vendor in one install aren't told apart. That is left
  to a later version.
- Changing the agent's tool schema moves the prompt version and the
  tool-set hash.
- `design/04 §3.1` and `§8.1`, and `design/08 §2`, `§4` and `§7.1`, gain the
  derivation, the identifier rule, `vendor_ids` and the frozen binding.
  `design/03` gains the source label contract.
