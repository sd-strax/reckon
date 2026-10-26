# 0061 — Absence is citeable: a complete, empty answer mints an `x-null-observation`

Issue: #61 · PR: #72 · Status: accepted

## Context

An outcome on a prediction must cite evidence; the engine refuses one that
does not. That rule is the mechanism that stops the agent from declaring
results it never observed. It had no answer for the most common finding in
an investigation: we looked, and there was nothing. A query that returns
COMPLETE with zero results is decisive — the prediction "a change record
exists" is disconfirmed — but it returns no object, so there was nothing to
cite. In an eval trial the agent was refused for an empty citation and, on
retry, cited an unrelated logon record to make the write go through. The rule
that exists to prevent false claims had forced a false citation.

Constraints. STIX 2.1 cannot say it: `observed-data` must observe at least one
object, and a `sighting` sights an indicator, not a change record. The domain
model admits custom objects only for gaps STIX has (the `x-` SCOs), with the
gap named in "Adopted vs invented". The capability layer is pure I/O and
normalization: it may record what it did, never what it means. And telemetry
persistence is deferred at v0, so a receipt keyed by API call would have
needed a store that does not exist.

## Decision

- **A custom STIX SDO, `x-null-observation`.** It records that a question was
  asked of a source with complete coverage and nothing came back: the verb,
  the subject entity (the call's typed input, verbatim), the other
  parameters, the window searched, the sources that answered, and provenance
  DIRECT. Where the subject's identity is unambiguous from the input alone
  (an address, a domain, a URL, a host) it carries `object_refs` to those
  SCOs, so the absence hangs in the graph off the thing it is about. An
  account is left in `subject` until entity shaping is shared with the
  normalizers (design/03 §7.4).
- **Minted by the resolver on one mechanical condition.** Coverage COMPLETE
  and zero events. A PARTIAL, UNAVAILABLE or FAILED empty answer mints
  nothing, so an absence the layer cannot vouch for is not citeable and the
  only honest outcome is INCONCLUSIVE. The epistemic distinction lives in
  whether the object exists, not in a check someone must remember to write.
- **Identity is UUIDv5** over verb, sorted sources, subject, parameters and
  window: the same question answered empty twice is one object; a different
  window is a different absence.
- **It is cited like an observation.** Its id rides `observed_data_refs` in
  the result, so the agent cites it with no new instruction and the prompt
  version does not move. It is persisted in `stix_objects` with every other
  normalized object, opens from any citation, is labelled in the report and
  the evidence view as "nothing observed: verb from source over window
  (coverage complete)", and travels in the archive bundle.
- **The evidence rule is unchanged.** It admits the object by shape as it
  admits any STIX id; the gate from adr/0051 and the attestation path from
  adr/0054 are untouched.

## Alternatives rejected

- **Relax the evidence rule for empty results.** The exception for "nothing
  found" is the exact hole a model would walk through.
- **A query receipt as a telemetry event** (OCSF API Activity). Honest, and
  it would also have recorded the window for positive results, but it needed
  a telemetry store that is deferred, and it says "we asked and got nothing"
  where the record needs "nothing was there". The window gap for positive
  results remains and is smaller.
- **A STIX sighting with count zero.** The spec allows the number, but a
  sighting sights an indicator or malware; a change record is neither.
- **A system-authored note.** adr/0054 made notes a person's statement; an
  engine-authored note blurs who said what.
- **Observed data with no objects.** STIX forbids it, correctly.

## Consequences

- The demo's "no change record" beat and the second scenario's "no travel
  notice" now produce a citeable object; a DISCONFIRMED prediction over an
  empty query records honestly.
- No prompt change, no baseline rerun. Whether the agent cites the new ref
  unprompted is a question the next eval runs answer; if it does not, that is
  a tool-description or prompt question handled under adr/0057's process.
- `object_refs` for accounts is a follow-on once entity shaping is shared;
  until then the account is named in `subject` and the object still opens.
- Tool descriptions are not part of the prompt version; this record did not
  change them, and the gap is noted for design/10.
