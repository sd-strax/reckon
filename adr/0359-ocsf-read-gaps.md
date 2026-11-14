# 0359 — Cloud alerts keep instance logins apart from provider users, an email's thread id never joins messages, and the newer group and registry fields are read

Issue: #359 · PR: TBD · Status: accepted · Amends: adr/0356

## Context

adr/0356 moved every normalizer onto OCSF's own classes and fields, from
1.1.0 to 1.9.0. A review of that work found four places where a valid OCSF
event is still read wrongly.

1. **Cloud detection findings.** A detection finding always carries
   `cloud.provider`. OCSF requires it at every version. adr/0356 said that
   when a finding names a provider, every evidence actor is a provider user
   (an IAM user or role, for example). But an alert from a cloud VM can name
   a login on the VM itself, such as Linux user `1000`. That login only means
   something on its own machine. Filed as a provider user, it has no host
   scope, so user `1000` on two VMs becomes one account.
2. **Email thread id.** From 1.4.0, OCSF defines `email.uid` as the id of
   the email thread, not of one message. adr/0356 made `email.uid` a weak
   message with an `aliases` edge to the strong one. Two messages in one
   thread then share that weak node. Reads follow `aliases` edges, so the
   two messages read as one, and each one's evidence shows up under the
   other. Clicks (4012) and attachment events (4011) name their email only
   by `email_uid`. From 1.4.0 OCSF deprecates `email_uid` with "use the
   email.uid attribute instead", so a click names a thread, never one
   message. Neither
   class carries a Message-ID at any version.
3. **Group Management users.** OCSF 1.9.0 adds `users[]` to Group
   Management (3006) and deprecates the single `user`. reckon read only
   `user`. An event that sends only `users[]` added nobody to the group.
4. **Registry value data.** From 1.6.0, a registry value's data can arrive
   in a field for its type: `reg_string_data`, `reg_integer_data`,
   `reg_binary_data` or `reg_string_list_data`. OCSF tells sources to use
   these when the type is known. reckon read only `data`. A value sent in
   a typed field came in empty.

The rules that bound the answer:

- Identity never merges on a weak or ambiguous signal (adr/0211, adr/0324).
  A missed join can be added later by an `aliases` edge. A wrong merge is
  silent.
- A normalizer reads a field when it is present and never checks the
  version number (`03 §4.16`).

## Decision

- **Instance login or provider user.** An evidence actor on a cloud finding
  is a provider user only when the event says so:
  - `actor.user.account.type_id` (else its caption, `account.type`) is a
    user type of the finding's own
    provider: 3 AWS IAM User or 4 AWS IAM Role for AWS, 6 Azure AD
    Account for Azure, 5 GCP Account for GCP; or
  - the id the principal would be keyed on has the provider's principal
    form. For AWS that is an ARN in any AWS partition (`arn:aws:`,
    `arn:aws-cn:`, `arn:aws-us-gov:` and the like), an IAM unique id
    (`AIDA` or `AROA` and at least 16 more capital letters or digits), or
    the root principal: a 12-digit id equal to the finding's own account
    id or the user's own account id. The check is on the key itself, so an
    ARN in one field never vouches for a different id in another. When the
    provider's session row (`03 §4.15`) gives the key, the check is on that
    value; a session issuer such as `sshd` is no signal.

  Account types that name a container, such as 10 AWS Account or 13 Azure
  Subscription, say where the user lives, not what kind of user it is.
  They give no signal. Nor does a user type of another provider.

  A host or directory account type (1 LDAP, 2 Windows, 7 Mac OS, 9 Linux,
  19 ActiveDirectory) is always read as on any other event, whatever the
  provider: by the account rule (`03 §7.2`), scoped to the host where the
  rule scopes it. With no signal either way, the actor is read the same
  way. The rule is a table keyed by provider, so a new provider adds a
  row.

  A provider user type with no durable id still mints nothing, as in
  `03 §4.15`: a name alone is not unique across accounts.
- **Email thread id.** A thread id never identifies a message.
  - `email.uid` is a thread id at every version. It is kept on the message
    as `thread_uid`. No identity and no `aliases` edge come from it.
  - A message is identified by its Message-ID.
  - With no Message-ID, it is identified by sender, subject, the event's
    time to the second, its recipients, and its `thread_uid` when present
    (`03 §4.10`). The recipients are the header to, cc and bcc, plus the
    envelope `smtp_to` and Delivered-To (`delivered_to_list` from 1.4.0,
    else `delivered_to`). They keep apart the separate copies of a
    phishing or spam run. The thread id keeps apart messages from
    different threads.
  - A click or an attachment event creates no `email-message`. Its
    `email_uid` is kept on its observation as `thread_uid`.
- **Group Management users.** Every entry of `users[]` is a target, and
  `user` is read when no entry of `users[]` names a user. An entry names
  a user when it has a real name, `uid` or `uid_alt`, by one check the
  normalizer and the event view share. Each target gets its own
  `member-of-group` edge. The event view and the account "seen as" read
  both fields.
- **Registry value data.** One shared rule reads a value's data. It takes
  the first typed field that holds a value, then `data`. An empty typed
  field counts as absent, so it never hides the next one. The value is
  then written as adr/0341 writes `data` on a `windows-registry-key`
  value: a string as it arrives, anything else as its JSON text, and a
  number a float64 cannot state exactly left out. `data_type` still comes
  only from `reg_value.type` or `type_id`. The normalizer and the event
  view both use the rule, so the value's data and the event view's Data
  field always agree.
- The detection, email, email-file, email-URL, account and registry
  normalizers each go up one version, which marks their stored results as made by an older version.
- Each case has a test event that carries only the newer or narrower form.
  Before, the examples sent the old and new fields together.

## Alternatives rejected

- **Treat every actor on a cloud finding as a provider user, as before.**
  It merges logins from different machines into one account.
- **Treat an actor with no signal as a provider user.** That merge is
  silent. Taking the host login instead only misses a link, which an
  `aliases` edge can add later.
- **Branch on `metadata.version` for email, keeping the old link below
  1.4.0.** No normalizer checks the version (`03 §4.16`). And a source that
  reports an older version can still put a thread id in `email.uid`, which
  would merge messages again.
- **Keep a weak `email-message` from a click's `email_uid`.** It names a
  thread. Clicks on two different messages in one thread would read as one
  message.
- **Leave the no-Message-ID key as sender, subject and second.** Separate
  copies of one phishing or spam run would read as one message.
- **Join list registry data with a newline.** adr/0341 already writes
  non-string data as its JSON text. Two rules would store one value two
  ways.

## Consequences

- A cloud alert whose actor gives no account type and no principal id no
  longer links to the provider user. It is read by the account rule
  instead, as the same user on any other event would be.
- With no Message-ID, two records of one message logged at different
  seconds, such as a receive and a scan, read as two messages.
- On Azure and GCP there is no principal id form to check. There, the link
  to the provider user depends on the alert giving the account type.
- Only AWS, Azure and GCP have rows. An actor on a finding from any other
  provider is read by the account rule, even where that provider's API
  calls mint principals.
- With no Message-ID, two records of one message that list different
  envelope recipients, such as a receive and a delivery, read as two
  messages.
- Nothing re-runs stored results yet (`RenormalizePass` is not built). Until
  it is, results stored by the older versions keep their links. That
  includes cloud principals made from host logins.
- A click or an attachment event no longer names an email. It still names
  the URL or file, the user and the host, and it keeps the thread id.
- Below 1.4.0 an attachment appears only as its own event (4011). There,
  reckon no longer records which email held the attachment.
- With no Message-ID, one email logged once per recipient reads as
  several emails. That is a missed join, not a wrong merge.
- Groups changed by 1.9.0 sources show all their members.
- Registry writes made by newer sources show their data. Two different
  writes to one value no longer fold together in the event view.
