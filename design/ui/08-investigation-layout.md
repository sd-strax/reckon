# 08 — Investigation Layout: lenses, companion, inspector

This sheet defines how the investigation document is organized: the regions of the
investigation view, the four lenses, the conversation as a companion column, and the
inspector every citation opens. It supersedes `02 §2.2` (panel), `02 §2.8` (evidence in
reach) and the row anatomy of `02 §2.9`, and extends `03 §3.4` and `03 §3.7`. The decision
and its alternatives are `adr/0079-investigation-layout.md`. Reference screens for the
reference scenario are in `mockups/`.

Binding rules apply unchanged (`00`): the engine is the record, engine words render
verbatim, every element names its source, and an element with no source is a gap in
`00 §6`.

---

## 8.1 Regions

```
┌ header: title · verdict · stage indicator · primary action ────────────────┐
├ work queue ┬ lens tabs: Summary │ Story │ Reasoning │ Response ┬ companion ┤
│            │ the active lens                                  │ column    │
│            │                                    [inspector] ◀─┤           │
└────────────┴──────────────────────────────────────────────────┴───────────┘
```

| Region | Width | Notes |
|---|---|---|
| Work queue | 220–270px | §8.3. Collapsible. |
| Lens area | fluid, content max 900px | §8.4. One lens at a time. |
| Companion column | 340–420px | §8.5. |
| Inspector | 440–480px | §8.6. Overlays the companion column's side of the lens area; never replaces the lens. |

Below 1280px the work queue collapses to an icon rail; below 1024px the companion column
becomes a toggled drawer. The inspector always overlays. (The remaining layout-floor
question in `00 §7` is the minimum width at which the lens area stays readable with the
inspector open.)

## 8.2 Header

| Element | Source |
|---|---|
| Title, investigation id | `GET /api/investigations/{id}` |
| Case reference ("from INC…") | The Seed extension when the seed is a case (`14 §1`) |
| Tenant label | Rendered only when tenancy is enabled (`05`); absent otherwise |
| Verdict pill | The current verdict act (`00 §6.2`) |
| Stage indicator | Derived, below |
| Primary action | Derived from the stage, below |
| Nearest approval expiry | Min `expires_at` over `REQUESTED` actions (`03 §3.6`) |

**Stage indicator.** Five steps over the derived lifecycle (`00 §2.4`); no new aggregate
state.

| Step | Done when | Current when |
|---|---|---|
| Investigate | a verdict act exists | `ACTIVE`, no verdict |
| Verdict | a verdict act exists | — (a point, not a span) |
| Contain | every non-`notify.*` action is terminal | verdict recorded and a non-terminal non-`notify.*` action exists |
| Notify | every `notify.*` action is terminal and no comms thread awaits a reply (`00 §4`) | containment done, notification outstanding |
| Close | `CONCLUDED` | every action terminal, not yet concluded |

**Primary action**, one per stage: none while investigating (the work is in the lens);
**Review N proposed actions** when actions are `REQUESTED`; **Send notification** when
only notification remains; **Conclude** when the conclude preflight passes (`02 §2.10`).

## 8.3 Work queue

Two sections, in this order.

- **Investigations** — the analyst's open investigations as a triage queue (`13 §4`):
  ordered needs-me-first (`ui/06` tree rules). A row shows the tenant
  label when tenancy is enabled, the title, the current stage step, and the case reference
  when seeded from a case. Source: `GET /api/investigations`.
- **Cases** — the case system of record's queue, read-only (`14 §4.2`, `adr/0030`), listing
  **only cases with no investigation**. Each row has **Start investigation**. A case that
  already has an investigation is not listed here: it appears once, as its investigation.
  Source: `query_external_cases` filtered against the seeds of existing investigations.

## 8.4 Lenses

The default lens follows the stage: Reasoning while investigating; Response from verdict
through containment and notification; Summary once concluded. The analyst's last choice
per investigation overrides the default (view state, not investigation state, like
`02 §2.12`'s last-seen position).

### Summary — where it stands

| Element | Source |
|---|---|
| Four tiles: verdict, containment progress + nearest expiry, notification status, open questions | Verdict act; `/actions`; comms threads (`00 §4`); `/hypotheses` (`INCONCLUSIVE` / `UNTESTED`) |
| "What happened" narrative, with its authorship line ("drafted by reckon · edited by …") | Gap `00 §6.18` |
| Key entities with role and status | Grouping objects (`00 §2.1`) + citation roles; status words from the verdict and actions |
| Still to do | `REQUESTED` actions with countdowns, owner action items (gap `00 §6.17`), outstanding notification, open predictions |
| Procedures followed | `consulted_*` with `used = true` (`00 §6.9`) |
| Copy handoff note, Export report | `GET /api/investigations/{id}/export.md` (`00 §6.10`) |

### Story — event time

The lens that answers the two-clocks question (`00 §7`): everything here is in event time;
investigation time appears once, as a divider where the investigation started.

| Element | Source |
|---|---|
| Attack path: entities as nodes, typed relations as the labels between them, the span in seconds | The two-layer graph (`01`), restricted to entities cited by a `SUPPORTED` hypothesis; ordered by first event time |
| ATT&CK techniques | Detection finding types and hypothesis labels (`03 §4.12`) |
| Timeline rows: time, source class, a generated sentence with entity chips, citing predictions | Cited OCSF events + an entity-scoped event query (gap `00 §6.13`); sentences per class (gap `00 §6.12`) |
| Baseline row (collapsed repetition, e.g. a nightly logon) | Gap `00 §6.14` |
| Row emphasis: expected / unusual / cited-against | Cited by a `CONFIRMED` prediction of a `SUPPORTED` hypothesis → emphasized; attested-benign context → "expected" |
| Divider: investigation start | Seed timestamp |
| "Not yet known" note | `INCONCLUSIVE` / `UNTESTED` predictions |

Source filters (Auth, Process, Network, Detection) filter by OCSF category.

### Reasoning — the competing set

The tracker of `02 §2.9` (`adr/0047`) with these changes:

- A **verdict basis strip** heads the lens: each hypothesis handle with its status, the
  open predictions, and whether every discriminator is tested.
- **Prediction rows are one line at rest**: status glyph (✓ `CONFIRMED`, ✕ `DISCONFIRMED`,
  ? `INCONCLUSIVE`/`UNTESTED`) with its word for screen readers, the ordinal, a short title,
  a `splits H<n>` marker when the prediction discriminates, and the evidence count (or
  "attested" for a manual test). Expanding shows the full statement, the attestation quote
  with its author by name, the evidence chips, and what it decides against. The short title
  is gap `00 §6.12`; until it exists, the row shows the statement truncated to one line.
- A **Cards / Matrix** toggle. The matrix lists every prediction against every live
  hypothesis: ✓ consistent, ✕ inconsistent, ? untested or inconclusive. A prediction's
  owner cell follows its outcome; each rival in its `discriminates` list gets the opposite;
  other cells are blank. A footer counts inconsistencies per hypothesis. Open rows are
  tinted. Source: `/hypotheses` only.

### Response — containment and handoff

`03 §3.4`'s plan block with these additions:

- **Requested action cards** keep every decision-grade element of `03 §3.3` and add a
  **policy** fact: whether the tenant's response policy pre-authorizes this action
  (`AUTO_POLICY` would fire) or it needs an approval routed to its owner. Gap `00 §6.16`.
- **Owner action items**: work only an owner outside reckon can do (rotate a credential,
  change a perimeter rule), tracked until confirmed. Gap `00 §6.17`.
- **Notification**: the outbound `notify.*` draft (`00 §4`) assembled from the case record
  (what happened, what was done, what is needed), with Edit and Preview & send — the
  mandatory preview is the approval.

## 8.5 Companion column

- Holds the analyst's turns and the agent's prose, scoped to the investigation
  (`02 §2.2` composer rules stand).
- **What a turn records renders as a link**, one line each: "H1·P5 confirmed by
  attestation", "Proposed 3 actions + 1 owner item". Clicking scrolls the lens to the
  object (switching lens if needed) — the object itself is never restated.
- **Consulted knowledge** renders as chips under the turn (`02 §2.11`).
- **Activity disclosure**, collapsed: "Activity · 3 tool calls, 2 records". Expanding shows
  the tool-call blocks (`02 §2.3`) and the records written. No sequence numbers, no
  "committed" lines in the reading path; both remain in the thread and the export.
- **Proposed actions** in a turn render as action cards (`03 §3.3`), not as prose.

## 8.6 Inspector

Every citation, everywhere (prose, prediction rows, action cards, timeline rows, entity
chips, knowledge chips), opens the inspector. It supersedes `02 §2.8`'s read-only editor
tabs. `Esc` closes it; the analyst never leaves the investigation.

**Frame**, identical for every type:

1. Type label, pin, close. When reached from another inspector: back and a breadcrumb.
2. A human name for the object.
3. **In this case**: what in this investigation cites it (predictions, actions), from the
   `ref_appearances` fold scoped to this investigation (`00 §6.4`).
4. Tabs: the typed view first, then secondary tabs.

Pinning keeps an inspector open while a second one opens beside it for comparison (at most
two).

**Evidence** (an OCSF event or observed-data). Tabs: Details · Provenance · Raw.

| Element | Source |
|---|---|
| Sentence ("svc_backup interactive RDP logon to WIN-FILE01 from 10.0.4.20") | Gap `00 §6.12` |
| Field table, curated per OCSF class (authentication: user, logon type, source, destination, result; process: name, pid, command line, parent, user; network: source, destination, port, domain, protocol) | `GET /api/evidence/{ref}` + gap `00 §6.12` |
| Encoded command lines shown decoded beside the original | Client-side decode of the cited field; the original is always shown |
| "Around it": the events just before and after on the same entity | Gap `00 §6.13` |
| Source line: capability verb, coverage word | The capability result envelope (`03 §6.1`) |
| Provenance tab: verb, adapter, the query sent, coverage, ingest time, normalizer version, content hash | Capability provenance (`03 §4.13`) |
| Raw tab: syntax-highlighted JSON, collapsible, copy | `GET /api/evidence/{ref}` |

**Entity** (account, host, IP, domain, process).

| Element | Source |
|---|---|
| Type, value, aliases (linked, never merged), id on hover | STIX object + alias edges (`00 §2.8`) |
| Role and status in this case; event and action counts | Citations + `/actions` targets |
| Normal behaviour: the baseline and how this case deviates | Gap `00 §6.14`; absent when no baseline exists — never inferred |
| Actions on this entity | `/actions` where the entity is a target |
| Other investigations | `GET /api/entities/{ref}/appearances` |
| Pivot | Stages a question in the composer (`02 §2.4`) |

**Procedure excerpt.** Procedures are whatever was imported — a PDF, a document, a web
page, markdown — so the inspector is anchored on the **excerpt the agent retrieved**, not
on the document's structure. Tabs: What the AI read · Whole document · Versions.

| Element | Source |
|---|---|
| Title, format, source name or URL, importer and import time | SOP entry meta (`06 §2.4`) |
| In this case: which turn retrieved it, applied or only consulted, the actions it led to | `consulted_sops` on the interpretation (`00 §6.9`) |
| The excerpt, verbatim, with "excerpt k of n retrieved", its location in the source, relevance band | The retrieved-snippet side store (`06 §6.2`) + gap `00 §6.15` (location) |
| "Exactly as retrieved · hash matches current version" or "updated since this case consulted it" with a diff | Content hash at retrieval (`06 §6.2`) vs the current revision (`06 §2.2`) |
| Open original at this location | Gap `00 §6.15` (retained original) |

## 8.7 Density rules

- At rest a row carries one status and one count. Everything else is behind expansion.
- One canonical place per object; elsewhere, a link to it.
- People by display name; principal ids, UUIDs and hashes on hover or in a Provenance tab.
- Machine timestamps on hover unless the time is the content (timeline, evidence).
- `text-3` only for non-essential metadata; anything the analyst must read is `text-2` or
  stronger (`ui/01`).

## 8.8 Out of scope here

The entity graph beyond the attack path (`13 §4` Evidence graph, v1 rich); presence and
multi-analyst cues (`05 §8`); the async approval web surface (`05 §11.3`); the mobile
layout.

---

*The lenses render the same aggregate as the thread; the thread remains the audit lens and
the source of the export. Nothing in this sheet introduces state of record.*
