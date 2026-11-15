# Agent Roles and Model Tiering — Design Proposal (DRAFT, not yet an ADR)

## 0. Framing

reckon runs one `agent.Session` per investigation: one model (`claude-opus-4-8` by
default), one system prompt, one tool set, one loop (`agent/session.go`). Every model
call in that loop resends the same prefix (system + tools) and the whole conversation,
and the conversation carries every tool result the model has read since the last
compaction — raw OCSF JSON up to 128 KB per page (`adr/0209`). The side products
(summary, narrative, SOP draft) replay that same conversation on the same model
(`adr/0234`). The eval's judge and reader are the only places a second, cheaper model is
used today (`adr/0059`, `adr/0069`).

This note proposes splitting the loop into **roles**, each with its own model, prompt,
tool subset and — the part that matters most for cost — its own **context**. The thesis:

> The token drawdown is a *context* problem before it is a *price* problem. Raw evidence
> is read by the most expensive model, kept in its history, and re-read on every round of
> every later turn. Moving the reading of evidence to cheap, disposable contexts shrinks
> the expensive context by an order of magnitude; choosing cheaper models for the roles
> that extract rather than judge is the second-order win.

Nothing here changes an architectural commitment. The AI remains a delegate; every write
still goes through the backend under the delegate token; the engine gates (aggregate
write protection, evidence_refs floor, discriminator gate, blast-radius escalator) hold
whichever model or how many models sit in front of them. The deterministic guards
(`[clock]`, `[reasoning state]`, `[engine state]` anchor, the reconciliation footer) stay
exactly where they are, on the role that talks to the analyst.

| Owned here | Owned elsewhere (authoritative) |
|---|---|
| The role split, each role's contract, model tier and context | The loop mechanics and turn commit (`05 §3.4`, `agent/`) |
| How delegated work enters the transcript, the tool-call log and the usage record | Prompt management discipline (`09`), engine-text ledger (`adr/0057`, `adr/0232`) |
| The model profile (config shape) and its eval attribution | The eval harness and its regression rule (`10`) |

---

## 1. Where the tokens go today

Read from `agent/session.go`, `context.go`, `bounded.go`, `anthropic.go` and the eval's
pricing table (`eval/report.go`):

1. **Every round is a full-context call.** A turn runs up to 16 model↔tool rounds
   (`defaultMaxToolRounds`). Each round resends system + tools + the whole history.
   Prompt caching makes the resend a cache read ($0.50/M on Opus 4.8 instead of $5/M)
   but every new tool result is a cache *write* ($6.25/M) the first time it is seen,
   and then a cache read on every later round of this turn and every round of every
   later turn until compaction.
2. **Tool results are the mass of the history.** One page is up to 128 KB (~40k
   tokens). A look-and-report turn that makes three or four pivots adds 60–120k tokens
   of OCSF JSON to the conversation. The prose the analyst reads is a few hundred tokens.
3. **Compaction is late by design.** `fitContext` acts at three quarters of the budget
   (~150k tokens) and condenses to half (`adr/0230`). The session therefore runs with
   75–150k tokens of history most of the time, so the *steady-state* cache read per
   round is $0.04–$0.08 before any new work is done.
4. **Opus for everything, including the trivial.** A ⚡ `Test P3` turn (one declared
   test query, one result, one `record_prediction_outcome`) and "is the isolation done?"
   (answered from the `[engine state]` block) pay the same per-token price and carry the
   same history as a full investigative pivot. The three side completions replay the
   whole conversation with the tools defined, on Opus, to write prose.
5. **Output at $25/M.** The `answer-shape` rule produces a four-section report per
   look-and-report turn, plus rationales, on the most expensive output tier.

Two things are *not* the cause: the system prompt (≈2.5k tokens, byte-stable, cached)
and the intrinsic tool definitions (≈12 KB, cached). They are already handled well.

### 1.1 An illustrative turn (estimate, not a measurement)

Assumptions: prefix 12k tokens (system + ~25 tools), three pivots returning 30k tokens
each, six rounds, ~800 output tokens per round, Opus 4.8 at the repo's pricing table.
The eval baselines do not store usage, so this is arithmetic over the loop's shape, to
be replaced by a measured profile (§6, phase 0).

| | Today (one Opus context) | Proposed (lead + retriever) |
|---|---|---|
| Cache writes | ~100k tokens of new results × $6.25/M ≈ $0.63 | ~8k tokens of digests × $6.25/M ≈ $0.05 |
| Cache reads over 6 rounds | ~335k × $0.50/M ≈ $0.17 | ~80k × $0.50/M ≈ $0.04 |
| Output | ~5k × $25/M ≈ $0.12 | ~5k × $25/M ≈ $0.12 |
| Retriever (Haiku 5.5) | — | 3 tasks × ~32k in × $0.10/M + out ≈ $0.02 |
| **Turn** | **≈ $0.90** | **≈ $0.25** |
| History carried into the next turn | ~110k tokens | ~15k tokens |

The last row is the structural win: the next ten turns stop paying a cache read on
100k+ tokens per round, and compaction stops throwing away evidence the analyst may ask
about. The per-turn saving compounds across the session.

---

## 2. The design principle: split by what the step needs

A role is justified when a step needs a **different context** or a **different kind of
model work** from the lead. Three kinds of work appear in the loop:

- **Judgment** — framing rivals, deciding against them, deciding what to pivot on next,
  speaking to the analyst, requesting an action. Needs the conversation, the engine
  state and the strongest model. This is the lead.
- **Directed reading** — "read this 90 KB of logons and tell me the distinct sources,
  accounts, and anything after 02:00, with the refs". Needs the raw data and the
  question; does not need the conversation; needs a cheap model with a large window.
- **Composition over the record** — narrative, summary, SOP draft. Needs the engine's
  record (hypotheses, tests, actions, thread), not the model's working memory; no
  tools; a mid-tier model writes prose as well as Opus does.

Everything below follows from putting each step in the cheapest context that can do it
honestly, and keeping the trust properties in mechanisms, not in a model's goodwill
(`implementation/agent-reliability.md`).

---

## 3. The roles

### 3.1 Interactive roles (client-side, in the sidecar, BYOK)

| Role | Model tier (default) | Context | Tools | Produces |
|---|---|---|---|---|
| **Lead** | Opus (`claude-opus-4-8` today; evaluate `claude-opus-5-5`) | The conversation (prose + digests), engine blocks | Intrinsic reasoning/action tools + `gather_evidence`, `consult_knowledge`, `test_prediction` | The analyst-facing reply, hypotheses, predictions, decisions, action requests |
| **Retriever** | Haiku (`claude-haiku-5-5`); Sonnet when a task needs a pivot decision | Per task, disposable: the task, the raw pages | Capability verbs, `read_result` | A ref-gated evidence digest with coverage |
| **Tester** | Sonnet (`claude-sonnet-5-5`) | Per prediction: owner hypothesis, rivals' expectations, the prediction's `test_query`, the result | The one verb the prediction declares, `record_prediction_outcome` | A recorded outcome citing refs, or a reason it could not decide |
| **Librarian** | Haiku | Per question | `recall_sops`, `read_sop`, `recall_similar_investigations`, `prior_investigations` | The matching sections verbatim with `sop_id`/revision/cut/section so provenance and `cited` still work |
| **Scribe** | Sonnet | The engine record (`narrativeRecord`) + the collapsed prose of the conversation; no tools | none | Summary narrative, investigation narrative, SOP candidate |
| **Router** (free text only) | Haiku, ~1k tokens | The analyst's message + the turn's engine blocks | none | A turn kind; ambiguous → lead |

### 3.2 Async roles (server-side, Temporal, tenant credentials — `05 §3.4`)

| Role | Model tier | Notes |
|---|---|---|
| **Post-conclusion summariser** | Sonnet or Haiku, via the Batch API | `temporal/summary_activities.go` writes the structured baseline deterministically and leaves `GeneratorModel` empty "no LLM narrative yet". The narrative, IOC suggestions and candidate SOP are author-pending by `07 §1.3`; none is latency-sensitive, so the 50%-off batch path fits. |
| **Eval judge / reader** | Sonnet (already) | Unchanged; the precedent for role-scoped models in this repo. |

### 3.3 What is deliberately *not* a role

- **An action-payload formatter.** Payload malformation under long context is real
  (`agent-reliability.md §7`), but the fix is `strict: true` on `request_action`'s
  schema and the smaller lead context this design produces — not a second model
  between the lead's decision and the engine.
- **A prose auditor / verifier.** `agent-reliability.md §3` deferred a second-model
  auditor on purpose: an auditor can hallucinate, the deterministic stack cannot. The
  footer, the anchor and the clock stay the trust mechanism. Any model-based check of
  the lead's prose belongs in the eval (where the judge already lives), not in the loop.

---

## 4. Role contracts in detail

### 4.1 Lead

Prompt: the current ledger (`agent/prompt.go`), minus the rules that only existed
because the lead read raw results itself (`coverage` is restated as "the digest's
coverage line"; `raw-data-verbatim` becomes "ask the retriever for `verbatim`"), plus
one new section on delegation (when to ask for evidence, how to phrase a purpose, that a
digest's refs are the only refs it may cite). Each rule gets a ledger entry as today;
`PromptVersion` is computed per role (§5.4).

Tools: the eleven intrinsic tools stay, except `read_result` (paging is the
retriever's). The capability verbs are **not** offered to the lead in the end state; in
the migration they stay available with a small page (§6). Three delegate tools are added.

```
gather_evidence
  purpose      string   what the answer is for, in the lead's words (required)
  calls        []{verb, entity, window, extra}   optional; omitted = retriever chooses
                                                  among the verbs the catalog offers
  want         "digest" | "verbatim" | "both"    verbatim reproduces exact field values
  max_facts    int

→ {
  digest         string            prose answer to the purpose, ≤ N tokens
  facts          []{claim, refs[]} each claim with the engine refs that support it
  coverage       {status, missing[]}   COMPLETE / PARTIAL / UNAVAILABLE_* per call
  verbatim       []{field, value, ref}  only when asked
  capability_call_ids []string      the engine's ids for the calls made (adr/0092)
  dropped_refs   []string           refs the retriever wrote that were not in its inputs
}
```

`consult_knowledge{question, tags?}` and `test_prediction{prediction_ref}` have the same
shape: a purpose in, a structured result with provenance out.

Everything else on the lead is unchanged: the `[clock]`/`[reasoning state]`/`[engine
state]` blocks on the user message, the periodic anchor, the reconciliation footer folded
into its own history, the commit of one interpretation per turn, rehydration from prose.

### 4.2 Retriever

A small `Session`-like loop (`agent/delegate.go`) with its own system prompt
("you read telemetry for an investigator; you answer the purpose; you cite refs; you
state coverage; you do not reason about guilt"), the verb tools, `read_result`, a round
budget of ~6 and a token budget per task. Its history is created for the task and
discarded. It runs at `effort: low`.

**Ref gating (deterministic, the trust seam).** The retriever's `facts[].refs` and
`verbatim[].ref` are checked against the set of UUIDs that appeared in the raw tool
results *this task* received (the same regex and bookkeeping as `rememberIDs`). A ref
not in that set is removed and listed under `dropped_refs`; the lead's `seenIDs` is fed
the task's set. This extends the existing ground-truth stack to delegates without
judging prose: the engine can attest that every ref the lead will go on to cite was
produced by a real call. What it cannot attest is that the *claim* beside the ref is a
correct reading — that is a quality property graded by the eval (G1, coverage, and a
new assertion: a fact's refs resolve to events consistent with the claim, checkable
offline against fixtures).

**Structured output.** The result is a JSON object validated against a schema
(structured outputs, or `strict` on a single "answer" tool); a response that does not
validate is retried once at the retriever's own cost, then returned to the lead as an
error result — the lead decides what to do, as with any failed tool.

**Why Haiku is safe here.** The retriever extracts; it does not decide. Where the
reading *is* the decision (a discriminating prediction), the work goes to the tester on
Sonnet. The retriever's model is part of the profile, so a tenant that wants Sonnet
reads can have them.

### 4.3 Tester

Productises the eval's reader (`adr/0069`): given one prediction, run its declared
`test_query` (tool + parameters are already data on `reckon-prediction`), read the
result, and record CONFIRMED / DISCONFIRMED / INCONCLUSIVE with `test_result_refs` —
or decline with a reason (coverage, a manual test, the engine's refusal). Its context is
the hypothesis statement, the rivals' expectations for this prediction
(`discriminates`), the prediction, and the result. It never sees the conversation and
never calls `evaluate_hypothesis`: deciding the hypothesis stays with the lead, which
is where the discriminator gate and the `decide-against-rivals` rule are graded.

The ⚡ `Test P<n>` and ⚡ `Run tests` turns route straight to it (the label is a
deterministic signal; no router needed); the lead's `test_prediction` tool reaches the
same code when the lead is in a free turn. A tester outcome enters the turn transcript
and the thread exactly as the lead's own `record_prediction_outcome` would, with the
delegate named (§5.2).

### 4.4 Librarian

Runs the recall tools for a question and returns sections verbatim with their
provenance fields. `recordConsulted` runs on the dispatch (shared code), so
`consulted_sops` / `consulted_similar_investigations` on the turn are correct and the
`cited` rule (`adr/0236`, `adr/0303`) is unchanged: the lead must still name the
`sop_id` in its own text. `adr/0129` holds — knowledge still reaches the model only
through a tool call made during the turn, never by injection.

### 4.5 Scribe

`SummarizeConcluded`, `DraftNarrative` and `ProposeSOP` move off the lead's
conversation. The scribe reads what `narrativeRecord` already assembles (clock, status
and verdict, hypothesis block, action block, thread acts, tests) plus the collapsed
prose of the conversation (the shape `rehydrate` produces), and writes with no tools on
Sonnet. `adr/0103` already states the narrative is "of the record, not of the
conversation"; this makes the summary and SOP draft the same. The `adr/0234` problems
(a second analyst message in a row, tools defined with `tool_choice: none`, fitting to
the lead's window) disappear because the request is not a continuation of the lead's
history. The scribe stays behind the sidecar's refuse-when-busy entry so cancel still
reaches it.

### 4.6 Router

Only for free-text analyst messages, and conservative: it picks a cheaper path only for
two shapes it can name — a **state question** ("is the isolation done?", "where does P2
stand?") answered by a Sonnet call over the engine blocks plus a `list_actions` read,
and a **verbatim data request** answered by the retriever in `verbatim` mode and
relayed — and sends everything else, including anything ambiguous, to the lead.
The ⚡ family and slash commands never pass through it. Its cost is ~1k tokens on Haiku;
its errors are bounded to "an Opus turn that could have been cheaper", never the
reverse, because the two cheap paths are themselves held to `never-assume-state`
(they read the record on the turn).

---

## 5. Orchestration mechanics

### 5.1 Process and code shape

Everything interactive stays in the sidecar process (`reckon investigate --stdio`):
one BYOK key, one `Backend` client, one delegate token. In Go:

```go
type Role string
const (RoleLead Role = "lead"; RoleRetriever = "retriever"; RoleTester = "tester";
       RoleLibrarian = "librarian"; RoleScribe = "scribe"; RoleRouter = "router")

type ModelSpec struct {
    Model          string
    Effort         string // "", low, medium, high
    MaxTokens      int
    ContextWindow  int
    MaxRounds      int
    TaskTokenBudget int   // delegates only
}
type ModelProfile map[Role]ModelSpec

type Config struct {
    Backend *Client
    LLMs    func(Role) LLM      // one provider, per-role model; tests script per role
    Profile ModelProfile
    ...
}
```

`Session` keeps the lead's state. Delegates are `delegateLoop` values built per call
from the profile: own prompt, own tools, own `resultStore`, own usage. A delegate tool
on the lead is a `tool` whose `run` builds the loop, runs it under the turn's context
(cancel propagates), and returns its structured result as the tool result — so to the
lead's loop a delegate is just a tool, and `dispatchTool`, the transcript, the tool-call
log and the hooks need no special case beyond tagging.

### 5.2 One transcript, one tool-call log, one commit

A turn is still committed as one interpretation with one transcript. Delegated work is
nested in it:

```
[tool_use gather_evidence id=tu_1] {"purpose": "...", ...}
[delegate retriever id=tu_1 model=claude-haiku-5-5]
[delegate:tu_1 tool_use enumerate_logons id=tu_1.1] {...}
[delegate:tu_1 tool_result enumerate_logons id=tu_1.1 error=false] {...raw...}
[delegate:tu_1 assistant] ...
[delegate end tu_1 rounds=2 usage=...]
[tool_result gather_evidence id=tu_1 error=false] {"digest": ..., "facts": [...]}
```

`ToolCall` gains `Delegate string` (the role) and `ParentCallID`; every real verb
dispatch a delegate makes is a row in `ai_tool_calls` with its `capability_call_id`
(`adr/0092`), so "said vs did" (`agent-reliability.md §6`) is still a table lookup.
The committed act records the models used: `actor.model` stays the lead's; a new
`delegate_models` map and a per-role `usage` ride the interpretation payload, and the
event view / Person inspector can show "with evidence read by claude-haiku-5-5".

### 5.3 Context and caching per role

- Lead: unchanged window budget; the history is prose and digests, so compaction rarely
  fires and rehydration is unchanged. Its cached prefix (tools → system) is stable; its
  history no longer churns the cache with 40k-token pages.
- Delegates: a fresh context per task, under a task token budget; no caching value
  beyond the small per-role prefix. The retriever's prefix is its tools and a short
  system prompt; placing one breakpoint on it is still worth it across tasks in a turn.
- Caches are model-scoped; nothing is shared between roles and nothing needs to be.

### 5.4 Versioning and attribution

`PromptVersion` becomes per role (each role has its own ledger and engine-text entries;
`TestPrompt_ComponentsFrozen` pins each). `ToolSetHash` becomes per role. The eval's
attribution gains the **profile**: a map role → model, hashed, and per-role usage in the
report (the judge and reader already report apart). Baselines are keyed by
`(scenario, profile)`; the regression rule compares like with like as today. The
single-model profile (`all roles = claude-opus-4-8`) is the control arm and the
back-compat default until the tiered profile has accepted baselines on all three
scenarios.

### 5.5 Config surface

Workbench: `reckon.model` is kept as a shorthand that sets every role; a new
`reckon.models` object sets roles individually; a named preset (`balanced`, `quality`,
`single-model`) is the UI. CLI: `RECKON_MODEL` keeps its meaning; `RECKON_MODEL_<ROLE>`
overrides a role. Server: none of this reaches the backend except the attribution on
the record, by commitment 7 (`05 §2`).

### 5.6 Failure handling

A delegate failing (provider error, budget exhausted, invalid structured output) is an
error tool result to the lead, in the words the loop already uses for a tool that did
not run; the lead may retry, narrow the purpose, or report the gap under *Not covered*.
A delegate never writes except through the tools it is given, under the same delegate
token, so a misbehaving retriever cannot record an interpretation and a tester cannot
decide a hypothesis. Cancel stops a delegate between its own steps, as `adr/0239`
stops the lead; a dispatched verb finishes.

---

## 6. Migration path

Each phase ships with its own eval profile and baselines; a phase is accepted when both
original scenarios and the cloud scenario are clean on it (`adr/0058`, `adr/0339`).

**Phase 0 — measure.** The baselines do not carry usage. Add per-call usage (round,
bytes in, cache write/read, out) to the trial record and a per-turn breakdown to the
report; run the current single-model profile on all three scenarios to get the real
drawdown profile that §1.1 estimates. Also record how often compaction fires and what it
drops. One day of work; it decides how aggressive phases 1–3 should be.

**Phase 1 — big results go to a reader, side products go to the scribe.** The smallest
diff with the largest win. In `bounded.go`, a result over the bound is today shown as
page 1 plus `read_result`; instead it is handed to the retriever with the calling tool's
`purpose` (a new optional field on every verb tool; the prompt asks the lead to state
it) and the lead receives the digest. Small results still arrive raw, so the lead's
behaviour on short answers is unchanged. The three side completions move to the scribe
on Sonnet over the record. No lead prompt rule changes meaning. Expected: most of the
§1.1 saving, because the large pages are the mass.

**Phase 2 — the ⚡ family and knowledge.** Route ⚡ `Test P<n>` / `Run tests` to the
tester; add the librarian behind `consult_knowledge`. The lead's recall tools are
replaced by the one delegate tool; `consulted_*` provenance is verified unchanged by the
existing tests.

**Phase 3 — the lead stops reading telemetry.** Capability verbs leave the lead's tool
set; `gather_evidence` is the pivot tool; the router handles free-text state questions
and verbatim requests. This is the phase that touches the prompt ledger most (the
delegation section, the restated coverage and verbatim rules) and so the one that most
needs the eval.

**Phase 4 — server-side async roles.** The post-conclusion narrative and SOP candidate
on the Temporal worker under tenant credentials, through the Batch API, author-pending
as `07` requires. Independent of phases 1–3.

In parallel, two single-model levers worth measuring on the lead in the eval before
phase 3, because they change the baseline the roles are compared against: adaptive
thinking with an explicit `effort` (the adapter sends no thinking configuration today,
so Opus 4.8 runs without it), and `claude-opus-5-5` as the lead (lower list price,
thinking always on, `medium` effort by default — a different cost/quality point that
the eval, not a guess, should place).

---

## 7. Alternatives considered

- **Swap the single model to Sonnet.** Cheaper per token, but the repo chose Opus for
  honesty under tooling and the eval has Sonnet baselines that are not uniformly clean.
  It also leaves the structural problem — every round rereads every page — untouched.
  Model tiering without context tiering is the smaller half of the win.
- **Provider-side context editing or compaction** (`clear_tool_uses`, server
  compaction). Complementary: it trims history after the fact, but the raw page still
  enters the expensive context once and is read there. It also edits history the
  provider contract constrains; the session's own compaction already does the honest
  version of this with the analyst told.
- **Shrink `MaxToolResultBytes`.** Cuts cost per page and multiplies `read_result`
  rounds; the lead ends up reading the same bytes in more calls, or presenting partial
  coverage as an answer — the failure the `coverage` rule exists to prevent.
- **Hosted multi-agent (Managed Agents).** The interactive loop is client-side and
  BYOK by commitment, its tools are the engine's HTTP API under the delegate token, and
  its transcript is the audit record. A hosted loop fits none of those; the async roles
  could use it later but the Batch API meets their need with less surface.
- **A model-based verifier of the lead's claims.** Rejected for the loop per
  `agent-reliability.md §3`; it belongs in the eval, where it already exists as the
  judge.

---

## 8. Open questions

- **Retriever purpose quality.** A badly phrased purpose yields a digest that misses
  the pivot the lead would have made reading raw data. A prompt contract, graded by the
  eval; whether the lead should be allowed one raw look (a small page) before
  delegating is a tuning question for phase 3.
- **Fact consistency assertion.** G4 covers fabricated ids; an assertion that a digest's
  claim is consistent with the events its refs name needs an offline checker over the
  fixtures. Its shape (deterministic where it can be, judge where it cannot) follows
  `adr/0059`.
- **Per-tenant profiles.** Whether the profile is an analyst setting, a tenant dial, or
  both; the attribution on the record is the same either way.
- **Latency.** More calls per turn; parallel `gather_evidence` calls in one round and
  streaming the lead's text keep the analyst's wait flat in the common case, but the
  worst case (a retriever that pages through 64 MB) needs its task budget to be
  visible in the panel's step markers.

---

*End of design proposal. Builds on `05 §3.4` (the loop and the two execution paths),
`09` (mechanism vs prompt), `10` (the eval as the acceptance gate), `adr/0209`,
`adr/0230`, `adr/0234` (the mechanics this changes), `adr/0059`/`adr/0069` (the
role-scoped model precedent) and `implementation/agent-reliability.md` (the trust stack
that stays). It defines no new primitive and no server-side change beyond attribution.*
