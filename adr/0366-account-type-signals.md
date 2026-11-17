# 0366 — An account is typed Windows only on a Windows signal, and an "@" seed roots on the account the record holds under its name

Issue: #366 · PR: pending · Status: accepted

## Context

The account rule (`design/03-capability-layer.md §7.2`, adr/0324) gives each
user object an `account_type`. The type decides what the account is keyed
on, so it decides which records become one account. The rule reads the type
from signals in the record: the OCSF account type, a SID, a domain, the
device's operating system, and whether the source is an identity provider.

Two cases were typed wrongly.

- **An "@" name with no signal became a Windows domain account.** Step 7 of
  the rule typed a principal name from a source that names no operating
  system as `windows-domain`. Nothing in the record said Windows. A Google
  service account, or an Azure user whose event gives no account type, was
  filed as a Windows domain account and could merge with a real one of the
  same name.
- **Account type 19 was not read.** OCSF 1.8.0 adds 19 (ActiveDirectory
  Account). The rule knew 1, 2, 6, 7, 9 and 15 only. A record that said the
  account was Active Directory got the reading of a record that said
  nothing, so a bare name with no device became the weak `login`.

Step 7 existed so the record and the seed agree. The seed resolver typed an
"@" token as `windows-domain` too (`server/seed_resolve.go`), so a seed of
`alice@contoso.com` and the record's account met. The seed has less to go on
than a record: it is a string the analyst typed, with no source and no
account type. Removing the guess from the record alone would leave it in the
seed.

The seed already looks in the evidence store, but only after the guess
misses. When the guessed `windows-domain` account is not stored, the server
lists every stored account under the name, of any type, and the analyst
picks one; when they are all one cloud alias set, it picks that set's
strong account. When the guessed account is stored, the seed roots on it
and the other accounts under the name are never seen.

## Decision

- **Type 19 is a Windows signal.** It is read as type 2 (Windows Account)
  is. A name with a domain, a SID or an "@" becomes `windows-domain`. A bare
  name becomes the weak `windows` account, where a bare-login seed already
  resolves.
- **An "@" name is typed only on a signal that says what it is.** The step
  that typed a principal name from a source naming no operating system as
  `windows-domain` is removed. With no signal, the principal name is the
  weak `login` account, keyed on the name alone, lowercased, with no host.
  It is never scoped by the host: a principal name names a domain, not a
  machine.
- **An "@" account seed roots on the account the evidence store holds under
  its name.** The server does not guess a type for an "@" token. It reads
  the stored accounts whose login, principal name or identifier equals the
  token (`evidence.Store.FindAccounts`).
  - One account, counting an account and its aliases as one, is the root.
  - Several: the analyst picks one from the list the server already returns
    (`known_accounts`).
  - None: the seed names the weak `login` account, the one a record with no
    signal produces. The existing "no account in the record" answer still
    comes first, and "start anyway" roots there.

  A seed of `DOMAIN\sam` or of a bare login is unchanged. A seed by
  `account_ref`, and a person seed, are unchanged.
- **The event view matches an address against the weak login too.** An
  address-form `uid_alt` was matched against a `windows-domain` account's
  login and principal name only (adr/0359). It now also matches a `login`
  account's login, since that is where an unplaced principal name lives.
- **The normalizers whose account reading changes go up one version.**

## Alternatives rejected

- **Keep the seed on `windows-domain`.** It is the same guess the record no
  longer makes, moved to the place with the least evidence.
- **Ask the analyst the account type on every "@" seed.** The store already
  answers when the name is unambiguous, and the existing list already asks
  when it is not. A second question would repeat the list.
- **Type an unplaced principal name `local`, scoped by the host.** A
  principal name is not a machine's account. Scoping it by host would split
  one user into one account per machine.
- **Type a bare name with 19 as `windows-domain`.** It would be keyed on the
  bare login, which no other record or seed produces, so it would join
  nothing.

## Consequences

- A principal name from a source with no signal no longer merges with a
  Windows domain account of the same name. The two join only when a record
  names both, or through the person they belong to (§7.3).
- An "@" seed sees every stored account under the name, so a stored Windows
  account no longer hides a stored cloud or weak account.
- On an empty store an "@" seed roots on the weak login. Records that arrive
  with a Windows or cloud signal mint their own accounts, which the seed
  reaches only through an alias or the person.
- The cloud-only preference becomes the one-account case of the new rule.
- Accounts stored under the old reading keep their old ids. They are read
  again under the new rule when their events are normalized again.
