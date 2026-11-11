# 0334 — An adapter that queries a store it does not own discovers that store's schema, carries a recorded profile per source version, and reports what it cannot answer instead of failing

Issue: #334 · PR: #336 · Status: accepted

## Context

Some adapters decode a vendor API's response; others build a query against a
store whose schema someone else owns — a vendor-managed table, a security
lake, a hosted search index — and name its fields in SQL, SPL or KQL. The two
fail differently. A response decoder that meets a missing field sees an
absent value and carries on. A query that names a missing field is rejected
by the store before it runs, so one absent column breaks every call.

The first adapter of the second kind, the AWS Security Lake adapter
(adr/0322), was built from a fixed column list. Against a live lake its
health probe failed with a column-not-found error, and every query would have:
the lake's CloudTrail table is AWS's mapping of a subset of OCSF 1.1.0, and it
lacks fields the adapter assumed (`status_id`, the session's credential and
id, a resource's name and region, the response's error message and code). The
table is versioned per source version, by its name, and stored as Iceberg,
which permits adding columns. The hand-built test pages agreed with the
adapter by construction, so no offline test could see it.

The adapter contract (`design/11`) said nothing about how an adapter learns a
store's schema, what it does with a capability the schema cannot express, or
how it is checked against a real store. The engine's own rule for schema
versions (`03 §4.16`) carried the same assumption from the other side:
"every field a normalizer requires exists at the floor" assumed a source
fills every field its OCSF version defines.

## Decision

**Every adapter declares where its schema comes from.** `describe` gains
`schema_source` with a `kind`: `response`, for an adapter that decodes a
vendor API's response, whose obligation is tolerant decoding (an absent field
is absent, never an error); or `query`, for an adapter that names fields in a
query against a store it does not own.

**A `query` adapter discovers, declines and reports.**

- It reads the store's schema on every health probe, and before a call
  when it holds none (refined by adr/0335), and builds its projection and predicates from the fields that
  exist, never from a fixed list.
- It carries one profile per source version it supports — the store, the
  version, and the fields it reads — and names them in `describe`. A profile
  is recorded from a real store: the store's schema as captured
  (`schema.json`) and a few real result pages (`pages/`), scrubbed of account
  identifiers, under the adapter's test data, so offline tests run
  against what a live store returns, not against what the author expected.
- A call that needs a field the store lacks is declined with FALLTHROUGH
  and a reason, so the engine tries another binding or reports the
  capability unavailable; it is never sent to fail at the store.
- Health stays the whole-adapter signal — a core field missing (the event
  time) is unhealthy — and gains an additive list of degraded capabilities,
  each with the operation, the capability and the reason ("credential filter:
  `actor.session.credential_uid` absent").
- A live-check runbook beside the adapter's example configuration says how
  to set up a real store, what to capture, how to compare it with the
  committed profile, and how to tear it down. It is run before a release and
  when the store's vendor announces a new source version; continuous
  integration cannot reach a vendor's store, so this is where drift is
  caught.

**The engine shows a limitation, not a new status.** `list_capabilities`
keeps the verb `available` and attaches limitations, each naming the adapter
that reports it. A verb that walks its bindings in priority order reports its
highest-priority healthy binding's limitations and stops there, so a fallback adapter is not started just to be listed; a later
binding may still answer what the first declines. A fan-out verb, which
invokes every binding, reports every healthy binding's limitations. The verb
still answers the calls it can express. A declined call is a failed binding, so its empty
answer is never COMPLETE and never a citeable absence (`03 §6.1`,
adr/0061).

**Conformance enforces it.** `reckon adapter test` fails an adapter whose
`describe` has no `schema_source` or an unknown kind, a `query` adapter that
names no profile, a profile without its store, version and fields, or a
profile name that is not one path element or repeats another, and a health
result whose degraded list does not parse. A repository test fails an
in-tree `query` adapter without a recorded profile (its schema and at least one
result page) for every profile it describes, or without its live-check runbook. Exemptions are recorded here
and nowhere else, so adding one is a decision on record, and an exemption
fails once it no longer applies. The one exemption this decision
recorded, for the Security Lake adapter, ended when #335 brought that adapter
to the rule (adr/0335).

**The engine's version rule is corrected** (`03 §4.16`): a field the schema
defines at the floor may still be absent from a given source, which maps a
subset; it is read with the same field detection as a field above the floor.

## Alternatives rejected

- **The rule in prose only.** The next author skips it; the check is what
  holds it, and it fails for an author who never read the spec.
- **A `degraded` verb status for one unexpressible filter.** The verb still
  answers other calls; status says whether a verb resolves, and a limitation
  is a fact about some of its calls.
- **Versioning by source version alone, without discovery.** A vendor's
  subset of a standard, or an added column within a version, still breaks a
  fixed projection.
- **Profiles for response adapters too.** Their failure mode is tolerant
  decoding, which their tests already exercise; the cost would buy nothing.
- **Listing only what every healthy binding lacks.** Unsound twice: a
  fan-out verb invokes every binding, so one binding's gap comes back on every
  such call; and a fallback binding that is not applicable to a call would
  erase the primary's limitation.
- **A shared adapter kit for schema discovery now.** One adapter of this kind
  exists; the repository extracts a pattern on its third use.

## Consequences

- The plugin protocol gains two additive fields: `schema_source` in
  `describe` and `degraded` in the health result. Every in-tree adapter
  declares its kind. The host reads `degraded` leniently: a malformed value is
  dropped and logged, never a reason to mark the instance unhealthy;
  conformance is where it fails.
- `list_capabilities` and the capabilities API carry per-verb limitations.
  The agent does not read them yet: its capability view keeps only status,
  and showing limitations to the model is a change to the model-facing text
  (adr/0232), left for when an adapter reports one in practice. A declined
  call still reaches the model as a failed binding.
- A `query` adapter ships its profiles and a live-check runbook; the scaffold
  emits both, and the authoring pipeline (`design/12 §7`) runs the live check
  before release.
- #335 brings the Security Lake adapter to the rule, with the live table
  captured in the run that found this as its first profile.
- The same rule governs any future adapter over a store it does not own: a
  SIEM's search, a hosted index, another lake.
