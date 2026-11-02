# 0233 — Each tool has one definition, and a call reaches only the tools the model was offered

Issue: #233 · PR: #296 · Status: accepted

## Context

`agent/tools.go` declared each intrinsic tool twice: `intrinsicTools` built its
JSON schema by hand, and a `case` in `dispatch` declared an anonymous struct and
decoded the model's input into it. Nothing connected the two. A property added
to a schema and not to the struct was shown to the model and dropped by
`json.Unmarshal`; a field added to the struct and not to the schema was accepted
and never offered.

That second direction was already live. `request_action` decoded its targets
into `ActionTarget`, which carries `asset_criticality`. The schema never offered
that field, but a value the model sent anyway was forwarded to `POST /api/actions`
and reached Gate 2 as `ctx.targets[].asset_criticality`, an input to tenant
policy.

The `default` arm of `dispatch` sent any name it did not recognise to
`InvokeCapability`, without looking at the session's tool list. A misspelled
intrinsic and a verb `buildTools` had trimmed both reached the backend, and the
model read an error about capabilities for what was a mistake about tools.
A verb sharing an intrinsic's name was offered twice and silently shadowed.

Separately, `session.go` switched on tool names after dispatch to parse recall
results, so the shape of a recall response was known in two places.

## Decision

- **The input struct is the schema.** Each intrinsic tool is one `toolSpec[In]`:
  name, description, and a `Run` taking the decoded `In`. A small generator
  (`agent/toolschema.go`) renders `In` from `json`, `desc`, `required` and
  `nonempty` tags. A field without a description, a type it cannot render and an
  override naming no field all panic: they are programming errors reached by
  every session start, so by every test. What the model is shown is what the
  decoder reads.
- **The catalog parts stay computed.** `request_action` overrides `action_type`
  and `parameters` with the schemas the action catalog produces; a Go type cannot
  state them. Everything else about that tool is generated.
- **The change is byte-preserving.** `agent/testdata/intrinsic_tools.golden.json`
  was captured from the hand-written schemas before they were removed, with and
  without an action catalog. The generated definitions match it exactly, so the
  model is shown what it was shown before and no eval baseline is rerun.
- **`targetInput` holds only what the model may say about a target.** The
  model's `asset_criticality` now stops at the decoder. This is the one input
  whose behaviour changes.
- **One step builds what is offered and what is reachable.** `buildTools`
  returns the list and the same tools by name, and the session assigns them
  together. Verbs are tools too. `dispatchTool` resolves the name there, exactly,
  and anything else is an `isError` result naming the tool: `unknown tool "x":
  not one of the tools defined for this session`. The turn continues, so the
  model can correct itself, and the backend is not called.
- **Two tools of one name fail the build.** `refreshContext` returns the error.
- **Provenance is a return value.** `Run` returns `toolResult{Content, Consulted,
  Similar}`. A recall tool parses the response it received and the session
  records what comes back, unconditionally. A tool mutates no session
  provenance, and `session.go` has no name switch.
- **A recall bounds its own content.** What a turn consulted is what the model
  was shown, so the recall tools parse the bounded bytes: a hit left off the
  page was not put in front of the model. `selfBounded` tells dispatch not to
  bound again; `read_result`, already a page, uses it too.

## Alternatives rejected

- **Keep hand-written schemas and add a conformance test.** Two artefacts and a
  test whose reach decides what can drift. Generation removes the second
  artefact.
- **A third-party schema generator.** The shapes in use are few and the output
  has to match existing bytes exactly; about sixty lines of `reflect` do it with
  no new dependency.
- **An `after` hook on a tool for provenance.** It hangs a side effect on a
  definition, couples tools to the turn's audit state and invites more of the
  same. A returned value keeps the tool a function and the recording in the
  session.
- **Parse provenance from the raw response in the tool.** It would record hits
  the model never saw whenever a recall was paged.
- **One input type for both recall tools.** Their `query` and `tags` descriptions
  differ, and those bytes are what the model reads. They keep separate types and
  share `recallBody`.
- **Repair a near-miss name.** The engine does not infer which tool the model
  meant. It reports the name it got.

## Consequences

- A property added to a tool is shown to the model in the same edit, and any
  change to what the model is shown fails the golden.
- A tenant verb named like an intrinsic tool stops every session on that install,
  with an error naming the tool, instead of being shadowed.
- A model that calls a trimmed verb is told it has no such tool rather than why
  the verb is degraded; the prompt's degraded-capabilities section still says so.
- The server still decodes `asset_criticality` from the request body, so this
  closes the model's path to the field and not a direct caller's.
- #232 can hash these definitions knowing the first pin is of unchanged text.
