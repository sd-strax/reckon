# 0244 — What crosses the wire is decoded, and a version mismatch fails closed on the host

Issue: #244 · PR: #289 · Status: accepted

## Context

The sidecar refused a protocol mismatch, but the host sent `PROTOCOL_VERSION` and only logged the reply, so the pair failed closed only because one side checked. The API-version handshake ran in the tree and the `checkBackend` command. A panel opened or reloaded against a backend upgraded under a running window called it anyway.

Everything inbound was a hand-kept copy of a Go struct, cast with `as`. Sidecar results defaulted with `?? ""`, and `BackendClient` ended in `(await res.json()) as T` behind 39 call sites whose mappers defaulted too. A renamed field became blank output with no error. A candidate SOP with no `title` threw outside `draftSOP`'s `try` and left the panel on "drafting". The webview router called `handlers[msg.type](msg)`, so an unknown type was a `TypeError`.

## Decision

**The host enforces the protocol version.** `start()` compares the `initialize` reply with `PROTOCOL_VERSION`. A mismatch or a missing value throws a clear error, the child is killed (adr/0239), and the analyst sees why on the turn.

**Every panel load checks the API version.** A load calls `status()` first. If `incompatibilityReason` has an answer, the panel shows it and makes no other call. That covers investigation, draft and case panels. A refresh of a panel already loaded does not check.

**Decode, do not cast.** What the sidecar and the backend send starts as `unknown` and is read through small decoders in `wire/decode.ts`, with one shape per result, notification and response in `wire/sidecarShapes.ts` and `wire/backendShapes.ts`. A required field that is missing or mistyped is a named error: "unexpected response from proposeSOP: title missing". Required means what the Go tag says: a field without `omitempty`. A slice Go always sends may be `null` but must be present, so a renamed list is an error and not an empty one.

**Unknown fields are ignored**, so a version can add fields without a bump.

**A failure surfaces where the analyst is looking.** A malformed result fails the turn or the load. A malformed notification is dropped, the turn runs on, and a notice at the turn's end says part of it could not be shown: what the turn commits is read from the backend, so only the live view lost a piece. `/status` is the handshake itself, so a body it cannot read means incompatible, not unreachable. A write whose answer the workbench ignores does not parse it.

**Fixtures are the cross-language check.** A Go test generates two samples of each wire type by reflection, under `workbench/src/wire/fixtures`: the zero value, showing what Go omits or sends as null, and every field filled. The Go test fails on any difference, which `make wire-fixtures` rewrites. The workbench test decodes every sample, requires a decoder for each and a sample for each decoder, and checks that every key a decoder names is in the filled sample, which catches the rename of an optional field. The protocol and API versions are in the samples too, so the hand-kept constants on both sides are held equal. Drift fails `make ci` or `make workbench-ci`.

**An unknown webview message is logged and ignored.** Webview payloads are not validated beyond `type`: the page is the same build as the host, and `protocol.ts` is already their single definition.

## Alternatives rejected

- A schema and generated bindings now: that is the single-definition decision, and it is deferred to #278, to be made with these fixtures in hand. It is also why this adds no dependency.
- A hand-written `decode` per response returning the mapped view: decoding and mapping would mix, and the checks would be less uniform than one shape per response.
- Rejecting unknown fields: every additive change would become a breaking one.
- Hand-written samples: they would drift the way the structs did. Generating them means a changed tag changes the sample.
- A malformed notification failing the turn: the turn is running and may commit, and reporting a failed turn would be false.
- Validating webview messages: same build, nothing to drift against.

## Consequences

- The decoders still restate the Go shapes by hand. The fixtures catch drift; they do not remove the second copy. Removing it is #278.
- The workbench and the backend now must agree: a required field removed without an API version bump fails loudly where it used to show a blank.
- Inline `map[string]any` responses are stated as structs in the Go fixture test, so for those few (the envelopes, the transcript, the SOP import) the sample is a mirror of the handler and not read from it.
- A loaded panel's optional slices keep degrading as before, so a malformed optional slice shows as unavailable and is logged at debug level.
- A panel load costs one more request.
