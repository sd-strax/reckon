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

Data: events, times, process ids, the decoded command, the case ids and the procedure text
come from the fixture scenario and `demo/knowledge/`. Tenant labels on the queue
(Northwind, Fabrikam), approval windows, the response-policy outcomes, the dispatch route
for `account.disable`, and the narrative wording are illustrative. Bracketed values in the
procedure inspector (`[FORMAT]`, `[SOURCE NAME OR URL]`) stand for whatever the import
recorded.

Colors, type and spacing are the tokens of `01-design-system.md` (dark theme). Fonts load
from Google Fonts for viewing convenience only; the workbench bundles them (`06`, Webview
hardening).
