# 0371 — Every email address is parsed and written back in its shortest RFC 5322 form before it is stored

Issue: #371 · PR: pending · Status: proposed · Amends: adr/0357, adr/0095

## Context

`stix.EmailAddress` cleans up an email address read from telemetry before
it becomes an `email-addr` (adr/0357). The normalizer stores what it
returns, and the event view matches against it, so the two agree.

The cleanup had two paths. A value that was already an addr-spec was kept
as written. A mailbox with a display name was parsed with Go's `net/mail`,
and the parser's `Address` was kept. That field is the decoded address: the
local part with its quoting and escaping removed. For a plain local part
that changes nothing. For a quoted one it changes the address.

The mailbox `Alice <"\"alerts\""@example.com>` has a local part holding
literal quote characters. Decoded, it comes out as `"alerts"@example.com`,
where those quotes now act as syntax. That is a different mailbox, the
same as a genuine `"alerts"@example.com`. Both got one `email-addr` id, so
a crafted sender merged with a real one. A message with no Message-ID has
an id built from its addresses (adr/0359), so two such messages could merge
too.

The two paths also disagreed with each other. `"alice"@example.com` was
kept quoted, while `Alice <"alice"@example.com>` became `alice@example.com`.
RFC 5322 §3.4.1 treats the quoted and unquoted forms of the same local part
as one address, but they got two ids.

## Decision

- **Every address is parsed, then written back.** `stix.EmailAddress`
  trims the value and parses it with `net/mail`, whether or not it has a
  display name. It then writes the parsed address back out as an addr-spec,
  through `mail.Address.String()` with no name, minus the angle brackets.
  The result is checked with `IsAddrSpec` and stored.
- **The form written is the shortest one.** The local part is quoted only
  when it can't be a dot-atom or would start or end with white space
  (below), with the escaping it needs. So
  `alice@example.com`, `"alice"@example.com` and
  `Alice <"alice"@example.com>` are all `alice@example.com`, one id.
  `"\"alerts\""@example.com` keeps its quotes and escapes, and stays apart
  from `"alerts"@example.com`, which is `alerts@example.com`.
- **A local part that would start or end with white space stays quoted.**
  `String()` counts every non-ASCII character as an ordinary one, so it
  writes `"\u3000alerts"@example.com` (an ideographic space first) without
  quotes. That value starts with white space, so it isn't an addr-spec and
  the sender would be dropped. Letting it through would be worse: the id
  trims white space, so it would get the id of `alerts@example.com`. Such a
  local part is wrapped in quotes. It holds no quote or backslash, so it
  needs no escaping. A local part that ends with white space is quoted the
  same way, so both ends follow one rule.
- **A quoted local part the old path dropped is kept.**
  `Alice <"a@b"@example.com>` decoded to `a@b@example.com`, which isn't an
  address, so it was dropped. Written back, it is `"a@b"@example.com`.
- **The domain is written as parsed.** A domain literal such as
  `[192.0.2.1]` stays as it is. The id already folds case (adr/0357).

## Alternatives rejected

- **Writing back only on the display-name path.** It fixes the merge, but
  one mailbox quoted two ways still gets two ids.
- **Keeping the raw text between the angle brackets.** It avoids the
  decode, but a mailbox could then be stored in any quoting a source chose,
  and the two paths would still disagree.
- **Quoting every local part.** It is unambiguous, but it changes every
  stored plain address and the form analysts read.
- **A hand-written serializer.** `mail.Address.String()` already writes the
  quoting and escaping RFC 5322 needs.

## Consequences

- One mailbox has one `email-addr`, however a source quoted it, and an
  address whose local part holds quotes can't be mistaken for another.
- The stored `value` of an address a source quoted when it didn't need to
  is written without the quotes.
- The event view matches by the same cleanup, so it binds the same ids.
- The event view folds a sender and recipients by the cleaned-up address,
  so two spellings of one mailbox fold together. A sender or recipient
  that isn't an address folds by its trimmed text, ignoring case.
- The email normalizer moves to its next version, since the values it
  stores change.
- `design/03`'s cleanup sentence for email addresses (§4, the
  `email-addr` bullet) and adr/0357's rule gain the write-back, and
  adr/0095's slot rule gains the cleaned-up address.
