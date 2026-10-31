# 0237 — The sidecar depends on no backend code, and a session ends when its panel or its person does

Issue: #237 · PR: #279 · Status: accepted

## Context

`internal/sidecar` imported `server` for one constant, which put aggregate, supervisor, temporal and knowledge into its dependency graph. The binary is unaffected, since `reckon investigate --stdio` and `reckon serve` are one executable; the cost is the package coupling and its test build.

Sessions never ended. `service.sessions` was only ever added to, and the workbench's map cleared only when the process died. Each session holds its full conversation, and sign-out touched neither map, so after a sign-in as someone else the next turn on the same investigation carried the previous person's conversation, tool results included, into the model's context. Backend authorization still held on every call; the leak was in what the model had already been told. The sidecar cannot detect this: it holds no identity and pulls a token per call.

`proposeSOP` returned `agent.SOPCandidate` directly, so the agent's JSON tags were the protocol.

## Decision

**The API version is a leaf.** `internal/apiversion` imports nothing; `server.APIVersion` is an alias of it. A test asserts the sidecar's dependencies exclude `server`.

**`closeSession` ends one session.** It removes the session, refuses anything new on it, cancels what is running, waits for that to stop, then records any turn held by a failed commit (adr/0231), answering only after that. The workbench calls it when a panel closes. A new session on the same investigation waits for the close, so it rehydrates from a complete thread; the conversation returns as text, as after a sidecar respawn.

**Work without a panel releases its session.** A conclude-time summary outlives the panel and holds off the close until it finishes, since the panel closing does not undo the conclude. Work that started as the panel closed, a SOP draft, and the tree's context reset each release the session when they end if no panel holds the investigation.

**A change of person ends the process.** The workbench's `Session` fires `onDidChangeIdentity` when the human token's `sub` changes, signing out included, and before `onDidChange`. The transport tears the process down. That drops every session and in-flight turn at once, with no window for a stale turn to run, and the next turn spawns clean. A token with no readable `sub` counts as a change. The claim is read for keying only; the backend verifies the token.

**The SOP candidate is a sidecar struct**, `proposeSOPResult`, with the same six keys, so the wire is unchanged.

**`turn/tool_call` is display data.** `name` and `input` are opaque to the protocol, which promises no tool vocabulary. No code changes: the webview only renders them.

## Alternatives rejected

- A "drop all sessions" RPC on identity change: a turn in flight could race it, and it leaves the process and its API key alive.
- An idle cap on sessions: with close and identity change covered, live sessions are bounded by open panels and in-flight work.
- Keying sessions by person in the sidecar: it would have to be told who is signed in, which is the extension's sole concern.
- Translating tool names into a protocol vocabulary: nothing branches on them.

## Consequences

- A panel reopened after close rehydrates as text, so tool detail and the prompt cache are not carried over.
- A turn in flight on a change of person is stopped without a commit, and held turns are lost with the process: the previous person's token is gone, so neither could be recorded.
- A turn that starts while its panel closes, before the process is up, runs until it ends and is then released. It is not cancelled at the close.
