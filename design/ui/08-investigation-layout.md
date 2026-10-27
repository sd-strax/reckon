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

Two sections, in this order. The region's header also opens the Knowledge view (`ui/09 §9.1`).

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
- A **Knowledge section** heads the column: what the agent may read in this investigation,
  with include toggles (`ui/09 §9.6`). It replaces the knowledge rail.
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

### 8.6.1 The remaining types

Every object a surface can cite has an inspector in the same frame. Reference screens:
`mockups/07`–`11`.

**Absence** (`x-null-observation`, `adr/0061`). Not an event: a search that answered in
full and found nothing. Tabs: Search · Provenance · Raw.

| Element | Source |
|---|---|
| Name ("No connections from WIN-FILE01 to 185.220.101.5 in the 7 days before") | Verb, subject and parameters of the absence object |
| `COMPLETE · 0 EVENTS` | Its coverage (an absence object exists only for a complete answer) |
| Verb, subject, what was looked for, window, sources answered | `GET /api/evidence/{ref}` (the absence object) |
| What it does and does not say; the standing note that a partial empty answer is never citeable | Fixed copy (`adr/0061`) |
| Re-run with a wider window | Stages the same verb in the composer (`02 §2.4`) |

**Observed-data bundle** (one capability answer, many events). A list, not a field table.
Tabs: Events · Envelope · Raw.

| Element | Source |
|---|---|
| Event count, first and last observed | The `observed-data` envelope |
| Events grouped by source (repetitions collapsed, each opening its evidence inspector) | The bundle's events via `GET /api/evidence/{ref}`; grouping is display |
| Objects it contains | The envelope's object refs, as entity chips |
| Produced by (opens the capability call) | Gap `00 §6.20` |

**Detection finding** (Indicator + Sighting + vendor Identity, `03 §4.12`). Marked **inferred
by the vendor**: the one inferred object the capability layer produces. Tabs: Finding ·
Evidence · Raw.

| Element | Source |
|---|---|
| Title, finding id, vendor, time, severity | Indicator + vendor Identity |
| Techniques | The finding's types (`03 §4.12`) |
| Sighting: where, when, count | Sighting |
| The evidence the vendor attached, each opening its evidence inspector | Recursed evidence, normalized through the registry |
| How far to trust it: which predictions confirmed the underlying events independently | `/hypotheses` test-result refs that cite the same events |

**Attestation** (`note`, `adr/0054`). The name is the quoted statement itself.

| Element | Source |
|---|---|
| Statement, verbatim | The note |
| Who said it (as recorded), who recorded it (display name), when | The note + the recording act's actor (`adr/0077`) |
| How it was obtained | The note's text as recorded; nothing inferred |
| The question it answers; the prediction and the outcome it produced | The prediction whose `test_result_refs` hold the note |

**Prediction.** Tabs: Record · History.

| Element | Source |
|---|---|
| `P<n>`, short title, status, owner hypothesis, statement | `/hypotheses` (`adr/0047`); short title gap `00 §6.12` |
| Discriminates (the rivals expecting the opposite) | `discriminates` |
| Test kind (tool or person), test results (each opening its inspector) | `test_kind`, `test_result_refs` |
| History: proposed, accepted, tested, withdrawn, with actors | The thread's interpretation events for this prediction (`/thread`) |

**Hypothesis.** Tabs: Record · Wording history.

| Element | Source |
|---|---|
| Handle, status, claim, predictions tested over total | `/hypotheses` |
| Predictions as one-line rows (`§8.4`) | `/hypotheses` |
| Wording history: proposed text, restatement at adoption, parent when refined | The acknowledgment and refinement events (`/thread`); `parent_ref` |
| The competing set with each status; open the matrix | `/hypotheses` |

**Action** (`x-action`). The card (`03 §3.3`) is the summary; the inspector is the record.
Tabs: History · Authorization · Targets.

| Element | Source |
|---|---|
| State history with actor and time per transition | The action's `action.*` events (`02 §3`) |
| Tier, distinct targets against the escalation threshold, escalation reason | The requested action (`04 §1`) |
| Policy decision, the policy that decided, shadow policies that would have fired | `action.policy_evaluated` (`04 §4`, `§4.4`) |
| Approver (display name) | The approval event |
| Per-target results, including `UNKNOWN` residuals | `action.resulted` (`08 §6c`) |
| Reversibility class; reversed-by / reverses links in both directions; retry lineage | `reversibility`, `reversed_by_ref` / `reversal_attempted_by_ref` / `reversal_of_ref`, `retry_of` (`00 §6.6`) |

**Notification thread** (`ui/04`). Tabs: Thread · Sent message.

| Element | Source |
|---|---|
| Channel action, sent time, thread status | The `notify.*` action + its comms thread (`00 §4`) |
| Thread: sent, replies, acknowledgments | `GET /api/investigations/{id}/comms` |
| Follow-up dueness and the escalation rule (prompts, never sends) | Derived at read time (`00 §4`) |
| The exact message sent | The frozen body on the approved action |

**Owner action item.** Gap `00 §6.17`.

| Element | Source |
|---|---|
| What, owner, why, when asked, status | The owner action item record |
| History | Its events |
| Mark confirmed / waive with reason; counted by the conclude checklist until one happens | The conclude preflight (`02 §2.10`) |

**Similar past case** (`06 §3`, case summaries). Tabs: Summary · Why it matched.

| Element | Source |
|---|---|
| Title, concluded date, disposition, duration, band | `POST /api/knowledge/recall_similar_investigations` match + its summary |
| Narrative, techniques, primary entities, actions taken with outcomes | The structured summary (`06 §3.1`) |
| Why it matched | `match_rationale` |
| In this case: consulted or applied | `consulted_similar_investigations` (`00 §6.9`) |
| Open that investigation; include in context | `investigation_ref`; the knowledge gate (`06 §5.1`) |

**Turn** (an interpretation and the turn that wrote it). Tabs: Answer · Tool calls ·
Consulted · Transcript.

| Element | Source |
|---|---|
| The analyst's ask, the answer, the model | The thread entry + `GET /api/interpretations/{id}/transcript` |
| Records written, each with type and what it cites; derivation mode; actor and delegate | The interpretation events (`01`) |
| Tool calls with coverage and counts, each opening its capability call | The committed turn record; gap `00 §6.20` for opening one |
| Consulted knowledge: applied or only consulted | `consulted_*` (`00 §6.9`) |

**Capability call.** The object the Story timeline, the residual panel and the bundle point
at. Tabs: Call · Result · Raw.

| Element | Source |
|---|---|
| Verb, binding, adapter operation, priority | The capability result envelope (`03 §3`, `§6.1`) |
| Coverage and how many bindings answered | `03 §6.1` |
| Parameters sent, exactly | The templated parameters (`03 §3.3`) |
| What it produced (bundle, absence object, or nothing) | The result's refs |
| Other bindings for the verb, including installed-not-enabled | `GET /api/capabilities` (operator view, `13 §4`) |
| Addressable by id from any surface | Gap `00 §6.20` |

**Person** (a principal).

| Element | Source |
|---|---|
| Display name, principal id, issuer | The actor on their acts (`adr/0077`) |
| What they did in this case | Events in this investigation whose actor is this principal |
| Roles that authorized each act | The roles the token carried on each act (`adr/0105`), the union across this case with "current roles may differ"; acts recorded before the field existed show none and the inspector says so |

**Alias** (an alias edge). Linked, never merged.

| Element | Source |
|---|---|
| The two identities, each with its own event count | Both STIX objects + their citations |
| Why they are linked: basis, by whom, when | The alias edge and its provenance (`01`) |
| Remove link (separates the evidence again; nothing was merged, so nothing is lost) | An analyst act on the edge |

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
