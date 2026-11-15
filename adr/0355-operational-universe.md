# 0355 — One spec lists every item of D3FEND, STIX, OCSF and ATT&CK and says what the engine does with it

Issue: #355 · Status: accepted

## Context

The engine takes its vocabulary from open standards. STIX gives it entities
and reasoning objects (`design/01-domain-model.md`). OCSF gives it telemetry
(`design/03-capability-layer.md §4`). ATT&CK gives it technique labels.
D3FEND gives it response actions (`design/04-action-authorization.md §2`).

Each standard was adopted where a feature needed it. No spec said which
parts of each standard the engine uses and which it leaves out. Only
D3FEND's three response tactics had a full accounting
(`design/04-action-authorization.md §2.2`).

So nobody could say what the engine covers. A missing capability, such as
reading a host's vulnerabilities, was not recorded anywhere as missing.

## Decision

- **A new spec, `design/15-operational-universe.md`, states the whole.** It
  lists every D3FEND technique, every STIX object and every OCSF class, and
  ATT&CK at tactic level. The D3FEND list is version 1.6.0. The STIX and OCSF
  lists come from the schemas checked in under `third_party/`.
- **Every item has one disposition.** The four from
  `design/04-action-authorization.md §2.2` are kept: Action, Reversal,
  Handoff, and Architecture, which the new spec names Recommendation. The new
  ones are Entity, Telemetry, Read, Knowledge, Export, Deferred and Out of
  scope.
- **Every in-scope item has a status.** Built means it is in the code.
  Specified means a spec describes it. Proposed means the new spec is the
  first place that names it.
- **The engine runs no detection of its own.** Every capability is something
  a tenant's tool knows or can do, reached through an adapter. So every
  Detect technique in scope is a Read. An analysis that only exists inside a
  sensor is read as that sensor's detection, through `search_alerts`.
- **Deception is out of scope.** The engine has no verb, entity or action for
  decoys.
- **The spec adds nothing by itself.** A proposed row is a candidate. It
  becomes real through its own issue and decision record.
- **The existing catalogues keep their authority.** The verb catalogue, the
  action catalogue, the entity types and the normalizers still define what
  they define. The new spec points to `design/01-domain-model.md` for the
  STIX version and to `design/03-capability-layer.md §7` for deviations.
  Where a catalogue and the spec disagree, the spec lists the difference in
  its §8.

## Alternatives rejected

- **A "Finding" disposition for analyses that run inside a sensor.** It
  treated a capability as covered because an alert would arrive. A tenant
  with no such sensor gets no alert, and nothing would show the analysis was
  never made. As a Read, an unbound verb answers `UNAVAILABLE_TENANT`.
- **Extend the tables in `design/03` and `design/04`.** Those specs are split
  by read and write, which suits authorization. Completeness needs one list
  across both.
- **List only what the engine uses.** The gaps are the point. An item the
  engine leaves out has to say so, with the reason.
- **List ATT&CK technique by technique now.** It needs a pinned ATT&CK
  version, which is not decided. The spec covers tactics and records the rest
  as open.

## Consequences

- A new verb, action, entity or normalizer is checked against the spec, and
  the spec's row changes status in the same PR.
- The spec proposes six read verbs: vulnerabilities, software inventory, host
  configuration, threat context, file analysis and web activity. None is
  built by this record.
- Four questions stay open in the spec's §8. The capability list reports
  whether `search_alerts` is bound, not which analyses the bound source runs.
  A D3FEND id on a read verb must not change the text the model reads.
  ATT&CK is not listed at technique level. The link from a technique to its
  countermeasures has no home yet.
- The lists follow the standards' versions. When D3FEND, OCSF or STIX moves,
  the tables are redone against the new version.
