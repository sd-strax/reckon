# 0320 — A cloud principal is a `user-account` identified by the provider's durable identifier alone, with the provider as its type; the account and its resources are two custom SCOs; an API Activity event is one observation carrying the operation

Issue: #320 · PR: pending · Status: accepted

## Context

A cloud control-plane event — an API call against a cloud account — arrives
as OCSF API Activity (class 6003) with an actor, an `api` block, a `cloud`
block and a list of resources. Nothing in `identity/` could mint an id for a
cloud account or a cloud resource, and the normalizer registry had no entry
for 6003, so every such event fell to the opaque default
(`design/03-capability-layer.md §4.13`): a raw payload the model could read
and nothing to pivot on. The only cloud awareness was `account_type = cloud`
on identity-provider sign-ins.

Identity is deterministic UUIDv5 over each type's identity-contributing
fields (`03 §7.1`) and a one-way door once real data is recorded against it;
aliasing is an explicit edge, never a merge (`design/01-domain-model.md`).
The normalizer shapes inputs and the resolver only folds (`03 §7.4`). A new
`x-` type needs a reason nothing adopted fits (`01 §Invented`). Nothing the
model reads may change (`adr/0057`, `adr/0232`).

The fields relied on were checked against the OCSF schema at 1.3.0 and
1.7.0: `actor.user.{uid,uid_alt,name,type_id,account,credential_uid}`,
`actor.session.{uid,issuer,credential_uid,is_mfa}`, `api.{operation,
service,request,response}`, `cloud.{provider,account,org,region}`,
`resources[].{uid,name,type,region,cloud_partition}`, `status_id`. None is
removed between the two; `user.credential_uid` (for
`programmatic_credentials`) and `cloud.project_uid` (for `account.uid`) are
deprecated and read as fallbacks. OCSF documents `user.uid` as, for example,
"the AWS user ARN" and `user.credential_uid` as "the AWS Access Key ID";
where a source places an assumed role's session is not documented and is
checked on live data when the first adapter lands.

## Decision

**The principal is a `user-account`; no new type.** STIX already models a
cloud principal — user, role, service principal, service account — with
`user_id`, `account_login`, `account_type`, `is_service_account` and
`display_name`, and its id-contributing properties are the tuple the
resolver already uses. Every verb, inspector and aliasing rule that takes a
user takes a cloud principal unchanged. The normalizer shapes it
(`03 §4.15`): `account_type` is the provider slug (`aws`, `azure`, `gcp`)
rather than `cloud`, because `cloud` names the identity provider's sign-in
account, which lives in another namespace (a provider's principal id is not
a directory's user id) and relates to the control-plane principal by an
`aliases` edge, the rule `03 §7.3` already applies to a directory account
and a domain account. **Identity rests on the provider's durable principal
identifier alone**, as `user_id` with an empty `account_login`: in a cloud
control plane the unique identifier is the principal and the name is a
label, kept as `display_name`. The identifier is `actor.user.uid` (else
`uid_alt`) for every provider, with a per-provider row only where a
provider's actor identifier is session-scoped; today one row exists, for
`aws`, preferring `actor.session.issuer` (the role a session was assumed
from, so every session of one role is one principal). **No identifier, no
principal:** an event carrying only a name, or only `actor.app_name`, mints
nothing and keeps the name on the observation; the engine never composes an
identity the source did not give, so two principals share an id only when
the provider gave them one. A role is marked `is_service_account`; the
credential id and the session are properties and extensions, never identity.

**`x-cloud-account`, a custom SCO on `(provider, account_uid)`.** The
account is an observable the telemetry names and the owner the resources
reference, so it is a SCO in the interpretation layer rather than an
`identity` SDO (the SDO already means "the detecting vendor" in `03 §4.12`)
and not an `x-host` (it is not a machine). It is resolved from
`cloud.account.uid`, else the actor's account, else the deprecated project
id; when the actor's account differs from the account the call ran in, both
are minted, so a cross-account call shows the caller's home account beside
the one it acted in. Region is not on the account; it rides the observation.

**`x-cloud-resource`, a custom SCO by strongest identifier.** `(provider,
uid)` when the source gives the provider's unique resource id, which is
unique within the provider and already carries its account, so a resource
read across accounts is still its owner's; else `(account_ref, type, name)`
under the event's account; else the resource is not minted. This is the
`x-host` priority pattern. The engine parses no provider id to find an
account.

**Edges and the observation.** `belongs-to`, the STIX 2.1 relationship
vocabulary adopted as is, from a resource to the event's account only when
the resource was identified under that account by name — a resource named
by a provider-unique id may be shared, provider-managed or another
account's, and the engine asserts no owner it cannot see — and from the
principal to its own account, the one the event names in
`actor.user.account.uid`, never the account the call merely ran in. There
is no relationship from principal to resource: the operation is the
observation. One
`observed-data` per event references the principal, the account, each
resource and the source address, and carries `operation`, `service`,
`activity`, `status`, `error`, `error_message`, `response_code`,
`request_uid`, `region`, `session`, `credential_uid`, `user_agent`,
`app_name`, `principal_name` and `resource_count` in its extensions, the way the account-change normalizer keeps the activity
and the privilege (`03 §4.9`). An event is never split per resource.

**Surfaces.** The event view (`capability/ocsfview`) gains a 6003 template
(principal, operation, resource, account, outcome, with the group-by slots
of those) and the category word `application` for OCSF category 6; the
workbench's entity type lists gain the two SCOs so they render as chips and
open in the inspector; the null-observation subject resolver reads a cloud
account or resource root, so a later verb's empty, complete answer can cite
the resource it asked about (`adr/0061`). Nothing the model reads changes.

**A third fixture scenario, cloud-rooted.** `exposed-bucket-via-access-key`:
a pipeline principal's long-lived key, used daily from one address for a
month, used from a new address to enumerate, make a bucket public, create a
key on a second principal and attach an administrative policy, with one
denied call before the escalation and a finding on the bucket as the seed.
Hand-authored OCSF, as `03 §9.1` requires of v0. In this change it is
exercised by a normalizer test that walks every event: typed objects, one
id per principal, account and resource across events, no opaque result. The
demo and eval wiring follow with the verbs that reach these events, since
no verb exists for them yet.

## Alternatives rejected

- **`x-cloud-principal`.** Forks the user surface (every user-taking verb,
  aliasing, the inspector, the graders) for what STIX already models;
  identity gain, none.
- **`account_type = cloud` for control-plane principals.** Puts a
  provider's principal ids and a directory's user ids in one namespace, and
  would merge a sign-in and a control-plane call into one SCO before anyone
  has decided they are one account.
- **The principal's name in its identity.** A name alone merged same-named
  principals across every account of a tenant; composing the account into
  the name would invent an identity the source did not give and split it
  from the identified principal the moment a source names one.
- **The session issuer first for every provider.** The issuer is the role
  only where a provider rewrites the actor into a session-scoped identifier;
  elsewhere it is a token service shared by every principal, and two
  principals would collapse into it. Hence a default and a per-provider row.
- **`belongs-to` from every resource to the event's account.** A
  provider-managed policy or another account's bucket named by its unique
  id would be recorded as the calling account's.
- **An `identity` SDO for the account.** Wrong layer, and the word already
  means the vendor.
- **Account-scoped resource identity only.** Mis-attributes a resource named
  by its unique id to the account that merely called it.
- **Parsing provider ids in the engine.** Vendor knowledge in the
  normalizer; the source and the adapter own it.
- **A relationship type per operation, or one observation per resource.**
  The graph would grow with the API surface and one event would stop being
  one observation.

## Consequences

- Two custom SCOs and one adopted relationship type enter the model;
  `01 §Invented` records why the principal is not a third.
- `account_type` gains provider slugs beside `cloud`, `windows-domain`,
  `unix`, `ldap`. Whether a control-plane caller that is itself the
  directory account should be shaped to stitch with its sign-in is deferred
  until such a source exists; the slug table is where that row goes.
- Three false-split modes, each the `x-host` kind and each remedied by
  `aliases`: an `aws` source that carries the full session identifier as
  the principal id and names no issuer; a principal seen once with its
  primary identifier and once with only the alternate; a resource seen
  once by unique id and once by bare name. There is no false-merge mode:
  no name enters the tuple, no identifier is composed by the engine, and a
  numeric identifier that JSON decoding has rounded is not read as one.
  The first live source records which forms it fills.
- A principal with no provider identifier is not an entity in the graph; a
  source that carries names only yields observations with `principal_name`
  and no pivot until it carries identifiers.
- A resource named by its unique id has no owner edge; the account pivot
  for such a resource runs through the observations that reference both.
- A 6003 event never goes opaque again; a provider that fills the fields
  differently is a row in the shaping table, not a new type.
- The prompt, the tool definitions and the engine text are untouched;
  `PromptVersion` holds, and the new scenario's vocabulary joins the
  prompt guard's denylist.
