# 0321 — Three cloud control-plane verbs join the catalog: activity by principal, resource, account, operation or service with the entity optional; one resource's current configuration as a 5023 snapshot; one resource's change timeline

Issue: #321 · PR: #328 · Status: accepted

## Context

After adr/0320 a cloud control-plane event normalizes to a principal, an
account, resources and an observation carrying the operation, and nothing
in `DefaultCatalog` can ask for one. "Which principal made this bucket
public", "what did this access key do this week", "what is this resource
now" had no verb; the cloud scenario adr/0320 added was exercised only by a
normalizer test, with no demo binding and no evaluation driver.

The conventions a bulk, fleet-wide verb follows were set by adr/0210: every
verb tool has one wire shape (one `entity` object, a `window`, scalar
extras); a verb whose question spans the fleet takes its entity optionally;
`int` renders as an integer; a fixture may answer several verbs from one
recording; the catalog's text is guarded against fixture vocabulary. A
verb's inputs are not validated against its descriptor beyond the tool
schema; the hunt verbs accept an unfiltered sweep, bounded by `limit` and
the result budget of adr/0209. OCSF Cloud Resources Inventory Info (class
5023) exists from 1.4.0; its resource object carries `data`, `labels`,
`tags` and `owner` from 1.4.0 and `created_time`, `modified_time` from
1.7.0.

## Decision

**Three descriptors, in `03 §2.10`.** `get_cloud_api_activity(subject?,
window, operation?, service?, outcome?, limit?)` over API Activity (6003);
`get_cloud_resource_context(resource)` returning one Cloud Resources
Inventory Info event (5023); `get_cloud_resource_history(resource, window,
limit?)`, the mutating calls against one resource, oldest first. Tools render
from the catalog, so `tool_set_hash` moves where a tenant binds them and
`PromptVersion` holds; no engine text is added.

**One optional entity, keyed by kind.** The activity verb's principal,
resource and account filters are one `subject` entity — the wire carries one
entity object, keyed `user`, `cloud_resource` or `cloud_account` — and the
binding addresses whichever keys are present. The entity's wire shape —
the exact keys, with the provider — is written into the three descriptors'
input text, because the model learns a verb from its descriptor alone and a
guessed key against a source that treats an absent filter as unfiltered is
not an empty answer but a wrong one. A principal may be given by
its credential id (`user.credential_uid`), because the first question about
a leaked key names the key. Absent the entity, `operation` or `service`
asks across accounts. The at-least-one-filter rule is the verb's stated
contract, not an engine check: nothing validates inputs against a
descriptor today, the hunt verbs set the precedent, and a source adapter
that cannot answer an unbounded query refuses it under `03 §6.2`.

**History is its own verb.** A real source answers "what changed on this
resource, in order" from a configuration timeline, not from the raw trail,
and the question wants the whole ordered timeline rather than a filtered
page; `get_failed_auth_burst` beside `enumerate_logons` is the precedent for
a pre-filtered convenience verb over the same class. In the fixture a
mutating call answers both verbs from one file (`matches.verb` as a list);
a read is never a history entry.

**Context is a 5023 snapshot, normalized by `03 §4.15`'s identity rules.**
A new normalizer (`03 §4.18`) mints the account, the resources and, when it
carries an identifier, the owner as a cloud principal, exactly as the
activity normalizer does, so the snapshot of a bucket and the call that made
it public meet on one `x-cloud-resource`. The configuration — policy,
exposure settings, tags, labels, criticality, created and modified — rides
the observation's extensions and is read through the event view, which
gains a 5023 template; it is never an entity. The class's version range is
1.7.0 at both ends, because its first source is a configuration adapter the
engine ships, shaped to 1.7.0; a 1.4.0-shaped snapshot normalizes the same
by field detection.

**The cloud scenario is wired.** `DemoScenarios` gains it; its capability
config binds the three verbs plus alerts, indicator context and case lookup
to the fixture adapter, with `outcome` defaulted to `ALL` so the one denied
call is reachable and the fixture matchers stay exact; its action config
reuses the sign-in scenario's set (disable and enable the user, block and
unblock the address, ticket and notify); an evaluation driver on the
sign-in scenario's skeleton seeds from the finding, pivots on the bucket,
asks what the principal did against its baseline, asks whether the bucket is
public now, takes a manual attestation, contains, and hands off. Two 5023
fixtures (the bucket, the user) and one case-detail fixture are added; the
two history duplicates are removed in favour of the verb list. Baselines are
produced by a deliberate evaluation run, not in this change.

## Alternatives rejected

- **Three entity inputs on the activity verb.** The wire has one entity
  slot; three would render as one property three times. One `subject` keyed
  by kind is adr/0210's shape.
- **An engine-side at-least-one-filter check.** A new validation mechanism
  for one verb, where none exists and the hunt verbs accept a sweep; the
  rule is stated and the adapter enforces what it cannot answer.
- **History folded into the activity verb as a flag.** Hides that a real
  source answers it elsewhere and in a different order.
- **A `query_cloud(filter)` escape hatch.** That is `query_logs` with a
  dialect; typed verbs are what the agent should reach for first.
- **Context from the trail.** The trail cannot say what a resource is.
- **Context deferred until a live configuration source exists.** The
  fixture answers it, and it is the first thing an analyst asks.
- **Reading the typed 5023 objects (`databucket`, `table`, …) now.** No
  source fills them yet; `resources[]` is where the fixture and the first
  adapter carry the resource.
- **Keeping a read in the history fixture.** It contradicts the verb.

## Consequences

- `DefaultCatalog` grows by three; `tool_set_hash` moves on every install
  that binds them; `PromptVersion` holds.
- A second cloud normalizer and view template; `HandledClasses` gains 5023;
  the 5023 version range is recorded under `03 §4.16`'s rule.
- A third bundled scenario, cloud-rooted, with bindings, actions and a
  driver: a prompt rule must now hold on three shapes of case, and the
  evaluation attributes the new tool set against no baseline until one is
  recorded. The prompt ledger's `Validated` lists name the scenario only
  after a recorded run accepts each rule on it; this change leaves them.
- The activity verb can be called with no filter; the result is bounded by
  `limit` and the result budget, as a fleet sweep is. A source adapter
  states what it refuses.
- Still missing from `03 §2`: the verbs adr/0210 lists.
