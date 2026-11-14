# 0342 — The work queue is the editor's sidebar; the investigation document has no queue column

Issue: #342 · PR: #343 · Status: accepted · Supersedes: the work-queue bullet of `0079`, and the
Knowledge entry point of `0107` (the left region's header) · Knowledge entry point superseded by 0344

## Context

The workbench rendered the work queue twice. The editor's sidebar has had an
**Investigations** view and a **Cases** view since the first workbench slice (`13 §7`,
`adr/0030`). `adr/0079` then gave the investigation document its own left column
(`ui/08 §8.3`): investigations needs-me-first with their stage step, the case queue
filtered to cases with no investigation, a **Capabilities** fold, and a **Knowledge** link
in its header (`adr/0107`). Nothing in `0079` said what became of the sidebar views, so
both stayed, under separate rules. The sidebar listed every case in the queue and
grouped investigations by lifecycle. The column hid investigated cases and showed the
stage step. An analyst saw the same investigation and the same case twice on one screen,
labelled differently.

The constraints are the workbench discipline (`13 §3`). reckon owns its own real estate,
prefers commands to menus, and assumes no workspace folder. Nothing the column offered
may be lost.

## Decision

- **The investigation document has no work-queue column.** Its regions are the lens area
  (with the inspector over its right edge) and the companion column. The column, its
  collapse to an icon rail below 1280px, and the document's `queue` message and its
  `queue.get` / `queue.start` requests are gone.
- **The sidebar is the work queue.** The reckon view container holds three views, in
  this order: **Investigations**, **Cases**, **Capabilities**.
  - **Investigations** keeps its lifecycle groups, its needs-me-first order, and its expiry,
    pending and unseen cues. An open investigation's row adds its **stage step**
    (Investigate · Verdict · Contain · Notify · Close) to its description. This is the cue
    the column carried. One pure function derives the step from the investigation summary.
    The step names are defined once, and the sidebar and the document's progress strip
    both read them. The view's title bar carries
    **Knowledge**, which opens the Knowledge view (`adr/0107`), beside **＋** and refresh.
  - **Cases** keeps listing every case in the configured queue. A case under investigation
    keeps its marker and leads to its case page, which offers **Open investigation**
    (`adr/0030`, #158). A case with no investigation gains an inline **Start
    investigation**. This is the column's one-click start, and it runs the same start from
    a case that the case page runs.
  - **Capabilities** is new. It lists one row per verb: available, then degraded, then
    unavailable, each group sorted by verb, with a status icon. The view's description is
    *N/M available*. An unavailable verb that an installed, supportable, disabled adapter
    could serve offers an inline **Set up…** action. That action runs the enablement flow
    natively:
    1. The analyst confirms in a modal bound to their identity.
    2. One input appears for each field of the adapter's schema. A secret field takes a
       secret *reference* through a password input, never the literal (`11 §4.3`).
    3. The flow applies the change.

    The view then reloads. Every open investigation document re-reads its enablement. The
    document's own apply path (the form a tool row's **Set up** opens, where an agent's gap
    is met) and this one share the confirm, the secret capture and the apply. The flow is
    written once. A capability layer that is off, or one with no verbs configured, shows
    one row saying so. Signed-out and unreachable states show the same action rows as the
    other two views.
- **The Knowledge view keeps its own section list** (`ui/09 §9.1`). That list is navigation
  inside the Knowledge document, not a queue; this decision does not touch it.

## Alternatives rejected

- **Keep the column and drop the sidebar views.** The column exists only inside an open
  investigation, so with none open there would be no queue at all. The sidebar is the
  editor's own place for navigation and is there before any document opens.
- **Keep both and make their rules agree.** Two renderings of one queue drift; these
  already had. Aligning the rules twice leaves the duplication on screen.
- **Capabilities as a status-bar item only.** A status-bar item can carry the count. It
  cannot carry the per-verb rows or the set-up action.
- **A sidebar webview to reuse the document's enable form.** That adds a second webview
  host for one form. The document's apply already ends in native input; the sidebar flow
  is that native input from the start.

## Consequences

- The lenses get the column's width, 220–270px. On a narrow editor, the analyst hides
  the queue with the editor's own sidebar toggle rather than a reckon control.
- The stage step is visible without opening an investigation.
- The webview no longer reads the investigation list or the case queue on every load.
- `ui/08 §8.1` and `§8.3`, `ui/09 §9.1` and the road test describe the sidebar. The
  reference screens under `design/ui/mockups/` still draw the column. They stay as the
  record of `0079`'s layout and are not the current one.
