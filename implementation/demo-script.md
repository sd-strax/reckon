# reckon demo — the Northwind walkthrough

A narrative demo of the full reckon loop on the bundled demo world: an
AI-native investigation that goes from an alert to a grounded verdict to a
reversible action, and then **compounds** — the concluded case teaches the
knowledge corpus, so the next investigation starts smarter.

Where [`road-test.md`](road-test.md) is the "does the whole thing still hang
together" checklist, this is the **story** you tell an audience. It runs against
the same `lateral-movement-via-rdp` fixtures, now seeded through `reckon demo
seed` rather than baked into `init`.

The fictional org is **Northwind**, a mid-size company whose SOC runs reckon.
Everything below is fixtures — no real tenant, no real adapters.

---

## 0. One-time setup (before the audience arrives)

```
reckon init                 # a fixture-free real install
# --- configure embeddings (see below) before starting ---
reckon start --detach       # brings up the stack and returns once it is ready
                            # (first run downloads deps: add --timeout 10m)
reckon dev-auth             # a local login (the shipped realm has none)
reckon demo seed            # populate the demo world — refuses if not virgin
                            # (--scenario impossible-travel-signin wires the second
                            #  bundled scenario instead; `reckon demo scenario <id>`
                            #  swaps later with the stack down)
reckon stop && reckon start --detach   # restart so the fixture capabilities activate
```

Rebuilding later (after a rehearsal, or when the fixtures change) is one
command: `scripts/demo-reseed.sh` (or `make demo-reseed` from the repo) runs
stop → reset → start → seed → restart unattended, because `start --detach`
returns when the stack is up instead of holding the console.

**Configure vector recall first.** The knowledge rail's relevance scores,
similarity bands, and the "have we seen this before?" case recall are all
cosine-similarity over embeddings — the full product, not a keyword fallback.
Add an embeddings backend to your config (`~/.reckon/config.yaml`) before the
first `reckon start`:

```yaml
knowledge:
  embeddings:
    base_url: https://api.openai.com/v1
    model: text-embedding-3-small
    api_key: env://OPENAI_API_KEY        # a secret REFERENCE, never a literal
```

`api_key` is a secret *reference*, never a literal. For a demo, `env://` is the
least ceremony — `export OPENAI_API_KEY=sk-…` in the shell that runs the stack;
for a longer-lived install use `keychain://<service>/<name>` instead. Either way
this is a *separate* key from your Anthropic BYOK key (the reasoning LLM has no
embeddings endpoint). `reckon demo seed` **refuses without an embeddings
backend** — the demo can never fall into the keyword-only path, because the prior
cases are embedded at seed time so similarity recall can rank them.

`reckon demo seed` then does two things:

- **Seeds two prior concluded cases** into the corpus — an RDP pivot on
  `FINANCE-07` and LSASS dumping on `WORKSTATION-22` — embedded at write time.
  This is the history the compounding loop recalls by similarity.
- **Wires the fixture scenario** (telemetry to query) and **stages the SOP docs
  to disk** (`~/.reckon/sops/`) — it prints the path. The SOPs are *not* loaded
  into the corpus; you import one **live** on stage (Act 2), as part of the story.

Then in the **workbench**: reload, sign in, set your BYOK `ANTHROPIC_API_KEY`.
The fixtures are live and the prior cases are consultable.

> If you seeded onto a dirty install it will refuse and tell you the counts —
> run `reckon demo reset` first (it wipes everything back to a clean install),
> then re-seed.

---

## Act 1 — Start where the analyst starts (the case queue, then the gap)

Nobody opens a blank page and types a hostname at the start of a shift. Work
arrives as cases in the system of record, already assigned and prioritized, and
that is where this investigation begins.

Look at the **Cases** view, under Investigations in the reckon sidebar. It is
the team's queue in the case system of record — the demo tenant names it in
its capability config (`policies.case_queue: SOC Tier 2`) — and three open
incidents are already there: a phishing report, a DLP bulk download, and
**INC0010042 — Suspicious interactive RDP logon to `WIN-FILE01` by
`svc_backup`**, P2, escalated from Tier 1. Click it. The case opens
**read-only**: Tier 1's narrative, the work notes, the case number, and one
button. It does not link out — the document is the read. Say that nothing on
this page writes to the case system. Press **Start investigation**. The new document opens on the case:
its seed line reads *From case … INC0010042* with one small action at its
right, **Note the case**. Nothing interrupts you. When you want the ticket to
know — now, or after the verdict — press it: the comment that tells INC0010042
an investigation was opened here appears, editable; **Post note** requests it
as a `ticket.comment` **action** in this investigation, not fired behind the
analyst's back. It sits pending on the tracker and the seed line now reads *noted
· awaiting approval*; approve it when you want the comment to land. *(The tenant dial `cases.note_on_seed` makes this on-demand,
automatic, or never.)* *(Afterwards the row in the Cases view
reads "investigating" and leads back to this investigation — a case does not
quietly get two. New Investigation → From a case… remains the explicit way to
open a second one.)*

*(The fixture answers the configured queue and nothing else: a free-text
lookup with no queue returns zero cases — which is what Act 3 relies on.)*

The investigation opens **rooted on the case**: the title is the case title, the
seed chip reads `case fixture:lateral-movement-via-rdp: INC0010042` — provenance
is honest that this is a replayed fixture, not a live tenant; say so — and the
agent's standing instruction is to read the full case before it pivots and to
treat the body as an *external claim to verify*, not established fact. Ask it:

> *"What does the case claim, and what have we actually confirmed?"*

The agent calls `get_external_case_details`, restates Tier 1's narrative — the
14:32 UTC RemoteInteractive logon from `10.0.4.20`, the encoded PowerShell thirty
seconds later, the backup on-call who never answered, so legitimate maintenance
is **unconfirmed either way** — and separates it from what we have observed
ourselves: nothing yet. *(That unconfirmed line is Act 3's second hypothesis,
handed to us by the ticket.)*

Then, before pulling a single log, ask in the composer:

> *"Before we dig in — do we have an established runbook for containing RDP
> lateral movement?"*

The agent calls its `recall_sops` tool, finds **nothing**, and says so — *"no SOP
on file for this."* It does **not** invent a procedure: the agent is explicitly
instructed that an empty retrieval is evidence of absence. It may note the prior
case it *does* remember — the RDP pivot on `FINANCE-07` — surfaced on the
**knowledge rail** with a similarity band and a score (the LSASS case on
`WORKSTATION-22` may appear further down, or not at all; promise the one). So
the honest starting state: **we remember a similar incident, but we have no
codified playbook.** That gap is the setup.

*(Say this too, the first time the agent runs: the reasoning happens on your
own key, in the editor. The engine behind it holds no model key, ever. What it
holds is the record.)*

*(Say this one out loud — it's the differentiator: an empty corpus makes the
agent admit it, not hallucinate a runbook.)*

## Act 2 — Teach it the playbook, and watch it land

Northwind *does* have that runbook, sitting in a wiki —
**`~/.reckon/sops/lateral-movement-rdp-containment.md`** (the path `demo seed`
printed). Open it in a tab and walk the audience through it: plain markdown, YAML
frontmatter — title, author, tags, recommendation. Ordinary institutional
knowledge, the kind every SOC has.

Bring it into reckon **live**: run **Import SOPs…**. The Import SOPs panel opens
beside the document with the tab you're reading already listed and ticked —
press **Import**. The row turns green: *Added to the SOP library as "Lateral
Movement via RDP — Containment"*. No library to manage, no re-navigating to the
file, no ceremony. *The import capability, shown, not seeded behind the
curtain.* (**Add files…** on the same panel is the route for documents you
haven't opened.) Now ask again:

> *"Now — with our runbook in hand, what's the containment procedure here?"*

This time `recall_sops` finds the *"Lateral Movement via RDP — Containment"* SOP
**you imported thirty seconds ago**, and the agent grounds its answer in it and
cites it. **Voilà** — the same question, a different answer, because the corpus
changed under it.

Then turn it loose on the investigation itself:

> *"Pull the process, logon, and network telemetry for WIN-FILE01 and tell me if
> this is lateral movement."*

The agent queries the fixture telemetry, assembles the host timeline (the RDP
logon, post-logon discovery, the C2 beacon), and the analytical surfaces
(timeline, entities, evidence) populate as it works.

**The compounding payoff.** As it reasons, the **knowledge rail** shows — with
relevance scores and similarity bands — the RDP SOP you just imported *and* the
prior `FINANCE-07` case, flagged as a near-match. Nobody wired them to this
investigation; the retriever found them by meaning. The analyst decides what the
AI reasons over: tick both to include them. *(The retriever proposes, the analyst
disposes — the injection dial. And the scores and bands are the vector backend at
work, the reason we configured embeddings up front.)*

## Act 3 — Decide (disprove the innocent explanation)

Drive the hypotheses to a verdict the way [`road-test.md` §2–3](road-test.md)
lays out — but the point of this act is that there are **two** of them. Ask:

> *"Frame the competing hypotheses — your leading explanation and the concrete
> alternative that would make it wrong."*

The agent frames **H1** — *stolen `svc_backup` credentials used for
hands-on-keyboard lateral movement over RDP (T1021.001)* — and **H2** — *an
administrator used `svc_backup` over RDP for scheduled maintenance.* The
handles it picks vary from run to run (`H1 adversarial-lateral-movement`,
`H2 legitimate-admin-activity`, and the like); read them off the screen rather
than promising them. Both land `PROPOSED`; the tracker shows them as two
stacked cards, claims in full, each with its tally. *(One claim each. The agent is not allowed to frame a
rival it cannot name a discriminating test for — no strawmen.)*

**Adopt H1** — the adopt card shows the statement *editable*: sharpen it
(*"…from 10.0.4.20, 14:32 UTC"*) and adopt. Say out loud: *the original stays in
the thread; the analyst taking ownership is the moment the claim gets precise.*
Adopt H2 too — it is the one we intend to kill.

**⚡ Propose tests** on H1. The agent reads *both* hypotheses and records
predictions, at least one of which **discriminates**: *"svc_backup's 30-day
logon baseline shows only Network logons from BKP-01 at 02:00 — never
RemoteInteractive, never from 10.0.4.20"* — H1 predicts it, H2 predicts the
opposite (the row carries a **vs H2** chip). Then add the analyst's own test
with **+ prediction** on H2: *"a change ticket or maintenance window covers
WIN-FILE01 for 2026-04-20"* — environmental knowledge the agent does not have;
tick *discriminates H1*. *(Either actor authors tests. The analyst's is usually
the sharpest.)* If the room asks "what about just calling the admin?": tick
**A person tests this** on a prediction instead — the row says *a person tests
this*, and **Record result** takes what they said as an attestation the record
cites, labelled as a human statement, never as telemetry. Say the rule out loud,
because it is the one an audience burned by automation wants to hear: *the AI
can propose this test and ask you for the answer; it can never record the
answer. Only a person can.* Expect **⚡ Run tests** to skip that row and say so
— it stays untested until you press Record result.

**⚡ Run tests.** The baseline query (user-rooted, 30-day window) returns seven
nights of `Network` logons from `BKP-01` and the single `RemoteInteractive`
from `10.0.4.20` — **CONFIRMED** for H1, and the chip says it cuts against H2.
The change-record lookup comes back **COMPLETE with zero cases** — and that
absence is itself an object: the engine mints a *nothing observed* record
carrying the question, the source and the window, and the agent cites it to
record **DISCONFIRMED** for H2. Open the citation from the evidence chip: it
reads *Nothing observed: query_external_cases … (coverage complete)*. *(Say
this one too: "we looked and found nothing" is the most common finding in an
investigation, and here it is evidence you can open, not a sentence in a
report.)* Pin the two evidence chips.

Press **⚡ Decide** on H1 *before* H2's test has run, if you want to show the
guard: the agent declines and names the outstanding test (its words vary), and
if it tried anyway the engine would refuse the write; the guard note on the
row is the stable text — *test it, withdraw it, or record its result
yourself* — and **Record result** is your own way through if the test was a
phone call. Run it, then decide: **H2 REFUTED**, **H1 SUPPORTED**, each citing the
decisive test. The verdict dialog's residual is clean: no undecided
hypothesis, no untested test. Record **MALICIOUS**.

**The line for the room:** *reckon did not just prove the attack — it disproved
the innocent explanation, and the record shows which test did it.*

## Act 4 — Act (evidence precedes action)

Now, and only now, remediate. Request **host isolation** on `WIN-FILE01`. Watch
the authorization chain: Gate 2 evaluates the action, the trust tier is set by
blast radius, a human approves, the action dispatches through the (fixture)
write adapter, and the result comes back honestly. Isolation is reversible —
show the reversal if you want to make the "we never claim an undo we can't
verify" point.

## Act 5 — Conclude and compound (the whole reason for the demo)

Conclude the investigation with its verdict. Two things happen, both client-side
on the BYOK key — **the backend never holds a model key**:

1. **A knowledge summary is written** — a deterministic structured baseline
   (seed, techniques, actions, outcome) enriched by a model narrative drawn from
   the investigation's own conversation. This is what makes the case
   *recallable* later.

2. **A candidate SOP is drafted.** The model generalizes across *this* case and
   the `FINANCE-07` case it consulted — not a retelling of one investigation,
   but a reusable procedure — and opens it as an **editable markdown draft** with
   a prompt: **Add to SOP library** or **Discard**. The analyst reviews it (edits
   if they like), and accepts. *(If an existing SOP already covered it, the model
   says so and proposes nothing — no duplicate clutter.)*

**The other loop closes too.** This investigation was opened *from* a case, and
any handoff the agent files on the way out (`ticket.create` for the reimage,
`ticket.comment` on INC0010042 itself) goes back into the same system of record.
When IT finishes and that ticket lands in the queue again, it is a legitimate
new seed — *cases are investigated, tickets are acted upon*, and the same record
can be both at different moments.

**Close the loop out loud:** the corpus now holds a new SOP that *this*
investigation produced. Start another investigation and its knowledge rail will
surface that very SOP. Each concluded case makes the next one better — with a
human deciding, every time, both what gets acted on and what gets learned.

---

## The other story (same acts, different nouns)

The demo world carries a second scenario, `impossible-travel-signin`: an
account's office sign-in followed two hours later by a successful sign-in from
another country over a legacy mail protocol. No host, no process tree; the
benign alternative is "they were travelling, or on a VPN", and the one test
only a person can run is asking them. Every act above has its twin: the case in
the queue, the competing set, the people-tested prediction closed with Record
result, the empty travel-notice search minting a *nothing observed* record,
containment of the account and the address with no host to isolate. For a room
of identity people, swap before they arrive:

```
reckon stop && reckon demo scenario impossible-travel-signin && reckon start --detach
```

## Reset (graduate to real work)

When the demo's done:

```
reckon stop
reckon demo reset           # wipes investigations, knowledge, fixtures — and the realm
reckon start --detach       # a clean, real install
reckon dev-auth             # recreate the local login (the reset took it)
```

`reset` removes the Postgres cluster (both reckon databases re-initialize empty
on next boot), Temporal's store, and the seeded fixtures. Keycloak keeps its
realm in that same cluster, so your login goes with it: the shipped realm comes
back empty on the next boot, and `dev-auth` recreates the local user and the
direct-access grant. Only the secret store survives. The same install an
analyst test-drove is now an empty, real one, ready for their own adapters and
their own cases.

---

## The one-sentence version

reckon runs the investigation loop with the analyst in command of every
consequential decision — and every concluded case feeds the next, so the SOC's
knowledge compounds instead of walking out the door with the analyst who built
it.
