# 0367 — One read of an OCSF user object, used by every reader, and an account's key id binds before its name

Issue: #367 · PR: pending · Status: accepted

## Context

Reckon reads an OCSF user object in three places: the normalizer, which mints
accounts; the event view, which shows an event's actor and targets and binds
them to stored accounts; and an account's "seen as" line. Each read the object
its own way.

- **Name and domain.** `ocsf.UserName` trims, drops placeholders and splits
  `domain\sam` (adr/0359). The normalizer and the view's targets used it. The
  view's actor read the raw fields, so a `-` domain showed as `-\admin` and bound
  nothing. "Seen as" had a third copy.
- **Login and principal name.** The normalizer writes `domain\name` only when
  the name has no `@`. The view did not, so `jdoe@contoso.com` with the domain
  `contoso.com` never met the account the normalizer stored.
- **Ids.** The view bound a target by its name first and by its uid only when
  the name bound nothing, so a target with a SID could bind another account by
  the bare-name suffix rule. The actor read no ids, so an actor named only by
  uid showed no "By" row, and two such actors became one group.
- **Account type.** `ocsf.AccountTypeID` reads `account.type_id`, else the
  `account.type` caption, but only the cloud-principal check used it. The account
  rule (`design/03-capability-layer.md §7.2`) matched captions for four types
  only, and could match a different type from the caption than from the id. The
  account rule and the cloud normalizer each had their own partial caption
  table.

## Decision

- **One read of a user object, in `internal/ocsf`.** It returns the name,
  domain, uid, uid_alt, the `domain\sam` login, the principal name and the
  account type id. The normalizer, the event view (actor and target alike) and
  "seen as" all use it. The login and principal rules move into it from the
  normalizer.
- **The account type is `ocsf.AccountTypeID` everywhere:** the id when present,
  else OCSF's id for the caption, ignoring case, else Unknown. An unknown caption
  is Other. The account rule switches on the id only, so a caption counts for
  every type, and the id wins when both are present. The cloud normalizer names
  a cloud account's type by OCSF's caption for that id.
- **The view binds an id that keys an account before the name.** A user object
  binds first by its uid, to an account keyed on that user_id; then by its name;
  then by its uid_alt. An actor is read and bound as a target is.
- **The normalizers whose output changes go up one version.** Accounts stored
  before then keep their old reading until they are re-read (`RenormalizePass`,
  #368).

## Alternatives rejected

- **Move each reader onto `ocsf.UserName` and leave the rest.** It fixes the
  name and domain only. The login, the principal name, the ids and the type
  would still be read three ways.
- **Fix the account type in the group view only (#398).** The view would read
  the type one way and the account rule another, and the account rule would keep
  ignoring most captions.
- **Keep name before id in the bind.** The SID says exactly which account a user
  object is, and the bare-name suffix rule can bind a different one.

## Consequences

- The view and "seen as" agree with the normalizer about who a user object is.
- A record that states its account type only as text now gets that type.
- A target or actor carrying a key id binds by it, which changes some bindings
  in account events. A missed link is still preferred to a wrong one.
- #398 narrows the view's id binds by account type using this read.
