# Write-Side Adapter Contract — Spec

## 0. Framing

This spec defines the **write-side adapter contract**: the I/O surface by which an authorized
`reckon-action` is dispatched to the outside world and its result is recorded. It is the symmetric twin of
the read-side capability contract in `03-capability-layer.md`. Where `03` covers query verbs (binding
→ resolver → adapter → normalizer → `CapabilityResult`), this spec covers action types (binding →
resolver → adapter → `WriteResult`). The architecture is deliberately the same shape; the differences
— idempotency, no normalizer, asynchronous SOAR callbacks — are called out where they occur and
justified.

The action **domain model is already specified elsewhere and is authoritative**; this spec does not
restate it. It owns only the adapter-facing contract.

| Owned here (`08`) | Owned elsewhere (authoritative) |
|---|---|
| The `request_action` request and the agent's action tools | Authorization, trust tiers, CEL policy (`04`) |
| `ActionDescriptor`, action bindings, the action resolver | The `reckon-action` primitive, lifecycle, `Execution` record (`01`, `04 §3`, `04 §6.1`) |
| The write adapter operation signature + idempotency model | Action lifecycle events + payloads (`02 §3`) |
| The `WriteResult` envelope | Temporal dispatch topology, manifest/registry (`05 §6.2`, `05 §11`) |
| Action fixtures | The action-type taxonomy (`04 §2`, `07 §7`, `07 §8`) |

### Out of scope

- **Authorization.** Whether an action *may* dispatch is settled by the time this contract engages.
  This spec begins at the `APPROVED → EXECUTING` transition.
- **The action-type catalogue.** Tiers, reversibility, and D3FEND mappings live in `04 §2`/`§2.1`
  and `07`. This spec defines the *carrier* (`ActionDescriptor`), not the catalogue.
- **Vendor-specific adapter internals.** As on the read side, transport is an adapter concern.
- **Credential resolution.** Adapters receive a `credentials_ref`; resolution is the uniform scheme
  in `05 §10.2`.

---

## 1. The shape of the write path

Symmetric to the read pipeline (`03 §3`), with the resolver embedded in the `ActionLifecycle`
Temporal workflow (`05 §6.2`) rather than a synchronous request:

```
request_action (LLM/analyst)            <- §2
  -> pick the binding, derive identifiers, freeze both   <- §4 (adr/0406)
  -> reckon-action REQUESTED                 (04 §3.2; authorization in 04)
  -> ... APPROVED ...                   (04 §5)
  -> ActionLifecycle workflow           (05 §6.2)
       use the frozen action binding    <- §4, §7.1
       build idempotency key            <- §6
       dispatch-ledger guard            <- §6
       adapter.dispatch(op, params, idempotency_key) -> WriteResult   <- §5, §5-envelope
       classify outcome                 <- §5
       emit ActionDispatched / ActionResulted (02 §3)
```

The principle from `05` commitment #5 holds: adding a remediation tool is "register an adapter, add
bindings, declare an action descriptor." No structural change.

---

## 2. The `request_action` request and the agent's action tools

`request_action` is the request that proposes a state-changing action: the body of
`POST /api/actions`, the same for the analyst and the AI. Its canonical schema lives here (it is a
capability-surface object, symmetric to the read verbs in `03 §5.3`); `04 §5.6` references it and
owns only what happens to the request inside the authorization machine.

```
request_action(
  action_type        string            -- from the ActionDescriptor catalogue (§3)
  targets            list<TargetSpec>  -- the things acted on (04 §3.1, §8.1)
  parameters         object            -- action-type-specific inputs (validated vs §3 input schema)
  evidence_refs      list<EvidenceRef> -- STIX ids justifying the action (01); an event is cited
                                          through its observation, and a bare event id is refused (adr/0364)
  rationale          string            -- why this action should happen
  investigation_ref  grouping--<uuid>  -- the owning investigation aggregate
)
```

**The agent reaches it through one tool per action type** (adr/0380), as it reaches the read side
through one tool per verb (`03 §6.3`). The model never writes `request_action` itself.

- **One tool per action type.** The agent loop builds a tool for each action type in the
  session's frozen catalog. An unavailable type's tool says it cannot be carried out in this install.
  `POST /api/actions` refuses a request for a type with no usable binding, live, before anything is
  recorded (422); the re-request and reversal paths do the same, after a reversal's own checks. A
  usable binding has a configured, enabled adapter and a matching scope; health does not count, so a
  degraded type is accepted. The catalog, which has no request scope, marks a type unavailable when no binding has a configured, enabled adapter.
  The tool is named `request_` followed by the action type with each `.` written as `_`
  (`ticket.create` → `request_ticket_create`). Tool building fails when two names collide.
- **Inputs at the top level.** A tool's properties are its type's non-entity inputs (`§3`), with the
  inputs the descriptor requires as its required keys. Every tool also carries the fields every
  request shares: `targets`, `rationale`, `evidence_refs` (required) and `retry_of` (optional).
  There is no nested `parameters` object in the tool.
- **A target is the entity.** For a type with target types, a target in the tool's schema is its
  `entity_ref` only. The engine derives the string the tool receives (`§4`, adr/0406), so the model
  is never asked for it. The loop passes no `resolved_identifier` on for such a type. For a type
  with none (ticket, notify), a target also carries `resolved_identifier`, the destination only
  the requester can name.
- **The loop builds the request.** Every action tool runs one handler. It takes `action_type` from
  the tool, puts every key that is not a shared field into `parameters`, drops a `null` value for a
  declared optional input, and sends `request_action`. The engine validates it against `§3` as for
  any caller, so an undeclared key is refused by name.
- **The description comes from the descriptor.** It starts with the action type the tool requests
  (`Action type: ticket.create.`), so the model can match the type id it reads in the state rows,
  `list_actions` and the engine's refusals to the tool that names it (adr/0386). Then it holds the
  descriptor's `Intent`, tier and reversibility, "degraded" when that is the type's status, and
  the guidance every request shares: the request goes through authorization, the agent proposes and
  never approves, and it must cite evidence. An unavailable type's description says it cannot be
  carried out in this install and leaves the guidance out.
- **No catalog, no action tools.** With the action layer off, the catalog and `POST /api/actions`
  both answer 503, so the session offers no action tools. The loop tries the fetch up to three
  times before it reads a 503 that way. Any other error that survives the retries fails the session
  start.
- **The eval record keeps the tool list.** The session exposes the tools it built, each with its
  kind and, for an action tool, its action type. The eval's trial record keeps that list, and the
  graders look each call up in it, never at the tool name's prefix (`design/10 §3`).
- **Plain JSON Schema.** The tool definitions use no provider-specific schema features and no
  strict decoding. A malformed call is refused by the engine's validation, and the model sends it
  again.

**The AI/analyst boundary.** The AI may *make* a `request_action`, through its action tools, but it can never construct an
`Authorization` record or set `status` — those fields are write-protected at the aggregate command
handler (`04 §5.6`, enforced per `02-persistence.md §2.1`/`§4`). Making the request produces a `reckon-action` in
`REQUESTED` and its producing `action-request` Interpretation, in one transaction. Everything between
`REQUESTED` and `APPROVED` is `04`'s; this contract resumes at dispatch.

`TargetSpec` (defined in `04 §3.1`) carries both `entity_ref` (stable STIX id) and
`resolved_identifier` (what the adapter actually sends downstream). Resolution is frozen into the
`reckon-action` at request time (`04 §8.1`); the write adapter consumes `resolved_identifier`, never
re-resolves. For a typed action the engine derives it from the stored entity (`§4`), and a value
the requester sent is not used.

---

## 3. `ActionDescriptor`

The write analog of `CapabilityDescriptor` (`03 §5.3`). It declares the action surface the LLM sees
and the bindings target.

```go
type ActionDescriptor interface {
    ActionType() string          // e.g. "host.isolate", "ticket.create"
    Inputs() InputSchema         // schema for request_action.parameters
    Intent() string              // LLM-facing description of effect + when to use
    TargetTypes() []string       // STIX types the action acts on (04 §3.1); empty for a
                                 // destination action (ticket, notify)
    // Auth/taxonomy fields below are DECLARED here but OWNED elsewhere:
    DefaultTier() Tier           // T2|T3 — authoritative in 04 §2; see tier/surface
                                 // mapping below (there is no T1 descriptor)
    Reversibility() Reversal     // reversible / best_effort / irreversible — 04 §7 (the
                                 // classification gates the REVERSED claim, 04 §7.1)
    D3FEND() string              // optional technique id — 04 §2.1
}
```

**Why `T2|T3` and not `T1|T2|T3`: the tiers map one-to-one onto architectural surfaces.** T0 = a
capability read verb (`03`); T1 = interpretation-layer CRUD, recorded as Interpretations with **no
descriptor** (`01`/`02`); T2/T3 = an `ActionDescriptor` + write adapter (this spec + `04`). A "T1
descriptor" is a category error — and, since `DefaultTier` is declared by adapter authors (§8), it
would be a tier-escape hatch: an external write registered below Gate 2 entirely. Frictionless
dispatch comes from an auto-approve policy (`04 §4`), never from tier-escape; the enum makes that
structural at the contract, matching the aggregate's own `T2|T3` validation.

`DefaultTier`, `Reversibility`, and `D3FEND` are surfaced on the descriptor (and in the adapter
manifest, `05 §11`) for discoverability, but `04`/`05` remain their single source of truth — `08`
must not diverge from `04 §2`'s categorization table. An action type appears in `list_action_types`
once its descriptor is registered and at least one binding exists (mirroring `03 §5.1`).

---

## 4. Action binding model

Symmetric to the read binding (`03 §3`). A binding maps an `action_type` to a concrete adapter
operation in tenant config:

```
ActionBinding:
  action_type   string          -- the descriptor this fulfills
  adapter       string          -- adapter name (e.g. "crowdstrike_falcon", "tines_playbook")
  operation     string          -- concrete write op on that adapter (e.g. "rtr.isolate_host")
  priority      int             -- highest applicable wins (as read side)
  identifier    map             -- STIX type -> template over target.entity (adr/0406);
                                   required for a type with target types, absent otherwise
  params        object          -- templated parameter mapping (request params + target -> op inputs)
  external_approval_substitutes_reckon  bool  -- SOAR gate ownership (§7; default false)
```

**The identifier rule** (adr/0406). A binding for an action type with target types declares
`identifier`: for each STIX type it handles, a template that says how the string the tool receives
is derived from the stored entity. The templates use the read side's engine (`03 §3.3`) under one
root, `target.entity`, which holds the stored entity's properties.

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

A rule names a field that means the same thing everywhere, such as a hostname, a login, an
indicator's value or a file's hash. Or it names a vendor's own id, through `vendor_ids`.

**`vendor_ids`.** When the engine loads a target entity, it adds `vendor_ids` beside its properties:
a map from vendor to that vendor's id for the entity. The vendor is a source label up to its first
`:` (the source label contract, `03 §5.5`). A host's ids are the device `uid` of each device object
naming the host or its alias set, across every record, with contested aliases cut. A device's
`uid_alt` and its agents' ids are other ids, not vendor ids, so they are not read. A stored object
keeps only its first source's properties, so a host's own `source_ids` would miss a later source. Any other entity's ids are its stored `source_ids`. A vendor whose ids all agree maps to
that id. A vendor whose ids disagree, as after a sensor reinstall, is left out, and a refusal names
the conflict. A tool only takes its own vendor's id, so the vendor is enough to find it. Telling
apart two tenants of one vendor is left to a later version. A containment binding that takes a Falcon
device id declares:

```yaml
identifier:
  reckon-host: "${target.entity.vendor_ids.falcon}"
```

**Config load checks the rule.** A binding for a type with target types must have an `identifier`.
Each key must be one of the type's target types. A binding for a type with none (ticket, notify)
must not have one. `target.entity` is valid only inside `identifier`, and an identifier reads nothing
else. So `params` never reads the entity, and dispatch renders only from the frozen request. An
identifier rule names at least one field, so no two targets get the same literal. It takes no optional marker (`?`), no default (`??`) and no transform that does not give
text; only `lower`, `trim` and `upper` are allowed. So a rule either renders every field it names, or
the binding gives way. Every binding needs an adapter and an operation. No two bindings of one action
type may name the same adapter and operation, because that pair is how dispatch finds the frozen
binding again. Everything dispatch would refuse for a static reason is refused here, never after
approval.

**The binding is chosen at request time** (adr/0406). When an action is requested, the engine loads
each target's entity from the evidence. It walks the usable bindings by priority: adapter configured
and enabled, scope matching (below). A binding applies when it has a rule for every target's type,
every rule renders from that target's entity, and its params render. The first that applies is
chosen. Each target's `resolved_identifier` is set to its rendered rule, and the binding's adapter and
operation are frozen on `ActionRequested`. A binding that does not apply gives way to the next. That
is not the dispatch fall-through below, because nothing has been sent. If none applies, or a target's
entity is not in the evidence, the request is refused with a 422 that names the reason, and nothing
is recorded. If the install has no write bindings, or a typed action has no evidence store to read its
targets from, the request is refused with a 503, and nothing is recorded. A ticket or notify action keeps the requester's destination, and its binding is chosen
and frozen the same way. A binding is identified by its action type, adapter instance and operation;
priority is not part of it.

**The action resolver** (the dispatch-step analog of the read resolver, `03 §3.2`): given
`(action_type, reckon-action, tenant)`, take the binding frozen on the action, check it is still
configured, enabled and in scope, render its parameter template against the frozen `TargetSpec`s and
`parameters`, and hand off to the adapter operation. It never selects another binding. If the frozen
one can no longer be used, dispatch fails with nothing sent, as a missing binding does.

Unlike the read resolver, the action resolver does **not** fall through to a lower-priority binding on
a failed call — a partially-executed state change must not be silently re-attempted against a
different tool. Binding selection is resolved once, at request time; retries (`§6`) re-use the same
binding.

**Source scope is a write-side precondition** (`03 §3.5`): a binding whose adapter instance is
scoped to a different organization than the investigation's `source_scope` is *not applicable* —
filtered before selection, never merely deprioritized. A scope mismatch across every binding means
**no binding**: the action type reports unavailable for that investigation and nothing is
dispatched. Dispatching an action against the wrong organization's tooling is the failure this rule
exists to make structurally impossible; no-fall-through and scope filtering together guarantee the
one selected binding is in the right organization or does not exist.

---

## 5. Write adapter operation contract

Write operations extend the same `Adapter` interface as the read side (`03 §5.3`); an adapter may
serve read operations, write operations, or both. The write operation differs from `Invoke` in two
ways: it takes a mandatory `idempotency_key`, and it returns a `WriteResult` rather than an
OCSF-shaped response (there is no normalizer on the write path — a write produces an *outcome*, not
an observation).

```go
type WriteAdapter interface {
    // ... Name(), Class(), Health() as in 03 §5.3 ...
    SupportedActionOps() []string
    Dispatch(ctx context.Context,
             operation string,
             params map[string]any,
             idempotencyKey string) (WriteResult, error)
}
```

Adapters remain out-of-process JSON-RPC binaries over stdio (the shape `generate-adapter-scaffold`
produces); a write op is one more JSON-RPC method whose params include `idempotency_key` and whose
result is the `WriteResult` envelope below.

### The `WriteResult` envelope

The write analog of `CapabilityResult` (`03 §2`). Its fields map field-for-field onto the `Execution`
record (`04 §6.1`) and the `ActionResulted` payload (`02 §3`) so the contract and the event log agree
by construction:

```
WriteResult {
  final_outcome      SUCCEEDED | FAILED | PARTIAL | TIMEOUT   -- = Execution.final_outcome
  per_target_results map<target_index, OK | FAIL | UNKNOWN>   -- = Execution.per_target_results
  adapter_request_id string         -- correlation id (see §6 / 05 §6.2)
  error_class        RETRYABLE_ERROR | FATAL_ERROR | null  -- matches 04 §6.1 Attempt.outcome; null on success
  error_detail       string?        -- human-readable
  audit_depth        FULL | EXTERNAL -- FULL for DIRECT, EXTERNAL for SOAR_PLAYBOOK (§7; 04 §6.1)
  raw_response_ref   ref?            -- optional pointer to the stored adapter response
}
```

**Error classification** reuses the read-side taxonomy (`03 §6.2`) collapsed to what the executor
acts on: `RETRYABLE_ERROR` (rate limit, transient 5xx, network blip → retry within the budget, `§6`) and
`FATAL_ERROR` (auth, malformed request, target-not-found → no retry, `FAILED`). This matches the
categories in `04 §6.1`/`§6.2`.

**`UNKNOWN` per-target** is a first-class result, not an error: it means the call left reckon but the
adapter cannot confirm the per-target effect. It never coerces to `OK` — see the idempotency residual
in `§6`.

### The optional `verify` operation (reversal verification, 04 §7.3)

An adapter MAY additionally implement a verification operation for reversals:

```go
    // Verify reports whether the effect of a previously-dispatched operation is
    // observably GONE from the tool's vantage point — the evidence that lets a
    // BEST_EFFORT reversal earn the REVERSED claim (04 §7.3). Same result
    // vocabulary as Dispatch: OK = verified gone, UNKNOWN = removed what this
    // tool governs but cannot confirm the rest (propagation, partner feeds),
    // FAIL = still in effect.
    Verify(ctx context.Context,
           operation string,
           params map[string]any) (WriteResult, error)
```

Semantics per `04 §7.3` (state ownership): for a `REVERSIBLE` action the tool owns the effect state,
so `Dispatch` success on the inverse *is* the verification and `Verify` is unnecessary. For a
`BEST_EFFORT` action, `Dispatch` success on the inverse only records an *attempt*
(`action.reversal_attempted`, `02 §3`); a `Verify` returning all-targets-`OK` upgrades the outcome to
the verified undo (`action.reversed`). An adapter that does not implement `Verify` simply never
upgrades — the conservative default. The claim is earned per-dispatch by evidence, never granted by
configuration. Interface specified at v0; concrete implementations land with the real vendor write
adapters (Phase F). The fixture write adapter may declare verification verdicts in fixture files to
exercise the path.

---

## 6. Idempotency model

State-changing actions dispatch inside the `ActionLifecycle` Temporal workflow (`adapter_request_id`
= the Temporal workflow id, `05 §6.2`). The hazard the read side never has: a workflow
replay/crash/retry must not double-execute a real-world effect (double-isolate a host, double-file a
ticket). The model is layered, and honest about the one case nobody can resolve.

**(a) Mandatory idempotency key.** Every write op receives a `idempotency_key` that is **stable and
deterministic** — derived from `f(reckon-action.id, target)` — so the same logical action against the same
target always presents the same key. Adapters declare `honors_idempotency: true|false` in their
manifest (`05 §11`); for `true` adapters the downstream vendor dedups on the key (the standard
`Idempotency-Key` pattern). The key is the second line of defence: it protects the case where reckon
*does* re-issue after an ambiguous failure.

**(b) reckon dispatch-ledger guard (primary).** Before any outbound call, the workflow checks whether
an `ActionDispatched` event already exists for this `(reckon-action, attempt)`. Because the `reckon-action`
lifecycle is event-sourced (`02 §3`) and Temporal execution is deterministic on replay, a crashed or
replayed workflow sees the prior `ActionDispatched` and **short-circuits re-issue** rather than
calling the adapter again. reckon owns this guarantee from its own event log — it does not depend on
the vendor honouring the key. (a) and (b) compose: the ledger prevents re-issue in the common
crash/replay case; the key catches the residual races (a) can't see.

**(c) The honest residual.** If the call left reckon but no result returned (network partition after
send, adapter died mid-call), the per-target result is `UNKNOWN` and the action terminates `FAILED`
with `confidence = LOW` and **no success inference** — the analyst re-requests. If the run itself
stopped after recording the dispatch and before any result, the next run of the workflow finds the
dispatch with no result and records `OUTCOME_UNKNOWN` instead of calling again; a person checks the
tool and resolves it (`04 §6.2`). This is the same non-inference rule `04 §6.2` already applies to
`TIMEOUT`: reckon never pretends to know an effect it cannot confirm. Pre-dispatch state
reconciliation (a `probe(target)` op that lets the executor skip a call when the target is already in
the desired state) would shrink this residual but is **not** required by this contract — it is noted
as a possible per-adapter capability in `§9`. When the
first attempt fails before the call, nothing was sent, so the action ends `FAILED` with every target
`FAIL`, `attempts` 0 and the reason (`04 §6.2`, adr/0384). A later attempt that fails before the call
is retried as any other failure, because an earlier attempt may have reached the tool.

---

## 7. Dispatch sequence and SOAR delegation

### 7.1 Direct (`DIRECT`) dispatch

Inside `ActionLifecycle` (`05 §6.2`), on `APPROVED`:

1. Take the binding frozen at request time (`§4`, adr/0406). The dispatched record names its
   adapter. If it is no longer configured, enabled or in scope, the action fails with nothing sent.
2. Build the idempotency key (`§6a`); run the dispatch-ledger guard (`§6b`).
3. `Dispatch(op, params, idempotency_key)`; apply the retry budget — default **3 attempts, 2s/8s/30s
   backoff** (`04 §6.1`, `§6.3`); each attempt is an `Execution.attempts` row.
4. Classify the `WriteResult`; emit `ActionDispatched` then `ActionResulted` (`02 §3`) with workflow
   context as the principal carrier (`05 §6.2`).

**Partial success across targets** terminates `SUCCEEDED` with `final_outcome = PARTIAL`; failed
targets are **not** auto-re-dispatched — recovery requires a new `request_action`, so every action
stays independently auditable (`04 §6.2`).

### 7.2 SOAR-delegated (`SOAR_PLAYBOOK`) dispatch

When the binding sets `external_approval_substitutes_reckon: true` (`04 §5.7`), the action is handed
to an external orchestrator (Tines/Torq/customer-authored) that owns the approval gate and the
execution. The contract differences:

- `Authorization.mode = EXTERNAL_DELEGATED`; `primary_approver_ref` is populated from the SOAR's
  **async callback** once the orchestrator confirms approval (`04 §5.7`).
- `audit_depth = EXTERNAL` on the `WriteResult`/`Execution` — reckon records the call to the
  orchestrator, the playbook id, and the final outcome, but not the orchestrator's internal steps;
  reviewers needing the full chain pull the SOAR's own logs (`04 §6.1`).
- `adapter_request_id` correlates the reckon-side workflow to the orchestrator run id returned on the
  callback.

Idempotency for SOAR dispatch still applies at (a)/(b): the key travels to the orchestrator (which
may or may not honour it) and the ledger guard prevents reckon re-issuing the playbook trigger.

---

## 8. Extension mechanism

Symmetric to `03 §5`.

**Add a write action type (three artifacts):**
1. An `ActionDescriptor` (`§3`).
2. At least one `ActionBinding` mapping it to an adapter operation (`§4`).
3. The action type listed in the serving adapter's manifest `action_types` (`05 §11`).

**Add a write adapter (four artifacts):**
1. A `WriteAdapter` implementation exposing named write operations (`§5`).
2. Adapter config schema (YAML), including `honors_idempotency` (`§6`).
3. Manifest `action_types` declarations (`05 §11`).
4. Bindings for the action types it fulfils.

No normalizer step exists on the write side (a write yields an outcome, not an observation) — this is
the one artifact the read-side extension list (`03 §5.2`) has that this one drops.

---

## 9. Action fixtures

The write analog of `03 §9`. A **fixture write adapter** (`adapterClass = FIXTURE`) lets v0 dispatch
exercise the full lifecycle — request → authorize → dispatch → `ActionResulted` → reversal — without
touching a real tool. A fixture declares the outcome it returns:

```json
{
  "fixture_meta": {
    "scenario": "contain-lateral-movement",
    "matches": { "action_type": "host.isolate", "params": { "resolved_identifier": "WIN-A14" } },
    "delay_ms": 200
  },
  "result": {
    "final_outcome": "SUCCEEDED",
    "per_target_results": { "0": "OK" },
    "audit_depth": "FULL"
  }
}
```

Fixtures store **outcomes**, not vendor responses (mirroring the read side's "store OCSF, not raw
vendor JSON" rationale, `03 §9.1`): this keeps fixtures exercising the dispatch/lifecycle/audit
machinery rather than a specific adapter. Fixtures can declare `PARTIAL`, `FAILED`, `TIMEOUT`, and
`UNKNOWN` results to drive the failure-path and reversal tests. As on the read side, a tenant may mix
fixture and real write bindings per-binding (`03 §9.3`). A fixture write adapter that also exposes a
`probe` op can exercise the optional reconciliation path (`§6c`).

---

## 10. Open questions / deferred to v1+

- **Composite / atomic multi-effect actions.** Whether a single `reckon-action` may bundle multiple
  effects (atomic in the audit and authorization sense), or multi-step stays the agent loop's job
  with per-action gates. v0 defers to the agent loop (`05`, "guard against half-finished AI
  multi-step responses").
- **Pre-dispatch state reconciliation as a requirement.** `§6` makes `probe(target)` optional; a
  future revision may require it for reversible state-change action types if the `UNKNOWN` residual
  proves operationally costly.
- **Idempotency key derivation across retargeting.** The `f(reckon-action.id, target)` key is stable per
  target; the precise canonicalization (and whether `attempt_no` participates) is settled at
  implementation time alongside the dispatch ledger.
- **Credential-resolution specifics for write creds.** Deferred to the uniform scheme in `05 §10.2`;
  write creds live in vault and are resolved only inside the execute step (`05 §6.2`).

---

*End of spec. The read-side counterpart is `03-capability-layer.md`; the action domain model is
`04-action-authorization.md` (authorization), `02-persistence.md §3` (events), and
`05-component-architecture.md §6.2` (dispatch topology).*
