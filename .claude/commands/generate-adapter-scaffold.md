---
description: Scaffold a new in-tree out-of-process adapter under cmd/reckon-adapter-<name>/
---

Scaffold a new in-tree adapter: a static Go binary under `cmd/reckon-adapter-<name>/` that speaks the reckon plugin protocol (`design/11-adapter-plugins.md` §2, §4) on stdin/stdout, plus its manifest, bundling and build lines. Read `design/11` §3–§4 and §7 and `design/03-capability-layer.md` §5 first.

## Ask before generating

1. **The vendor and product**, to name the adapter (`adr/0331-adapter-naming.md`): the product name alone when it is the vendor's name (`okta`, `servicenow`); otherwise `vendor-product` (`crowdstrike-falcon`, `aws-security-lake`); a further adapter on the same product appends its role (`vendor-product-role`). Lower-case words joined by hyphens. The name is the directory suffix, the binary `reckon-adapter-<name>`, the manifest `name`, the bundled registry key and the tenant-config instance key. Propose the name and confirm it.
2. **The class** (`03 §5.4`): `NATIVE_API` (default when unstated), `MCP` (a bridge to a vendor MCP server, with a `runtime` block), or `CUSTOM`.
3. **Read, write or both**: which catalog verbs (`capability.DefaultCatalog()`) and action types (`action.DefaultActionCatalog()`) it serves. Use the engine's own descriptors; never invent a verb.
4. **Where its fields come from** (`11 §4.4`), which sets `schema_source.kind`:
   - `response`: it calls a vendor API and decodes the response (most adapters).
   - `query`: it names fields in a query (SQL, SPL, KQL) against a store whose schema it does not own (a vendor-managed table, a security lake, a hosted search index). For a `query` adapter also ask for the store, the source versions to support, and how its schema is read (a catalog API, `DESCRIBE`, an index mapping).

## Layout (copy the shape of `cmd/reckon-adapter-servicenow/`)

- `main.go`: package doc (what it bridges, the class, the surfaces); the JSON-RPC envelope (`message`, `wireError`), the `server` loop over Content-Length framing (`readFrame`, `write`), and `classErr` for the error class in `data.class` (`03 §6.2` read classes `RETRY`/`FALLTHROUGH`/`UNHEALTHY`/`FATAL`; `08 §5` write classes `RETRYABLE_ERROR`/`FATAL_ERROR`). The framing is the adapter's own; do not import the host's.
- `describe.go`: `describe() adapterplugin.DescribeResult` with the verb and action descriptors (from the engine catalogs), `Operations` with their param schemas, `DefaultReadBindings` / `DefaultWriteBindings` in the catalog's canonical input vocabulary, `ConfigSchema` (secrets marked `x-secret`), and `SchemaSource`:
  - `response`: `&adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceResponse}`.
  - `query`: `Kind: adapterplugin.SchemaSourceQuery` with one `adapterplugin.SchemaProfile{Name, Store, Version, Fields}` per supported source version; `Fields` lists every field the projection and the predicates name (nested paths dotted, an array element as `name[].field`).
- `bridge.go`: the `bridge` (configured instance + vendor client under a mutex), `handle` dispatching `initialize` / `describe` / `configure` / `invoke` / `dispatch` / `health`, `configure` validating config (a literal secret is refused; references are resolved by the host before configure, `11 §4.3`), the OCSF shaping on invoke (`03 §5.2`: the adapter produces OCSF events, never STIX), idempotent dispatch keyed on the idempotency key (`08 §6a`), and `health` returning `adapterplugin.HealthResult`.
- `<vendor>.go`: the hand-rolled vendor client (stdlib `net/http`; raise any third-party dependency and its license before adding it).
- `<name>_test.go`: tests against an `httptest` mock of the vendor API: tolerant decoding of a response missing optional fields, the error-class mapping, dispatch idempotency, and that protocol params never reach the vendor.
- `testdata/raw_response/`: recorded vendor responses (`03 §9.1`), scrubbed of tenant identifiers and secrets.

## A `query` adapter, additionally (`11 §4.4`, adr/0334)

The reference is `cmd/reckon-adapter-aws-security-lake` (until #335 lands, it predates discovery and is exempt from the repository test; follow §4.4, not its fixed projection).

- **Schema discovery**: read the store's schema at `configure` and on every `health` probe, and build the projection and predicates from the fields that exist, never from a fixed column list.
- **Profiles**: `testdata/profiles/<profile.name>/` for every profile `describe` lists, recorded from a real store: `schema.json`, the store's schema as captured, and `pages/`, one file per real result page, at least one. `schema.json` is valid JSON; `pages/` is a real directory; each is a regular, non-empty file (a symlink does not count, and a dotfile such as `.gitkeep` is not a page). The profile name is one path element (no `/`), unique within `describe`. The repository test fails a profile missing either part. Offline tests read these, not hand-built pages.
- **Decline**: a call that needs a field the discovered schema lacks returns `FALLTHROUGH` with a reason naming the field; it is never sent to fail at the store. Test it against a profile missing that field.
- **Degraded health**: `HealthResult.Degraded` lists `adapterplugin.DegradedCapability{Operation, Capability, Reason}` for each capability the schema cannot express ("credential filter: `actor.session.credential_uid` absent"). `Healthy` stays the whole-instance signal: a field no call can do without (the event time) is unhealthy.
- **`examples/adapters/<name>/LIVE.md`**, the live-check runbook, with these sections:
  - *Stand up*: the store, the account or tenant, the subscriber or credentials, and what generates events.
  - *Capture*: the commands that read the store's schema and a few result pages, and where they land (an ignored `data/` directory, never committed raw); then scrub a copy (account identifiers, principals and addresses replaced with same-shape placeholders) into `testdata/profiles/<profile>/`.
  - *Compare*: diff the captured schema against `testdata/profiles/<profile>/schema.json`; a new or missing field is a profile update (and a new profile for a new source version).
  - *Tear down*: everything *Stand up* created.
  - *When*: before a release and when the vendor announces a new source version.

## Manifest, bundling and build

- `examples/adapters/<name>/manifest.yaml`: `manifest_version: 1`, `name`, `version`, `protocol_versions: [1]`, `class`, `exec: ["./reckon-adapter-<name>"]`, a `runtime` block only for an MCP bridge, the claimed `summary` (verbs, action types), and the `config_schema` mirroring `describe`. Add `examples/adapters/<name>/tenant-config.yaml` enabling the instance and its bindings, in the style of the servicenow example.
- `internal/bundledadapters/manifests/<name>.yaml`: a byte-identical copy of the example manifest; embed it in `bundled.go` (`//go:embed` + a `registry` entry `{Name, Manifest, Binary: "reckon-adapter-<name>"}`). `TestRegistryNamesAgree` checks the name everywhere it is a contract.
- `Makefile` `build`: `go build -o bin/reckon-adapter-<name> ./cmd/reckon-adapter-<name>`.
- `Dockerfile`: add `/src/bin/reckon-adapter-<name>` to a `COPY --from=build … /usr/local/bin/` line.

## Done when

- `go build ./cmd/reckon-adapter-<name>` and its tests pass.
- `reckon adapter test <install-dir> --config <f.json>` passes: handshake, describe, `schema_source`, health (and a well-formed `degraded` list).
- `TestInTreeAdapters_SchemaSource` (`cmd/reckon/adapter_schema_test.go`) passes: it reads every in-tree adapter's `describe` and, for a `query` adapter, requires each profile directory and `LIVE.md`.
- Ask before running `make ci`.
