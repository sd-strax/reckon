# reckon road test — investigate → conclude → remediate

A manual, end-to-end exercise of the v0 fixture stack through the **workbench
UI**. It follows the natural analyst loop: understand the host, drive the
hypotheses to a verdict, *then* act — because remediation lands clean only when
evidence precedes it.

Everything is in the workbench except the one step tagged **[simulate vendor]**
(an outside party replying — no UI by design in v0). It runs entirely against
the `lateral-movement-via-rdp` fixture scenario; there are no real adapters.

> Keep this in sync when a surface changes. It doubles as the "does the whole
> thing still hang together" check before a demo. Findings from a run are filed
> as short issues under a `road-test-*` label; the known ones are noted inline
> below as *(known: #N)* so a run does not re-report them.

**Prereqs:** stack running on the current binary, **demo world seeded**
(`reckon demo seed` once, after `start` + `dev-auth`, then restart — a plain
`init` is fixture-free, so the demo is opt-in), workbench reloaded, signed in,
BYOK `ANTHROPIC_API_KEY` set. Start a **fresh** investigation so the arc is
clean. (`reckon demo reset` wipes it all back afterward.)

A change to the demo's capability or action config
(`~/.reckon/capability/*.yaml`, fixtures) needs a **stack restart**; a workbench
change needs only **Developer: Reload Window**.

---

## The layout

One investigation panel, four regions:

- **Header** — the title (double-click to rename), the stage strip
  (Investigate · Verdict · Contain · Notify · Close — progress, not buttons),
  **Verdict…**, and the lifecycle buttons (**Pause** / **Resume**,
  **Conclude…**, **Reopen…**, **Archive…**).
- **Left region** — **Investigations** (yours, in progress), **Cases** (the case
  system's queue, with **Start investigation**), and the **Capabilities** fold
  (what the tenant can query, with **Enable…** forms).
- **Lenses** — four views over the same record:
  - **Summary** — verdict, containment, notification, what happened, key
    entities, still to do, procedures followed; **Copy handoff note**,
    **Export report**.
  - **Story** — attack path, ATT&CK, the event-time timeline, the evidence
    graph, not yet known.
  - **Reasoning** — the verdict-basis line, the hypothesis cards with their
    predictions, **Pinned evidence**.
  - **Response** — the actions grouped by kind, and the comms threads.
- **Companion** (right, resizable from its left edge) — **Conversation** (the
  agent chat and composer) or **Thread** (the whole record in order, every act
  as a line), plus the **Knowledge** section.

Clicking any chip, card title or act line opens an **inspector** pane over the
lens (evidence, procedure, turn, action, comms thread, past case); **‹ back**
walks the stack.

---

## 1. Seed + investigate — this populates the analytical surfaces

**reckon: New investigation** (`Ctrl/Cmd+Shift+I`, Command Palette only for
now — *known: #162*) → type **`WIN-FILE01`** → confirm the live hint reads
*"→ investigate host WIN-FILE01"* → **Start investigation**. The new draft
opens on an empty Summary *(known: #160)*: switch the companion to
**Conversation** and start there.

> *"Investigate WIN-FILE01 for signs of compromise — what happened on this host?
> Pull the process, logon, and network telemetry and tell me if this looks like
> lateral movement."*

**Expect:** the agent queries the fixture telemetry (RDP logons, the C2 network
flow, the detection), describes what it found, and **proposes a competing set**
(default `reasoning.hypothesis_framing: leading_null`): a leader such as
**`H1 svc_backup compromise`** and one concrete alternative such as
**`H2 legitimate backup`**. They appear as cards in the **Reasoning** lens;
tool-result rows in the conversation carry evidence chips and a **📌 Pin**.

Read the answer as an analyst would: does it lead with the finding, cite each
claim, say what it could not check, and keep hypotheses as proposals? *(known:
#137 — shape, scoping before framing, "a connection is not exfiltration";
#136 — rendering)*. It should notice **both** RDP logons (jdoe at 14:32:11,
svc_backup at 14:32:47).

Ask **"have we seen this before?"** only *after* telemetry: similar-case recall
compares behaviour, and a bare host name has none. On a fresh demo there is one
seeded prior case; after a concluded run on this host there should be two
*(known: #163 — no exact by-entity lookup; an over-filtered recall reads as
"not seen before")*.

> **On activation (no button).** A fresh investigation starts as a **Draft**.
> Reasoning, hypotheses, pins and the verdict are all allowed in draft. It
> **activates automatically** when you approve its first action (step 4).
> A draft started by mistake cannot be discarded yet *(known: #161)*.

## 2. Drive the hypothesis loop

Work the cards in the **Reasoning** lens:

- **✓ Adopt** — an AI-**proposed** hypothesis must be adopted before the agent
  can test or decide it (`trust.ai_reasoning` is off by default). The adopt
  card shows the **statement editable** — sharpen it (add the source IP and
  time) and adopt; the original stays in the record. Adopt **H2** as well.
- **⚡ Propose tests** → the agent reads *both* hypotheses and records
  falsifiable predictions; at least one **discriminates** (the rival predicts
  the opposite). **Expect** the account-baseline test: *"svc_backup's 30-day
  logon baseline shows only Network logons from BKP-01 at 02:00…"*.
- **+ prediction** on **H2** → add your own (*"a change ticket or maintenance
  window covers WIN-FILE01 on 2026-04-20"*, discriminating H1). **Withdraw** any
  test that comes out the same under both (a status, never a deletion).
- **A person tests this** — a prediction only a human can answer (the account
  owner confirms the session; choose this when adding the prediction) is
  closed with **Record result** and an attestation (who, how). The agent
  proposes such tests but never records them.
- **⚡ Run tests** / **⚡ Test this** → outcomes recorded citing evidence. The
  baseline query (user-rooted, 30-day window) → `CONFIRMED`; the change-record
  lookup → COMPLETE, zero cases → `DISCONFIRMED` for H2.
- **📌 Pin** the decisive evidence. It appears under **Pinned evidence**, where
  the pin's remove button (with a reason) supersedes it — it stays visible,
  struck.
- **⚡ Decide** on H1 while a discriminating test is still untested → the agent
  **declines** and names the outstanding test. Run it, then decide: **H2
  REFUTED**, **H1 SUPPORTED**.

**Expect:** the cards and the Summary's **Still to do** agree on what is
outstanding *(known: #140)*; act lines in the Thread say what each act was
*(known: #138, #153)*.

## 3. Verdict — grounded in the pins

**Verdict…** (header, top right — not the stage strip, *known: #142*) → the
dialog shows the pin gate and the residual *(known: #143)* → **MALICIOUS** →
rationale → **Record verdict**.

**Expect:** the Summary's Verdict tile updates; a ⚖ line in the Thread.

*Verdict revision:* record a second verdict. **Expect:** the earlier ⚖ line is
struck and tagged SUPERSEDED in the Thread, and only the new one counts.
*(known: #155 — the replaced verdict is dim, far up the Thread, and the line
omits the disposition)*

## 4. Remediate — evidence precedes action

Ask, one at a time or together:

> *"Isolate WIN-FILE01, block the C2 IP at the perimeter, disable svc_backup,
> and notify #itops on Slack that it's contained and ready for reimage — follow
> up in 48h."*

**Expect** approval cards in the conversation and in the **Response** lens,
each grounded (an action without cited evidence is refused by the engine):

- `host.isolate` → WIN-FILE01, REVERSIBLE
- `ioc.block` → the C2 indicator, BEST_EFFORT
- `account.disable` → CONTOSO\svc_backup, REVERSIBLE (any other account fails
  in the demo, *known: #149*)
- `notify.slack` → **Approve · Send**: the message text is the approval surface
  *(known: #124, #127 — not every card shows what it will write; no edit
  before approve)*

Approve them. **Expect:** the first approval **activates** the investigation;
dispatches succeed; each card shows SUCCEEDED; the notify opens a comms thread
under **Response** (`#itops`, awaiting reply, 48h clock).

### 4a. Reverse

On the succeeded **host.isolate · WIN-FILE01** card → **Reverse…** → reason →
a **host.unisolate** card at the same tier → approve. **Expect:** the original
shows **REVERSED**. Reverse is offered only on REVERSIBLE actions *(known:
#151 — none for BEST_EFFORT `ioc.block`)*, and stays offered after a reversal
was requested *(known: #152)*.

### 4b. Expiry and re-request

Leave a pending card alone past its window (30 minutes). **Expect:** it shows
**EXPIRED** with **↻ Re-request…**; re-requesting creates a new card marked
"↻ retry of …". Approve or reject it. *(known: #152 — the original keeps
offering Re-request; repeated retries can send the same message twice)*

### 4c. Comms round-trip

**[simulate vendor]** IT replies. Grab the thread id, then post an inbound reply:

```bash
INV=<the investigation id>
curl -s http://localhost:8080/api/investigations/$INV/comms \
  -H "Authorization: Bearer $TOKEN" | jq -r '.threads[] | "\(.thread_id)  \(.target)  \(.status)"'

curl -s http://localhost:8080/api/comms/inbound \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"thread_id":"<THREAD_ID>","author":"Mike Torres","body":"Reimage scheduled tomorrow 9am."}'
```

**Refresh** (live updates are not wired). **Expect:** the thread shows
`replied` → **Acknowledge** → **Snooze 24h** → **Mark done**. (**Follow up…** is
refused today, *known: #150*.)

## 5. Conclude

**Conclude…** → the helper shows the verdict it consumes and warns about open
comms threads. **Expect:** status → CONCLUDED.

Concluding drafts a candidate SOP in the background; **don't reload the window
for a minute** after concluding, or the draft is lost silently *(known: #156 —
to become an explicit step; #157)*. If one is warranted, an editable draft
opens with **Add to SOP library** / **Discard**.

**Export report** (Summary lens) → the report opens rendered beside the panel;
**Save report…** writes it where you choose. Read it as the ticket attachment
it would be.

**Reopen…** a concluded investigation with a reason → ACTIVE again; the earlier
conclusion stays in the Thread. Concluded investigations leave the
Investigations list *(known: #158)*.

---

## Lenses — check these anytime (they're views, not steps)

- **Thread** (companion): read top to bottom — investigate → hypothesis acts →
  pins → verdict → actions → comms → conclude, in causal order. The "case file
  a colleague could read at handoff" test.
- **Evidence inspector:** click any cited chip → human title, provenance,
  structured fields, and where else this entity was cited. Raw JSON is one
  click away.
- **Story → Timeline:** the two-clocks view — the decisive fact is the gap
  between the two RDP logons, in *event* time. **Story → evidence graph:** the
  two-layer structure, ungrounded reasoning flagged. *(known: #141 — both can
  come up empty when the thread cites evidence only indirectly)*
- **Response:** every action with its status, grouped by kind, newest first
  *(known: #123, #125)*.
- **Knowledge** (companion): the SOPs and past cases; **Search**; *Import SOPs…*
  is still a command *(known: #128, #129–#132)*.
- **Capabilities** (left): an unavailable integration → **Enable…** → a form
  from the integration's schema → apply; the capability surface updates
  without a restart.
- **Pause / Resume** (header): the composer is parked while paused.
- **reckon: Reset agent context** (Command Palette): a ⟲ boundary in the
  Thread; the agent re-grounds from the record on the next turn.

**Not reachable in the demo:** T3 approval with a typed challenge, two-party
approval and policy auto-approval need a policy file under
`capability.policy_dir` (see `examples/policies/`) and a restart; only
`email.purge` defaults to T3. A filtered telemetry question never comes back
empty, so a cited absence cannot be exercised *(known: #164)*.

---

## Appendix — the dev token (only for the step-4c curl)

```bash
TOKEN=$(curl -s http://localhost:8543/realms/reckon/protocol/openid-connect/token \
  -d grant_type=password -d client_id=reckon \
  -d username=reckon-admin -d password=reckon | jq -r .access_token)
```

Short-lived — re-run on a 401. If it fails outright, run `./bin/reckon dev-auth`
once first to provision the local principal and enable the direct-access grant
(dev/CI only).

**As you go, note per surface:** did it work, did it read right, and did anything
need a curl that should have had a UI.
