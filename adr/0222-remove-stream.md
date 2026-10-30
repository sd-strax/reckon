# 0222 — The backend's WebSocket stream is removed; clients learn of changes by reading

Issue: #222 · PR: — · Status: accepted

## Context

`GET /api/stream` was a WebSocket that pushed a frame per committed aggregate
event to subscribed clients. An in-process hub (`server/hub.go`) fanned the
frames out, each HTTP handler that recorded something called `publishDeltas`
after its commit, and the module carried `github.com/coder/websocket` for it.

Nothing consumed it. The workbench, the sidecar and the CLI never opened the
stream; the workbench re-reads after its own commands and polls while a
dispatch is in flight. Two gaps meant it could not have been relied on as it
stood:

- Only HTTP-handler writes were published. The events the Temporal workflows
  record (dispatched, resulted, reversed, reversal attempted, expired, outcome
  unknown) never reached it, and those are the changes a client does not cause
  itself.
- A subscriber whose 32-frame buffer filled had frames dropped with a log line
  and was not told, so it could not know to reload.

The need for push is concurrent work on one investigation by several people
(`design/05 §8`). A single analyst's screen is kept current by re-reading.

## Decision

- `server/hub.go`, `server/stream.go` and their test are deleted, with the
  `/api/stream` route, the hub on `Backend`, and the 11 `publishDeltas` calls.
- `github.com/coder/websocket` leaves `go.mod`.
- Clients learn of changes by reading. `design/ui/00-backend-binding.md`,
  `implementation/workbench-react.md §3.3` and the feature inventory (C12) say
  so.
- `design/05 §8` (shared investigation) is unchanged: it specifies the push
  path for concurrent work, post-commit `NOTIFY` and a `LISTEN`er fanning out,
  not the removed hub.

## Alternatives rejected

- **Fix the stream** (publish at the handler's commit so workflow events are
  carried; close a lagging client so it reloads): it would make a feature with
  no consumer correct, and the dependency and the per-handler calls would stay.
- **Leave the code unused:** every new handler copies a `publishDeltas` call,
  and the module ships a dependency for a route nobody opens.

## Consequences

- A change a client did not make (an action's result, an expiry, an
  auto-approval, a second approver) appears on its next read. Making that read
  cheap is the client's work, not a push channel's.
- Bringing push back starts from `design/05 §8.1`. The removed pieces are in
  history at the parent of this change: the hub and handler were about 250
  lines. The client side was sketched as one connection per open investigation
  that refreshes the slices an event's family touches when the sequence number
  is the next one, and reloads everything otherwise. Whatever replaces the hub
  must carry workflow-recorded events and tell a client that missed frames.
- `adr/0219`'s note that its listener timeouts leave the stream alone no
  longer applies.
