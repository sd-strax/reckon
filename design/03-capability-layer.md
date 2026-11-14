# Capability Layer Specification

A self-contained design for the abstraction between LLM/analyst and the SOC's
heterogeneous tool ecosystem in the investigation environment. This spec
consumes the Investigation Domain Model as its authoritative dependency and
produces a vendor-neutral, transport-neutral capability surface that normalizes
tool outputs into STIX-shaped observations and OCSF events.

---

## 1. Design Principles

The capability layer exists to keep three things clean: the LLM's tool-selection
space, the domain model's invariants, and the framework's extensibility. Every
design choice below derives from one of those.

**Vendor neutrality is achieved by intent verbs, not lowest-common-denominator
schemas.** A capability expresses *what the analyst is trying to learn*, not the
union or intersection of what every EDR happens to expose. If a vendor cannot
fulfill a capability, that is a graceful-degradation problem, not a schema
problem.

**Transport is an adapter concern, not a layer concern.** The capability layer
is agnostic to whether a tool speaks MCP, REST, GraphQL, gRPC, SQL, or reads
files off disk. MCP is one transport among several — preferred where it exists
and is production-ready, but not assumed. What matters is that every adapter
produces well-formed OCSF events at its outbound boundary. By the time data
crosses the adapter boundary into the capability layer, transport details are
gone.

**The capability layer is pure I/O plus normalization.** It does not reason. It
does not produce `x-interpretation` records (Interpretations live inside the
investigation aggregate per 02-persistence.md §2.1; the agent loop is their only
legal author). It does not make hypothesis judgments. Its outputs are typically
`derivation_mode = DIRECT` because every observation it emits traces to a real
tool call. The single exception is the `detection_finding` normalizer (§4.12),
which emits Indicators and Sightings with `derivation_mode = INFERRED` and
`provenance.tool = vendor_name` because vendor detections are themselves
inferences — just made upstream of our system. Even there, the capability
layer never wraps the imported nodes in an Interpretation; the agent loop does
that when an investigation engages with them.

**Identity is computed once, at the normalizer boundary.** Cross-tool stitching
depends on every producer arriving at the same UUIDv5 for the same entity. The
normalizer is the only place identity is computed, and the rules are
deterministic and documented.

**Raw responses are sacred.** Every tool response becomes an immutable
`OcsfEvent` before any normalization runs. Normalization can be re-run, fixed,
versioned, or replayed; the raw payload is the ground truth and never mutated.

---

## 2. Verb Catalog

The v1 capability set is 22 verbs, grouped by intent. Each verb has a typed
signature referencing domain model types. Inputs are described as parameter
records; outputs are STIX objects (and the implicit `OcsfEvent` always written
to telemetry).

Conventions for signatures: `Entity` means a STIX SCO id or a canonical
identifier (string) the layer will resolve. `TimeWindow` is
`{ from: timestamp, to: timestamp }`. `Limit` is an optional integer with
per-verb defaults. The composite inputs below (`ProcessFilter`, `QuerySpec`,
`scopes`) name the concept; on the wire every verb has one shape — an entity,
a window and scalar extras — and a composite is flattened to its scalar
fields, each a template root a binding addresses (§3.3); a field that names an
entity (`ProcessFilter.host`, `.user`) rides as the call's entity, and a
map-valued field (`QuerySpec.parameters`) has no wire slot and is not carried.
A verb whose question is fleet-wide (`get_process_executions`,
`get_persistence_artifacts`) takes its entity optionally: absent, it asks
every host; given, it narrows. Every verb returns, in addition to its declared
outputs, a `CapabilityResult` envelope:

```text
CapabilityResult {
  ocsf_event_refs:    list<OcsfEvent.id>      // every raw response written
  observed_data_refs: list<ObservedData.id>   // normalized observations
  entity_refs:        list<Entity.id>         // SCOs extracted/resolved
  provenance:         Provenance              // uniform fields
  coverage:           Coverage                // structured outcome (see §6)
  degradation_notes:  list<string>            // human-readable detail
}

Coverage = COMPLETE
         | PARTIAL                  // some bindings succeeded, some failed
         | UNAVAILABLE_TENANT       // no binding exists for this verb in tenant
         | UNAVAILABLE_TRANSIENT    // bindings exist, all currently unhealthy
         | FAILED                   // bindings healthy, all calls failed
```

The agent loop is what attaches these refs to an `x-interpretation` of type
`extraction`. The `coverage` field is structured so the agent loop can branch
on outcome class without parsing prose; `degradation_notes` carries
human-readable detail (e.g., which adapters failed and why).

### 2.1 Entity resolution and context

**`resolve_entity(identifier: string, hint_type?: EntityType) -> Entity`**
Canonicalizes a free-form identifier (e.g., `"8.8.8.8"`, `"contoso\\jdoe"`,
`"WIN-DC01"`) into a STIX SCO with a deterministic UUIDv5 id. Does not call
external tools unless `hint_type` is unknown and disambiguation requires a
directory lookup. This is the canonical entry point for entering an
investigation by entity.

**`get_user_context(user: Entity) -> {user_account: Entity, group_memberships: list<Entity>, manager_ref?: Entity, account_status: string}`**
Pulls IdP/directory context for a user (Entra ID, AD, Okta). Output is one or
more `user-account` SCOs plus relationships materialized as ObservedData.

**`get_host_context(host: Entity) -> {host: Entity, owner_ref?: Entity, os: string, last_seen: timestamp, sensors: list<string>}`**
Asset/CMDB context for a host. Returns enriched `x-host`.

**`get_indicator_context(indicator: Entity) -> {ti_matches: list<ObservedData>, reputation: string, first_seen?: timestamp}`**
Threat-intel enrichment for an IP, domain, hash, or URL.

### 2.2 Process and execution

**`get_process_ancestry(process: Entity, depth?: int = 5) -> list<ObservedData>`**
Walks parent chain of a process. Outputs ordered ObservedData each linking
parent-child `process` SCOs.

**`get_process_descendants(process: Entity, depth?: int = 3, limit?: int = 100) -> list<ObservedData>`**
Children/grandchildren of a process. Outputs ObservedData with parent and
descendant `process` SCOs.

**`get_process_executions(filter: ProcessFilter, window: TimeWindow, limit?: int = 200) -> list<ObservedData>`**
Search for process executions matching a filter
(`{image_hash?, image_path?, command_line_contains?, host?, user?}`). OCSF class
1007 maps directly.

**`get_module_loads(process: Entity, window: TimeWindow) -> list<ObservedData>`**
DLLs/modules loaded by a process. OCSF class 1005 (module_activity) where
vendors expose it.

### 2.3 Authentication and access

**`enumerate_logons(target: Entity, window: TimeWindow, outcome?: SUCCESS|FAILURE|ALL) -> list<ObservedData>`**
Logons to or by a host/user. OCSF class 3002. `target` may be a `user-account`
or `x-host`.

**`get_failed_auth_burst(target: Entity, window: TimeWindow, threshold?: int = 5) -> list<ObservedData>`**
Convenience verb returning failed auth bursts above threshold. Same telemetry as
`enumerate_logons` but pre-filtered server-side where the underlying tool
supports it.

**`get_privilege_changes(target: Entity, window: TimeWindow) -> list<ObservedData>`**
Group membership and privilege grants/revocations. OCSF classes 3006
(group_management) and 3005 (user_access), §4.9.

### 2.4 Network and lateral movement

**`get_network_connections(endpoint: Entity, window: TimeWindow, direction?: INBOUND|OUTBOUND|ALL, limit?: int = 500) -> list<ObservedData>`**
Network conns from/to a host, IP, or process. OCSF class 4001.

**`resolve_dns(name: Entity | string, window: TimeWindow) -> list<ObservedData>`**
DNS resolutions. OCSF class 4003.

**`get_lateral_movement_signals(host: Entity, window: TimeWindow) -> list<ObservedData>`**
Composite verb: surfaces remote logons, SMB/WMI/RDP traffic, and process
executions consistent with lateral movement. Returns ObservedData with
`derivation_mode = DIRECT` per source; the agent decides what's a hypothesis.

### 2.5 File and persistence

**`get_file_activity(file: Entity, window: TimeWindow) -> list<ObservedData>`**
Reads/writes/deletes touching a file or hash. OCSF class 1001.

**`get_persistence_artifacts(host: Entity, window: TimeWindow) -> list<ObservedData>`**
Registry run keys, scheduled tasks, services, autoruns, WMI subscriptions.
OCSF classes 201001 registry_key_activity and 201002
registry_value_activity (Windows extension), 1006 scheduled_job_activity,
1005 module_activity.

### 2.6 Email and identity

**`get_email_messages(filter: EmailFilter, window: TimeWindow, limit?: int = 100) -> list<ObservedData>`**
Email metadata (sender, recipient, subject, attachments, urls, verdict). OCSF
class 4009 (email_activity).

**`get_email_clicks(user: Entity, window: TimeWindow) -> list<ObservedData>`**
URL clicks from email. OCSF class 4012 (email_url_activity) where exposed;
otherwise vendor-specific.

### 2.7 Generic queries and pivots

**`query_logs(spec: QuerySpec, window: TimeWindow, limit?: int = 1000) -> list<ObservedData>`**
Escape hatch for SIEM-style queries against Splunk/Sentinel/Chronicle.
`QuerySpec = {tool, query_text, parameters}`. Result is normalized best-effort:
rows with recognizable fields become typed ObservedData; others are wrapped as
opaque ObservedData with the raw OcsfEvent attached. The agent should prefer
typed verbs and fall back to `query_logs` only when no typed verb fits.

**`pivot_on_indicator(indicator: Entity, window: TimeWindow, scopes?: list<string>) -> list<ObservedData>`**
Fan-out search across all telemetry sources for sightings of an indicator.
`scopes` filters to e.g. `["edr", "proxy", "email"]`.

**`search_alerts(filter: AlertFilter, window: TimeWindow) -> list<ObservedData>`**
Detection findings/alerts touching an entity or matching criteria. OCSF class
2004 (detection_finding).

### 2.8 Capability discovery

**`list_capabilities(tenant?: string) -> list<CapabilityDescriptor>`**
Returns the capabilities resolvable in the current tenant, including which are
degraded or unavailable. Used by the agent loop on session start to right-size
the tool set advertised to the LLM. A verb whose bound adapter reports a
degraded capability for the bound operation (`11 §4.4`) stays available and
carries `limitations: [{adapter, capability, reason}]` (§6.3): it still
answers the other calls, and a call a binding cannot express is declined by that
binding (`§6.2`), not sent.

### 2.9 External case lookup

**`query_external_cases(filter: CaseFilter, window: TimeWindow, limit?: int = 50) -> list<ObservedData>`**
Search the customer's case management system of record (TheHive,
ServiceNow SOC, Jira SOC, Splunk ES Cases, custom) for cases matching the
filter. `CaseFilter` supports text query, **queue** (the work queue the SoR
assigns by — assignment group / project / team, mapped by the adapter to its
own vocabulary; `14 §4.2`), status, label, technique, and entity-mention
filtering. Output normalized as `ObservedData` wrapping case
references — each case becomes an entity-shaped observation with metadata:
title, status, summary, key entities mentioned, link back to the SoR. Goes
through the standard normalizer pipeline and lands in the telemetry layer
with `provenance.tool = sor_name`.

**`get_external_case_details(case_id: string) -> ObservedData`**
Fetch full content for one case in the SoR, including comments,
attachments metadata, and linked entity references. Used as a follow-up
after `query_external_cases` returns hits worth deep-diving.

**Implemented (v0):** ServiceNow, via the native adapter's read surface —
each incident is shaped as an OCSF Incident Finding (class_uid 2005) and
normalized to an `ObservedData` that carries the case metadata in its
extensions. The case fields are `finding_info_list[]`, `assignee_group` and
`src_url`, and the system of record's name is in `unmapped.sor` (adr/0356).
Entity extraction from case bodies is deferred, since SNOW incidents carry no
structured entity fields.

These verbs are the read-side of the "have we seen this before in our
SoR?" question; the internal-similarity counterpart lives in the knowledge
service (06-knowledge-service.md §4). The two surfaces are deliberately
separate — internal similarity over reckon's own concluded investigations is
a sibling-of-LLM context retrieval, while external case lookup is a
vendor-tool capability call producing telemetry like any other read-side
adapter.

### 2.10 Cloud control plane

**`get_cloud_api_activity(subject?: Entity, window: TimeWindow, operation?: string, service?: string, outcome?: SUCCESS|FAILURE|ALL = ALL, limit?: int = 200) -> list<ObservedData>`**
Control-plane API calls — OCSF API Activity (class 6003, §4.15) — matching
every given filter. The entity is optional, the fleet-wide shape of §2's
conventions: given a `user-account` it asks what that principal did (the
account may be named by its credential id, since "what did this access key
do" names the key, not the user); given an `x-cloud-resource`, who touched
it; given an `x-cloud-account`, what happened in it; absent, `operation` or
`service` alone asks who called that anywhere in the window — the
"across accounts" question. The entity's wire shape is stated in the
descriptor's own text — `{"user":{"name"}}` or `{"user":{"credential_uid"}}`,
`{"cloud_resource":{"provider","uid"}}` or `{"cloud_resource":{"provider",
"name","type","account_uid"}}`, `{"cloud_account":{"provider","uid"}}`
(§3.3.1) — because the model learns a verb from its descriptor alone and a
guessed key is not an empty answer but a wrong one against a source that
treats an absent filter as unfiltered. At least one filter is the verb's contract, stated
in its intent: an unfiltered window of an audit trail is not a question. The
engine does not enforce it (no verb's inputs are validated beyond the tool
schema, and the hunt verbs of §2.2 and §2.5 accept an unfiltered sweep,
bounded by `limit` and the result budget); a source adapter that cannot
answer an unbounded query refuses it under §6.2.

**`get_cloud_resource_context(resource: Entity) -> {resource: Entity, account_ref?: Entity, configuration: ObservedData}`**
"What is this resource now": the current configuration of one cloud
resource — policy, exposure settings, tags, labels, owner, created and
modified times — as an OCSF Cloud Resources Inventory Info event (class
5023, §4.18) normalized to the same `x-cloud-resource` and `x-cloud-account`
ids §4.15 mints from activity, so the snapshot and the calls that produced it
meet on one node. The parallel of `get_host_context` for a resource; the
half of the story the trail cannot tell (is the bucket public *now*).

**`get_cloud_resource_history(resource: Entity, window: TimeWindow, limit?: int = 200) -> list<ObservedData>`**
The calls that created, changed or deleted one resource in the window
(activity Create, Update, Delete), oldest first: the change timeline. The
same class as `get_cloud_api_activity`, pre-filtered and ordered, kept as
its own verb because a real source answers it from a different place (a
configuration timeline rather than the raw trail) and because the question
wants the whole ordered timeline, not a filtered page — the
`get_failed_auth_burst` precedent beside `enumerate_logons`.

A fixture may answer the activity verb and the history verb from one
recorded mutating call (`matches.verb` as a list, §9.2); a read is never a
history entry.

---

A note on the count: 22 query verbs (plus the §2.9 external case lookup verbs and the §2.10 cloud control-plane verbs) hits the upper end of the 15-25 target. I
considered collapsing `get_failed_auth_burst` into `enumerate_logons` and
`get_email_clicks` into `get_email_messages`. I kept them separate because they
map to distinct analyst intents and benefit from server-side pre-filtering when
the underlying tool supports it. If schema budget becomes a problem, those four
are the first candidates to merge.

---

## 3. Resolution Model

### 3.1 Approach

Resolution is **declarative mapping with pluggable adapters**, not a rule
engine. Every verb has one or more `CapabilityBinding`s per tenant. A binding
names the adapter (e.g., `crowdstrike_falcon`, `defender_xdr`, `splunk_es`), the
concrete operation on that adapter, and a parameter mapping. At call time the
resolver picks the highest-priority binding whose preconditions are satisfied
(entity type matches, required tool features available, tenant has credentials
configured).

A rule engine was rejected because the matching logic is structural (verb +
entity types + tenant capabilities), not behavioral. Declarative bindings are
diff-friendly, version-controllable, and lend themselves to the extension story
below.

### 3.2 Tenant config

Tenant config is YAML, loaded at startup and watchable for hot-reload. The
shape:

```yaml
tenant: acme-corp
adapters:
  crowdstrike_falcon:
    class: native_api          # mcp | native_api | custom | fixture
    base_url: https://api.crowdstrike.com
    credentials_ref: vault://acme/cs
    enabled: true
  defender_xdr:
    class: native_api
    tenant_id: 11111111-2222-3333-4444-555555555555
    credentials_ref: vault://acme/m365
    enabled: true
  splunk_es:
    class: native_api
    base_url: https://splunk.acme.internal:8089
    credentials_ref: vault://acme/splunk
    enabled: true
  acme_cmdb:
    class: custom              # homegrown REST service
    base_url: https://cmdb.acme.internal
    credentials_ref: vault://acme/cmdb
    enabled: true
  threat_intel_mcp:
    class: mcp
    server_url: https://ti.partner.com/mcp
    credentials_ref: vault://acme/ti
    enabled: true
  fixture:
    class: fixture
    enabled: false             # toggled true in v0 / dev / replay

bindings:
  get_process_ancestry:
    - adapter: crowdstrike_falcon
      operation: rtr.process_tree
      priority: 100
      params:
        device_id: "${entity.host.external_id}"
        process_id: "${entity.process.pid}"
    - adapter: defender_xdr
      operation: advanced_hunting
      priority: 50
      params:
        kql: |
          DeviceProcessEvents
          | where DeviceId == "${entity.host.external_id}"
          | where ProcessId == ${entity.process.pid}
          | ...
  enumerate_logons:
    - adapter: defender_xdr
      operation: advanced_hunting
      priority: 100
      params: { ... }
    - adapter: splunk_es
      operation: search
      priority: 80
      params: { ... }
  get_host_context:
    - adapter: acme_cmdb
      operation: assets.lookup
      priority: 100
      params: { hostname: "${entity.host.hostname}" }
  get_indicator_context:
    - adapter: threat_intel_mcp
      operation: lookup
      priority: 100
      params: { indicator: "${entity.value}" }

policies:
  default_window:
    investigation: PT24H
    hunt: PT7D
  rate_limits:
    per_adapter_qps: 5
  caching:
    default_ttl: PT15M
```

The resolver algorithm:

1. Receive `(verb, params, tenant)`.
2. Look up bindings for `verb` in tenant config, sorted by priority desc.
3. For each binding, check adapter is enabled, its source scope matches the
   investigation's (§3.5), credentials are valid, and required input fields
   are present.
4. Render parameter template against the input.
5. Invoke the adapter; on success, return the raw response to the OCSF writer
   and then the normalizer.
6. On binding failure (adapter down, auth error, schema mismatch), proceed to
   next binding. On all bindings exhausted, return a partial `CapabilityResult`
   with `degradation_notes`.

Why priority rather than a single binding per verb: in real tenants, the "best"
tool depends on the entity. If an EDR has the host but the process originated
from a logon visible only to the IdP, you want the resolver to try the EDR
first, fall through to the SIEM if the device isn't enrolled, and surface
partial results rather than throwing. Priority + preconditions gives you that
without writing a rule DSL.

### 3.3 Parameter templating

Bindings render concrete adapter parameters from the capability call's input by
template expansion. The template language is **string substitution with
typed-path references and a sealed-but-extensible function set**. It is not a
full expression language; it does not evaluate arbitrary code; it cannot
perform conditionals or loops. This bounded surface is deliberate — the binding
config is the integration surface, written by many people, deployed per
tenant, and "your investigation broke because of a runtime template error" is
not an acceptable failure mode.

#### 3.3.1 Path syntax

Templates appear as `${...}` expressions in YAML scalar values. Three forms:

```text
${path.to.field}                 # required: missing field rejects the binding
${path.to.field?}                # optional: missing field omits the param
${path.to.field ?? "default"}    # default: missing field substitutes the literal
```

A path is a dotted reference walked against the binding's input context.
Available path roots:

```text
entity        # the input entity, or composite for multi-entity verbs
              #   - single-entity verbs: entity.<field>
              #   - composite verbs:     entity.<role>.<field>
              #     where <role> is the parameter name in the verb signature
              #     (e.g., entity.process.pid, entity.host.hostname)
window        # the TimeWindow input: window.from, window.to
tenant        # tenant config values, for parameterizing by tenant policy
              #   (e.g., tenant.policies.default_window.investigation)
verb          # the verb name itself, useful for adapter-side logging
```

Composite-verb disambiguation: when a verb takes multiple entities (e.g.,
`get_process_ancestry(process)` is single, but the process input itself
references a host), the template path mirrors the input shape:
`${entity.process.pid}`, `${entity.process.host.external_id}`. Adapters
expecting a flat parameter shape must walk the structure themselves; the
templating layer doesn't flatten.

The entity object is keyed by kind, and the keys are the wire contract the
verb descriptors state in their input text: `host.{hostname,domain,
external_id}`, `process.{pid,…}`, `user.{name,account_login,credential_uid,
…}`, `ip`, `domain`, `url`, and for the cloud verbs (§2.10)
`cloud_resource.{provider,uid,name,type,region,zone,group,namespace,
account_uid}` and `cloud_account.{provider,uid}`. A cloud root carries its
`provider` because every cloud identity is scoped by it (§7.2) and the
absence resolver cites a subject only through it (§4.13).

#### 3.3.2 Transformations

Functions are applied with pipe syntax and chain left-to-right:

```text
${entity.host.hostname | upper}
${entity.user.account_login | lower | splunk_quote}
${window.from | iso8601}
${entity.process.pid | int}
```

**Built-in functions (always available):**

```text
String:    upper, lower, trim
Time:      iso8601, epoch_ms, epoch_s
Type:      int, string, bool
Defaults:  default(value), coalesce(...)
Quoting:   json_quote, json_escape
```

**Adapter-registered functions:** adapters may register additional functions
that exist only in bindings targeting that adapter. The Splunk adapter
registers `splunk_quote`, `splunk_escape`, `spl_field`. The Defender adapter
registers `kql_quote`, `kql_escape`, `kql_table_ref`. The CrowdStrike adapter
registers `cs_device_filter`, `cs_time_format`. This keeps adapter-specific
serialization concerns inside the adapter that owns them, while preserving a
uniform template surface for binding authors.

A binding's templates are parsed and validated against the registered function
set for its adapter at config-load time. Unknown function names cause the
configuration to be rejected at startup; the server does not start with
invalid bindings.

#### 3.3.3 Structured parameters

Templates produce only scalar values. Parameter structure (objects, arrays)
comes from the YAML itself; templates fill leaf values:

```yaml
params:
  device_id: "${entity.host.external_id}"
  process_id: "${entity.process.pid | int}"
  filters:
    - field: "host"
      value: "${entity.host.hostname | lower}"
    - field: "user"
      value: "${entity.user.account_login | lower}"
  options:
    include_children: true
    max_depth: 5
```

This keeps the templating language simple while still expressing arbitrary
parameter shapes. If an adapter needs a dynamically-sized list, the binding
author authors it explicitly in YAML; templates never produce collections.

#### 3.3.4 Validation and failure modes

**Config-load validation** (server startup):

- Every `${...}` expression is parsed; syntax errors reject the config.
- Every path root is recognized (`entity`, `window`, `tenant`, `verb`).
- Every function reference exists in the built-in set or in the target
  adapter's registered set.
- Default-value literals are valid YAML scalars.

If any binding fails validation, the server logs the offending binding and
its tenant, refuses to start, and surfaces the error. Bindings cannot fail
silently into production.

**Runtime validation** (per capability call):

- Required paths (`${path.to.field}` without `?` or `??`) that resolve to
  missing or null values mark the binding **not applicable** for this input.
  The resolver moves to the next priority binding, exactly as if the binding
  did not exist for this entity type.
- Optional paths (`${path.to.field?}`) that resolve to missing values cause
  the containing param to be omitted entirely.
- Type-coercion functions (`int`, `bool`) that fail on non-coercible input
  mark the binding not applicable; same fall-through behavior.
- Adapter-registered functions are responsible for handling their own input
  validation; failures are surfaced as binding-not-applicable.

A binding being not-applicable at runtime is **not** an error from the LLM's
perspective. It contributes to coverage classification (§6) only if all
priority bindings are not-applicable; in that case, the result is
`UNAVAILABLE_TENANT` with a degradation note explaining which inputs were
missing. This surfaces as actionable feedback ("this verb needs a
`host.external_id` we don't have for this entity") rather than as a silent
failure.

#### 3.3.5 What the templating language deliberately does not include

Conditionals, loops, arithmetic beyond type coercion, regular expressions,
arbitrary method calls, file or network access, environment variable
references. If a binding genuinely needs any of these, the right answer is to
extend the adapter (with a registered function or with adapter-side logic)
rather than to extend the templating language. This boundary is the
load-bearing simplification.

### 3.4 Multi-binding fan-out

For a small set of verbs (`pivot_on_indicator`, `search_alerts`, `query_logs`
with scopes), one capability call legitimately should hit multiple adapters and
merge results. These verbs are marked `fanout: true` in the binding config; the
resolver invokes all enabled bindings in parallel, writes one OcsfEvent per
response, and the normalizer dedupes ObservedData by content hash before
returning.

### 3.5 Source scope: one tenant, many organizations

A single analyst team frequently works one aggregated event feed that spans
**multiple independently administered organizations** — subsidiaries, business
units, acquisition silos, or environments operated on another organization's
behalf. Each organization has its own identity provider, its own EDR tenancy,
its own credentials; the aggregation platform tags every event with the
organization it came from. The analyst's *surface* is unified; the *tools
behind it* are not.

Named adapter instances (11 §5) already let one tenant run `okta-acme` and
`idp-meridian` side by side. What priority-plus-preconditions alone cannot do
is **route a call to the right one**. Two concrete failure modes make this
load-bearing rather than cosmetic:

- **Wrong-data reads.** `get_entity_context(jdoe)` in an investigation rooted
  in org A's alert must not be answered by org B's directory. Fall-through
  makes this worse, not better: if `jdoe` exists in *both* orgs (common logins
  do), the highest-priority binding answers confidently — with the wrong
  organization's user.
- **Mis-routed writes.** The action resolver is deliberately no-fall-through
  (08 §4); it must select *the one correct* binding. Without a routing basis,
  `account.disable(jdoe)` against the wrong organization's IdP is the worst
  single failure this layer could produce.

The mechanism is one optional field on each side of the call, and one matching
rule:

- **Instances declare a scope.** The enablement stanza (11 §5) gains an
  optional `scope: <string>`. Scope lives on the *instance*, not the binding —
  an instance holds one organization's credentials, so the instance is the
  truthful granularity; its bindings inherit it. Adapters themselves are
  **scope-unaware**: the mechanism is entirely engine-side (config + resolver),
  and the adapter contract (§5.3) is untouched.
- **Investigations carry a scope.** The Seed (01, Extension 1) gains an
  optional `source_scope`, set at creation — for an AlertSeed, derived from the
  aggregation platform's organization tag on the seeding alert; for
  Entity/Question/Case seeds, supplied by the caller (`14-case-seed.md §3`:
  a case seed's read runs at the caller-supplied scope). Immutable, like the
  rest of the seed.
- **The matching rule (fail-closed).** An *unscoped instance* is applicable to
  any investigation (the single-organization case and genuinely shared tools —
  a threat-intel adapter serves every scope). A *scoped instance* is applicable
  **only** to an investigation carrying the identical scope; an unscoped
  investigation does not match a scoped instance. Scope filtering runs before
  the priority walk (§3.2 step 3) — a scope-mismatched binding is *not
  applicable*, never merely lower-priority. On the read side an all-filtered
  verb degrades with `degradation_notes` like any other coverage gap (§6.1);
  on the write side it means *no binding* — the action type reports unavailable
  for that investigation and nothing is dispatched (08 §4).

```yaml
adapters:
  okta-acme:    {adapter: okta, scope: acme,     config: {org_url: "https://acme.okta.com"}, ...}
  idp-meridian: {adapter: msft, scope: meridian, config: {...}, ...}
  threat_intel: {adapter: ti,   config: {...}, ...}   # unscoped: serves every scope
```

**Scope participates in identity.** Deterministic identity (§7) is
tenant-scoped; in a scoped deployment it must be scope-scoped *within* the
tenant, or the graph conflates entities across organizations — org A's `DC01`
and org B's `DC01` are different machines, and a shared UUIDv5 would attach one
organization's evidence to another's entity (a silent cross-organization merge,
exactly what §7's aliasing rules exist to forbid). Mechanism: when a scope is
present, the identity namespace is derived per scope — `uuidv5(tenant_namespace,
scope)` — and all per-type rules (§7.2) apply unchanged within it. This
*upholds* the "same entity → same id" commitment: same-named entities in
different organizations are not the same entity.

Deferred to v1+: scope sets/hierarchies (an instance serving several
organizations); per-entity scope within one investigation (a cross-organization
intrusion is, in v0, one investigation per scope joined by linkage edges,
01 §Relationships); deriving `source_scope` automatically during detection
normalization (§4.12) from the OCSF metadata organization fields.

---

## 4. Normalization

Normalization is per-OCSF-class and produces three things: zero or more STIX
SCOs (with computed UUIDv5 ids), one or more `ObservedData` SDOs, and the edges
connecting them to the originating `OcsfEvent`. All outputs carry uniform
provenance.

Device objects (`device`, `src_endpoint`, `dst_endpoint`) are shaped once, by
the shared host extractor, for every class: OCSF `device.uid` is the device's
own id and `agent_list[].uid` the sensor's, but neither is identity; the host
is keyed on its hostname and keeps both as properties, and when one record
names the hostname beside a MAC or a device id the extractor also emits the
weak host and its `aliases` edge (§7.2, §7.3).

### 4.1 OCSF process_activity (class_uid 1007) → STIX

Input: an OCSF process_activity event with fields including `process.pid`,
`process.name`, `process.cmd_line`, `process.file.path`, `process.file.hashes`,
`process.parent_process.*`, `device.uuid`, `device.hostname`, `actor.user.name`,
`time`.

Output:

- One `process` SCO for the process: `pid`, `command_line`, `name`, optional
  `created_time`. STIX 2.1 process SCOs are not first-class identifiable by
  themselves; identity is composed (see §7).
- One `process` SCO for the parent process if present.
- One `file` SCO for the executable image, with hashes if present.
- One `x-host` SCO for the device.
- One `user-account` SCO for the executing user if present, shaped by the
  account rule (§7.2): the record's strongest identifier is in
  `object_refs`; each lesser identifier it carries mints its own account
  outside them, with `aliases` to the strongest. Version 3 (adr/0324).
- One `ObservedData` SDO with `object_refs` pointing to all of the above,
  `first_observed = last_observed = time`, `number_observed = 1`.
- Edges: `extracted-from` from each SCO to the `OcsfEvent`; `derived-from` from
  the `ObservedData` to the `OcsfEvent`.
- STIX `Relationship` objects for parent-child (`relationship_type:
  "parent-process-of"` between the two process SCOs; distinct from the
  `parent-of` edge, which 01-domain-model.md reserves for x-hypothesis
  refinement).

### 4.2 OCSF authentication (class_uid 3002) → STIX

Input: OCSF authentication event with `user.name`, `user.domain` (the account
being authenticated, as OCSF defines it), `dst_endpoint.hostname`,
`src_endpoint.ip`, `auth_protocol`, `logon_type`, `status` (SUCCESS/FAILURE),
`time`; optionally `actor.user.*`, whoever started the logon (the initiator,
adr/0356); optionally `src_endpoint.location`, the
source's reported place as an OCSF Geo Location (`city`, `region`, `country`
or the country as the source named it in `desc`, `lat`/`long`, `provider`),
carried on the event and read by the event view, not extracted into a STIX
object (`adr/0208`).

Output:

- One `user-account` SCO for the event's `user`, shaped by the account rule
  (§7.2): the record's strongest identifier is in `object_refs`; each lesser
  identifier it carries mints its own account outside them, with `aliases`
  to the strongest. A bare login from a source that names no OS and no host
  is the weak `login` account, so the account is kept. Version 3 (adr/0324).
- When the event carries `actor.user`, a second `user-account` SCO for the
  initiator, shaped the same way. It is a separate account with no
  `x-authenticated-to` of its own; when it names the same account as `user`,
  it is that one object. Version 5 (adr/0356).
- One `x-host` SCO for the destination.
- One `ipv4-addr` (or `ipv6-addr`) SCO for the source IP if present.
- One `ObservedData` SDO referencing all three, plus a `description`-style note
  in custom extension fields recording `logon_type` and `status`.
- Edges as in §4.1.
- An `x-authenticated-to` Relationship between the user and host (derivable from
  STIX `Relationship` with custom type since STIX has no first-class auth
  relationship).

### 4.3 OCSF network_activity (class_uid 4001) → STIX

Input: OCSF network_activity with `src_endpoint.ip`, `src_endpoint.port`,
`dst_endpoint.ip`, `dst_endpoint.port`, `dst_endpoint.domain` (optional),
`connection_info.protocol_name`, `traffic.bytes_in/out`, `time`, optional
`actor.process.*` and `device.*`.

Output:

- `ipv4-addr`/`ipv6-addr` SCOs for source and destination IPs.
- `domain-name` SCO for destination domain if present (with a `resolves-to`
  Relationship to the dst IP).
- `network-traffic` SCO (STIX 2.1 native) wrapping the connection, with
  `src_ref`, `dst_ref`, `src_port`, `dst_port`, `protocols`.
- `process` SCO and `x-host` SCO if process/device info present.
- One `ObservedData` referencing all of the above.
- Edges as in §4.1.

### 4.4 OCSF dns_activity (class_uid 4003) → STIX

Input: OCSF dns_activity with `query.hostname`, `query.type`, `answers[]`
(each with `rdata` and `type`), `src_endpoint.ip`, `time`, optional
`device.uuid`.

Output:

- `domain-name` SCO for `query.hostname`.
- `ipv4-addr` / `ipv6-addr` SCOs for each answer of type A/AAAA.
- `domain-name` SCO for each answer of type CNAME.
- STIX `Relationship` of type `resolves-to` from the queried domain to each
  resolved address (one per A/AAAA answer).
- `ipv4-addr`/`ipv6-addr` SCO for the requesting source if present.
- `x-host` SCO for the device if present.
- One `ObservedData` referencing the queried domain, the answers, and the
  requestor. `first_observed = last_observed = time`.
- Edges as in §4.1.

### 4.5 OCSF file_activity (class_uid 1001) → STIX

Input: OCSF file_activity with `file.name`, `file.path`, `file.hashes`,
`file.size`, `activity_id` (CREATE/READ/MODIFY/DELETE), `actor.process.*`,
`device.uuid`, `time`.

Output:

- `file` SCO for the file, with hashes if present and a `parent_directory_ref`
  pointing to a `directory` SCO if `file.path` carries a directory.
- `directory` SCO for the parent directory.
- `process` SCO for the actor process if present.
- `x-host` SCO for the device.
- `user-account` SCO for the executing user if present in `actor`, shaped
  by the account rule (§7.2): the record's strongest identifier is in
  `object_refs`; each lesser identifier it carries mints its own account
  outside them, with `aliases` to the strongest. Version 3 (adr/0324).
- One `ObservedData` referencing all of the above. The `activity_id` is
  preserved in custom extension fields on the ObservedData (STIX has no
  first-class read/write/modify distinction at the ObservedData level).
- Edges as in §4.1.

### 4.6 OCSF registry_key_activity (class_uid 201001) and registry_value_activity (class_uid 201002) → STIX

Input: one of two OCSF Windows-extension classes, both handled by one
normalizer.

- registry_key_activity carries `reg_key.path`, the full key path with the
  hive first.
- registry_value_activity carries `reg_value.{path,name,data,type,type_id}`.
  Here `path` is the key the value lives under. From 1.6.0 the data can
  instead arrive in a field for its type: `reg_string_data`,
  `reg_integer_data`, `reg_binary_data` or `reg_string_list_data`.

Both carry `activity_id`, `actor.process.*`, `device.*` and `time`. Class
1003 is OCSF's Kernel Activity, not a registry class (adr/0356).

Output:

- `windows-registry-key` SCO (STIX 2.1 native).
  - `key` is the full key with its hive spelled out. It comes from
    `reg_key.path` on a key event and from `reg_value.path` on a value
    event.
  - A leading `HKLM`, `HKU`, `HKCU`, `HKCR`, `HKCC`, `\REGISTRY\MACHINE` or
    `\REGISTRY\USER`, in any case, becomes the full hive name in upper
    case. The rest of the path keeps the source's case.
  - A path with no known root is kept as written.
  - When the event names a value, `values` holds that one value as
    `{name, data, data_type}`. The name is as written, and empty for the
    key's default value. The data is a string, with non-string JSON written
    as its JSON text. `data_type` is set when the source's type is one of
    STIX's `REG_*` names (`reg_value.type`, else the name of
    `reg_value.type_id`).
  - A key event has no `values`.
  - An event with no key path mints no object.
  - Identity is in §7.2.
- The value's `data` comes from the first typed field that holds a value
  (`reg_string_data`, `reg_integer_data`, `reg_binary_data`,
  `reg_string_list_data`, from 1.6.0), else from `reg_value.data`. An empty
  typed field counts as absent. The value is then written by the rule
  above: a string as it arrives, anything else as its JSON text. The event
  view reads the data by the same rule (adr/0359).
- `process` SCO for the actor process.
- `x-host` SCO for the device.
- One `ObservedData` referencing the registry key, actor, and host. The
  `activity_id` (CREATE/MODIFY/DELETE) is preserved in custom extension
  fields.
- Edges as in §4.1.

One object stands for one host, key and value. This is because the analyst
pivots on the value (one autorun under the `Run` key), not on the key.
Identity is host-scoped, so registry keys with the same path on different
hosts are different entities (§7.2, adr/0341).

### 4.7 OCSF scheduled_job_activity (class_uid 1006) → STIX

Input: OCSF scheduled_job_activity with `job.name`, `job.file.path`,
`job.user.*`, `activity_id`, `device.*` and `time`. The command line is read
from `job.job_actions[].cmd_line` (1.9.0), else from the deprecated
`job.cmd_line`.

Output:

- `x-scheduled-task` custom SCO with fields `name`, `path`, `command_line`
  (the first action's), `command_lines` (every action's, when there are
  several), `principal_user_ref`. Identity rule: `(host_ref.id, name, path)`.
- `user-account` SCO for the principal user (the user the task runs as),
  shaped by the account rule (§7.2): the record's strongest identifier is in
  `object_refs`; each lesser identifier it carries mints its own account
  outside them, with `aliases` to the strongest. Version 3 (adr/0324).
- `x-host` SCO for the device.
- One `ObservedData` referencing all of the above.
- Edges as in §4.1.

`x-scheduled-task` is a custom SCO covering Windows scheduled tasks, cron
entries, systemd timers, launchd jobs, and similar persistence mechanisms.
Identity is host-scoped.

### 4.8 OCSF module_activity (class_uid 1005) → STIX

Input: OCSF module_activity with `module.file.path`, `module.file.hashes`,
`actor.process.*`, `device.uuid`, `time`.

Output:

- `file` SCO for the loaded module (DLL, .so, etc.), with hashes if present.
- `process` SCO for the loading process.
- `x-host` SCO for the device.
- STIX `Relationship` of custom type `loads` from the process to the file.
- One `ObservedData` referencing all of the above.
- Edges as in §4.1.

### 4.9 OCSF account_change (3001), user_access (3005) and group_management (3006) → STIX

Input: three OCSF classes, shaped by one function. Each class is read only
for its own fields (adr/0356).

- account_change (3001) covers create, enable, disable, password change and
  the like. It carries `user.*` (the target), `user_result` and
  `actor.user.*` (the initiator).
- user_access (3005, User Access Management) covers assigning and revoking
  privileges. It carries `user.*` and `privileges[]`.
- group_management (3006) covers adding and removing a member. It carries
  `user.*`, `group.{name,domain,uid}` and `privileges[]`. From 1.9.0 the
  members are `users[]`, and `user` is deprecated. Every entry of `users[]`
  is a target, and `user` is read when no entry of `users[]` names a user
  (adr/0359).

All three carry `activity_id`, `device.*` and `time`.

Output:

- `user-account` SCO for each target user.
- `user-account` SCO for the initiator if present. Both are shaped by the
  account rule (§7.2): the record's strongest identifier is in
  `object_refs`; each lesser identifier it carries mints its own account
  outside them, with `aliases` to the strongest, and an alias the target and
  the initiator share is recorded once. Version 3 (adr/0324).
- `x-host` SCO for the device if present (often the IdP itself for cloud
  events).
- For group_management (3006): STIX `Relationship` of custom type
  `member-of-group` from each target user to a `x-group` custom SCO. Identity for
  `x-group`: `(directory_ref.value, group_name)` where `directory_ref` is
  the AD/Entra/Okta tenant identifier.
- One `ObservedData` referencing all of the above. The `activity_id` is
  preserved in custom extension fields. For user_access and
  group_management, the privileges are kept in extension fields as the list
  `privileges`.
- Edges as in §4.1.

The `activity_id` is the high-information field for this class — the agent
loop reasons differently about a password reset vs. a privilege grant. The
normalizer does not split into multiple ObservedData per activity type;
downstream consumers filter on the extension field.

### 4.10 OCSF email_activity (class_uid 4009) → STIX

Input: OCSF email_activity with `email.from`, `email.to[]`, `email.cc[]`,
`email.subject`, `email.message_uid`, `email.uid` and `time`.

- From 1.4.0, `email.files[]` (each a file object) and `email.urls[]` are
  read when present.
- Below 1.4.0, an attachment is its own email_file_activity event (4011,
  §4.11a).
- Blind copy is `email.bcc[]` from 1.9.0. Below that, a source that reports
  one puts it in `unmapped.bcc[]`.

Output:

- `email-message` SCO (STIX 2.1 native) with `from_ref`, `to_refs`, `cc_refs`,
  `bcc_refs` (from `email.bcc[]`, else `unmapped.bcc[]`), `subject`, `body_multipart` (if available), `is_multipart`,
  and a `message_id` field populated from `email.message_uid`.
- `email-addr` SCO for each address (sender, recipients, cc, bcc).
- `file` SCO for each attachment in `email.files[]`, with hashes if present.
- `url` SCO for each URL in `email.urls[]`.
- STIX `Relationship` of native type `contains` from the email-message to
  each attachment file and url.
- One `ObservedData` referencing the email-message and all of its parts.
- Edges as in §4.1.

`email-message` identity is the `message_id` **alone** when present, else
`(from_ref.value, subject, date, recipients, thread_uid)` as a fallback.
`date` is the event's time to the second. The recipients are the sorted,
lowercased to, cc, bcc, `smtp_to` and Delivered-To (`delivered_to_list`,
else `delivered_to`) addresses, and
`thread_uid` counts only when present. They keep apart the separate copies
of a phishing or spam run, and messages from different threads (adr/0359). This is a deliberate deviation
from strict STIX 2.1, which uses `(message_id, from_ref.value)`. The RFC
requires `message_id` to be unique. Identity on it alone lets two sources'
records of one message join even when they disagree on its sender.

A message may also carry `email.uid`. From 1.4.0 OCSF defines it as the id
of the email thread, not of one message. It is kept on the message as
`thread_uid` at every version. It creates no identity and no `aliases` edge,
because two messages in one thread would then read as one (adr/0359).

Same stitching-precedence rationale as the
`process`/`email-addr`/`user-account` deviations (§7.2). Cross-tool stitching
for emails is more reliable than for processes because `message_id` is unique.

### 4.11 OCSF email_url_activity (class_uid 4012) → STIX

Input: OCSF email_url_activity with `url.url_string`, `email_uid`,
`actor.user.*` (the clicker), `time` and optional `device.*`. `email_uid` is
the source's id for the email, matched to `email.uid` from 1.4.0. Class 4011 is email_file_activity (§4.11a),
not this class (adr/0356).

Output:

- `url` SCO for the clicked URL.
- No `email-message`. From 1.4.0 OCSF matches `email_uid` to `email.uid`,
  which is a thread id, so a click names a thread, never one message. The
  `email_uid` is kept in the observation's extension fields as
  `thread_uid` (§4.10, adr/0359).
- `user-account` SCO for the clicker, shaped by the account rule (§7.2):
  the record's strongest identifier is in `object_refs`; each lesser
  identifier it carries mints its own account outside them, with `aliases`
  to the strongest. Version 3 (adr/0324).
- `x-host` SCO for the device if present.
- STIX `Relationship` of custom type `clicked` from the user-account to the
  url.
- One `ObservedData` referencing all of the above.
- Edges as in §4.1.

### 4.11a OCSF email_file_activity (class_uid 4011) → STIX

Input: OCSF email_file_activity with `file.*` (the attachment), `email_uid`
(the source's id for the email, as for a click), `actor.user.*`, `activity_id`, `time`
and optional `device.*`. Below 1.4.0, this is the only place an attachment
appears (adr/0356).

Output:

- `file` SCO for the attachment, with hashes if present.
- No `email-message`, as for a click (§4.11). The `email_uid` is kept in
  the observation's extension fields as `thread_uid`.
- `user-account` SCO for the actor, shaped by the account rule (§7.2).
- `x-host` SCO for the device if present.
- One `ObservedData` referencing all of the above, with the `activity_id`
  kept in extension fields.
- Edges as in §4.1.

### 4.12 OCSF detection_finding (class_uid 2004) → STIX

Detection findings are structurally distinct from the other classes in this
section: they are not passive observations, they are vendor *interpretations*.
A vendor's detection is a claim that something is bad. The normalizer
respects this.

Input: OCSF detection_finding with these fields:

- `finding_info.title`, `finding_info.uid` and `finding_info.types[]` (for
  example, MITRE ATT&CK technique IDs).
- `severity_id`, else the `severity` caption.
- `confidence_score`, else `confidence_id`.
- `device`, the host the detection fired on, and `time`.
- `evidences[]`, the artifacts the detection fired on. At the floor an
  artifact may carry `actor`, `process`, `file`, `src_endpoint`,
  `dst_endpoint`, `query`, `api` and `connection_info`. Later versions add
  `device`, `user`, `url` and others.

Output:

- STIX `Indicator` SCO with `pattern` (vendor-specific or a STIX pattern if
  the detection exposes one), `pattern_type`, `valid_from`, `confidence`
  (see below), `name` from
  `finding_info.title`, and `indicator_types` from `finding_info.types[]` — by convention, MITRE ATT&CK
  technique IDs (e.g., `T1486`, `T1078.004`) where vendors emit them. This
  is the canonical path for technique data into the interpretation layer;
  hypotheses produced by the agent loop also carry technique IDs as labels
  per 01-domain-model.md.
- Confidence is a number from 0 to 100, as STIX defines it.
  - It comes from `confidence_score`, capped to 0–100. A score of 0 is kept.
  - Without a score, it comes from `confidence_id`. Low, Medium and High
    become 15, 50 and 85.
  - When the finding has neither, or the level is unknown, the confidence is
    left out. It is never written as 0, because 0 means "no confidence" in
    STIX.
- STIX `Sighting` SDO linking the indicator to the observation below, through
  `observed_data_refs`. `Sighting.confidence` mirrors the Indicator's.
  - The entities the detection names are that observation's `object_refs`.
  - They are not written to `where_sighted_refs`. STIX allows only identity
    and location objects there, as it names who saw the indicator. Nothing
    fills `where_sighted_refs` today (adr/0356).
- The finding's own `device` gives the host. It does so whether or not the
  finding has `evidences[]`.
- Each artifact in `evidences[]` is read with the same extractors the
  activity normalizers use:
  - a process from `process` or `actor.process`
  - a file from `file`
  - addresses and domains from the endpoints and `query`
  - accounts from `actor.user`, and from `user` when present
  - a URL from `url`
  - the host from `device`, else from the finding's own `device`

  An artifact is not an event and has no class, so none is sent through the
  registry (adr/0356).
- The finding's own `cloud` and `resources[]` give the cloud account and
  resources, by §4.15's rules. An alert and the API calls about one
  resource then meet on the same objects.
- On a finding that names a cloud provider, an artifact's `actor.user` is
  shaped as that provider's cloud principal (§4.15) only when the event
  says it is one (adr/0359):
  - `actor.user.account.type_id` (else its caption, `account.type`) is a
    user type of the finding's own provider (AWS 3 or 4, Azure 6, GCP 5);
    or
  - the id the principal would be keyed on has the provider's principal
    form. For AWS that is an ARN in any AWS partition (`arn:aws:`,
    `arn:aws-cn:`, `arn:aws-us-gov:` and the like), an IAM unique id
    (`AIDA` or `AROA` and at least 16 more capital letters or digits), or
    the root principal: a 12-digit id equal to the finding's own account
    id or the user's own account id. The check is on the key itself, so an
    ARN in one field never vouches for a different id in another. When the
    provider's session row (§4.15) gives the key, the check is on that
    value; a session issuer such as `sshd` is no signal.

  A container type, such as 10 AWS Account, gives no signal. Nor does a
  user type of another provider.

  A host or directory account type (1, 2, 7, 9, 19) is read by the account
  rule (§7.2), as on any other event, whatever the provider. So is an
  actor with no signal either way. A login on a VM only means something on
  that VM.
- One `ObservedData` referencing the entities named in the detection,
  separate from the Sighting. This preserves the raw "this entity was seen"
  signal independent of the vendor's interpretation.
- Edges: standard `extracted-from`/`derived-from` for entities and
  ObservedData. The Indicator and Sighting carry **no** `produced-by` edge —
  they sit in the store as imported vendor claims, attributed via
  `created_by_ref` and `provenance.tool` (see below).
- Provenance on the Indicator and Sighting: `derivation_mode = INFERRED`,
  `tool` = the vendor name. This is the **single exception** to the rule
  that capability-layer outputs are `DIRECT` — the vendor's claim is, by
  definition, an inference, just not one made by our agent loop.

The Indicator and Sighting are interpretation-layer artifacts but the
capability layer **does not** emit a synthetic `x-interpretation` to wrap
them. That would violate two invariants: (a) capability §1, "the layer is
pure I/O plus normalization; does not reason; does not produce
x-interpretation records," and (b) 02-persistence.md §2.1 aggregate ownership
— Interpretations live inside the investigation aggregate, and capability
has no legal write path into it.

Instead:

- The Indicator and Sighting are written to the per-tenant STIX object
  store with `derivation_mode = INFERRED`, `provenance.tool = vendor_name`,
  and STIX-standard `created_by_ref` pointing at a per-tenant **vendor
  Identity SDO**. The vendor Identity is itself a deterministic UUIDv5
  within the tenant namespace (computed from the vendor name), auto-created
  on first ingest. No per-tenant "ingestion service owner" config is
  needed — the upstream attribution lives in `created_by_ref` and
  `provenance.tool`.
- The agent loop (or analyst) wraps these in an Interpretation of type
  `extraction` *when an investigation engages with them* — i.e., when the
  Indicator / Sighting is brought into the investigation's scope via
  `MemberAdded` or referenced as evidence. The Interpretation's
  `actor.principal` is the engaging analyst (or the system principal for
  fully-automated paths); `actor.delegate` is the AI agent if AI-driven.
- Vendor Indicators and Sightings sitting in the store **without** a
  produced-by edge to any of our Interpretations are valid — they are
  imported, not produced by reasoning in our system. The
  `provenance.tool = vendor_name` and STIX `created_by_ref` to the vendor
  Identity together preserve the upstream attribution. (See
  01-domain-model.md INVARIANTS for the system-produced-vs-imported
  distinction.)

The agent loop is responsible for deciding how much weight to give
vendor-emitted Indicators relative to its own reasoning.

### 4.13 Normalization framework

Every normalizer is a pure function `(OcsfEvent) -> NormalizationResult`.
Normalizers are registered by `class_uid`. A default normalizer handles
unrecognized classes by emitting an opaque `ObservedData` with `object_refs`
empty and the OcsfEvent attached via `derived-from` — the LLM can still see the
raw payload, just without typed entities.

An adapter that sends a record OCSF has no class for sends it as a Base
Event (class 0). The vendor's fields go under `unmapped.<vendor>`, and the
event view shows the scalars there (adr/0356).

**Absence.** When every applicable binding answered (coverage COMPLETE) and
no event came back, the resolver mints an `x-null-observation`
(01-domain-model.md §NullObservation, adr/0061) over the question that was
asked — verb, subject entity, parameters, window, sources — and returns its id
among `observed_data_refs`, so the caller can cite the absence as it would cite
an observation. This is normalization of the call's outcome, not reasoning: the
condition is mechanical and no interpretation is produced. A PARTIAL,
UNAVAILABLE or FAILED empty answer mints nothing.

Normalizers are versioned. The version is recorded in the ObservedData's
provenance. If a normalizer is improved, prior OcsfEvents can be re-normalized
into new ObservedData; old ObservedData remains valid (immutable in spirit, even
if the model permits mutation). Re-normalization writes new ObservedData and
links them to the same OcsfEvent. The ObservedData id (§7.1) carries the
version but not the normalizer's name, and the opaque default is version 1,
so a typed normalizer that takes over a class the default used to handle
starts at version 2: a payload stored opaque before the normalizer existed
keeps its row, and the typed observation gets its own id beside it rather
than colliding with it and being discarded on write.

### 4.14 Custom OCSF classes for non-standard tools

Homegrown tools and niche vendors often emit data that doesn't cleanly map to
any standard OCSF class. Two options, in order of preference:

1. **OCSF vendor extension class.** OCSF supports vendor-specific class
   extensions in the 9000+ range. The adapter assigns a stable extension
   class_uid, documents the payload schema, and a corresponding normalizer is
   registered. This is the right path for any internal tool whose data the
   investigation will reason over repeatedly.
2. **Opaque ObservedData fallback.** The default normalizer (§4.13) handles
   unrecognized classes. Acceptable for v0 and for one-off integrations, but
   loses typed-entity stitching — the LLM sees raw payload, not graph nodes.

The choice is per-tool. A custom CMDB integration probably warrants a custom
class; a one-off enrichment script probably doesn't.

### 4.15 OCSF api_activity (class_uid 6003) → STIX

Input: an OCSF API Activity event — a control-plane call against a cloud
account — with `actor.user.*` (the principal: `uid`, `uid_alt`, `name`,
`full_name`, `type_id`, `type`, `account.{uid,name,type_id}`,
`programmatic_credentials[].uid`, `credential_uid`), `actor.session.*`
(`uid`, `issuer`, `credential_uid`, `is_mfa`), `actor.application.name`
(1.9.0) else the deprecated `actor.app_name`, `api.operation`,
`api.service.name`, `api.request.uid`, `api.response.{code,error,error_message,message}`,
`cloud.provider`, `cloud.account.{uid,name,type_id}`, `cloud.org.uid`,
`cloud.region`, `resources[].{uid,name,type,region,zone,group.{name,uid},namespace,cloud_partition}`,
`src_endpoint.ip`, `activity_id`, `activity_name`, `status_id`, `status`,
`http_request.user_agent`, `time`. The class's version range (§4.16) is
1.1.0, the floor, through 1.9.0, the ceiling: every field the normalizer
needs exists at 1.1.0; `resources[].zone` (from 1.5.0) and
`programmatic_credentials[].uid` (from 1.6.0) arrive above the floor and
are read when present, contributing nothing when absent; the two fields
deprecated inside the range (`cloud.project_uid` since 1.4.0,
`user.credential_uid` since 1.6.0) are read as fallbacks behind their
replacements. An
identifier that arrives as a JSON number is read only when it is an exactly
representable integer; a value JSON decoding has already rounded is not an
identifier and mints nothing.

Output. **No provider, no cloud identities:** API Activity also describes
non-cloud application APIs, and without `cloud.provider` there is no
namespace for a cloud identity, so an event with no provider yields only
its ObservedData (the operation, its outcome, `principal_name`) and the
source address — never a principal, an account or a resource that two
unrelated applications' coincident ids could share. With a provider:

- One `user-account` SCO for the principal. A cloud principal — a user, a
  role, a service principal, a service account — is a `user-account`, not a
  type of its own: STIX already carries `user_id`, `account_login`,
  `account_type`, `is_service_account` and `display_name`, and every verb,
  inspector and aliasing rule that takes a user takes it unchanged. Shaping
  (§7.4, the normalizer's contract): `account_type` is the provider slug
  (`aws`, `azure`, `gcp`), **not** `cloud` — `cloud` names the identity
  provider's sign-in account, which lives in another namespace and relates
  to the control-plane principal by an explicit `aliases` edge (§7.3).
  **Identity rests on the provider's durable principal identifier alone**,
  carried as `user_id` with an empty `account_login`: in a cloud control
  plane the unique identifier — the ARN, the directory object id, the
  service-account email — *is* the principal, and the human-readable name
  is a label (`display_name`), never identity. The durable identifier is
  `actor.user.uid`, else `actor.user.uid_alt`, for every provider; a
  provider row applies only where a provider's actor identifier is
  session-scoped, and today there is one: for `aws`, `actor.session.issuer`
  is preferred when present (the role a session was assumed from, so every
  session of one role is one principal). **No identifier, no principal**:
  an event carrying only a name, or only `actor.app_name`, mints no
  `user-account`; the name rides the ObservedData as `principal_name`. A
  name is never composed into an identity the source did not give.
  `is_service_account` is set for a role or a System/Service `type_id`; the
  credential id and the session ride as properties and extensions, never as
  identity.
- One `x-cloud-account` SCO for the account the call ran in, from
  `cloud.account.uid`, else `actor.user.account.uid`, else the deprecated
  `cloud.project_uid`; and, when `actor.user.account.uid` names a different
  account, a second `x-cloud-account` for the principal's home account (a
  cross-account call shows both). Identity `(provider, account_uid)` (§7.2).
- One `x-cloud-resource` SCO per `resources[]` entry that can be
  identified: by `(provider, uid)` when the source gives the provider's
  unique id, else by `(account_ref, type, region, zone, group, namespace,
  name)` under the event's account, else not at all (§7.2). The engine does no provider-specific
  parsing of an id; a resource identified by its unique id carries no
  `account_ref`, since the id is its own statement of ownership.
- `ipv4-addr` / `ipv6-addr` for `src_endpoint.ip`.
- STIX `Relationship` of type `belongs-to` (the STIX 2.1 vocabulary, adopted)
  from a resource to the event's account **only when the resource was
  identified under that account** (the name rule) — a resource named by a
  provider-unique id may be shared, managed by the provider, or another
  account's, and the engine does not claim an owner it cannot see; and from
  the principal to its own account, the one `actor.user.account.uid` names,
  never inferred from the account the call ran in.
- One `ObservedData` referencing all of the above. The operation is the
  observation: `operation`, `service`, `activity`, `status`, `error`,
  `error_message`, `response_code`, `request_uid`, `region`, `session`,
  `credential_uid`, `user_agent`, `app_name`, `principal_name` and
  `resource_count` ride in its extensions, the
  way §4.9 keeps the activity and the privilege. There is no relationship
  from principal to resource and no split per resource: one event is one
  observation, and the graph does not grow a relationship type per API verb.
- Edges as in §4.1.

Failure modes, all splits and never merges (with no name in the tuple and
no identifier composed by the engine, two principals can share an id only
when the provider gave them one): an `aws` source that puts the full
assumed-role identifier in `actor.user.uid` and names no issuer makes each
session of one role a distinct principal; a principal seen once with `uid`
and once with only `uid_alt` is two principals; a resource seen once by its
unique id and once by a bare name is two SCOs, as is one named once with
its region, zone, group or namespace and once without, or with its name's
or namespace's case varied by the source.
Each is the `x-host` kind of split and each is remedied by an `aliases`
edge, not by a rule change. Whether a control-plane caller that *is* the
identity provider's account (one directory serving both) should be shaped to stitch with its
sign-in is deferred until such a source exists; the provider rows are where
that goes.


### 4.16 OCSF schema versions: a floor, a ceiling, and feature detection

The engine pins no single OCSF version. Each normalizer is written against a
**version range** for its class (adr/0356).

- The **floor** is 1.1.0 for every class, since that is the version AWS
  Security Lake's native sources write. A class introduced later has its
  introducing version as its floor (1.4.0 for
  cloud_resources_inventory_info, §4.18). Each class's fixture declares its
  version in `metadata.version`.
- The **ceiling** is the newest published schema the normalizers were
  checked against, 1.9.0.
- Class numbers are OCSF's.
- The full OCSF schema for every release from 1.1.0 to 1.9.0 is checked in
  at `third_party/ocsf-schema/`, with OCSF's licence and notice.

Three checks run against these copies (adr/0356). `unmapped.*` passes all of
them, as it is OCSF's place for a source's own fields.

1. **Registration.** Every registered class number must exist in the ceiling
   copy, under the OCSF name the normalizer uses.
2. **Lookups.** The tests record every field path that a normalizer or an
   event-view template reads. Each recorded path must exist in the ceiling
   copy for its class. The check only sees the reads the tests make, so each
   normalizer's tests cover its fallbacks.
3. **Events.** An event is walked field by field against the copy for the
   version it declares in `metadata.version`. An event that declares no
   version fails.
   - **Fields.** Each field must be one that OCSF defines for the event's
     class at that version.
   - **Shape.** A list field must hold a list, an object field an object,
     and a scalar field a scalar. This catches a field written in the wrong
     shape, such as `privileges` as one string instead of a list.
   - **Scalar type.** A string field must hold a string, a boolean field a
     boolean, an integer or long field a whole number, and a float field a
     number. A derived type is checked as the type it is built on, which the
     schema's own type list gives. So a `timestamp_t` must be a whole
     number, and a `username_t` or an `ip_t` must be a string. A `json_t`
     field accepts anything. The format of a string is not checked.
   - **Required fields.** Every field the class itself requires must be
     present. Required fields inside objects are not checked. A field that
     belongs to a profile is required only when the event lists that profile
     in `metadata.profiles`.
   - **Enums.** An `_id` must be one of the schema's values, and its caption
     sibling (such as `activity_name`) must match that value's caption,
     letter case aside. The value Other (99) passes any caption, as that is
     where OCSF puts the source's own word. A caption with no `_id` beside
     it must be one of the enum's captions.
   - **The fixture walk.** A test walks every event under `fixtures/` this
     way. The `fixture_meta` block is the replay's own and is not walked.
   - **The adapter walk.** Each in-tree adapter's tests walk the events the
     adapter emits the same way. So an adapter cannot write a field, a shape
     or a type that OCSF does not define.

**Times.** An OCSF `timestamp_t` field, such as `time` or
`process.created_time`, holds epoch milliseconds as a whole number. OCSF's
datetime profile adds a twin with the same name and `_dt`, such as `time_dt`.
The twin holds the same instant as an RFC 3339 string.

- A normalizer, and the event view, reads the number first and the twin as a
  fallback.
- A string in the number's field is not OCSF, and is not read.
- Fixtures carry both, so the data stays readable. Where a version defines
  no twin for a field, the fixture carries the number alone.
- Adapters write the number. An adapter whose source gives the time as text
  writes the twin too.

OCSF reserves breaking changes for a
major version; within a major line an attribute is added as optional or
deprecated in place, and none of §4.15's fields was removed or made required
anywhere from 1.1.0 to 1.9.0 — which is what makes a range workable. The
rules, which §4.15 follows and every class is held to:

- **A version defines fields; a source fills a subset.** A source that
  writes a version need not fill every field the version defines: AWS
  Security Lake writes OCSF 1.1.0 API Activity without `status_id`
  (adr/0334). A field defined at the floor is therefore read with the same
  field detection as a field above it, and *requires* below means a field
  without which the normalizer cannot produce its output at all, named per
  class.
- **Read the floor, benefit from the ceiling.** Every field a normalizer
  *requires* exists at the floor. A field that arrives above the floor is
  read when present, and its absence changes nothing: an empty property, an
  empty tuple component. A floor-shaped event and a ceiling-shaped event of
  the same thing normalize to the same entities with the same ids; the
  ceiling event carries more properties.
- **Detect the field, not the version.** No normalizer branches on
  `metadata.version`. Producers backport attributes, mis-declare versions
  and mix versions in one feed; the presence of the attribute is the only
  reliable signal. The version stays on the stored OcsfEvent
  (`metadata.version` in the payload, reachable from the ObservedData's
  `derived-from` edge) for diagnostics; it is not copied into the
  observation and never drives shaping.
- **Dual-read across a deprecation.** When a version inside the range
  deprecates a field the floor still carries, the normalizer reads the
  replacement first and the deprecated field as the fallback, for as long
  as the floor is below the deprecating version. The class's ADR names each pair (§4.15:
  `programmatic_credentials[].uid` over `user.credential_uid`,
  `cloud.account.uid` over `cloud.project_uid`, `actor.application.name` over
  `actor.app_name`; §4.7: `job.job_actions[].cmd_line` over `job.cmd_line`).
- **Identity may admit an above-floor field, empty when absent.** A field
  that exists only above the floor may be identity-contributing (§7.2) when
  leaving it out would merge distinct entities. Its absence contributes an
  empty component, so two sightings that both omit it still meet, and a
  sighting that carries it splits from one that does not: a split of the
  `x-host` kind, named in the class's failure modes and repaired by
  `aliases`. The alternative — identity over floor fields only — is a merge
  no edge can undo once data is recorded against the shared id.
- **Test both ends.** A class's fixture is written at the floor and declares
  it in `metadata.version` (every scenario does, adr/0356); the normalizer's tests carry
  one ceiling-shaped event of the same entities and assert the same ids.
  Moving a floor — dropping the oldest source — is an ADR, because it
  changes which fallback reads may go.

### 4.17 OCSF inventory_info (class_uid 5001) → STIX

Input: an OCSF Device Inventory Info event with `time` and `device.*`: hostname,
domain, uid, uid_alt, mac, ip and os, plus agent_list and owner from 1.2.0.
OCSF defines no criticality on a device, so the source's criticality is read
from `unmapped.criticality`.

Output:

- `x-host` SCO for the device, shaped by the shared host extractor (§4
  preamble, §7.2). An inventory record that names the hostname beside the
  source's device id or the MAC also yields the weak host that identifier
  mints, outside `object_refs`, with an `aliases` edge to the strong host:
  this is the record through which a source's later activity named only by
  its device id joins the host (§7.3, adr/0211).
- `ipv4-addr` / `ipv6-addr` SCO for `device.ip` if present.
- One `ObservedData` referencing the host and address, its extensions
  carrying `os_name`, `owner_name` and `criticality` when present.
- Edges as in §4.1.

### 4.18 OCSF cloud_resources_inventory_info (class_uid 5023) → STIX

Input: an OCSF Cloud Resources Inventory Info event — a snapshot of one or
more cloud resources as they are now — with `cloud.{provider,account.{uid,
name,type_id},region,project_uid}`, `resources[].{uid,name,type,region,zone,
group,namespace,data,labels,tags,criticality,owner,created_time,
modified_time}`, `activity_id`, `activity_name`, `time`. The class first
exists at OCSF 1.4.0; `created_time` and `modified_time` arrive at 1.5.0.
Its version range (§4.16) runs from 1.4.0, the version that introduced it,
to 1.9.0 (adr/0356). The first source for the class is a configuration
adapter the engine ships, shaped to 1.7.0, and the bundled fixture declares
1.7.0. A test holds that a 1.4.0-shaped snapshot normalizes to the same ids
by field detection.

Output. The identity rules are §4.15's, unchanged, so a snapshot of a
resource and the calls against it meet on one node:

- **No provider, no cloud identities**: without `cloud.provider` the event
  yields its ObservedData only.
- One `x-cloud-account` from `cloud.account.uid`, else the deprecated
  `cloud.project_uid`; identity `(provider, account_uid)`.
- One `x-cloud-resource` per `resources[]` entry that can be identified, by
  `(provider, uid)` else the name-rule tuple under the event's account
  (§7.2); `belongs-to` to the account only under the name rule.
- One `user-account` for `resources[].owner` when it carries an identifier
  (`owner.uid`, else `owner.uid_alt`), shaped as a cloud principal (§4.15:
  provider slug as `account_type`, identifier as `user_id`, empty
  `account_login`, the name as `display_name`); the owner's name rides the
  observation as `owner_name` either way, as `principal_name` does in §4.15,
  and an owner without an identifier yields no SCO. No identifier is
  composed.
- One `ObservedData` referencing all of the above, carrying the
  configuration in its extensions: `activity`, `activity_id`, `region`,
  `resource_count`, and for the first resource `resource_uid`,
  `resource_name`, `type`, `configuration` (the resource's `data`, as
  decoded), `labels`, `tags`, `criticality`, `created_time`,
  `modified_time` (both written as RFC 3339, read from the field's epoch
  milliseconds, else from its `_dt` twin, §4.16), `owner_name`. The configuration is data the model reads
  through the event view, never an entity: a policy document is a property
  of the resource at a moment, and the moment is the observation. The schema
  version is not copied (§4.16).
- Edges as in §4.1.

The 1.7.0 `databucket`, `database`, `table`, `container` and `idp` objects
the class may carry instead of `resources[]` are not read: the fixture and
the first adapter carry the resource under `resources[]`, and a source that
fills only the typed objects is a row for the shaping table, not a new
normalizer. Failure modes are §4.15's; a snapshot and an activity event that
name the same resource by different identifiers (unique id in one, bare
name in the other) are the same split, remedied by `aliases`.

### 4.19 OCSF user_inventory (class_uid 5003) → STIX

Input: an OCSF User Inventory Info event, a directory's record of one user,
with `user.{uid, uid_alt, name, full_name, email_addr, domain, groups[].{name,
uid, domain}, org.{name, ou_name}, ldap_person.{job_title, employee_uid,
email_addrs, manager.uid}}`, `metadata.product.{vendor_name, name}`,
`activity_id`, `activity_name`, `time`, and, under `unmapped` (OCSF carries no
attribute for them), the on-premises identifiers a cloud directory holds for
a synced user: `on_premises_security_identifier`,
`on_premises_sam_account_name`, `on_premises_domain_name`, and
`account_enabled`. `user.uid` is the directory's own id for the user (an
object id, an object GUID, a user id, or a SID for an on-premises directory
keyed by it). The domain family's SID is `user.uid` when it is one, else
`user.uid_alt` when it has the SID form, else the unmapped field. Its
version range (§4.16) is 1.1.0 to 1.9.0: the bundled fixture declares 1.1.0;
every field identity reads (`user.uid`, `uid_alt`, `name`, `domain`,
`email_addr`) and `unmapped` are present at the floor, and `ldap_person`, read
only for properties, is read when present.

Output. This is the only normalizer that mints a person (adr/0324); every
other class names accounts only, and no other observation's `object_refs`
holds a person.

- One `identity` with `identity_class: individual`, the person, keyed
  `(directory, directory_id)` (§7.2): the directory slug is derived from
  `metadata.product`, the product name, or with none the vendor name, matched
  whole against known directories (`entra` for Entra ID or Azure Active
  Directory, `okta`, `google` for Google Workspace or Cloud Identity, `ad`
  for an on-premises Active Directory, `workday`); any other product by its
  full name, so a managed domain service that names a provider is its own
  directory; with no product name and an unknown vendor, the vendor name,
  else the source tool; lowercased, spaces as dashes. The id is `user.uid`.
  Properties: `name` and `display_name` (`full_name`, else the
  principal name), `mail` (`email_addr`, else the first
  `ldap_person.email_addrs`), `principal_name`, `department`
  (`org.ou_name`), `organization`, `title` (`ldap_person.job_title`),
  `employee_id` (`ldap_person.employee_uid`), `manager_ref` (the person
  `ldap_person.manager.uid` names in the same directory), `enabled`,
  `directory`, `directory_id`. No uid or a placeholder uid mints no person.
- The accounts the record names, each by the account rule (§7.2): the
  domain account family, the SID when the record carries one, the principal
  name, `domain\sam` when the sAMAccountName and its domain are present,
  strongest first, the lesser aliased to the strongest outside `object_refs`;
  and, for any directory but an on-premises one, the directory's sign-in
  account, `cloud`, keyed on the directory's id, with the sign-in name's
  account aliased to it. A personnel system (`workday`) names a person and no
  account. The cloud and domain accounts are not aliased to each other: they
  are two accounts of one person, joined by the person.
- `belongs-to` from each account the record minted to the person, DIRECT,
  with the record's provenance.
- `member-of-group` from the sign-in account (the domain account's strongest
  when there is none) to an `x-group` per `groups[]` entry, the directory
  `group.domain` else the slug, as §4.9 records memberships.
- One `ObservedData` referencing the person, the strongest account of each
  family and the groups; the activity rides its extensions. The schema
  version is not copied (§4.16).
- Edges as in §4.1.

Version 2, as §4.17: a record stored before the normalizer existed went
through the opaque default at version 1. Failure modes: two directories that
describe one human are two persons, linked by `aliases` from a record or a
person; a directory record that omits the on-premises identifiers leaves the
domain accounts unowned until one names them or a person asserts the owner.

---

## 5. Extension Mechanism

### 5.1 Adding a new verb

Three artifacts:

1. A `CapabilityDescriptor` (Go interface implementation) declaring the verb
   name, input schema, output type, and intent description for the LLM.
2. At least one binding in tenant config mapping it to an existing adapter
   operation (or a new one).
3. Optional: a normalizer if the verb returns OCSF classes not yet handled.

The verb appears in `list_capabilities` automatically once the descriptor is
registered and at least one binding exists. The agent loop fetches the
descriptor list at session start and constructs the LLM tool definitions from
it. There is no hard-coded tool list anywhere.

### 5.2 Adding a new tool integration

Four artifacts:

1. An `Adapter` implementation: connects, authenticates, exposes named
   operations, returns raw responses with `class_uid` and `class_name`
   populated. The adapter is responsible for translating the tool's response
   format into OCSF-shaped JSON before handing to the OcsfEvent writer.
2. Adapter config schema (added to the YAML).
3. Bindings in tenant config for whatever verbs the adapter can fulfill.
4. New normalizers only if the adapter emits OCSF classes not yet handled.
5. For an adapter that queries a store it does not own (`11 §4.4`): a recorded
   profile per supported source version and a live-check runbook.

The contract: an adapter's job ends at producing a well-formed OCSF event. It
does not produce STIX. That separation is what lets normalizers be reused across
adapters and what makes vendor swap a config change.

### 5.3 SDK shape

Go backend, so:

```go
type Adapter interface {
    Name() string
    Class() AdapterClass // MCP | NATIVE_API | CUSTOM | SOAR_PLAYBOOK | FIXTURE
    SupportedOperations() []string
    Invoke(ctx context.Context, operation string, params map[string]any) (AdapterResponse, error)
    Health() HealthStatus // {Healthy, Message, Degraded []{Operation, Capability, Reason}} (11 §4.4)
}

type CapabilityDescriptor interface {
    Verb() string
    Inputs() InputSchema
    Output() OutputType
    Intent() string
}

type Normalizer interface {
    ClassUID() int
    Version() int
    Normalize(event OcsfEvent) (NormalizationResult, error)
}
```

All three are registered with the capability registry rather than discovered by
reflection — Go has no `ServiceLoader`/JAR equivalent. In-tree adapters
self-register from their package `init`; out-of-process adapters (the
transport-neutral path, see §5.4) are declared in tenant config and reached over
their protocol. Either way an extension is a new package or a config entry, not
a change to the core dispatch. The out-of-process packaging, discovery, and
enablement model — process-per-adapter over JSON-RPC/stdio, manifest +
`describe` handshake, explicit per-op enablement — is specified in
`11-adapter-plugins.md`; the semantic contract above is unchanged by it.

### 5.4 Adapter classes

Adapters fall into five classes. The contract is identical across all of them;
the class is metadata for operators (visible in `list_capabilities` health
output) and a hook for shared infrastructure (e.g., MCP adapters share
connection pooling and protocol handling that native-API adapters don't need;
SOAR_PLAYBOOK adapters share the async-callback pattern).

**MCP adapters.** Wrap an MCP server. The adapter's `invoke` translates a
capability operation to an MCP tool call, awaits the response, and translates
back to OCSF. One MCP adapter instance per MCP server. Preferred where mature
MCP servers exist for the target tool, because the protocol handles auth flows,
schema discovery, and streaming uniformly.

**Native API adapters.** Talk directly to a vendor's REST/GraphQL/gRPC API
using the vendor SDK or a hand-rolled HTTP client. This is how most v0
integrations will look in practice — CrowdStrike Falcon API, Microsoft Graph,
Splunk REST, Okta API. The adapter owns auth, retries, pagination, and the
vendor-to-OCSF translation. More integration code per adapter than MCP, but
fewer assumptions about external server quality.

**Custom adapters.** For homegrown tools, internal services, data lake queries,
file-based sources, Kafka consumers, anything else. Same `Adapter` contract,
arbitrary implementation. A custom adapter for an internal CMDB is a thin REST
client; a custom adapter for a Snowflake-backed data lake is a SQL executor. As
long as it produces OCSF, it fits.

**SOAR_PLAYBOOK adapters.** Delegate write-side actions to an external SOAR
orchestrator (Tines, Torq, Splunk SOAR, customer-authored workflows). The
adapter's `invoke` translates an action dispatch to a playbook invocation
(typically a signed webhook POST), records the playbook run id as the
`adapter_request_id`, and either awaits a synchronous return or registers a
callback URL for the orchestrator to hit on completion. The async-callback
pattern is the common case — playbooks routinely take minutes to hours, and
the Temporal `ActionLifecycle` workflow holds the awaiting state. From the
authorization layer's perspective the dispatch is one step; from the audit
perspective the chain is truncated at the orchestrator boundary (see 04 §6.1
`audit_depth: EXTERNAL`).

Why a distinct class rather than "another custom adapter":
- The async-callback pattern is shared (the resolver and `ActionLifecycle`
  coordinate with the orchestrator's notification webhook).
- Approval-gate ownership and reversibility may shift per binding (04 §5.7,
  §7) — those fields are SOAR-specific binding-manifest metadata.
- Operators want to see at a glance which actions delegate vs run native; the
  class label surfaces that in `list_capabilities` and in action review
  panels.

SOAR_PLAYBOOK adapters do not change the layer's pure-I/O property — invoking
a playbook is one more outbound call shape, with normalization done on the
orchestrator's response just like any other adapter. The orchestrator runs
the playbook; reckon records its inputs and outcomes.

**Fixture adapters.** A special case of custom adapter for v0, replay, and
testing. See §9. v0 ships fixture SOAR_PLAYBOOK bindings (a "fake Tines"
that returns canned outcomes) so the SOAR delegation path can be exercised
end-to-end without a real orchestrator.

The OCSF-shaping responsibility lives in the adapter regardless of class. That's
the invariant that makes the layer transport-neutral: by the time data crosses
the adapter boundary, it's OCSF, full stop.

---

## 6. Edge Cases and Graceful Degradation

The capability layer's contract with the LLM on degradation is **structured,
not prose-based**. The agent loop branches on the `coverage` enum from
`CapabilityResult`; `degradation_notes` carries human-readable detail but is
never load-bearing for control flow.

### 6.1 Coverage classification

Every `CapabilityResult` carries one of five `Coverage` values:

**`COMPLETE`** — All eligible bindings were invoked and succeeded. The result
represents the verb's full possible coverage in this tenant. An empty result
under `COMPLETE` is meaningful: it is evidence-of-absence within the queried
scope.

**`PARTIAL`** — At least one binding succeeded and at least one failed. Only
applies to fan-out verbs (§3.4) where multiple bindings legitimately
contribute. The agent loop should treat results as incomplete; the
degradation notes name which sources are missing.

**`UNAVAILABLE_TENANT`** — No binding for this verb is configured in this
tenant, or all bindings were rejected as not-applicable to the input
(required template paths missing — see §3.3.4). This is structural and will
not change without config edits or different input. The agent loop should
not retry; it should either route around the verb or escalate to the
analyst.

**`UNAVAILABLE_TRANSIENT`** — Bindings exist and would have been applicable,
but every adapter is currently unhealthy (auth expired, rate-limited,
network unreachable). This is recoverable. The agent loop may retry the
verb later in the same session, or surface to the analyst that the relevant
tool is offline.

A call an adapter declines because its store cannot express it (`11 §4.4`)
answers FALLTHROUGH, which the walk counts as a failed binding; its empty answer
is therefore never `COMPLETE` and never a citeable absence (adr/0061).

**`FAILED`** — Bindings exist, adapters were healthy at call time, but every
attempt failed at the call site (vendor returned errors, schema mismatch,
query timeout). Distinct from transient because the layer cannot tell
whether a retry would succeed. Treated as recoverable but with lower
expectation than transient.

The classification is computed by the resolver from per-binding outcomes:

```text
all bindings succeeded             -> COMPLETE
some succeeded, some failed         -> PARTIAL    (fan-out only)
no bindings applicable to input     -> UNAVAILABLE_TENANT
no bindings configured              -> UNAVAILABLE_TENANT
all applicable bindings unhealthy   -> UNAVAILABLE_TRANSIENT
all applicable bindings called and  -> FAILED
  errored at the call site
```

For non-fan-out verbs, "succeeded" means the highest-priority applicable
binding succeeded; lower-priority bindings are not invoked. There is no
`PARTIAL` for non-fan-out verbs — either the chosen binding succeeded
(`COMPLETE`) or fall-through exhausted (`FAILED` or `UNAVAILABLE_*`).

### 6.2 Adapter error classification

Adapters classify every error as one of:

**`RETRY`** — Transient, same call might succeed if repeated. Rate limits,
network blips, vendor 5xx. The resolver does not retry within a single
capability call; it propagates to the resolver for fall-through.

**`FALLTHROUGH`** — This binding cannot service this call, but another might.
Auth scope insufficient, vendor returns "no data for this device,"
schema-version mismatch. The resolver moves to the next priority binding.

**`UNHEALTHY`** — The adapter as a whole is currently unusable. Auth
credentials invalid, vendor API entirely unreachable, certificate expired.
Marks the adapter unhealthy in `health()` until re-tested. The resolver
treats remaining bindings on this adapter as unavailable for the rest of
this call.

**`FATAL`** — Continuing would corrupt state or violate an invariant. Schema
violation in a write path, identity-rule failure, persistence layer
unreachable. Propagated as a thrown exception. `FATAL` errors should be
rare and represent bugs, not operational conditions.

### 6.3 The `list_capabilities` escape hatch

When the agent loop initializes, it calls `list_capabilities` and trims the
LLM tool set to verbs whose tenant configuration would resolve to at least
one currently-healthy binding. The LLM never sees verbs it cannot use. A verb
with limitations (§2.8) is still advertised: it answers the calls its stores
can express. Each limitation names the adapter that reports it. For a verb that
walks its bindings in priority order, the listing reports the highest-priority
healthy binding's limitations; a call that binding is not applicable to, or that
it declines, may still be answered by a later one, and probing stops there, so a fallback
adapter is not started just to be listed. For a fan-out verb (§3.4), which
invokes every binding, the listing reports every healthy binding's limitations:
a call one of them cannot express comes back `PARTIAL`.

When `coverage = UNAVAILABLE_TRANSIENT` or `coverage = FAILED` arrives
mid-session, the agent loop may re-call `list_capabilities` to refresh its
view and trim further. `UNAVAILABLE_TENANT` does not warrant re-listing —
tenant config doesn't change mid-session.

### 6.4 Empty-result semantics

An empty `observed_data_refs` list combined with `coverage = COMPLETE` is
evidence-of-absence: the queried tools were healthy, the query ran, and
nothing matched. The agent loop should treat this as a real signal — a
hypothesis predicting activity in this scope is weakened.

An empty `observed_data_refs` combined with any non-COMPLETE coverage means
absence-of-capability: we did not look, or did not look fully. The agent
loop must not draw inferences about whether the underlying activity
occurred.

The distinction within a single COMPLETE result between "filter was too
narrow" and "telemetry was not available for the time window" is left to
prose in `degradation_notes` and to analyst judgment. Both have the same
operational consequence — widen the filter, widen the window, or accept the
negative result — and the LLM does not need a structured signal to navigate
between them.

### 6.5 Optional richer signals from adapters

Adapters whose underlying tools expose richer coverage information (e.g.,
"Falcon sensor was offline for 4 hours of the requested 24-hour window")
**should** surface this in `degradation_notes` when the call otherwise
succeeds (`coverage = COMPLETE`). This is best-effort and not part of the
structural contract; the agent loop must handle its absence. Tools that
don't expose such signals don't get to fake them — silence on coverage
gaps within a window is the default.

---

## 7. Identity Computation

Identity is the most consequential single decision in the normalizer because it
determines whether the same `8.8.8.8` from CrowdStrike and from Splunk produces
the same SCO id (allowing graph stitching) or two distinct ids (fragmenting the
investigation).

### 7.1 Rule

Identity follows STIX 2.1 deterministic UUIDv5 rules with one structural
deviation: identity is **tenant-scoped**. The format is `<type>--<uuidv5>`.
The UUIDv5 is computed over a canonical-form JSON of the SCO's
identity-contributing fields, using a **per-tenant namespace UUID** — not
the global STIX 2.1 namespace `00abedb4-aa42-466c-9c01-fed23315a9b7` and
not a project-wide custom-object namespace.

Each tenant is assigned a fresh namespace UUIDv4 at tenant creation, stored
in the tenant CRUD record (02-persistence.md §1), and immutable thereafter.
Both standard SCOs (`ipv4-addr`, `domain-name`, `file`, etc.) and custom
SCOs (`x-host`, `x-scheduled-task`, `x-group`) use the
same per-tenant namespace; there is no separate custom-object namespace.

This makes same-value-different-tenant collision impossible: `8.8.8.8` in
tenant A and `8.8.8.8` in tenant B produce different `ipv4-addr--<uuid>`
values. The `IdentityResolver` (§7.4) takes a tenant id as input, looks up
the namespace UUID, and computes the id; it cannot compute a cross-tenant
or "global" id. Cross-tenant indicator sharing, when introduced, will be a
deliberate publish-to-pool action with its own (separate) namespace UUID;
deferred from v0.

**ObservedData id rule.** ObservedData ids are deterministic UUIDv5 within
the tenant namespace, computed from
`(class_uid, time_truncated_to_second, source_tool, normalizer_version, content_hash(payload))`.
Two adapters observing the same OCSF event in the same tenant — normalized at
the same normalizer version — produce the same ObservedData id, supporting
cross-investigation deduplication within that tenant. Re-normalization with a
newer normalizer version produces a *new* ObservedData with a different id
precisely because the version participates in the id computation (it is also
recorded in provenance; provenance alone could not change the id). Random UUIDv4
ObservedData is also permitted for cases where deterministic identity is
undesirable (e.g., one-off opaque enrichment results); the resolver records
which mode was used in provenance.

The deviation from strict STIX 2.1 is documented as a deliberate trade —
STIX is the *vocabulary* for the interpretation layer (01-domain-model.md
ARCHITECTURAL COMMITMENTS), not the *wire format*. STIX-conformant ids
remain available if the system ever needs to publish to a STIX-conformant
external consumer; that conversion happens at the export boundary, not in
the storage layer.

### 7.2 Per-type identity-contributing fields

These follow STIX 2.1 spec for native SCOs and are documented here for the
custom and composite cases. Four of them deliberately deviate from strict
spec, and each is described below: `process`, `email-addr`, `user-account`
and `windows-registry-key`. All identity tuples below describe what enters the
canonical-form JSON; the JSON is then hashed against the per-tenant
namespace UUID per §7.1.

**Standard cases (no deviation):**

- **`ipv4-addr` / `ipv6-addr`**: `value` (the address string, lowercase, no
  leading zeros).
- **`domain-name`**: `value` (FQDN, lowercase, trailing dot stripped, IDN
  punycoded).
- **`url`**: `value` (full URL, scheme lowercased, host lowercased, default
  ports stripped, fragment preserved, query-string keys sorted).
- **`file`**: hashes in priority order — `SHA-256` if present, else `SHA-1`,
  else `MD5`, else `(name, parent_directory_ref.value, size)`. Hash strings
  lowercased. The selected algorithm participates in identity alongside its
  digest (the canonical JSON carries the `{algorithm: digest}` pair, mirroring
  STIX 2.1's `hashes` contributing property), so equal digest strings under
  different algorithms can never merge two files.
- **`x-host`** (custom): `(hostname)`, the short hostname — trimmed,
  lowercased, a trailing dot removed, cut at the first dot — so `WIN-FILE01`,
  `win-file01` and `win-file01.corp.example` are one host (adr/0211). Only
  when a record carries no hostname is the host minted on a weak identifier:
  `(mac_address)` lowercased and colon-separated, else
  `(source_tool, device_uid)`; such a host carries `weak: true` and never
  joins another on its own. The DNS domain (the FQDN's remainder),
  `device.domain`, `device.uid`, `device.uid_alt`, the agent ids and the MAC
  are properties of the host, never identity; the device and agent ids are
  kept keyed by the source that reported them. Two machines with one short
  name in two domains share an id, which the record shows rather than guesses
  (§7.3).
- **`x-scheduled-task`** (custom): `(host_ref.id, name, path)`, name and path
  lowercased.
- **`x-group`** (custom): `(directory_ref.value, group_name)` lowercased.
- **`x-cloud-account`** (custom): `(provider, account_uid)`, provider
  lowercased; the uid as the provider issues it (a GUID-shaped uid
  lowercased).
- **`x-cloud-resource`** (custom): priority order — `(provider, uid)` when
  the provider's unique resource id is present (unique within the provider
  and already carrying its account; folded as the account uid is), else
  `(account_ref.id, type, region, zone, group, namespace, name)` under the
  event's account — every parent scope OCSF's resource object can express:
  type, region, the availability zone (`zone`, an above-floor field under
  §4.16, absent below 1.5.0 and so empty for a floor-version source) and
  the resource group (`group.name`, else `group.uid`) folded for case
  (closed or case-insensitive vocabularies), the `namespace` and the **name
  with their case preserved** — OCSF's `namespace` is a generic string with
  no case rule and resource names are case-sensitive for several resource
  types, and the resolver folds only where a vocabulary is
  case-insensitive — each scope empty when the source carries none; else
  the resource is not minted.
  For a source whose uniqueness scope lies outside those fields the unique
  id is the only strong identity. The `x-host` pattern: strongest
  identifier first, no parsing of the id.

**Deviation: `process`.** STIX 2.1 specifies no `id_contributing_properties`
for `process`, intentionally — the spec authors made the call that PIDs are
reused across reboots and within uptime windows, the `(host, pid)` tuple isn't
genuinely unique over time, and processes are inherently transient
observations rather than persistent entities. Strict STIX therefore assigns
every `process` SCO a random UUIDv4 and does not stitch across tools.

We deviate. Identity is
`(host_ref.id, pid, created_time_truncated_to_second)`. Cross-tool process
stitching is a load-bearing operation in this product — every multi-tool
investigation pivots on processes constantly, and `get_process_ancestry`,
`x-supports`/`x-refutes` evidence weighting, and the ObservedData cache all
depend on the same process producing the same SCO id across tool calls.
Without deterministic identity, the graph fragments at exactly the layer
analysts reason about most.

Failure modes:
- If `created_time` is unavailable from any tool reporting the process, the
  process is treated as a transient anonymous SCO with a random UUID. This
  matches strict STIX behavior — no stitching, but no false merges either.
- If two tools report the same process with `created_time` values that
  disagree by more than one second, two SCOs are produced for one process —
  a false split. Detectable: the resolver flags overlapping OcsfEvents on the
  same `(host, pid)` within a configurable window via a `Note` for analyst
  review.
- If a PID is reused after reboot at exactly the same one-second bucket, two
  distinct processes collapse to one SCO — a false merge. Vanishingly
  unlikely given clock skew between reboot and process spawn.

The deviation is reversible: switching to strict STIX is a normalizer change
with no domain-model impact.

**Deviation: `email-addr`.** STIX 2.1 follows RFC 5321, which permits the
localpart of an email address to be case-sensitive. We lowercase the localpart
unconditionally for identity computation. The RFC's case-sensitivity provision
is a theoretical property; real-world email infrastructure — SMTP servers,
IdPs, mail security tools, directory services — is uniformly case-insensitive
in practice, and treating `Alice@contoso.com` and `alice@contoso.com` as
distinct entities would fragment investigations across every email-touching
telemetry source. Cross-tool identity stitching is the load-bearing
requirement; the stitching requirement takes precedence over RFC literalism.

Failure mode: a mail system that genuinely treats localpart as case-sensitive
(vanishingly rare in enterprise contexts) would have two distinct mailboxes
collapse to one SCO. We accept this. The deviation is reversible at the
normalizer level if a future deployment context requires it.

Identity rule: `value` (full address with localpart and domain both
lowercased, trailing whitespace stripped).

**Deviation: `user-account`.** An account is identified by what its source
repeats, per `account_type` (adr/0324):

- `windows-domain`: the SID when the record carries one; else the principal
  name (`user@domain`, lowercased) when that is what the record carries;
  else `domain\sam` lowercased. A record carrying the SID beside a login also
  mints the login's account, outside the observation's `object_refs`, with
  `aliases` from it to the SID's account: the source said in that record
  that this login is this SID. A principal name from a directory source is a
  directory account, not a cloud sign-in.
- `windows`: a login with no domain and no SID from a Windows source is a
  weak account keyed on the bare login; it joins a domain account only when
  a record names both.
- `windows-local`: scoped by the host, `(host_ref, sam)`; the well-known
  principals are per-host identities by SID under this type, never persons
  and never placeholders: the system, local service and network service
  accounts, the built-in groups, the other fixed single-authority principals,
  service, application pool, application package, window manager and font
  driver host identities, and the mandatory labels, whose SIDs are computed
  the same on every machine. Domain SIDs (`S-1-5-21-…`) are never among them.
- `unix`: scoped by the host, `(host_ref, name)`. `root` on two hosts is two
  accounts. An explicit Unix signal, the account type or the device's
  operating system, outranks a domain name on the record; a SID outranks
  both, since nothing but Windows issues one.
- `local`: a bare login from a source that names no operating system, scoped
  by the host like `unix`.
- `login`: a bare login from a source that names neither an operating system
  nor a host (a network access record, for instance) is a weak account keyed
  on the login alone, the counterpart of the host's source-id tier; it joins
  nothing by itself and the event keeps its actor. A global key for a
  host-scoped login would make one account of every machine's same-named
  local account, which is why `local` and `unix` need the host.
- `cloud`: the identity provider's sign-in account, keyed on the provider's
  user id when the record carries it, else the sign-in name lowercased. A
  record is a sign-in record when its account type says so, or when its
  source is an identity provider by product or vendor name and it carries
  neither a SID nor a `domain\sam` login; a domain service a cloud provider
  runs is a domain, and a provider's host agent that names an operating
  system is a host's source, not a sign-in.
- Control-plane principals: below, unchanged.

`user_id` is a property, not part of the identity, of an account whose type
keys on a login; the SID's account carries it and the login's account aliases
to it from the record that named both. A placeholder login or identifier
(§7.4) reads as absent and the account falls to its next key or mints nothing.
`account_login`, principal names and SIDs are lowercased; a directory's or
provider's user id is lowercased when it is a GUID and otherwise kept as
given, the same fold the person's identifier uses, so one identifier folds
one way wherever it appears (control-plane principals keep adr/0320's fold).
The lowercasing is a deviation from strict STIX, which preserves case in
account_login. Same rationale as email-addr: Windows is case-insensitive in
account names, AD is case-insensitive, Entra is case-insensitive, and Unix,
while case-sensitive in principle, overwhelmingly uses lowercase account
names in enterprise practice. Treating `CONTOSO\Alice` and `contoso\alice` as
different accounts fragments every authentication-touching investigation.

Failure mode: a Unix host with two accounts differing only by case would
have them collapse to one SCO. Operationally indistinguishable from a
misconfiguration in most environments. Reversible at the normalizer level.

Note that `account_type` does **not** lowercase-collapse: an identity
provider's sign-in account (`cloud`) and a domain account (`windows-domain`)
for the same human are deliberately distinct SCOs even when the login
matches. They are, in fact, different accounts. What joins them is the
person they belong to (§7.3), by the directory record that names both or by
an analyst's assertion, never an implicit merge.

Cloud control-plane principals (§4.15) use the provider as `account_type`
(`aws`, `azure`, `gcp`), the provider's durable principal identifier as
`user_id` (`actor.user.uid`, with a per-provider row where the actor
identifier is session-scoped: on `aws` the session issuer, so every session
of one role is one principal) and an **empty** `account_login`: the
identifier is the principal and the name is a label. An event with no
identifier mints no principal. The identity provider's sign-in account for
the same human stays `cloud`; the two are different accounts and, when
known to be one person, are linked by `aliases`.

**Deviation: `windows-registry-key`.** STIX 2.1 builds the id from `key`
and `values`, with no host. Here identity is
`(host_ref.id, key, value_name)` (adr/0341).

- **Host-scoped.** A registry key is part of a host's state, so one path on
  two hosts is two entities. The host is kept in the identity tuple and in
  the observation's `object_refs`, as for `process`. It is never a property
  of the object.
- **`key` is the expanded key (§4.6) in lower case**, because registry key
  names are not case sensitive. So `HKLM\Software\Run` and
  `HKEY_LOCAL_MACHINE\SOFTWARE\Run` are one key.
- **`value_name` is in lower case**, because value names are not case
  sensitive either.
- **`value_name` is left out when the event names no value.** It is the
  empty string for the key's default value. A value with no name but
  `is_default` true is the default value. So a key and its default value
  are two entities.
- **The value's data and type are not part of identity.** A value whose
  data changes is still the same value, and the observation records the
  change.

Failure mode: a source might name a key by a root outside the expanded set
(§4.6). That key does not join the same key named by its hive.

### 7.3 Cross-tool stitching: practical implications

All cross-tool stitching is scoped to a single tenant by construction (§7.1):
the same `8.8.8.8` from tenant A's CrowdStrike and tenant A's Splunk produce
the same `ipv4-addr` SCO; the same `8.8.8.8` from tenant B's CrowdStrike
produces a *different* SCO. Stitching cannot leak across tenants.

Within a tenant a host is keyed on its short hostname (§7.2), and the key
fails in two directions; both are handled by explicit edges and facts, never
by a merge or a judgement in this layer (adr/0211).

*Under-merge*: a source that names a machine only by its own agent or device
id mints a weak host the hostname cannot join. When one record carries the
hostname and a MAC or a device id, or no hostname but a MAC and a device id,
the normalizer mints the weak host each lesser identifier alone would have
minted, outside the observation's `object_refs`, and emits `aliases` from it
to the host the record minted, DIRECT, with the record's provenance: the
source said in that record that its id names that host.
Reads by entity (the events filter, the window around an event, the pivot,
the appearances across investigations) expand the asked-for host to its
alias set: live `aliases` edges join and a live `distinct-from` pair is
never crossed; a host reachable from both ends of a split, each end reached
without passing through the other, is contested and reads alone; every other
host reads with the component it reaches through uncontested hosts, bounded
in depth (adr/0211). An event the
source later attributes to its id alone lands on the weak host and reads
under the strong one. Nothing is re-minted; withdrawing an alias splits the
reads again with no data to repair.

*Over-merge*: two machines with one short name share an id. The resolver
emits no note; the host's identity view lists what the record has seen under
the id, computed at read time from the device objects of the events that
reference it: the distinct domains, the agent and device ids per source, the
addresses. One hostname under two domains reads as seen under two domains,
and the analyst decides. Two hosts the record did not join are joined by an
analyst's `aliases`; two it wrongly joined or two the analyst knows apart are
separated by `distinct-from` (01-domain-model.md edge table). Both are human
acts, recorded with who and when, and withdrawn by marking, not deleting.

For users: an identity provider's `user-account` (account_type `cloud`) and
a domain `user-account` (account_type `windows-domain`) for the same human
will *not* stitch automatically: they have different `account_type`. This is
correct. They are different accounts. What they share is the **person**, an
`identity` with `identity_class: individual` (01-domain-model.md, adopted),
keyed by the durable identifier its directory gives it and scoped by that
directory. A person is minted only by the user inventory normalizer from a
directory's user record, or asserted by an analyst, never inferred from a
shared address or name. The record that names the person also names the
accounts, so it emits `belongs-to` from each account to the person and the
`aliases` among the accounts that §7.2's rule yields, DIRECT, with the
record's provenance; an analyst asserts an owner, or that an account is not
a person's (a `distinct-from` between the account and the person, the one
pair of unlike types the edge joins, which cuts a directory's ownership the
way a split cuts an alias), and withdraws either with who, when and a note.
The cut and the owner assertion have one scope, the account and every
account aliased to it, except an account the person owns by its own record
that was not split as well; the cut holds against the persons the cut person
is aliased to; one person holds a live owner assertion over that scope at a
time, an owner assertion over a live split is refused until the split is
withdrawn, and a split between two persons or the withdrawal of their join is
refused while it would leave an account asserted to two persons. Events reference accounts, never the person: the record says
an account acted and that the account belongs to a person, and the reads walk
account to owner, visibly. An investigation seeded on a person reads its
aliased persons, their owned accounts and each account's alias set; one
seeded on an account reads that account's alias set alone. Two directories
describing one human are two persons until a record or an analyst joins
them; a local, service, shared or machine account has no person unless one
says so (adr/0324).

The same rule separates a control-plane principal from a sign-in account:
an `aws` `user-account` (the role or user a call ran as, §4.15) and the
`cloud` `user-account` that signed in to the identity provider are
different accounts in different namespaces, and nothing merges them. A
resource seen by its unique id and again by a bare name is two
`x-cloud-resource` SCOs for the same reason a host seen by asset id and by
hostname is two `x-host` SCOs; `aliases` is the remedy in both cases.

### 7.4 Computation location

Identity is computed inside the normalizer, in a dedicated `IdentityResolver`
component. Adapters never compute STIX ids. This is a hard rule: it ensures
that two adapters can produce conflicting raw fields and the normalizer is the
single arbiter of how those become identity. The `IdentityResolver` takes a
tenant id as input, looks up the tenant's namespace UUID (cached), and
computes the deterministic id; it cannot produce a global / cross-tenant id.
The resolver is pure (given the same tenant + inputs, always produces the
same id), deterministic, and stateless; it can be unit-tested exhaustively
against fixtures.

Division of labor within that rule: the `IdentityResolver` applies only the
*folding* described in §7.2 — case, whitespace, and format canonicalization —
and refuses a *placeholder*: a value a source emits when it has none, which
names nothing and would otherwise mint one entity for every record that
carries it (an all-zero or all-one MAC, an unspecified, loopback or broadcast
address, `localhost` or a dash as a hostname, the nil GUID as a device,
user or person id, the hash of the empty file, the anonymous and nobody SIDs
and a login that names no one), and a value that does not parse as its type.
The well-known per-host principals are not placeholders: they name an
account, only never a person. The list is built in, short, and per type; a
placeholder field is treated as absent, so a host or an account falls to its
next key, an address mints nothing, and a
hash is passed over for the next one or for the file's name and path
(adr/0211). Per-type *shaping* of inputs (composing `account_login` as `domain\user`
versus an email address versus a bare Unix username, choosing which host
identifier tier is available, selecting the strongest file hash) happens in
the normalizer before the resolver is called, because only the normalizer
sees the vendor fields required to shape correctly. Two normalizers that
shape the same logical account differently will fragment identity — the
shaping rules in §7.2 are therefore part of the normalizer contract, not an
internal detail of the resolver.

---

## 8. Caching

### 8.1 Two distinct caches

The OcsfEvent immutability invariant lets us cache aggressively at the raw
layer, while ObservedData freshness is bounded by analyst expectations. So
there are two caches with different policies.

**Raw response cache (per adapter, per operation, per parameter set).** Key:
`hash(adapter, operation, normalized_params, tenant_id)`. Value: the raw
response. TTL: configurable per verb in tenant config, default 15 minutes for
telemetry queries, 1 hour for IdP/CMDB context, no TTL for hash lookups (a hash
is a hash). Eviction: LRU by size. Invalidation: explicit only — there is no
automatic invalidation, because the underlying OcsfEvent is immutable by
definition. If the analyst wants fresh data, they pass `bypass_cache: true` on
the capability call (exposed as a param the LLM can set, not a separate verb).

**ObservedData cache (per investigation, per capability call).** Key:
`hash(verb, normalized_params, investigation_id)`. Value: list of ObservedData
ids (just the ids — actual nodes live in the graph store). TTL: bounded by
investigation lifecycle; cleared on investigation conclusion or archive.
Purpose: prevent the agent loop from re-issuing the same
`get_process_ancestry(P)` three times in one session and cluttering the graph
with duplicate ObservedData.

### 8.2 Cache key normalization

`normalized_params` means params after canonicalization: time windows rounded
to a configurable granularity (default 1 minute), entity references resolved to
STIX ids, optional fields with default values made explicit, sort orders
applied consistently. Without normalization,
`{from: "10:00:00.123Z", to: "11:00:00.456Z"}` and
`{to: "11:00:01Z", from: "10:00:00Z"}` would miss the cache despite being the
same intent.

### 8.3 What does NOT get cached

`query_logs` with raw user-supplied query text bypasses both caches by default.
The query text is too high-cardinality and too potentially sensitive to cache
without explicit opt-in. The LLM can opt in by setting `cacheable: true` on the
QuerySpec when it's confident the query is deterministic and side-effect-free.

### 8.4 Cache and replay

Cache is a runtime performance optimization, not part of the persisted state.
Replay (walking the investigation event stream to reconstruct state — see
02-persistence.md §2.1) bypasses both caches by design. Replayed reads always
hit the immutable `OcsfEvent` rows and the persisted `ObservedData` graph
directly; cache state at original-write time is irrelevant to replay
correctness.

This means:
- Two analysts replaying the same investigation observe identical state,
  regardless of whether the original session had warm or cold caches.
- Cache eviction policy can be tuned freely without affecting historical
  reproducibility.
- The two caches' lifecycle bindings (raw-response TTL; ObservedData cache
  cleared on conclusion/archive) are operational concerns only — they
  don't constrain what was true in the persisted record.

---

## 9. Fixture Layer

### 9.1 What a fixture is

A fixture is a recorded OCSF event with the binding metadata needed to replay
it. Fixtures are stored as JSON files under `fixtures/<scenario>/<event-N>.json`:

```json
{
  "fixture_meta": {
    "scenario": "lateral-movement-via-rdp",
    "matches": {
      "verb": "enumerate_logons",
      "params": { "target.hostname": "WIN-FILE01", "outcome": "SUCCESS" }
    },
    "delay_ms": 50
  },
  "ocsf": {
    "class_uid": 3002,
    "class_name": "Authentication",
    "category_uid": 3,
    "activity_id": 1,
    "type_uid": 300201,
    "severity_id": 1,
    "time": 1776695531000,
    "time_dt": "2026-04-20T14:32:11Z",
    "metadata": { "version": "1.1.0" },
    "user": { "name": "jdoe", "domain": "CONTOSO" },
    "dst_endpoint": { "hostname": "WIN-FILE01" },
    "src_endpoint": { "ip": "10.0.4.55" },
    "logon_type": "Remote Interactive",
    "logon_type_id": 10,
    "status": "SUCCESS"
  }
}
```

Fixtures store **OCSF-shaped events**, not raw vendor responses. This is a
deliberate choice. Storing raw vendor JSON would mean fixtures are coupled to
specific adapters and the normalizer is the only thing under test in v0; we'd
have to re-record fixtures every time a vendor changes their API. Storing OCSF
means fixtures exercise the normalizer and the agent loop, which is what we
actually want to test in v0. The downside: the adapter-to-OCSF translation
layer is not exercised by fixtures. That layer needs separate unit tests with
recorded raw vendor responses.

For scenarios where adapter behavior matters (auth flow testing, error
injection, rate-limit handling), a second fixture format stores raw tool
responses (whether MCP or native API). A `query` adapter's profiles
(`11 §4.4`) are recordings of the same kind, kept with the adapter's tests under
`testdata/profiles/<profile>/`: `schema.json`, the store's schema as captured,
in JSON; and the directory `pages/`, holding one file per real result page, at
least one. Each is a regular, non-empty file (a symlink does not count; a
dotfile in `pages/` is not a page), and the repository test that enforces
`11 §4.4` fails a profile missing either. A recording is committed scrubbed:
account identifiers, principals and addresses replaced with placeholders that
keep their shape; the raw capture stays out of the repository. These are scenario-tagged
`raw_response` fixtures and used only by adapter-level tests, not by the agent
loop.

Each scenario directory may also carry an **asset-criticality sidecar file**
(`fixtures/<scenario>/asset-criticality.json`) mapping entity identifiers to
criticality classes; in fixture mode it populates
`TargetSpec.asset_criticality` for policy evaluation (04-action-authorization.md
§8.2).

### 9.2 How the resolver picks fixtures

The fixture adapter is a regular `Adapter` implementation with
`adapterClass = FIXTURE`. In tenant config, the v0 setup has:

```yaml
adapters:
  fixture:
    class: fixture
    enabled: true
    scenario: lateral-movement-via-rdp
  crowdstrike_falcon:
    class: native_api
    enabled: false
  defender_xdr:
    class: native_api
    enabled: false

bindings:
  enumerate_logons:
    - adapter: fixture
      operation: replay
      priority: 100
  get_process_ancestry:
    - adapter: fixture
      operation: replay
      priority: 100
  # ... bindings for every verb point to fixture in v0
```

When the resolver invokes the fixture adapter, the adapter loads the active
scenario, finds fixtures whose `matches` block satisfies the incoming params
(with simple wildcard support), waits `delay_ms` (to mimic real latency for UX
testing), and returns the OCSF event. From there, normalization runs unchanged.
The OcsfEvent gets `source_tool = "fixture:lateral-movement-via-rdp"` so it's
traceable but distinguishable from real telemetry.

**Matching.** The call's verb must be `matches.verb`, which is one verb or a
list of verbs (one recorded resolution answers both `resolve_dns` and
`pivot_on_indicator` without a second copy), and every `matches.params`
entry must equal the value at its dotted path in the rendered binding params.
`"*"` matches anything, including an absent param. A **list** matches any one of
its values and nothing else — `"outcome": ["SUCCESS", "ALL"]` on the successful
sign-ins and `"outcome": ["FAILURE", "ALL"]` on the failed ones lets one scenario
answer `SUCCESS`, `FAILURE`, and `ALL` differently, which is what a
discriminating question ("were there failures before the success?") needs.

**Time-scoped fixtures.** A fixture may declare `"time_scoped": true` in its
`fixture_meta`. It then replays only when its event time falls inside the
window the binding passed under the reserved `window` param pair
(`window: { from: '${window.from | iso8601}', to: '${window.to | iso8601}' }`);
zero or absent bounds are open, and unscoped fixtures replay regardless of
window, as before. This is how a scenario carries *history*: a baseline that
"is this normal for this account over 30 days?" reaches and a 24-hour query
does not. Combined with a parameter the caller controls (the shipped scenario
matches its `svc_backup` baseline on `target.user`, rendered from
`${entity.user.name?}` so a host-rooted query omits it), the same scenario
answers differently to different questions — the property a *discriminating*
prediction needs (01-domain-model.md §x-prediction) and the reason a
competing-hypotheses demo can refute its benign alternative on fixtures.

### 9.3 Mixed mode

Per-binding override of adapter is allowed, so a tenant can run
`get_user_context` against real Entra while running `enumerate_logons` against
fixtures. This matters for development against partial integrations and for
integration tests that want one real connection plus controlled telemetry.

### 9.4 Recording mode

A future enhancement (post-v0): adapters run in `record` mode against real
tools, capture raw responses + the OCSF translation, and emit fixture files.
This closes the loop on regression-testing real customer scenarios without
retaining sensitive tenant data — fixtures get scrubbed and committed to a
corpus. v0 ships with hand-authored fixtures only.

---

## 10. Open Questions and Deferred Decisions

These are deliberate non-decisions for v0; the spec accommodates either
resolution.

The verb catalog will grow. 22 is a v1 number. The extension mechanism (§5) is
the answer to "how do we get to 40?" without bloating the LLM context, because
`list_capabilities` per-tenant trimming means an analyst at a tenant with only
EDR and IdP coverage sees ~12 verbs, not all 40. Walking real v0 scenarios
through the catalog is expected to drive 1-3 catalog changes; doing it now
in the abstract would be overfitting.

The fanout-and-merge semantics for `pivot_on_indicator` need a real merge
strategy beyond content-hash dedup — specifically, when two adapters return the
same sighting with different timestamps, which wins. v0 punts: emit both as
separate ObservedData and let the agent reason. v1 should define a
canonicalization rule.

The provenance `query` field is currently typed as `string`, which is fine for
SIEM searches but loses fidelity for structured queries (KQL, EQL, SPL). v1
should consider a structured `query` representation that survives round-trip
through ObservedData and is searchable.

The `process` SCO identity rule (§7.2) deviates from strict STIX 2.1 by
including `created_time` in the identity tuple. This is a calculated bet that
process start times are reliably available from EDR telemetry; if it turns out
they're not, fallback to anonymous SCOs will fragment the graph. The
email-addr and user-account lowercasing deviations are similarly bets that
real-world enterprise systems are case-insensitive in practice. All three are
worth measuring on real data before declaring stable; all three are
reversible at the normalizer level.

The line between "native API adapter" and "custom adapter" is fuzzy by design.
Both implement the same contract; the distinction is operational (vendor SDK
vs. internal codebase). If this distinction stops carrying its weight,
collapsing to two classes (mcp / non-mcp) is acceptable.

The templating language (§3.3) is deliberately minimal. If real binding
authoring surfaces a recurring need that the function set doesn't cover, the
right move is almost always to extend the adapter (with a registered
function) rather than to extend the templating language. If that becomes
painful at scale — many adapters needing similar functions — promoting a
function from per-adapter to built-in is a non-breaking change.

The Coverage enum (§6) deliberately collapses some distinctions that were
considered and rejected as not load-bearing for agent-loop control flow —
specifically, the distinction between "0 results because filter was too
narrow" and "0 results because telemetry wasn't available for the window."
Both have the same operational consequence (widen and retry, or accept the
negative result) and the agent loop navigates them via prose in
`degradation_notes` and analyst judgment.

The detection_finding normalizer (§4.12) is the single capability-layer path
that produces interpretation-layer artifacts (Indicator, Sighting) with
`derivation_mode = INFERRED`. This breaks the otherwise-clean rule that the
capability layer emits only `DIRECT` observations. It's the right call —
vendor detections genuinely are inferences, just not ours — but it warrants
review if the boundary between observation and interpretation needs to be
re-litigated.

**Action dispatch / write-side adapter contract lives in
08-write-side-actions.md.** This spec covers the read side: 22 query verbs
(plus the §2.9 external case lookup verbs), the binding / resolver / normalizer
pipeline, and the `CapabilityResult` envelope. The write side — the agent-facing
`request_action` tool, write-side adapter operations, the idempotency model, the
`adapter_request_id` correlation contract that 04-action-authorization.md §6.1
and 02-persistence.md §3 `ActionDispatched` reference, and the action fixtures
that mirror §9 read fixtures — is specified in 08 as the symmetric twin of this
spec. The action-type taxonomy that contract serves is broader than
containment-class actions: ticketing (`ticket.create`, `ticket.update`), TI
publication (`ioc.publish_to_misp`, `ioc.publish_to_isac`,
`ti.contribute_to_attack`), document delivery (`document.deliver`), comms
(`comm.post`, `comm.page`), and IT-operations (`host.reimage`,
`credential.reset`) all use the same contract — see 07-post-conclusion-outputs.md
§7 and §8 for the post-conclusion action categories and
04-action-authorization.md §2 for the consolidated categorization table. Until
that contract is implemented, action dispatch in v0 prototype runs against
fixture stubs only (08 §9).

---

*End of spec.*
