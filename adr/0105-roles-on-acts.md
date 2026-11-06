# 0105 — What authorized an act is two facts with two owners: the gate reckon applied, and the roles the issuer asserted in the token

Issue: #105 · PR: pending · Status: accepted

## Context

Every act on the record names its actor by principal id and, since
`adr/0077`, by the display name the identity provider presented at the time.
It does not record what authorized the act. The Person inspector
(`design/ui/08 §8.6.1`) lists a principal's acts in the investigation and
says that roles live in the identity provider and shows none. A reviewer
asking whether this person was allowed to approve that action, then, cannot
answer from the record, and the identity provider only knows now.

Gap 19 of `design/ui/00-backend-binding.md §6` asks for the role that
authorized an act, captured from the token at command time beside the name,
display only: authorization keeps reading the live token and reckon mirrors
no roles. `implementation/workbench-react.md §8.8` renders that as the
token's role list on the actor.

Two things shaped the reading. First, "the roles in the token" is not the
principal's entitlements. It is the projection Keycloak's client scope
mappers chose for reckon, possibly federated from a directory, possibly
group-derived, dynamic or short-lived, of whatever cardinality the deployment
mapped. Presented as the person's roles it is a claim reckon cannot stand
behind. Presented as what the issuer asserted in a signed token at that
moment, it is a sound fact with an owner, and the exact set reckon's own gate
checked. Second, reckon has a fact of its own about every act: the role set
the route required, which the token satisfied. That is what actually
authorized the command in reckon, in reckon's small vocabulary.

This is about principals acting in reckon. An investigated account's roles
are evidence, fetched through a capability verb and cited with provenance,
and nothing here touches that.

## Decision

**Two fields on the actor, both display only.** `Actor.Roles` is what the
identity provider asserted in the token the act was made with: the realm
roles and the reckon client roles as the claims already hold them.
`Actor.RequiredRoles` is the gate reckon applied: the role set the act's
route required, any of. Both are captured by the one derivation every
command-issuing handler uses, which now runs on the request so the route's
gate can reach it; the middleware that enforces the gate records the set it
enforced on the request. Authorization, the approver invariant and every key
stay on `PrincipalID`. Events recorded before the fields existed have
neither; SYSTEM acts carry neither; an AI delegate carries both with its
delegate kind. An AUTO_POLICY approval, which the engine records in the
policy's accountable human's name, carries neither as well: no token was
presented for it and no route gate applied, and the action record already
names the policy that authorized it.

**Both, not one.** The gate alone is a near-constant across acts and says
nothing about the person. The asserted roles alone need reckon's route table
to judge, and that table changes. Together the record reads "required
analyst; the token asserted analyst, approver", and is verifiable in a year
with no outside knowledge.

**Nothing new is stored.** The actor rides each event as it does today, so
the events table, the thread, the action record's history and the signed
bundle's event stream carry the two fields by construction. The thread actor
view and the action history's actor serve them.

**The inspector says what each fact is.** Each act reads "as analyst" when
its gate was recorded. The summary shows the union of asserted roles across
the principal's acts, labelled as what the identity provider asserted in the
token at the time of each act, with the caption that current roles may differ
and reckon mirrors none. An act carrying neither reads as such, with the
two causes named, an older record or a policy's approval in the person's
name, and no claim as to which. SYSTEM acts attributed to the principal, a
dispatch or a result on their approval, read as the engine's and carry none.

## Alternatives rejected

- **The token's role list, presented as the person's roles** (§8.8 as
  written). A projection chosen by the deployment's mappers, of unbounded
  cardinality, read as entitlements it does not represent.
- **The gate alone.** Verifiable but uninformative: every analyst's every act
  reads the same word.
- **The asserted roles alone.** Not self-contained: judging them needs
  reckon's route requirements as they were at the time.
- **Resolving roles from the identity provider at read time.** `adr/0077`'s
  rejection stands: a record reads the same in a year as when written, and
  the identity provider holds no point-in-time view of a principal.
- **Naming which asserted role "really" authorized the act.** The gate says
  what was required and the token says what was held; choosing one of several
  would be an inference.

## Consequences

- A reviewer can verify from the record alone that an act met reckon's
  requirement at the time, and see what the issuer said of the principal
  then.
- Older acts and policy approvals show the absence plainly rather than an
  empty list read as "no roles".
- The architecture's rule that reckon mirrors no roles is qualified where it
  is stated: it holds for authorization, which reads the live token; the
  record keeps what a token asserted at an act, display only.
- The route table and the aggregate are unchanged; nothing authorizes
  differently. The actor's JSON grows by two optional lists.
- The export's "by" lines can adopt the two fields when next touched, as
  `adr/0077` left the name.
