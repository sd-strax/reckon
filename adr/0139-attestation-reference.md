# 0139 — An attestation points at its evidence; reckon never stores the artifact

Issue: #139 · PRs: #199, #206 · Status: accepted

## Context

A manually tested prediction rests on the analyst's typed statement
(`adr/0054`): honest as a human statement, but nothing backs it, so a reviewer
or the next analyst cannot check it. The issue asked for a reference to where
it happened, and later for an uploaded artifact: the exported email, a
screenshot of the chat, a call note.

reckon is a reasoning and investigation tool. The organization already keeps
evidence somewhere it governs (an object store, a document library, the case
system), with access control, retention and legal hold. Taking the files in
would make reckon a second evidence store: it would have to decide where files
live and how big they may be, how sensitive content is redacted, retained and
purged against an event log that never deletes, whether the agent may read a
file (a prompt-injection surface, and personal data sent to a model provider),
and whether the signed conclusion bundle (`07`) carries it. None of that is
reasoning.

## Decision

- **reckon records references to artifacts, never the artifacts.** There is
  no upload. The agent never fetches, reads or receives an artifact.
- An attestation may carry a **reference** to where it happened: a message
  link, a ticket number or a message id. It is recorded on the event, shown
  with the attestation, and written onto the STIX note as an
  `external_reference` (a URL as `url`, anything else as `external_id`).
  When the analyst would not keep the reference itself in the record (a
  private chat link, a mail message id), they can keep only its **SHA-256**:
  the server records `sha256:<hex>`, which becomes the reference's
  `hashes["SHA-256"]`. Anyone holding the original can show it matches; the
  record does not reveal it. (#199)
- An attestation may also carry an **artifact location**: where the evidence
  itself is kept, in enough detail that someone else in the organization can
  fetch it. (#206)
  - **location** (required once any field is given): the durable address,
    e.g. `s3://ir-evidence/2026/call-note.pdf`, a document URL, or a path.
  - **system**: what holds it, e.g. S3, SharePoint, the case system.
  - **access**: how a colleague gets it, e.g. the role or group, or who to
    ask.
  - **sha256** (optional): the digest of the artifact's content, so whoever
    fetches it can check it is the file that was attested. reckon never
    computes it, because it never has the file.

  It is a second STIX `external_reference` on the note: system as
  `source_name`, a location with a scheme as `url` and anything else as
  `external_id`, the digest as `hashes["SHA-256"]`, access as `description`.
  The vocabulary is STIX's, not invented.
- **A link that carries a credential is refused.** A pre-signed object-store
  URL or a shared-access link expires, and while it works it grants access to
  anyone reading the record. The aggregate rejects a location whose query
  carries a signature, credential or token parameter, and says to give the
  durable location and how to get access instead.
- Both are optional and never required to record a result. The checks live in
  the aggregate's command validation (`validateAttestation`), so no path
  bypasses them.

## Alternatives rejected

- **Upload the artifact.** That makes reckon an evidence store, and storing
  evidence is not reckon's job. Every question in the context would need an
  answer that the organization's own store already gives.
- **One free-text reference for both.** A bare string cannot say where an
  object lives, how to get access, and how to check the file, so a colleague
  would have to ask the analyst. Those are separate facts, and STIX already
  names them.
- **Accept pre-signed links for convenience.** The record outlives the link,
  and anyone who reads the record holds a working credential while the link
  lasts.
- **Hash every reference.** A ticket number is meant to be followed; hashing
  it by default would make the common case useless.

## Consequences

- An attestation is checkable by anyone in the organization who has access to
  the store the location names. That access is the organization's to grant;
  reckon grants nothing.
- Nothing about artifacts needs retention, redaction or purge in reckon.
- A location can go stale when the store moves or deletes the object. When a
  digest was recorded, a moved file can still be matched to the attestation.
- Issue #139 is closed by this record.
