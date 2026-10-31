# 0239 — A cancel stops a turn between steps, and the sidecar process is the transport's from the moment it spawns

Issue: #239 · PR: · Status: accepted

## Context

`SidecarTransport.start()` recorded the child only after `initialize` succeeded. A refused handshake (backend down, version mismatch) left the process running and the next turn spawned another, and a `dispose()` during the handshake left a live process on a disposed transport.

Nothing called `cancel`, so a turn could not be stopped, and turns had no time limit. A hung turn held the session, and every later turn on it was refused until the process restarted.

A cancel also has to be safe. The message history stays valid under cancel (adr/0231), but a cancel landing during a write tool can leave the write committed on the backend while the model's history records `context canceled`. The next turn's `[engine]` block shows the truth, the model's own history contradicts it, and it may repeat a `request_action`.

Progress sinks were keyed by session, and only the webview's `busy` flag kept two turns apart: a second turn overwrote the first's sink, was refused by the sidecar, and deleted the first's sink on its way out.

## Decision

**A cancel takes effect between steps, never inside a tool call.** A model call is cancelled at once; it writes nothing. A dispatched tool runs to completion on a context the cancel does not reach, bounded by `toolTimeout`. The loop then stops before the next model call, and that response's undispatched `tool_use`s are answered as not run. Stop, the time limit and `closeSession` (adr/0237) share this path, so the history always matches what the backend recorded.

**A stopped turn is not a failed turn.** `TurnResult.Stopped` and the wire's `stopped` replace an error for a cancel. The turn is committed as adr/0231 decided, with the `[engine] turn ended early` line. `Turn`'s error now carries only real failures. The line says `timed out` when the cancel carried that cause (`cancel`'s `reason`, a context cause in Go), so the record does not attribute a time limit to the analyst.

**The panel has a Stop button.** While a turn runs, Send is Stop and posts `turn.stop`; the label reads Stopping… until the turn ends, which a running tool can delay. The host calls `transport.cancel`. Actions already requested stay in the queue.

**Turns have a limit**, `reckon.agent.turnTimeoutMinutes` (default 10, minimum 1). On expiry the transport cancels with reason `timeout` and the panel shows the turn timed out.

**The transport owns the process from spawn.** `this.child` is set at spawn, and any failure in the handshake tears down. A teardown during the handshake disposes the handshake's connection, which fails it.

**One turn per investigation, on the host.** `turn()` refuses before touching the sink map, and a sink is deleted only by its owner. `TurnDriver` checks first and posts a notice, so a refused turn does not end the running turn's state in the webview. A stop requested before a turn reaches the sidecar keeps it from starting, which also covers a panel closing during the handshake.

## Alternatives rejected

- Cancelling tools too, and reconciling afterwards: the model would need to be told what really happened, and some outcomes cannot be known at that point.
- Killing the process to stop a turn: it would end every session and lose the turn's record.
- A time limit in the sidecar: the client owns the setting, and `cancel` already exists.

## Consequences

- Stop can take up to `toolTimeout` while a tool finishes, usually milliseconds. A tool past `toolTimeout` fails, and its write may still have happened: the limit bounds the wait and does not remove the original ambiguity for a tool that truly hangs.
- A response that arrives complete as the cancel lands enters the history, and its tools are answered as not run.
