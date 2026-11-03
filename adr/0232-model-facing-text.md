# 0232 — Every sentence the model reads is under a version and a ledger

Issue: #232 · PR: #299 · Status: accepted · Amends the scope of [0057](0057-prompt-ledger.md)

## Context

`PromptVersion` was the SHA-256 of the base prompt alone (adr/0057). The eval
baselines are accepted under it (design/10 §4.4), and it is the one identifier
saying which instructions a run was graded against. Most of what the model reads
was outside it:

- **The tool descriptions.** They carry rules: "never assume", "never you", "you
  can propose, never approve". Rewording one moved no hash, no golden and no
  ledger entry. This has already happened: after the four baselines were
  accepted at a8da184, 1a7fecf added the `read_result` tool and 3523b3e reworded
  `list_actions`, and the version did not move.
- **The engine text.** These are the fixed sentences of the `[engine]` blocks on
  the analyst's message (the turn clock, the reasoning state, the action-record
  anchor), the footers on the model's reply (the action-status reconciliation,
  the no-action attestation), the notes on condensed and paged context, and the
  answer to a cut-off tool call. Some are the imperatives the eval measures (H4,
  H7, H8). adr/0057 left the blocks out because a rendered block carries the
  investigation's own identifiers. The sentences around those identifiers are
  fixed code. The paging and condensing notes (1a7fecf, cf59268) also arrived
  after the baselines.
- **The base prompt's conditional paragraphs.** The case-seed instruction and
  the degraded-verbs paragraph only render on an investigation that has a case
  seed or a degraded verb. The base prompt is rendered with neither, so neither
  was hashed.
- **The side instructions.** The summary and SOP-draft completions take their
  whole prompt from three constants in `session.go`. They were not hashed,
  ledgered or tested.
- **The judge.** `JudgeRubricVersion` was a hand-set `"v1"`. It was set in
  286e42a with the E3 and R3 rubrics. 90e0a91 added the A4 rubric and left it at
  `"v1"`, so two runs graded by different text report the same rubric version.
- **The tool order.** It was stable in practice: the backend sorted verbs, and
  the intrinsics followed in declared order. No test held it. The tool list is
  the head of every request, and the prompt cache matches it byte for byte.

## Decision

- **`PromptVersion` covers what the turn loop puts in front of the model.** It
  is a hash over named components in a fixed order:
  - `prompt`: the base prompt for the framing dial.
  - `tools/<name>`: each intrinsic tool definition, rendered with no action
    catalog.
  - `tools/request_action+catalog` and `tools/<verb>`: the tool text that only
    renders with an action catalog or a capability verb. These are rendered from
    example inputs (`exampleCatalog`, `exampleVerb`), so the hash covers the
    code's words and never an install's catalog or verbs. The catalog's content
    is attributed apart, as `action_catalog_hash`.
  - `engine-text/<id>`: each engine-text template.

  The version is the hash of each component's name and digest. Each component's
  digest is pinned on its own (`TestPrompt_ComponentsFrozen`), so when the
  version moves, the failing test names the part that moved. The `prompt`
  component's digest is the old version, a870ddbf.
- **Engine text is declared once, with provenance.** `agent/engine_text.go`
  holds every fixed sentence as a `modelText`: an id, an origin, its revisions
  and the text, which is the fmt template where the sentence carries values.
  `state.go`, `reconcile.go`, `context.go`, `bounded.go`, `session.go` and
  `prompt.go` render from these entries. The hash covers the templates, never a
  rendered block, so the investigation's identifiers stay out of the version.
  Labels inside the reasoning-state block ("predictions:", "untested",
  "discriminates vs") are words the model reads and are entries too, and so is
  the structure around the values: each line's format, the `H<n>` and `P<n>`
  names, the tally's status words and their order, the list and footer
  separators, the clip marker, and the JSON keys a paged result is framed in
  (derived from the struct, so a renamed field moves the version).
- **`TestEngineText_RenderersUseTheLedger`** renders each renderer through its
  branches and cuts out the values it substituted. What remains must be a
  sequence of the templates' literal parts. A literal added inline is reported
  with the text no entry holds.
- **Tools carry ledger fields.** Each intrinsic `toolSpec` has `Origin` and
  `Revised`, in the prompt ledger's pointer form. A tool has no `Why`: its
  description states its purpose. The rules hiding in descriptions are now
  visible as such. Moving them into the prompt proper is a later prune that
  costs a baseline rerun, as adr/0057 left the base-prompt prune to #60.
- **Side instructions have their own version.** `SideInstructionVersion` hashes
  the three side instructions, with their own ledger entries and golden, and the
  eval stamps it on every run as `side_instruction_version`. No assertion grades
  the summary or the SOP draft. A change to their instructions re-attributes
  those products without asking for a baseline rerun, and `PromptVersion` moves
  only when graded behaviour might.
- **`TestModelTextLedger_Complete`** refuses an engine text or side instruction
  without an id or a valid origin, and an intrinsic tool without a valid origin.
- **`TestPrompt_NoScenarioVocabulary` covers everything hashed.** That is the
  catalog-shaped and verb-shaped tool text, the engine text and the side
  instructions, as well as the prompt and the tool descriptions.
- **`JudgeRubricVersion` is a hash.** It is the first 12 hex characters of a
  hash over the judge's instruction, the three frames of its question, and each
  rubric in `judgeRubrics`. That list is the only place a rubric is found: the
  judged graders name their assertion and `consultJudge` reads its rubric from
  the list, so a rubric cannot be judged and left out of the version.
  `TestJudgeRubric_Frozen` pins the version and `TestJudgeText_ComponentsFrozen`
  each part.
- **The reader has `ReaderVersion`.** The reader (adr/0069) decides the
  polarity of the scripted analyst's answer to a manual test, and the agent is
  shown that result on the next turn, so its instruction and question shape
  graded behaviour. Neither the prompt version nor the judge's covers them. A
  run in which the reader read an answer records `reader_version` and
  `reader_model`, and its spend apart from the agent's and the judge's.
- **The run records the tool set it offered.** `PromptVersion` hashes a
  synthetic verb and catalog, so an install's own verbs, their descriptions,
  schemas and availability are outside it, and `action_catalog_hash` covers
  only the action types. `Session.ToolSetHash` hashes the tool list as each
  request sends it and the verbs the prompt names degraded, and the run stamps
  it as `tool_set_hash` (`mixed` if the trials were offered different sets).
- **The session orders its own tools.** `buildTools` sorts the available verbs
  by name and appends the intrinsics in declared order, whatever order the
  backend served. A test feeds every order of the same capabilities and requires
  identical bytes.

## What is not covered

- **Error text.** This covers results the agent returns for a tool mistake
  (`unknown tool …`, `bad <tool> input: …`, the `read_result` range errors) and
  errors relayed from the backend. These report a refused call and carry its
  values. They are not standing instructions.
- **Verb descriptions and the action catalog in `PromptVersion`.** These are
  the tenant's words. They vary per install and are attributed by
  `tool_set_hash` and the catalog hash.

## Alternatives rejected

- **Hash the rendered blocks.** They carry the investigation id, hypothesis ids
  and action ids. The version would move every run and say nothing, which is
  the reason adr/0057 left the blocks out. Hashing the templates keeps what is
  fixed and drops what is not.
- **Fold the side instructions into `PromptVersion`.** A reworded summary
  instruction would then ask for a baseline rerun of behaviour it cannot affect.
- **Hash the tools under an install's catalog.** The version would differ
  between installs on the same code. The example inputs cover the code's words
  around the catalog. The catalog itself is the catalog hash's job.
- **Keep `"v1"` and bump it by hand.** That is how A4 landed unversioned.
- **Re-stamp the baselines with the new version.** It would claim that runs
  accepted at a8da184 saw `read_result`, the reworded `list_actions` and the
  paging and condensing notes. They did not.

## Consequences

- No model-facing byte changes in this PR. The engine text renders identically:
  the blocks, footers and notes were captured before the move and compared after
  it. The tool definitions match `intrinsic_tools.golden.json`, and the base
  prompt's digest is a870ddbf as before.
- `PromptVersion` moves once, because the hash reads more inputs: a870ddbf to
  60c97621 under `leading_null`, and 5de9f5af to bc5bd82c under `leading_only`.
  The regression rule compares results, not versions. The run prints a line for
  text version, reader model or tool set it does not share with the baseline
  (`VersionSkew`), and the summary carries them.
- The four committed baselines keep `prompt_version` a870ddbf. That is true of
  them: they were accepted before the tool and engine-text changes above, under
  a version that did not cover them. The next prompt change (#236) reruns them
  under the full version. Until then the support claim stays with these
  baselines: design/10 §4.4 states the interval, and each run prints the
  versions it does not share with them.
- The baselines keep `judge_rubric: "v1"`. The judge text is unchanged since
  they were graded, and its hash is caa20d15db77. New runs report the hash.
- The example fixtures' own strings are inputs to the version: editing one
  moves it although no model is shown them. The component order is hashed too.
  `TestPrompt_Frozen` says which happened: only the two components the fixtures
  render moved, or the version moved and no component did.
- The `Revised` entries were reconstructed from history. For each tool or
  sentence they list the commits whose diff changed one of its phrases.
- Every change to model-facing text is now a deliberate edit to the words, the
  entry's `Revised`, and the golden of the component that moved. Under
  `PromptVersion` it is followed by the baseline reruns.
