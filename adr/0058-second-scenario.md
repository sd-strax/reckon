# 0058 — The eval corpus has two scenarios of different shape, and the stack is wired for one at a time

Issue: #58 · PR: #63 · Status: accepted

## Context

Every prompt change since the competing-hypotheses work (#6) was motivated by
a failure on `lateral-movement-via-rdp` and graded against it. One scenario
cannot tell a principle from a patch: a sentence that helps it may hurt a case
of another shape, and nothing would show. The prompt ledger (adr/0057) records
which scenarios each rule was validated on; today every entry has one.

Constraints. The fixture adapter replays one directory and matches on rendered
binding params with wildcards, and the shipped bindings default their entity to
the scenario's host so a differently shaped query still resolves (design/03
§9.2). Two scenarios in one tenant config would therefore answer each other's
questions: an alert search would return both detections, a host-less logon
query would return the other scenario's host. The eval's regression rule keys
baselines by `(scenario, model)` (design/10 §4.4), and the demo lifecycle
(`reckon demo seed` / `reset`) assumed the one bundled scenario.

## Decision

- **A second bundled scenario, `impossible-travel-signin`, of a different
  shape.** Identity-rooted: an account's office sign-in followed two hours
  later by a successful sign-in from another country over a legacy mail
  protocol where MFA is not evaluated. No host, no process tree, no network
  connections; the verbs the first scenario leans on are unbound, and host
  isolation is unavailable in the action catalog. The benign alternative has a
  test only a person can run (asking the account owner) beside two the
  fixtures answer: the address's indicator context (OCSF 5021, a hosting/VPN
  exit with credential-stuffing sightings) and a burst of failed sign-ins from
  it before the success. A travel notice is a case search that is COMPLETE
  and empty. Its driver script keeps the first script's turn skeleton and
  assertion placement, so the two reports are column-for-column comparable.
- **The fixture matcher accepts a list of alternatives.** `"outcome":
  ["SUCCESS", "ALL"]` on successful sign-ins and `["FAILURE", "ALL"]` on
  failed ones lets one scenario answer `SUCCESS`, `FAILURE`, and `ALL`
  differently (design/03 §9.1). A discriminating question needs fixtures that
  discriminate; a wildcard would have returned the failures to every query.
- **The stack is wired for one fixture scenario at a time.**
  `reckon.DemoScenarios` maps each bundled scenario to its example-config
  pair. `reckon demo seed --scenario <id>` picks one on a fresh install;
  `reckon demo scenario <id>` swaps the fixtures and the merged config on a
  seeded install with the stack down and the databases untouched, so
  investigations and knowledge survive a swap. `runtime.SeedDemo` takes the
  scenario; every bundled scenario must seed and build under both loaders.
- **The driver declares its fixture scenario and the runner checks it.** A
  driver script carries `fixture_scenario`; before the first trial the runner
  makes one `search_alerts` call and compares the events' `source_tool` to it,
  failing with the swap command on a mismatch rather than grading the wrong
  world. `make eval SCENARIO=<driver>` selects the script.

## Alternatives rejected

- **Both scenarios in one tenant config, one fixture adapter instance each.**
  The read resolver takes the highest-priority healthy binding and the fixture
  adapter's empty result is a legitimate COMPLETE answer (evidence of
  absence), so there is no fall-through to the second instance; and shared
  wildcards would cross-answer anyway.
- **Both scenarios' fixtures in one directory.** The alert search would return
  both detections and the first scenario's baseline would change, which moves
  its accepted baseline for no behavioral reason.
- **A scenario with new verbs (email, DNS).** It would have exercised more
  normalizers, but adding verbs to the catalog is a capability-layer change
  (design/03 §5.1) and the tool surface the agent reads would differ between
  scenarios, confounding the comparison. The second shape is reached with the
  existing verbs.
- **Swapping scenarios through the full reseed.** `demo reset` wipes the
  cluster, including the realm and every investigation, for a change that only
  concerns two files on disk. The swap command does the file half alone.

## Consequences

- A prompt change (#60) is accepted only when both scenarios' runs are clean
  for a model; the baseline files are per `(scenario, model)`, so the second
  scenario starts with no baseline and its first clean run is accepted as one.
- Eval cost per prompt change doubles. That is the price of knowing a rule
  generalizes.
- The prompt guard (adr/0057) now carries the second scenario's identifiers;
  the prompt contains none of them.
- The demo world's documentation names the swap. The road test and the demo
  script remain written against the first scenario.
- The first driver gained `fixture_scenario`, which moves its driver hash; the
  hash is attribution, not a regression key, and the committed baselines are
  unaffected.
