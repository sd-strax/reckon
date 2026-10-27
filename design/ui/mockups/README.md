# mockups — reference screens for `08-investigation-layout.md`

Static HTML renderings of the investigation view for the reference scenario
(`fixtures/lateral-movement-via-rdp/`, case `INC0010042`). Open any file in a browser.
They are illustrations of the sheets, not specifications: where a mockup and a sheet
disagree, the sheet wins, and where a sheet and `00-backend-binding.md` disagree, the
binding wins.

| File | Shows | Sheet |
|---|---|---|
| `01-summary.html` | Summary lens: where it stands, narrative, key entities, still to do | `ui/08 §8.4` |
| `02-story.html` | Story lens: attack path and event-time timeline | `ui/08 §8.4` |
| `03-reasoning.html` | Reasoning lens at `VERDICT_REACHED`, with the companion column | `ui/08 §8.4`, `§8.5` |
| `04-response.html` | Response lens: requested actions, owner action item, notification | `ui/08 §8.4` |
| `05-prediction-rows.html` | A prediction row before and after; the matrix view | `ui/08 §8.4` |
| `06-inspector.html` | Inspector: evidence, entity (reached from it), procedure excerpt | `ui/08 §8.6` |
| `07-inspectors-evidence.html` | Inspector: absence, observed-data bundle, detection finding | `ui/08 §8.6.1` |
| `08-inspectors-reasoning.html` | Inspector: attestation, prediction, hypothesis | `ui/08 §8.6.1` |
| `09-inspectors-action.html` | Inspector: action, notification thread, owner action item | `ui/08 §8.6.1` |
| `10-inspectors-provenance.html` | Inspector: turn, capability call, similar past case | `ui/08 §8.6.1` |
| `11-inspectors-identity.html` | Inspector: person, alias | `ui/08 §8.6.1` |
| `12-knowledge-procedures.html` | Knowledge view: procedure list and detail | `ui/09 §9.3` |
| `13-knowledge-search.html` | Knowledge view: Search and Ask results; the past-cases list | `ui/09 §9.2`, `§9.4` |
| `14-knowledge-in-investigation.html` | Import panel; the Knowledge section in an investigation; a nudge; the conclude step | `ui/09 §9.5`–`§9.7` |

Data: events, times, process ids, the decoded command, the case ids and the procedure text
come from the fixture scenario and `demo/knowledge/`. Tenant labels on the queue
(Northwind, Fabrikam), approval windows, the response-policy outcomes, the dispatch route
for `account.disable`, and the narrative wording are illustrative. Bracketed values in the
procedure inspector (`[FORMAT]`, `[SOURCE NAME OR URL]`) stand for whatever the import
recorded.

In `07`–`11`, the detection `det-lm-001`, the `enumerate_logons` call and its parameters,
the svc_backup baseline and the prior case FINANCE-07 → DC-02 come from the fixtures and
`demo/knowledge/`. The absence result, the notification reply, the owner item's status, the
alias pair, the action and attestation timestamps, and the roles shown on the person are
illustrative; bracketed values (`[POLICY NAME]`, `[CHANNEL]`, `[PRINCIPAL ID]`,
`[DURATION]`, `[TIME]`) stand for what the engine would supply.

Colors, type and spacing are the tokens of `01-design-system.md` (dark theme). Fonts load
from Google Fonts for viewing convenience only; the workbench bundles them (`06`, Webview
hardening).

In `12`–`14`, the three procedures, their text, tags and recommendations, and the two past
cases come from `demo/knowledge/`. The fourth procedure (added from INC0010042), the
imported file names and outcomes, the relevance bands, the "cited in" counts and the Ask
answer's wording are illustrative.
