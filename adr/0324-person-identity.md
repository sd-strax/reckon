# 0324 — A person is its own entity, minted only from a record that names one; an account is identified by what its source repeats, owns its events, and belongs to a person by a record or a human's word

Issue: #324 · PR: pending · Status: accepted

## Context

A person is seen through every source under a different name. A domain
controller reports `CONTOSO\jdoe` with a SID; an endpoint agent reports a
bare `jdoe`; the identity provider reports `jdoe@contoso.example`; an audit
record carries the SID alone; a cloud control plane carries a provider id
(`adr/0320`). The `user-account` identity is `(account_login, user_id,
account_type)` with all three contributing (`design/03` §7.2), so the same
login with and without its SID is two accounts, the bare name a third, the
address a fourth, and the trail of one person splits four ways. The shaping
also classifies any login containing `@` as a cloud account, so a Windows
principal name reads as a sign-in account of the identity provider.

`adr/0211` settled the same problem for hosts and built the mechanism that
any entity type can use: a strong identifier per record, a weak entity for a
record that lacks it, an `aliases` edge a single record justifies, an alias
set the reads follow, `distinct-from`, the seen-as view, and the analyst's
join, split and withdraw on the entity. It left the account's shaping rule to
its own decision, and it deferred the seed-kind choice, because a bare word
typed as a seed can be a host or an account.

Accounts raise a question hosts do not. An investigation's first question is
who, and the record answers it only indirectly: an account acted, and someone
owns the account. Treating the person as the unnamed group of their accounts
leaves nothing to seed an investigation from, nothing for the record's
`entity:` tags and the recall of prior investigations to key on, nowhere to
hold what a directory says about the person, and no entity for the analyst's
"these accounts are one person" to be a fact about. The constraints are the
ones `adr/0211` worked under: identity is deterministic, the capability layer
does not reason (`design/03` §4), the record states what was observed and
what was asserted and never guesses, and a merge is never destructive
(`design/01` §3).

## Decision

**A person is an entity, and it is `identity`.** STIX 2.1's `identity`
object with `identity_class: individual` is the person; no type is invented.
A person's identity is the durable identifier a directory gives it, scoped by
the directory: an object id in a cloud directory, an object GUID in an
on-premises one, a user id in an identity provider, a worker id in a
personnel system. Two directories that describe one human are two persons
until a record or a person links them, by the same `aliases` edge hosts use,
and the same `distinct-from` keeps two apart. A person carries, as
properties, what its record says: display name, mail, principal name,
department, title, whether the account is enabled, and a reference to the
manager when the record names one. A personnel system's identifier is a
property of the person that system describes, never the key of another.

**A person is minted only from a record that names one, or by a human.** The
record is a directory's user object, normalized from the OCSF user inventory
class. Nothing else mints a person: not a shared address, not a matching
display name, not a sign-in. An analyst may assert that an account belongs to
a person or that two persons are one, and may withdraw either; both are
assertions with who, when, a note and a history, as `adr/0211` defines them.

**Events attach to accounts, never to persons.** An observation's
`object_refs` carry the `user-account` the source named. The person is
reached from the account by `belongs-to` (the STIX relationship `adr/0320`
adopted), emitted by the directory record that names both, DIRECT, with the
record's provenance, or asserted by a human. The record therefore says that
an account did something and that the account belongs to a person; it never
says the person did it. Reading an investigation seeded on a person walks
person, aliased persons, owned accounts, each account's alias set, and the
walk is what the inspector shows. Reading an account seeded alone follows the
account's alias set only.

**Not every account has a person.** A local administrator, a service account,
a shared mailbox, a break-glass account and a machine account are accounts
with no owner, shown as such. No rule attaches them to anyone; a person may.

**An account is identified by what its source repeats.** The shaping rule
(`design/03` §7.2, §7.4) per `account_type`:

- `windows-domain`: the SID when the record carries one; else the principal
  name (`user@domain`) lowercased when that is what the record carries; else
  `domain\sam` lowercased. A record that carries the SID beside a login mints
  the login's account as well, outside the observation's `object_refs`, with
  `aliases` from it to the SID's account, as the host rule does for an agent
  id beside a hostname: the source said in that record that this login is
  this SID. A principal name from a directory source is a directory account,
  not a cloud sign-in; the rule that classified any `@` as cloud is gone.
- `windows`: a Windows source that names a login with no domain and no SID
  mints a weak account on the bare login, `weak: true`, which joins a domain
  account only when a record names both.
- `windows-local`: a login whose domain is the machine itself is scoped by
  the host: identity `(host_ref, sam)`. Well-known principals (the system,
  local service and network service SIDs, built-in groups) are per-host
  identities under the same rule, never persons and never placeholders.
- `unix`: scoped by the host, `(host_ref, name)`. `root` on two hosts is two
  accounts; a directory-backed Unix account joins its person through the
  directory record like any other.
- `cloud`: the identity provider's sign-in account, keyed on the provider's
  user id when the record carries it, else the sign-in name lowercased, with
  the provider as a property.
- Control-plane principals are unchanged (`adr/0320`): the provider's durable
  identifier as `user_id`, the provider slug as `account_type`.

`user_id` is no longer part of the identity of an account whose type keys on
the login; it is a property, and the SID account the same record mints is
what carries it. `account_type` is still never folded.

**Placeholders are refused, as for hosts.** The identity package's list
(`adr/0211`) gains the account values that name no one: the anonymous and
nobody SIDs, the nil GUID as a user or person id, and the login values that
are a source's way of saying none. A placeholder field reads as absent and
the account falls to its next key or mints nothing.

**The seed names its kind.** A seed carries a kind, host, account or person,
chosen by the person seeding when the token is ambiguous; the server defaults
it from the token's shape and the client lets the analyst change it. A person
seed resolves through the directory record's identifier or, when the token
is an address or a principal name, through the account it names and that
account's owner.

**The mechanism is `adr/0211`'s, generalized.** The alias set, the
`distinct-from` cut, the assertion endpoints and the inspector's join, split
and withdraw accept accounts and persons as they accept hosts; `belongs-to`
gains an asserted form with the same payload; the seen-as view for an
account lists the logins, identifiers and sources it has been seen under;
the person's view lists its accounts with the record or person that attached
each. Normalizers that mint accounts move to the next version; nothing minted
before is re-identified. A conformance test normalizes one person as three
sources name them, a domain controller's logon, an endpoint's bare login, an
identity provider's sign-in, plus the directory record that names all three,
to one person owning the accounts the rule yields, with the aliases the rule
yields, and `make conformance` runs it beside the host's.

## Alternatives rejected

- **The person as the unnamed alias group of accounts.** Nothing to seed,
  tag, recall or describe; the analyst's assertion that accounts are one
  person has no subject; and every consumer would re-derive the group.
- **Email as the account's identity.** Most account records carry no
  address; a principal name and a mail address differ in many estates; an
  address changes with a name and is shared by shared mailboxes. It is the
  common join key and the human label, and it is a property.
- **A person inferred from a shared address or display name.** Inference in
  the capability layer, and wrong exactly where it matters: shared mailboxes,
  namesakes, recycled logins.
- **The person as a custom `x-person`.** STIX already has the individual
  `identity`; inventing a type beside it would need a justification the
  adopted object already removes.
- **Events referencing the person directly.** It would state what the record
  does not know, and it would erase the distinction between the account that
  acted and the human who owns it, which is the question an account
  compromise turns on.
- **Keeping `user_id` in every account's key.** It is what split the same
  login four ways; the SID's account carries the identifier and the login's
  account aliases to it from the record that names both.
- **A global Unix account keyed on the bare name.** `root` and every shared
  local name on every host would be one account, a merge no edge undoes.

## Consequences

- `identity` with `identity_class: individual` enters the adopted list as the
  person; `belongs-to` gains `user-account → identity` in the edge table with
  an asserted form; the user inventory class gains a normalizer; the seed
  carries a kind.
- An investigation can be seeded on a person and read everything their
  accounts did, with the account-to-owner walk visible; an investigation
  seeded on an account reads that account alone.
- A second directory, a personnel system among them, adds persons that
  stitch by record or by hand, never by guess; the host-identity rule's
  consequences for time apply here too: a recycled login or SID is a question
  of when each association was observed, left to the same follow-up.
- Account-minting normalizers move to the next version; observations minted
  before keep their ids and are not re-identified.
- The first directory adapters feed the user inventory class from the cloud
  directories most estates sign in through, one of which also carries the
  on-premises identifiers that attach the domain accounts; a personnel
  system follows when one is wired.
- Groups stay on the account (`member-of-group`); a person's groups are the
  union over owned accounts at read time.
