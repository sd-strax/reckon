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

- **Header** — the title (double-click to rename), the **Progress** strip
  (Investigate · Verdict · Contain · Notify · Close) with the next step's
  action beside it, **Verdict…**, and the lifecycle buttons (**Discard
  draft…** on a draft, **Pause** / **Resume**, **Conclude…**, **Reopen…**,
  **Archive…**).
- **Left region** — **Investigations** (yours, in progress, with **＋** to start
  one; concluded ones under a **Concluded** fold), **Cases** (the case
  system's queue, with **Start investigation** — a case already investigated
  opens its investigation), and the **Capabilities** fold.
- **Lenses** — four views over the same record:
  - **Summary** — verdict, containment, notification, what happened, key
    entities, still to do, procedures followed; **Copy handoff note**,
    **Export report**.
  - **Story** — the evidence graph, ATT&CK, the event-time timeline, not yet
    known.
  - **Reasoning** — the verdict-basis line, the hypothesis cards with their
    predictions, **Pinned evidence**.
  - **Response** — what is **Waiting on you** first, anything **Set aside**,
    then the actions grouped by category, and the conversations with others.
- **Companion** (right, resizable from its left edge) — **Conversation** (the
  agent chat and composer) or **Thread** (the whole record in order, every act
  as a line), plus the **Library** of procedures and past cases.

Clicking any chip, card title or act line opens an **inspector** pane over the
lens (evidence, procedure, turn, action, comms thread, past case); **‹ back**
walks the stack.

---

## 1. Seed + investigate — this populates the analytical surfaces

**＋** on the Investigations header (or **reckon: New investigation**,
`Ctrl/Cmd+Shift+I`) → type **`WIN-FILE01`** → confirm the live hint reads
*"→ investigate host WIN-FILE01"* → **Start investigation**. A typo
("WIN-FILE)1") is refused. The new draft opens on its first step:
**Start investigating…** puts a request in the composer, or type your own:

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
claim, say what it could not check, and keep hypotheses as proposals? Evidence
ids in its prose are chips; what it said on the way folds into the turn's
Activity. *(known: #137 — the answer's shape, scoping before framing, "a
connection is not exfiltration" — the governed prompt change.)* It should
notice **both** RDP logons (jdoe at 14:32:11, svc_backup at 14:32:47).

Ask **"have we investigated WIN-FILE01 before?"** at any point: the agent's
`prior_investigations` lookup finds concluded cases by the entities they
involved. Ask **"have we seen this pattern before?"** *after* telemetry:
similarity compares behaviour. Knowledge reaches the agent only through these
recalls — the Library is to browse; **Ask reckon to use this** on an item
stages the request.

> **On activation (no button).** A fresh investigation starts as a **Draft**.
> Reasoning, hypotheses, pins and the verdict are all allowed in draft. It
> **activates automatically** when you approve its first action (step 4).
> A draft started by mistake can be discarded (**Discard draft…**, with a
> reason); it leaves the queue and nothing is deleted.

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
  closed with **Record result** and an attestation (who, how, and optionally
  where it happened — a link, ticket or message id, or only its fingerprint —
  and where the evidence is kept: e.g. `s3://ir-evidence/2026/call-note.pdf`,
  kept in S3, access via the IR readers group). The result's inspector shows
  **Where the evidence is kept**. A pre-signed link (`?X-Amz-Signature=…`) is
  refused with a reason. There is no upload: reckon records the location,
  never the file. The agent proposes such tests but never records them.
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
outstanding (inconclusive reads `~`, untested `?`); act lines in the Thread say
what each act was, in words. A pin offers **Request action on this…** for when
the agent cannot act.

## 3. Verdict — grounded in the pins

**Record verdict…** beside the Progress strip once every hypothesis is decided
(or **Verdict…** at the top right) → the dialog shows the pin gate and what
this verdict does not cover → **MALICIOUS** → rationale → **Record verdict**.

**Expect:** the Summary's Verdict tile updates; a ⚖ line in the Thread.

*Verdict revision:* record a second verdict. **Expect:** both ⚖ lines stand out
in the Thread with their dispositions, linked ("replaced at …" / "replaces the
… verdict"), and the Summary's verdict tile says it replaced an earlier one.

## 4. Remediate — evidence precedes action

Ask, one at a time or together:

> *"Isolate WIN-FILE01, block the C2 IP at the perimeter, disable svc_backup,
> and notify #itops on Slack that it's contained and ready for reimage — follow
> up in 48h."*

**Expect** approval cards in the conversation and in the **Response** lens,
each grounded (an action without cited evidence is refused by the engine):

- `host.isolate` → WIN-FILE01, REVERSIBLE
- `ioc.block` → the C2 indicator, BEST_EFFORT
- `account.disable` → CONTOSO\svc_backup (or jdoe), REVERSIBLE
- `notify.slack` → **Approve · Send**: the message text is the approval surface

Every open card shows **Will do** — what it will write or change — and a
message or ticket write can be changed with **Edit…** before approving (the
record keeps what it said before). **Decide later** sets a card aside until its
window closes.

Approve them. **Expect:** the first approval **activates** the investigation;
dispatches succeed; each card shows SUCCEEDED; the notify opens a comms thread
under **Response** (`#itops`, awaiting reply, 48h clock).

### 4a. Reverse

On the succeeded **host.isolate · WIN-FILE01** card → **Reverse…** → reason →
a **host.unisolate** card at the same tier → approve. **Expect:** the original
shows **REVERSED**. On `ioc.block` it reads **Reverse (best effort)…**: the
original stays SUCCEEDED, marked reversal attempted. Once a reversal is
requested the original shows it instead of the button.

### 4b. Expiry and re-request

Leave a pending card alone past its window (30 minutes). **Expect:** it shows
**EXPIRED** with **↻ Re-request…**; re-requesting creates a new card marked
"↻ retry of …"; the original then shows "↻ re-requested as …" in place of the
button. Each action can be re-requested once.

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
`replied` → **Acknowledge** → **Snooze 24h** → **Follow up…** (a new message
on the same thread, approved like any) → **Mark done**.

## 5. Conclude

**Conclude…** → the helper shows the verdict it consumes and warns about open
comms threads. **Expect:** status → CONCLUDED.

The Summary of the concluded case shows whether its knowledge summary's
narrative was written. **Draft SOP from this case** asks reckon for a reusable
procedure: a decline says why (**Draft anyway**); a draft opens beside the
panel, editable, with **Add to SOP library** / **Discard** on the panel.

**Export report** (Summary lens) → the report opens rendered beside the panel;
**Save report…** writes it where you choose. Read it as the ticket attachment
it would be.

**Reopen…** a concluded investigation with a reason → ACTIVE again; the earlier
conclusion stays in the Thread. Concluded investigations stay findable under
**Concluded** in the left region.

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
  two-layer structure, every citing record's evidence, ungrounded reasoning
  flagged.
- **Response:** decisions first, then every action by category, newest first.
- **Library** (companion): procedures and past cases as separate lists;
  **Search** (relevance in words) with **Clear**; **Import procedures…**; a
  procedure's inspector shows only what the agent actually read here.
- **Capabilities** (left): an unavailable integration → **Enable…** → a form
  from the integration's schema → apply; the capability surface updates
  without a restart.
- **Pause / Resume** (header): the composer is parked while paused.
- **reckon: Reset agent context** (Command Palette): a ⟲ boundary in the
  Thread; the agent re-grounds from the record on the next turn.

**Not reachable in the demo:** T3 approval with a typed challenge, two-party
approval and policy auto-approval need a policy file under
`capability.policy_dir` (see `examples/policies/`) and a restart; only
`email.purge` defaults to T3.

A **cited absence**: ask *"did svc_backup log on to any host after 14:33Z on
Apr 20?"* — the answer is a complete, empty search the agent cites as "nothing
found".

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
