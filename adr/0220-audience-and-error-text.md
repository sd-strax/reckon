# 0220 — Every token must carry reckon's audience; a 500 names what failed, not why

Issue: #220 · PR: — · Status: accepted

## Context

The backend validated each JWT's signature, issuer and expiry, and checked its
`aud` claim only when `keycloak.client_id` was set. The default was empty and
`reckon init` did not set it, so a default install accepted any valid token
from the realm, whichever client it had been issued for. A realm shared with
other applications would have had its tokens for those applications accepted
here.

The bundled realm already stamps `aud: reckon` on tokens from both of its
clients (`reckon`, `reckon-agent`), and every token source (the CLI, the eval
runner, the workbench's login and agent flows) uses one of them.

Separately, 47 places in `server/` answered a server-side failure with the
error text itself (`"load action: "+err.Error()`), including the default
branch of `writeCommandError`. Database errors carry table, column and
constraint names and sometimes values; none of it helps the person who sees it.

## Decision

- **The audience check is always on.** `keycloak.client_id` defaults to
  `reckon` and names the audience every token must carry. There is no switch
  to skip the check: `authz.VerifierConfig` has no skip field, and
  `NewVerifier` refuses an empty client id, so the backend does not start
  without one. A realm whose tokens carry another audience sets `client_id`
  to it.
- **A 500 names what failed and gives a reference.** `writeInternalError`
  answers `{"error": "<what>: internal error (ref <ref>)", "ref": "<ref>"}` and
  logs the cause with the same reference, so a failure someone reports can be
  found in the log. Every 500 in `server/` goes through it.
- **4xx messages are unchanged.** They describe the caller's own request.
- **The capability invoke 502 keeps the adapter's error.** It reports an
  external tool's failure, which the analyst and the agent act on, not
  reckon's internals.

## Alternatives rejected

- **A skip-audience switch:** a realm without the audience mapper is fixed by
  adding the mapper or setting `client_id`; a switch would only make the
  unsafe setting easy to reach.
- **A generic 500 without a reference:** the cause would be in the log with
  nothing tying it to the failure the analyst saw.
- **Using the trace id as the reference:** no exporter ships at v0, so the id
  would point at nothing unless logged anyway; a random reference on the log
  line is the same thing with no dependency on tracing.

## Consequences

- A token without `aud: reckon` (or the configured audience) is a 401.
- A config that sets `client_id: ""` fails at backend start, naming the
  missing audience.
- A client sees less on a 500; anyone debugging reads the log line for the
  reference.
