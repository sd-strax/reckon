# 0164 — The demo answers the questions its scenario invites, and a failed action says what it means for the target

Issue: #164, #149 · PR: #TBD · Status: accepted

## Context

The RDP demo invites scoping questions it could not answer honestly:

- **Reads (#164).** The two incident-day logons replayed for any
  `enumerate_logons` query, whatever account or window was asked for, and the
  answer still reported COMPLETE. "Did svc_backup log on anywhere after
  14:33Z?" returned both 14:32 logons, one of them jdoe's, so a filtered
  question could never come back empty and the citeable absence
  (`x-null-observation`, adr/0061) could not be reached.
- **Writes (#149).** The scenario surfaces jdoe as a lead, but only
  svc_backup had scripted disable/enable responses; disabling jdoe failed at
  dispatch and the card showed the workflow engine's wrapper ("activity error
  (type: DoDispatch, scheduledEventID …)") with nothing on what it meant for the
  account.

## Decision

- **A fixture can say "this value, or none asked for".** A null in a matcher's
  list of alternatives matches a param the caller did not pass. The
  incident-day logons match `target.user` on their own account (bare and
  `CONTOSO\` forms) or on no account, so a host-rooted query still sees both
  and an account-rooted query sees only that account's.
- **The incident-day logons are time-scoped**, like the account baseline: a
  window that excludes them answers COMPLETE and empty, which mints the
  null observation. A query with no window still sees them.
- **Both accounts the scenario surfaces can be disabled and re-enabled**
  (`disable-jdoe.action.json`, `enable-jdoe.action.json`). An account the
  scenario never mentions still fails, honestly.
- **A failed dispatch records the activity's own reason**, not the workflow
  engine's wrapper (`ApplicationError.Message()`).
- **The card says what a failure means for the target** before the reason:
  "Did not complete — reckon cannot confirm any change on <targets>", "Partly
  done — … check each", "No answer from the tool — whether … changed is
  unknown". The engine records an unconfirmed effect as unknown, and the
  sentence says exactly that.

## Alternatives rejected

- **Have the capability layer drop or flag returned events outside the
  window it asked for.** Verbs name their windows differently and some
  answer about state, not events in time; the resolver would be interpreting
  parameters it only templates. A real adapter that ignores a filter is a
  defect its contract tests catch, the same way these fixtures now do.
- **Make every account's disable succeed** (a wildcard fixture). The demo
  would then claim to contain accounts it has never heard of.

## Consequences

- A query with a window that misses the incident day (for example "the last
  24 hours" against the demo's clock) now finds no logons, as a real source
  would; the agent's clock rule is what keeps it from asking that. The eval
  baselines are rerun with the next prompt change and will show whether it
  shifts any trial.
- Installed demos pick up the fixtures on the next `reckon demo seed`, or by
  copying them into `<base>/fixtures/` and restarting.
