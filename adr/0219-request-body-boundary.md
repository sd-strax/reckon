# 0219 — Every request body goes through one bounded, strict decode; the listener has read, write and idle timeouts

Issue: #219 · PR: #269 · Status: accepted

## Context

The backend decoded JSON request bodies by hand at 25 sites in `server/`.
Four bounded the body, with three different limits; the other 20 were
unbounded. None rejected unknown fields or data after the first JSON value.
Approve, reject and re-request discarded the decode error and carried on with
zero values: a malformed approve body on a T3 action came back as a 422 about
the missing typed challenge, and a malformed reject went through with no
reason. Edit reported a decode error as "parameters required". A body over a
limit came back as a 400.

The listener set only `ReadHeaderTimeout`, so a client that sent its headers
could hold a connection open indefinitely while trickling a body or never
reading the response.

Unknown fields were silently dropped by `encoding/json`: a misspelled key read
as an absent one, so a misspelled `challenge_response` produced a misleading
error and a misspelled `reason` recorded a rejection without one.

## Decision

- `decodeBody(w, r, dst, limit)` in `server/body.go` is the one decode path. It
  bounds the body with `http.MaxBytesReader`, decodes with
  `DisallowUnknownFields`, requires exactly one JSON value, and writes the
  error itself: 413 over the limit, 400 for anything else. An empty body is a
  400.
- `decodeOptionalBody` is the same for routes whose body may be absent
  (approve, reject, re-request, case note): an empty body leaves the target
  zero, and a body that is present is held to every rule above. A malformed
  or misspelled body on those routes is now a 400 and records nothing.
- Two limits: `maxBodyBytes` (1 MiB) for every route, and
  `maxInterpretationBodyBytes` (10 MiB) for `/api/interpretations`, which
  carries a turn's transcript.
- Edit reports a decode error as a bad body, and "parameters required" only
  when the body parsed with no parameters.
- The listener (`newHTTPServer`) sets `ReadHeaderTimeout` 5s, `ReadTimeout`
  30s, `WriteTimeout` 90s and `IdleTimeout` 120s. The write timeout sits above
  the agent client's 60s request timeout, so a slow capability call is ended
  by the client rather than cut mid-response. The WebSocket stream is not
  affected: `net/http` clears a connection's deadlines when it is hijacked.
- Every request the workbench, the agent client, the CLI and the eval driver
  build was checked against its server struct before unknown fields were
  rejected; none sends a key the server does not declare. The agent's
  knowledge tools copy only declared keys from model input into their request
  bodies, so a key the model adds never reaches the server.

## Alternatives rejected

- **Accept unknown fields:** a misspelled key stays indistinguishable from an
  absent one, which is how the approve and reject bodies went wrong.
- **A limit per route:** the routes differ only in whether they carry a
  transcript; more limits are more numbers to keep right for no difference in
  protection.
- **Timeouts from config:** no deployment has asked to tune them, and the
  write timeout is tied to the agent client's own constant, not to the site.
- **`http.TimeoutHandler` per route:** it buffers every response in memory and
  leaves the handler running after the client is answered; the listener
  timeouts bound the connection, which is what was unbounded.

## Consequences

- A client that adds a field must add it to the server struct in the same
  change, or every request gets a 400.
- A newer client talking to an older backend fails loudly on a new field
  instead of having it silently ignored.
- A capability call slower than 90s is cut off by the server; today the agent
  client gives up at 60s anyway.
