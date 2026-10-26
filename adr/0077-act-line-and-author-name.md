# 0077 — A chronicle act line is one line, and an attestation names its author as the login does

Issue: #77 · PR: #78 · Status: accepted

## Context

Two things seen in rehearsal on an investigation with a people-tested
prediction. A chronicle act line for a prediction outcome rendered its
statement one word per line: the attestation citation chip beside it carried
the author's Keycloak subject UUID and the source, did not shrink, and left
the text column no width. And the attestation named its author by that UUID
everywhere: the tracker line, the chronicle chip, the evidence view, the
export. The record is keyed by the principal id, correctly; showing the id to
a person is a different matter.

The chronicle's act lines are, by design, the connective tissue between
turns ("kept to a couple of refs so the line stays a line"). The tracker holds
the full statement; the chronicle does not need to repeat it in full.

## Decision

- **An act line is one line.** The text keeps a readable minimum width and
  ellipsizes, with the full statement in its tooltip; citation chips shrink
  and ellipsize rather than squeezing the text. A reader scans the chronicle
  as a sequence of acts and opens the tracker or the chip for the whole.
- **The actor carries a display name.** `Actor.PrincipalName` is the identity
  provider's human-facing name (Keycloak `preferred_username`), captured at
  command time from the JWT by the same derivation that sets the principal id
  and kind. It is display only: authorization, the approver invariant, and
  every key stay on `PrincipalID`. Events recorded before the field existed
  have it empty.
- **An attestation records the name beside the id.** `AuthorName` on the
  recorded attestation, the note's abstract, the export line, the view and
  the tracker all prefer the name and fall back to the id. The eval driver's
  scripted analyst therefore attests under its login name.

## Alternatives rejected

- **Resolve names at read time from the identity provider.** A record should
  read the same in a year as it did when written; the name at the time is the
  honest one, and the backend would otherwise call Keycloak on every render.
- **Store only the name.** The id is the key and the name can change.
- **Wrap act lines instead of ellipsizing.** Full statements in the chronicle
  duplicate the tracker and bury the sequence; one line per act is the
  surface's job.

## Consequences

- Attestations recorded before this change keep showing the id; new ones show
  the name.
- Every command now carries the name in its actor; other surfaces that print
  a principal (pins, verdicts, the export's "by" lines) can adopt it the same
  way when they are next touched.
- No prompt or engine-rule change; baselines stand.
