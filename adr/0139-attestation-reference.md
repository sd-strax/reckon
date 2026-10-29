# 0139 — An attestation can name where it happened, as written or as a fingerprint

Issue: #139 · PR: #TBD · Status: accepted (references only; uploads deferred)

## Context

A manually tested prediction rests on the analyst's typed statement
(`adr/0054`): honest as a human statement, but nothing backs it, so a reviewer
or the next analyst cannot check it. The issue asks for a reference to where it
happened, and later for an uploaded artifact.

## Decision

- An attestation may carry a **reference**: a message link, a ticket number or
  a message id. It is recorded on the event, shown with the attestation, and
  written onto the STIX note as an `external_reference` — a URL as `url`,
  anything else as `external_id`.
- When the analyst would not keep the reference itself in the record (a
  private chat link, a mail message id), they can keep only its **SHA-256**:
  the server records `sha256:<hex>`, which becomes the note's
  `external_references[].hashes["SHA-256"]`. Anyone holding the original can
  show it matches; the record does not reveal it.
- The reference is optional and never required to record a result.

## Alternatives rejected

- **Upload the artifact now.** It needs decisions on where files live, how
  sensitive content is handled and retained, and whether the agent may read
  them; none is needed to make an attestation checkable.
- **Hash every reference.** A ticket number is meant to be followed; hashing
  it by default would make the common case useless.

## Consequences

- Artifact upload stays open as a follow-on with its own ADR.
