# 0210 — The catalog carries the hunt verbs; a fleet-wide verb takes its entity optionally; composite inputs flatten to scalars on the wire

Issue: #210 · PR: #326 · Status: accepted

## Context

An investigation can start from a question (`QuestionSeed`, 01 §Seed), but
the agent's tools come from one catalog trimmed to what the tenant binds
(03 §6.3), and `DefaultCatalog` carried only entity-rooted verbs: logons to
this host, this process's parents, this host's connections. "Where did this
command line run", "which hosts gained persistence this week", "what resolved
this name" had no verb. The spec lists them (03 §2.2–2.7); the normalizers for
every class they return (1001, 1003, 1006, 1007, 1009, 3005, 4003) were
registered and tested; nothing declared the verbs.

Two constraints shaped the answer. Every verb tool shares one input shape
(`agent.verbToolDef`): an `entity` object, a `window`, and the descriptor's
other inputs as top-level scalars; the spec's composite `ProcessFilter` and
`QuerySpec` cannot be expressed as objects there. And these verbs return bulk;
they waited on adr/0209, which pages a large result from a session-held
snapshot, so a fleet sweep is honest about what the model has not read.

## Decision

- **Seven descriptors join `DefaultCatalog`**: `get_process_executions`,
  `get_persistence_artifacts`, `get_file_activity`, `resolve_dns`,
  `get_privilege_changes`, `query_logs`, `pivot_on_indicator`, with the §2
  signatures. A verb is resolvable the moment a tenant binds it (§5.1).
- **Composite inputs flatten to scalars on the wire.** `ProcessFilter` is
  `image_hash`, `image_path`, `command_line_contains` beside the entity;
  `QuerySpec` is `query_text` and `tool`, and its `parameters` map is not
  carried on the wire; `scopes` is a `list<string>`. Each scalar is a
  template root a binding addresses; `ProcessFilter`'s `host` and `user` ride
  as the call's entity. The spec keeps the typed signatures as the concept
  and says so in its §2 conventions.
- **A fleet-wide verb takes its entity optionally.** `get_process_executions`
  and `get_persistence_artifacts` can be called with no entity: that is the
  hunt. Given a host or account, the entity narrows. A complete, empty
  fleet-wide answer still mints an `x-null-observation` (adr/0061), keyed on
  the parameters and window, so the clean hosts are citeable absence.
- **Scalar inputs render by their type.** `int` is a JSON Schema `integer`,
  `list<string>` an array of strings, everything else a string
  (`verbScalarSchema`). The one existing verb affected, `query_external_cases`,
  now receives `limit` as a number, which its adapter already accepts.
- **A fixture may answer several verbs.** `matches.verb` is one verb or a
  list, so the scenario's DNS resolution answers `resolve_dns` and
  `pivot_on_indicator` from one recorded payload (03 §9.2).
- **The shipped lateral-movement scenario exercises six of the seven**: the
  command line on the known host and on a second host, autostart value and
  scheduled task on the first and the value on the second, the dropped file,
  the resolution before the connection, the group add. Every new fixture is
  time-scoped, so a window after the incident is absence. `pivot_on_indicator`
  is bound `fanout: true` on the one fixture adapter and the DNS, network and
  file fixtures answer it on the address, the name and the hash.
- **`query_logs` is declared and unbound in both demo scenarios.** A fixture
  cannot run a query, and a demo should not teach the model the escape hatch.
  It is proven through a stub adapter: a row of a known class normalizes
  typed, a row of an unknown class is wrapped opaque (§4.13), the answer is
  COMPLETE.
- **The catalog's text is guarded like the prompt's.** A catalog-level test
  refuses a fixture identifier in any descriptor's intent or input text, the
  rule adr/0057 applies to the prompt and intrinsic tools; one pre-existing
  example case id is exempted by name, and the exemption fails when it stops
  matching.

## Alternatives rejected

- **A separate hunt catalog or a `scope: fleet` flag on the call:** one
  catalog with an optional entity says the same thing with no new mechanism;
  the resolver, coverage and absence already behave correctly for an
  entity-less call.
- **A composite `filter` object in the tool schema:** every binding would
  need a path into an object the templating engine does not address, and the
  precedent (`query_external_cases` flattening `CaseFilter`) already exists.
- **Binding `query_logs` to a fixture:** a wildcard fixture would answer any
  query with the same events, teaching the model a verb the spec says to
  prefer last.
- **Duplicating fixture payloads per verb:** two files for one event drift;
  a verb list on the fixture keeps one recording.
- **Descriptors only, no fixtures:** the verbs would be unavailable everywhere
  until a real adapter exists, and the fleet shape the osquery lab needs would
  go unrehearsed.

## Consequences

The demo's lateral-movement tool set grows by six verbs; the agent may pivot
differently there, and the eval attributes the new tool set against a
baseline that did not record one. Any re-baseline is a deliberate run. The
second scenario is unchanged. Real endpoint and fleet adapters bind these
verbs with no engine change. Still missing from §2: `get_user_context`,
`get_process_descendants`, `get_module_loads`, `get_failed_auth_burst`,
`get_lateral_movement_signals`, `get_email_messages`, `get_email_clicks`.
`policies.default_window` remains declared in config and unread by code.

`scopes` (`pivot_on_indicator`) and `tool` (`query_logs`) ride the call as
extra template roots and reach every binding that templates them; the
resolver does not select bindings by them, because no binding attribute names
a source kind today (a binding's scope is the organization it serves, 03
§3.5), and the shipped demo bindings do not reference them. A routing rule,
if wanted, is a separate decision. PromptVersion moves through `tools/<verb>`
alone: the example verb now carries the `int` and `list<string>` input kinds
the renderer distinguishes, so a change to how any kind renders is visible.
No other versioned component moved; the per-tenant verb tool text, which
PromptVersion does not version, did change (`query_external_cases.limit` is
now an integer, and the demo gains six verb tools).
