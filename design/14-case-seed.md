# Case Seed — investigating from a system-of-record case

## 0. Framing

An incident lands in the org's system of record — a ServiceNow incident, a Jira
issue, a TheHive case — and an analyst decides it warrants investigation. This
is the most common real-world SOC entry point after alert triage, and reckon
could not express it: an investigation's Seed (`01 §Extension 1`) was one of
`alert | entity | question`, none of which is *"a case was handed to me."*

reckon already **reads** a case (`query_external_cases` /
`get_external_case_details`, `03 §2.9`) and **acts on** one (the `ticket.*`
action family, `04 §2.2`). This note adds the third relationship — **rooting an
investigation on a case** — by extending the Seed with a fourth shape and a
server-side creation flow.

**The vocabulary is deliberately split.** *Cases are investigated; tickets are
acted upon.* The write family stays `ticket.*` — acting on the external artifact
(comment, transition, close). The read/seed side says **case** —
`query_external_cases`, `CaseSeed` — investigating *from* the artifact. Two words
for two relationships to the same record; the split is intentional, not
inconsistent.

| Owned here (`14`) | Owned elsewhere (authoritative) |
|---|---|
| The `CaseSeed` shape + the case-creation flow | The Seed extension + lifecycle (`01`) |
| The fail-closed read contract | The read verbs + normalization (`03 §2.9`) |
| The analyst-pull trigger + the case picker | The action `ticket.*` family (`04`, `08`) |

### Out of scope

- **Push triggers** (a SoR webhook into reckon) — v0 runs on-prem/laptop behind
  NAT; a new *caller* of §3's flow later, not a new path.
- **Auto-creation / a polling feed** — a noise-policy decision needing
  watermark/dedup state. Deferred with the configured case queue (§4.2).
- **Status sync** — the investigation and the case keep independent lifecycles;
  linking them (close the case ⇒ conclude?) is workflow policy, deferred. Its
  smallest piece has landed on its own terms: the "opened in reckon" note on
  the seed case is a `ticket.comment` **action inside the new investigation**,
  offered under the tenant posture `cases.note_on_seed` (confirm by default,
  auto, off) — never a side effect of creation (`adr/0032-case-note.md`).
- **Ticket-as-action** — writing a case is `04`/`08`, unchanged.

---

## 1. The `CaseSeed` shape (a fourth variant, not a new primitive)

The Seed union becomes `alert | entity | question | case`. `CaseSeed` carries:

- **`case_id`** — the SoR's display identifier (`INC0010001`, `PROJ-123`). The
  authoritative pointer (the not-a-SIEM stance, `01 §Extension 1`: reckon stores
  the pointer, never a copied queue row).
- **`source`** — the SoR name as the capability layer labels it
  (`provenance.tool`: `servicenow`, …). Required — see §1.1.
- **`case_ref`** — optional, the STIX id of the ingested class-2005
  `ObservedData` (`03 §2.9`). The graph link; the parallel to AlertSeed's
  `detection_finding_ref`, but a *distinct field* (§1.1).
- **`source_scope`** — the standard optional scope (`01`, `03 §3.5`):
  caller-supplied for a case seed, exactly as for Entity/Question seeds. Absent
  = the single-organization deployment. Payload-derived scope (a SoR org tag,
  the AlertSeed precedent) is a v1 refinement.

`Seed.Summary()` renders **`case servicenow: INC0010001`** — the `source: id`
shape of an alert seed, prefixed `case ` so the triage queue distinguishes the
two at a glance without reading `seed_type`.

### 1.1 Why a fourth shape, not overloaded AlertSeed

Structurally, AlertSeed reuse fits — `alert_id/source/detection_finding_ref` is
shaped identically to `case_id/source/case_ref`, and both refs land in the same
STIX store. The decision to *not* reuse rests on one hard argument and one soft
one:

- **`source` cannot be the discriminator (hard).** The same tool is often both
  an alert feed and a case store — `03 §2.9` itself lists "Splunk ES Cases," and
  Splunk is a canonical alert source. So `source=splunk` is ambiguous between
  "a detector fired" and "a case was filed." Every downstream consumer that
  treats the two differently — the triage-queue seed icon (`ui/02`), the seed
  type enum (`ui/05`), the agent's opening framing (§4), analytics cuts
  ("detection-rooted vs case-rooted investigations") — needs a discriminator.
  A discriminator on the seed *is* the type tag: the fourth shape by another
  name, only uglier as a bolted-on flag. And a `detection_finding_ref` pointing
  at a class-2005 *case* ObservedData is a latent two-meanings-of-one-word
  divergence baked permanently into an immutable seed — the exact failure the B1
  audit finding named (`deferred-audit-findings`: the "T1 external action"
  category died at "the first place those two meanings diverge").
- **Reasoning posture (soft, becomes real with §4).** An alert-rooted
  investigation opens with *"a machine claims something happened — true
  positive?"*; a case-rooted one with *"a person claims something happened —
  read the claim, then verify it."* Different opening moves, same loop. This is
  aspirational until §4's seed-context work ships, and is honestly labelled so.

Cost of the honest choice: one added enum value in a field already holding three
strings, one validation branch, one display branch. No new table, no new
primitive — the `x-interpretation`-is-the-only-invented-primitive commitment
(`01`) is untouched.

---

## 2. Fail closed: no case read, no investigation

A case seed is only as trustworthy as the case actually read. **Any failure to
fully read the case fails the create, loudly, with the reason** — reckon never
roots an investigation on a case it could not load. In security a
half-loaded or phantom case is more dangerous than a create that failed
visibly: the analyst *believes* the ticket is in hand.

Three failure modes, three distinct loud results (all fail the create):

| Read outcome (`03 §6.1`) | HTTP | Meaning |
|---|---|---|
| Coverage `COMPLETE`, one case | `201` | the only success path |
| Coverage `COMPLETE`, **zero** cases | `404` | no such case (adapters that report absence as empty coverage) |
| `UNAVAILABLE_*` / `FAILED` / `PARTIAL` | `502`/`503` | SoR unreachable, errored, or the id resolved nowhere |

This dissolves the "where does `source` come from when the read failed" hole:
there is no partial-create path, so `source` and `case_ref` always come from a
real read of exactly one case.

**How an adapter reports not-found matters, and both are fine.** An adapter may
report "no such case" as empty-`COMPLETE` (→ `404`) *or* as `FALLTHROUGH` (the
resolver then finds no serving binding → `UNAVAILABLE` → `502`/`503`).
ServiceNow's single-case lookup uses `FALLTHROUGH` on a miss — deliberately, so
that with multiple case SoRs bound a miss on one falls through to the next
(the future multi-SoR routing, `§3` precondition). So in v0 a typo'd id most
often surfaces as `502`, not `404`. The distinction is cosmetic: **every outcome
short of exactly one case fails the create loudly** — which is the whole
contract. The create handler treats empty-`COMPLETE` as `404` and every other
non-single outcome as a loud `5xx`; it never half-creates.

---

## 3. The creation flow (one server-side path)

Every trigger funnels through one flow, so a future push/poll/queue trigger is a
new *caller*, not a new path (the seam pattern `/api/comms/inbound` uses). It
extends the existing `SeedInput` resolution (`server/investigations.go`: "the
server resolves it to a full seed … so the workbench never handles ids").

`POST /api/investigations` with `seed_input: {kind: "case", value: "INC0010001",
source_scope?: "<org>"}`:

1. The server invokes `get_external_case_details(case_id = value)` through the
   in-process capability resolver, threading `source_scope` (empty ⇒ unscoped,
   the single-org default). Apply §2's fail-closed contract to the result.
2. **Persist the result through the eager-promotion path**
   (`evidence.Store.Persist` / `03 §4.13`) — the same path the HTTP invoke route
   uses — *before* minting `case_ref`, so the ref names a durable, queryable
   `ObservedData` rather than a dangling deterministic id.
3. `Seed.Source` = the result's `provenance.tool` (the binding that served the
   read is the truth; the caller never names the SoR). `Seed.CaseRef` = the
   persisted `ObservedData` id. The investigation **Title** = the case title
   (`finding_info.title` → "Reimage request: WIN-FILE01"), the seed's display
   summary its `source: id` line.
4. `CreateInvestigation{Seed: {Type: "case", CaseID, Source, CaseRef,
   SourceScope}}` — the normal aggregate path; nothing new below the seam
   (event-sourced append + projection, `01`, `02`).

**`case_ref` is a frozen snapshot.** The class-2005 `ObservedData` id is
deterministic over the case *payload* (`03 §7`), so re-reading an edited case
later mints a *different* id. `case_ref` names the case *as it was at seeding* —
correct and intended; the live case is always re-readable by `case_id`.

**v0 precondition — a single case SoR.** With more than one case-SoR binding,
resolver priority (`03 §3`) picks one; a higher-priority binding returning empty
for an id it doesn't own would mask the real case (interacts with §2's
not-found). v0 assumes one case-SoR binding per (tenant, scope); explicit
per-request source selection is a v1 refinement.

---

## 4. Analyst pull (the v0 trigger) and what the agent does with a case

### 4.1 The case picker (workbench, v0)

The analyst drives — consistent with the product posture (the analyst acts, the
AI is a delegate; the agent deliberately has **no** create-investigation tool).
Two entry points onto §3's one flow:

- **By id:** New Investigation → *Case* kind → type/paste `INC0010001`.
- **By filter (filter → pick → seed):** "Seed from case…" prompts for an
  optional free-text filter + status, runs `query_external_cases`, shows the
  matches (number / title / status), and one selection seeds §3's flow from that
  row's `case_id`. The analyst types the filter each time — no stored state, no
  per-analyst config, no policy — yet the full pull-by-filter motion end to end.

Both paths, and any later trigger, are callers of the *same* server flow.

### 4.2 The configured case queue (tenant-level landed; per-analyst deferred)

T2/T3 analysts already own their cases *in the SoR* (assigned-to-me, my group).
Their day starts in a **queue**, not a search, so the picker opens on one.

**Landed, in its simplest form.** `query_external_cases` takes a structured
**`queue`** input — the work queue the SoR assigns by (a ServiceNow assignment
group, a Jira project/board, a TheHive team); the adapter maps it to its own
vocabulary (`03 §2.9`). The tenant names its queue once, in the capability
config's policies (`policies.case_queue`), and `GET /api/capabilities` reports
it. With a queue configured, opening *From a case…* runs the queue query
immediately and labels the rows; the free-text filter **narrows within** the
queue. Without one, the picker is the on-demand search of §4.1. Both are the
same server path; the queue is one more parameter on it, not a second flow.
The queue also has a standing home: the workbench's **Cases** view, where a
case opens read-only and an investigation starts from it, and a case already
under investigation leads to that investigation (`adr/0030-cases-pane.md`).
Both the view and the picker read through a **non-persisting** surface
(`GET /api/cases`): browsing is not evidence, and the case is promoted into the
telemetry layer once, when it seeds (§3).

Two things are deliberately *not* here. The queue is a structured value, never
a sentence: turning words into verb parameters is the agent's job, and the
capability layer stays pure I/O — a language layer behind the search box would
be a second reasoning surface with no investigation to attribute it to, and it
would make "no results" ambiguous (an empty queue, or a mistranslated query?).
And the picker **reads only**: a `ticket.*` write is an x-action inside an
investigation, grounded by evidence and recorded against a principal; before
the investigation exists there is nothing to record it against, so taking
ownership or changing state stays in the SoR's own interface.

**Deferred: per-analyst queues** ("assigned to me"). That needs a mapping from
the IdP principal to the SoR user, which v0 does not hold. When it lands it is
a second source for the same `queue`/assignee parameter — additive, not a
rewrite. The ad-hoc search survives alongside (a one-off query always outlives
a saved one).

### 4.3 Seed context for the agent

The agent does nothing new *mechanically*, but it must be *told* what seeded the
investigation — and today the system prompt renders only id/title/status, never
the Seed (`agent/prompt.go`). So this note adds **seed rendering to the session
prompt**: for a case seed, the source, `case_id`, title, and a directive to read
the full case (`get_external_case_details`) before pivoting. The case body is
**external narrative, not telemetry** — a claim to verify, which the coverage/
honesty rules (`03 §6.1`) and the footer's seen-id suppression
(`agent/reconcile.go`, `implementation/agent-reliability.md §3` — engine ids
quoted from ticket bodies are not fabrications) already handle.

**Loopback is legitimate, not a hazard.** A case reckon itself opened at
conclusion (the handoff of `07`) returning as a new investigation's seed *is* the
handoff-return workflow (IT finished the reimage → a verification investigation).
The prior investigation is discoverable through the case body and the
entity-appearances surface; no cycle guard is wanted.

---

## 5. Cross-references touched

- `01-domain-model.md §Extension 1` — the fourth shape; "one of three shapes" → four.
- `02-persistence.md` (`InvestigationCreated` payload) — the seed variant list.
- `03-capability-layer.md §2.9` — the read verbs this builds on; §3.5 source_scope prose (names AlertSeed only today).
- `06-knowledge-service.md` — AlertSeed-conditional technique surfacing gains the case case.
- `ui/02-investigation-core.md §2.7` (seed-kind picker) + `ui/05-data-model.md` (seed type enum).

## 6. Deliberate non-decisions (v0 scope cuts)

- **No dedup on `(source, case_id)`** — two analysts may root two investigations
  on one case, as two may on one alert; the appearances/search surface is the
  discovery mechanism. Semantically unrequired; revisit only on field pain.
- **No push/poller triggers** (§0, §4.2).
- **No seed-time case-body entity extraction** — the agent extracts entities
  in-session with provenance; a seed-time extractor is a second, unaudited path.
- **No status sync** between the case and the investigation.

---

*End of design note. Builds on `01-domain-model.md` (the Seed extension),
`03-capability-layer.md §2.9` (the case read verbs + normalization), and
`04`/`08` (the `ticket.*` action family it is deliberately distinct from). It
defines no new primitive — a fourth variant of an existing extension and a
server-side creation flow.*
