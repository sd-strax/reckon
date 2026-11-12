# 0339 — An action's state comes from the engine's record on the turn it is stated, never from the request's answer

Issue: #339 · PR: #340 · Status: accepted

## Context

Two prompt rules spoke about an action's status. `never-assume-state` said to
call `list_actions` for whether an action is pending, approved or executed.
`action-exists-only-with-id` said never to claim an action was requested,
queued, created or awaiting approval without the action id from the tool
result in hand. Read together, the second named "awaiting approval" as a claim
the held id licenses, so on a turn that requests actions and asks whether they
ran, a model could answer status from the request results and skip the first.

The cloud scenario's driver asks for containment and for its status in one
turn; the other two scripts ask on separate turns, so they never offered the
choice. Under the stronger baseline model, one trial of three answered from the
two request results with no `list_actions` call and failed H4. The turn's
reconciliation footer stated `REQUESTED` where the reply said
`PENDING_MANUAL`. Those are one state in two vocabularies: the action log's
word and the request-time word (`ActionStatus.PendingLabel`). So the reply was
accurate. The rule it skipped still matters: a request's answer is the state at
the instant of the request. A policy's approval happens inside the request,
but dispatch and its outcome come after it, and an approval deadline can pass;
whether an action executed is exactly what a request's answer cannot say.

Two engine surfaces the revision leans on had gaps of their own. The
`[engine state]` block rides an analyst message every few turns, not every
turn, and the prompt told the model to read the latest one and trust it; and
it listed a request whose approval deadline had passed as `REQUESTED` and
counted it as awaiting approval, where `list_actions` marks it expired. H4
passed a turn on any `list_actions` call, including one made before the
turn's requests, and on any turn the block rode, including for an action
requested after it.

## Decision

- **Four rules revised, none added** (adr/0060). `never-assume-state` is one
  instruction with its reason: after requesting an action, call `list_actions`
  before stating its state, because the request's result is the state at the
  moment of the request and it can change after.
  `action-exists-only-with-id` licenses a claim to have requested an action on
  this turn only by the id returned on this turn, drops "queued" and "awaiting
  approval" from what that id licenses, and says any action's state comes from
  the engine's record read on the turn it is stated: the `[engine state]` block
  when it lists the action, otherwise `list_actions`. Memory, the request's
  answer, and the engine's lines earlier in the conversation (a past block, a
  past footer) are not that record.
  `approval-inside-product` says an action that needs approval waits in the
  queue, not every requested action, and `delegate-not-principal` says the
  same of a proposed one.
- **The prompt's closing paragraph** says each block is the engine's record
  as of the turn it rode, and no longer only "read the latest one, and trust
  it".
- **One definition of expired.** An action is expired when it still awaits
  approval and its approval deadline has passed. The block marks such a row as
  no longer approvable and does not count it as awaiting approval;
  `list_actions` sets its `expired` flag by the same definition, so the two
  never disagree. A finished action whose deadline has passed is not
  expired.
- **H4 grades the rule.** A status turn passes on a `list_actions` read made
  after the turn's last accepted `request_action`, or on the block riding the
  turn when no request on the turn was accepted; a read before the last
  accepted request, or the block on a turn that had one, does not pass. Each
  call is paired with its result by id from the committed transcript: a read
  counts only when `list_actions` answered without error. A request that
  did not succeed is set aside only when the record shows it created nothing:
  every action in the trial's durable action record is accounted for by a
  request that succeeded. When the record holds an action no successful
  request returned, or was not captured, a failed or unanswered request may
  be the one that created it, and each counts as accepted. The read must also come before
  the answer: the agent's own text has to follow the read it relies on. The
  catalogue version moves.
- **Pending validation.** The four rules carry `PendingValidation: "#339"` until
  every scenario's runs on both models are accepted under the new version.
  Only the cloud scenario has a request-and-status turn, so on acceptance the
  new behaviour is validated on one shape; a further scenario with such a turn
  is what makes it two.
- **The eval README's checklist** names every scenario rather than "both", now
  that the corpus has three.

## Independent review

A fresh agent with none of the author's context reviewed the draft against
the rubric. Its findings and their disposition, before the hash was pinned:

- The draft named only `list_actions` as the record, shutting out the
  `[engine state]` block the prompt already tells the model to read and
  trust, and which H4 accepts. Now the block when it lists the action,
  otherwise a `list_actions` call; an action requested during the turn is not
  in the block, so the call is still needed for it. Substantive.
- "pending, approved, executed or failed" left out rejected, expired,
  reversed and an unknown outcome, and "report the status it returns" would
  have the model call an expired request pending. Now "what state an action
  is in" and "what the record shows". Substantive.
- Dropping the old trailing "if unsure call list_actions" left no route for
  an action from an earlier turn, whose id is not in this turn's results; read
  literally, the existence rule would have the model deny it. The existence
  rule now ends by sending both questions, whether an earlier action exists
  and what state any action is in, to the engine's record. Substantive.
- `approval-inside-product` said a requested action sits in the queue until
  the analyst approves it, which still licenses "awaiting your approval"
  without the record, though a policy's approval happens inside the request
  and the action is then dispatched. Revised as well: an action that needs approval sits there.
  Substantive; a third rule's hash moves.
- "including for an action you requested moments earlier" was the case, not
  the principle; the first sentence already says the request's answer is not
  the record. Cut.
- "queued" read as a status claim once status moved to the record. Dropped
  from the existence sentence.
- "on that turn" had no antecedent; now "on the turn you say it". The Why's
  "a request's answer is its state" reworded.
- The `list_actions` tool description already says never assume; the rule
  keeps only what it adds, that the request's answer is not the record.
- Checked clean: a reply that only reports a request and its id makes no
  status claim and needs no extra call.
- Declined: the sentence order inside the existence rule (taste).

After the review, the author found the same blanket claim the review raised
in `approval-inside-product` in `delegate-not-principal` ("Proposed actions
await their approval"), and revised it the same way. The tool descriptions
already say most proposals await approval; they did not move.

## Code review

Three rounds against the rules, not the scenarios. What changed from them is in
the Decision: the record is what is read on the turn (a past block or footer
is memory), the existence rule's scope, the block's expiry, H4's order. Also:

- `list_actions` flagged any action expired once its approval deadline had
  passed, a finished one too, and its description calls an expired action
  dead; the block flagged only a pending one. One definition now serves both:
  an action is expired when it still awaits approval and its deadline has
  passed.
- A rejected `request_action` creates no action, so H4 counts only accepted
  requests against the block.
- A failed `list_actions` call is not a read of the record, and pairing calls
  with results by position could shift on a call that never ran; H4 pairs by
  id.
- Known limit: H4 is turn-scoped and cannot tell which action a status
  question asks about. A turn that requests a new action and asks about an
  earlier one the block lists fails H4 though the rule allows the answer. No
  driver has such a turn; a driver that adds one names the action it asks
  about.
- Declined: the no-action footer on a turn that reports an earlier action. It
  says no action was requested this turn and that any claim of a new one is
  unconfirmed, which stays true; H8 is asserted only on turns that ask for a
  creation.

## External review

Two findings on H4, both taken:

- An error result does not prove no action was recorded. The engine records
  the request before it answers, so an answer lost on the way back reaches the
  agent as a failed call while the action exists. H4 now asks the record, not
  the error: the trial keeps the engine's action record as served at its end,
  and a failed request is set aside only when every action in it was returned
  by a request that succeeded. Telling a rejection from a lost answer by the
  error's wording was tried first and dropped: an error status sent after the
  record was written, and a call the loop never sent, both read wrongly.
  Counting every attempt, the reviewer's other option, would bring back the
  rejected-request case the code review closed.
- Order of calls alone let an answer given before the read pass: text sent
  with the `list_actions` call, then nothing after its result. H4 now requires
  the agent's own text after the read. Known limit: a grader that reads no
  prose cannot tell which sentence states the status, so a status stated
  before the read and unrelated text after it still passes; that is a judge's
  question.

Replayed over the eighteen trials of the six accepted runs, neither rule
changes a verdict.

## The first wording did not hold

The first revision stated where the state comes from as a definition (the
record read on the turn; memory, the request's answer and past engine lines
are not it), with no reason given to the model; the reason lived in the
ledger's Why, which the model never reads. On the cloud scenario at ten trials
the stronger model failed H4 on five, each time reporting the status field of
the request results it held, one reply saying so ("from the request records
just returned"). The states it reported were accurate and the footer showed
the record. The rule now gives the instruction and its reason in one sentence,
naming the field the model relied on; a second independent review of that
sentence restored the `list_actions` fallback to the existence rule, which
the shorter rule had dropped, and its proposal to spell out the states
("awaits approval, was approved, or ran") was declined as length the
instruction does not need.

If the reason does not move the model either, the next step is a mechanism,
not wording: the agent reads the action back from the record after each
request and returns that state in place of the request-time label.

## Alternatives rejected

- **Split the cloud script's turn** so status is asked on its own turn. H4
  would pass by never asking the question that exposed the gap.
- **Let H4 accept a same-turn request result.** The question was whether the
  actions executed; a request's answer cannot say.
- **Drop the block as a source** and name `list_actions` alone. The block is
  the record the prompt already tells the model to trust; leaving it wrong
  about expiry would be a defect whatever this rule says, so it is fixed
  instead.
- **Unify the status vocabulary** so the footer, the engine-state anchor and
  `list_actions` speak the request's `PENDING_MANUAL`. With the rule fixed the
  model reports what `list_actions` returns, so its text and the footer agree.
  The remaining split, between the request answer and the log, sits in the
  request's wire shape and the surfaces that render the footer; changing it is
  its own decision.

## Consequences

- The prompt version moves and every accepted baseline is void. Six runs:
  three scenarios, two models, under the new version, then `make eval-accept`
  for each and the flip from `PendingValidation` to `Validated` in the
  accepting commit.
- Before those, one run of the cloud scenario on the stronger model at five
  trials, not accepted: at the observed one-in-two failure rate of the first
  wording, five clean trials would arise by chance about once in thirty.
- A status reply on a request turn costs one more tool call.
