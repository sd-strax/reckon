# 0398 — The normalizer records which account each user object is, and the view and "seen as" read that record

Issue: #398 · PR: #411 · Status: accepted

## Context

The normalizer mints an account for each user object in a record. It decides
which account from the account type, the SID, the domain, the device OS and the
host (`design/03-capability-layer.md §7.2`). It keeps the accounts in the
observation's `object_refs`, but not which user object each one came from.

Two readers need that link, and each worked it out again by matching:

- **The event view** shows an event's actor, targets and group members, and
  links each one to an account. It matched the user object's uid, name and
  uid_alt against the event's stored accounts (adr/0367). Cloud principals had
  a matcher of their own.
- **An account's "seen as" line** lists the forms a user took in the account's
  records. It walked its own list of user-object places in each record, and
  kept an object when the object's first identifier was one of the account's
  keys.

Each matcher is a partial copy of the normalizer's decision, and each gap
between the copy and the decision showed up as a wrong or missing link:

- A cloud member and a directory member that share one GUID. The GUID matches
  both accounts, so neither member is linked, and the view shows one member
  for two accounts.
- A Unix, windows-local, cloud or LDAP account is stored under its bare name.
  A record that also names a domain didn't link to it, because the view looked
  for `domain\name`.
- "Seen as" missed a windows-local or Unix account's records for the same
  reason.

Adding the account type to the matching would be one more copy of the rule.

## Decision

- **The normalizer records which account each user object is.** The
  observation's `reckon-observation` extension gets one new property,
  `user_accounts`. It maps a user object's place in the record to the refs of
  the accounts the normalizer minted for it.
  - The place is the OCSF path, with the record's own list indices:
    `actor.user`, `users[1]`, `evidences[0].actor.user`.
  - The refs are the strong accounts, the ones in `object_refs`. Usually there
    is one. A directory record's user can mint two, its sign-in account and
    its domain account, which are separate accounts, not aliases. The list
    gives the one the view shows first. Lesser accounts stay reachable through
    their `aliases` edges.
  - A user object that mints no account has no entry.
  - Cloud principals (API activity's actor, a cloud finding's provider
    principal) are recorded the same way.
- **The view links a user object to the account at its place.** A user object
  with no entry is shown unlinked, by its login. The view's matchers are
  removed, the cloud principal's included. How a user is shown and keyed is
  unchanged from adr/0367.
- **"Seen as" reads the same map.** An account's records are the records whose
  map names the account or a member of its alias set. For each such place,
  "seen as" reads that user object's forms through `ocsf.ReadUser`. Its own
  list of places and its key match are removed.
- **The normalizers that mint a user's account go up one version.** There is
  no fallback for records stored before the change.

## Alternatives rejected

- **Read the account type in the view and narrow its matches by type.** It
  fixes the shared GUID only. It adds a third partial copy of the account rule,
  and needs a test to keep that copy in step with the normalizer.
- **Move the whole account rule into a package the view can import, and have
  the view compute each account.** The view would recompute what the
  normalizer has already decided, from the same record.
- **A relationship from the observation to each account.** STIX has no
  relationship type for "the actor of this record", and the place in the
  record would be lost.

## Consequences

- The view and "seen as" link exactly the accounts the normalizer minted. A
  change to the account rule reaches them with no change of their own.
- A user object the normalizer mints nothing for is shown unlinked, never
  matched to a stored account by its fields.
- Every normalizer that mints a user's account must write its entry. A class
  that doesn't shows its users unlinked, so a test runs every minting class.
