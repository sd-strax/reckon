# 0278 — The Go type is the one definition of every wire shape; TypeScript is generated from it and `make ci` checks both halves

Issue: #278 · PRs: #416 (step 1), #417 (step 2), #418 (step 3) · Status: accepted

## Context

Reckon has three connections between processes: the workbench and the sidecar
(JSON-RPC over stdio), the workbench and the backend (HTTP), and the sidecar
and the backend (HTTP, where the agent's calls go). Every shape on them was
written twice by hand: in Go, and again in TypeScript or in the agent's own Go
copies.

adr/0244 made drift visible: the workbench decodes every message at the
boundary, and golden samples generated from the Go types are tested against the
TypeScript decoders. Drift was still possible:

- The TypeScript half of the check ran only in `make workbench-ci`, because
  `make ci` never required Node (`implementation/module-layout.md`). Regenerating
  the samples turned `make ci` green while the decoders stayed stale.
- Requests were not checked. The sidecar decoded params without refusing
  unknown fields, so a renamed param became a default value.
- The agent's Go copies of backend shapes had no check at all, so a renamed
  field reached the model as a blank.
- Status words were decoded as any string, so a renamed word passed every test.
- Responses built as inline maps had no type, so their samples were restated by
  hand.

## Decision

- **The Go struct is the definition.** Every shape that crosses a connection is
  a named Go type, including the responses that were inline maps and the status
  fields that were plain strings.
- **An in-house generator writes the TypeScript.** It reads the Go types by
  reflection (stdlib `reflect`, no new dependency) and writes the decoders and
  types for responses, results and notifications, the types for request bodies
  and params, and the version constants. The generated files are checked in
  under `workbench/src/wire/`.
- **The sidecar ↔ backend shapes live in a shared Go package** that the server
  and the agent both import (adr/0385). Between two Go programs the compiler is
  the check, so nothing is generated there.
- **`make ci` checks both halves.** A Go test fails when the checked-in
  TypeScript differs from what the generator writes, and `make ci` runs the
  workbench's compile, lint and tests. Node is a prerequisite for `make ci`.
  The golden samples stay, and prove the generator's rules match what
  `encoding/json` sends.
- **The contract rules**, applied by the generator:
  - a response's unknown fields are ignored; a request's are refused, by the
    backend and the sidecar alike;
  - a field without `omitempty` is required; with `omitempty` it may be absent
    or null; a pointer without `omitempty` is a required key whose value may be
    null; `omitzero` is read as `omitempty`, since the field is left out when
    it is zero;
  - a `time.Time` is an RFC 3339 string; any other type with its own
    `MarshalJSON` or `MarshalText` is refused unless the generator lists it;
  - null and empty mean the same for a list or a map;
  - an `int64` is a count or a sequence below 2^53 and stays a JSON number; an
    identifier is a string; `uint64` is refused;
  - `json.RawMessage`, `any` and `map[string]any` are opaque; a field the
    workbench reads into needs a named Go type;
  - a named string type whose package lists its values is strict: the
    TypeScript is a union and its decoder refuses any other word. It lists
    them through a `WireValues() []string` method, and a zero sample writes
    the first word for a required field of that type;
  - a generated decoder reads every field the Go type sends.

## Alternatives rejected

- **JSON Schema as the source, generating Go and TypeScript.** The backend
  serves engine types directly, so the domain types would become generated code
  or gain a parallel layer of wire types with mapping between them. Reckon has
  no consumer outside its own Go and TypeScript. A schema can be emitted from
  the Go types later if one appears.
- **OpenAPI as the source.** It covers HTTP only, not the sidecar's JSON-RPC, and
  its generators give types, not the runtime decoders adr/0244 chose.
- **`go/types` instead of `reflect`.** It reads comments and constant sets, but
  needs `golang.org/x/tools` as a direct dependency, and docs are not a goal.
- **Keep `make ci` Go-only.** The TypeScript is generated from Go, so a Go change
  can break it. Checking only the Go half leaves the contract half checked.
- **A test that checks the agent's copies against the samples.** It keeps the
  copy. A shared package removes it.

## Consequences

- A shape change is made once, in Go. The generator updates the TypeScript, the
  compiler updates the agent, and `make ci` fails on anything left behind.
- A renamed request param or field fails at compile time inside one checkout,
  and is refused by name across versions.
- Engine developers need Node to run `make ci`.
- A new wire shape must be a named Go type the generator knows, and a new status
  word comes with an API version change (adr/0385).
