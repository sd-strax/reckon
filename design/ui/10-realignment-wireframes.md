# 10 — Realignment wireframes: the conversation in the centre, the case state beside it

Status: proposal for review (issue #433). This sheet is not yet in force. Until a decision
record accepts it, `08-investigation-layout.md` describes the investigation document, and
where the two disagree, `08` stands.

The wireframes show one change of emphasis. The conversation takes the centre and the width,
and nothing takes its place. One panel sits on the right. It says where the case stands, and
everything else opens in it: an inspector, the timeline, the reasoning, the report. The lenses
of `08 §8.4` stop being tabs. Their content stays, one step away. Nothing here changes the
engine, the record, or anything the agent reads.

## How to read the diagrams

- Each diagram is 100 columns wide and stands for an editor area about 1300px wide. One column
  is about 13px. Real text at 13px fits roughly 1.8 times as many characters as a diagram
  column shows, so a line that wraps here may not wrap on screen. The static HTML of step 2
  checks real content at real widths.
- Region widths are to scale: the case-state panel is 33 of 100 columns, about 400px.
- The content is the reference scenario (`fixtures/lateral-movement-via-rdp/`, case
  `INC0010042`). Event sentences follow the event view's wording. The account, hosts,
  addresses, process names and the detection title come from the fixture. Clock times after
  14:33, the questions, the answers, the check wording, the approval window and the
  "could not check" item are illustrative.
- `[ Button ]` is a control. `( Conclude )` in parentheses is a disabled control. `▸` is a fold
  that is closed. `▾` is a fold that is open. `◆` needs a person. `○` is not checked yet.
  `✓` was checked. `✕` could not be checked. `◇` waits on someone outside.
- The diagrams follow one case through an afternoon. Screen a has one check done. Screens b
  to d come after a second check. Screen e is later still, after an action was approved and
  a message was sent.

## Words on screen

The screen uses these words. The stored record keeps the engine's words, and every inspector's
Raw tab and the Thread show them unchanged.

| On the record | On screen |
|---|---|
| The hypothesis that leads: the supported one, otherwise the one proposed first | Leading explanation |
| A rival hypothesis | Other explanations |
| Hypothesis, as a noun | Explanation |
| Prediction | Check |
| `CONFIRMED` / `DISCONFIRMED` | held / did not hold |
| `UNTESTED`, tested by a tool | not checked yet |
| `UNTESTED`, tested by a person | needs a person |
| `INCONCLUSIVE` | checked, no clear answer |
| A verb with no source to answer it | could not check |
| A prediction that discriminates | tells the explanations apart |
| `P3`, `H2` | Not shown. The check or the explanation is named by its own words. The short codes stay on hover, in the full cards and in the Thread. |
| OCSF category (Authentication, Process, Network) | Sign-in, Process, Network |
| Null observation | Nothing found, with the search and its window |

## a. Mid-investigation

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       3 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────────────┬─────────────────────────────────┤
│ [Conversation] Thread                                          │ VERDICT                         │
│                                                                │ None recorded yet      Report › │
│                                                    You · 14:52 │ Active · opened 14:41           │
│                   Is this the backup job, or someone using the │                                 │
│                                               account by hand? │ OPEN ITEMS · 3                  │
│                                                                │ ◆ Did anyone on the backup team │
│ reckon · 14:53                                                 │   work by hand at 14:32?        │
│   This does not look like the backup job. svc_backup normally  │   needs a person                │
│   signs in over the network from BKP-01 at 02:00. This was a   │                 [Record answer] │
│   desktop session from 10.0.4.20 in the afternoon, and         │ ○ What is 10.0.4.20, and who    │
│   PowerShell started 23 seconds later with an encoded command. │   was at it?                    │
│                                                                │   not checked yet       [Check] │
│   Evidence · 4 rows                                            │ ○ Where did WIN-FILE01 connect  │
│  02:00 ×7  Sign-in svc_backup network logon to WIN-FILE01 from │   after PowerShell started?     │
│                    BKP-01 · 7 nights, 13 to 19 Apr · expected  │   not checked yet       [Check] │
│  14:32:47  Sign-in CONTOSO\svc_backup RDP logon to WIN-FILE01  │ ✕ VPN records for 10.0.4.20     │
│                    from 10.0.4.20                              │   could not check: no VPN       │
│  14:33:10  Process CONTOSO\svc_backup ran powershell.exe on    │   source is connected           │
│                    WIN-FILE01, child of mstsc-hijack.exe       │                                 │
│  14:33:10  Alert   Suspicious lateral movement via RDP         │ PENDING APPROVALS · 0           │
│                    followed by encoded PowerShell              │                                 │
│                                                                │ ▸ WHY · 1 of 4 checks done      │
│   Not checked yet: what 10.0.4.20 is, and where the server     │   Someone used svc_backup by    │
│   connected after PowerShell started.                          │   hand. One other explanation   │
│   Needs a person: the backup team, to say whether anyone       │   is still open.                │
│   worked by hand at 14:32.                                     │                     Reasoning › │
│                                                                │                                 │
│   → Noted 2 explanations and 4 checks · 1 check done           │ WHAT HAPPENED                   │
│   ▸ Activity · 4 tool calls, 7 records                         │ 4 events             Timeline › │
│                                                                │                                 │
│ ────────────────────────────────────────────────────────────── │ KEY ENTITIES                    │
│ Ask, pivot, or tell reckon what to do…                [ Send ] │ svc_backup    account · subject │
│                                                                │ WIN-FILE01   host · file server │
│                                                                │ 10.0.4.20           IP · source │
│                                                                │                                 │
│                                                                │ PINNED EVIDENCE · 2             │
│                                                                │ · Desktop sign-in outside the   │
│                                                                │   backup pattern                │
│                                                                │ · Encoded PowerShell 23s after  │
│                                                                │   the sign-in                   │
└────────────────────────────────────────────────────────────────┴─────────────────────────────────┘
```

**Principles shown.** 1: the conversation has the centre and two thirds of the width. 2: no
short codes and no record type names anywhere. 3: the header has no stage strip. It carries a
count of what is open. 8: each evidence row is one click from its record, and "could not
check" is on the panel without opening anything. 9: event sentences get the wide region, and
the state summary gets the narrow one.

**Collapsed by default.** The basis of the verdict (**Why**) shows one line. The turn's
**Activity** (tool calls and records written) is folded. The Timeline, Reasoning and Report
views are closed.

**What a click opens.**

- Header title: rename, as today. **3 open**: scrolls the panel to Open items. The number is
  the open items plus the pending approvals. "Could not check" is not counted. **Verdict…**:
  the verdict dialog with its checklist, available now. **( Conclude )**: disabled until a
  verdict is recorded, because a conclusion uses the verdict on record. Its tooltip says so.
- **Conversation / Thread**: Thread shows the same turns as the full audit record, with every
  act, sequence and tool call.
- **Timeline ›**, **Reasoning ›**, **Report ›** on the panel: each opens that view in the
  panel, in place of the case state, with **‹ Case state** to go back (screen e). Each link
  sits on the section it is the longer form of. **Timeline** is the events and the action
  history on one time axis. **Reasoning** is the full explanation cards and the matrix, with
  the toggle between them. **Report** is the "what happened" narrative with Draft and Edit,
  the verdict and its basis as they will read in the export, what is still open, the
  procedures cited, and the handoff note, the export and the procedure draft.
- An evidence row: the evidence inspector in the right panel (screen d).
- An account, host or address inside a sentence: the entity inspector.
- **→ Noted 2 explanations and 4 checks**: opens **Why** on the panel with the working shown
  (screen b).
- **Activity**: unfolds the tool calls. Each one opens its capability call inspector.
- An open item: the check's inspector. **[Record answer]** opens the form where a person's
  statement is recorded. **[Check]** asks reckon to run that one check.
- The "could not check" item: what was asked and which kind of source would answer it, with
  **Set up…** when an installed source could.
- **Why**: unfolds in place (screen b).
- A key entity: its entity inspector. A pinned item: its pin inspector.

## b. The basis of the verdict, with the working shown

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       2 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────────────┬─────────────────────────────────┤
│ [Conversation] Thread                                          │ VERDICT                         │
│                                                                │ None recorded yet      Report › │
│                                                    You · 14:58 │ Active · opened 14:41           │
│                   Check where the server went after PowerShell │                                 │
│                                                       started. │ OPEN ITEMS · 2                  │
│                                                                │ ◆ Did anyone on the backup team │
│ reckon · 14:59                                                 │   work by hand at 14:32?        │
│   WIN-FILE01 connected to 185.220.101.5 on port 443 fifteen    │   needs a person                │
│   seconds after PowerShell started. In the 7 days before, this │                 [Record answer] │
│   server never contacted that address.                         │ ○ What is 10.0.4.20, and who    │
│                                                                │   was at it?                    │
│   Evidence · 2 rows                                            │   not checked yet       [Check] │
│  14:33:25  Network WIN-FILE01 connected to 185.220.101.5 port  │ ✕ VPN records for 10.0.4.20     │
│                    443 (exfil.example.net) over tcp            │   could not check: no VPN       │
│  7 days    Nothing No connections from WIN-FILE01 to           │   source is connected           │
│                    185.220.101.5 in the 7 days before ·        │                                 │
│                    searched in full, 0 events                  │ PENDING APPROVALS · 0           │
│                                                                │                                 │
│   ✓ Checked: WIN-FILE01 contacted an untrusted host after      │ ▾ WHY · 2 of 4 checks done      │
│   14:33. It held.                                              │ Leading explanation             │
│   Two checks are left. Both would tell the two explanations    │   Someone used the svc_backup   │
│   apart.                                                       │   password by hand to open a    │
│                                                                │   desktop session on WIN-FILE01 │
│   ▸ Activity · 2 tool calls, 2 records                         │   and run PowerShell.           │
│                                                                │ Other explanations              │
│ ────────────────────────────────────────────────────────────── │   Authorised work by the backup │
│ Ask, pivot, or tell reckon what to do…                [ Send ] │   team. Not ruled out yet.      │
│                                                                │ ▾ Show the working              │
│                                                                │ ✓ The sign-in is far outside    │
│                                                                │   the account's normal pattern  │
│                                                                │   held · 9 evidence · tells the │
│                                                                │   two apart                     │
│                                                                │ ✓ WIN-FILE01 contacted an       │
│                                                                │   untrusted host after 14:33    │
│                                                                │   held · 5 evidence · tells the │
│                                                                │   two apart                     │
│                                                                │ ○ 10.0.4.20 is an ordinary user │
│                                                                │   workstation                   │
│                                                                │   not checked yet · tells the   │
│                                                                │   two apart                     │
│                                                                │ ◆ Backup team: nobody worked by │
│                                                                │   hand at 14:32                 │
│                                                                │   needs a person · tells the    │
│                                                                │   two apart                     │
│                                                                │                     Reasoning › │
│                                                                │                                 │
│                                                                │ WHAT HAPPENED                   │
│                                                                │ 5 events             Timeline › │
│                                                                │                                 │
│                                                                │ ▸ KEY ENTITIES · 4              │
│                                                                │                                 │
│                                                                │ ▸ PINNED EVIDENCE · 2           │
└────────────────────────────────────────────────────────────────┴─────────────────────────────────┘
```

**Principles shown.** 7: the whole competing set is one fold away, and the Reasoning view with
the matrix and the full cards is one more step. 10: this case has a second explanation that is
still open, so the panel shows it and the checks that would tell the two apart. A case with one
explanation and every check done shows the one-line basis and nothing else. 8: the "Nothing"
row is a cited absence. It names the search and its window, and it opens like any other
evidence. 6: the conversation tells what was just found. The panel holds the current state of
every check, once.

**Collapsed by default.** **Why** is closed until the analyst opens it, and **Show the
working** is closed inside it. Both remember their last state for the investigation. Key
entities and pinned evidence fold here only to keep the diagram short.

**What a click opens.**

- The text under **Leading explanation** or **Other explanations**: the explanation's
  inspector, with its record and its wording history. Editing the wording is offered there.
- A check row: the check's inspector, with its evidence and what it decides against.
  **9 evidence** opens the list of evidence for that check.
- **Reasoning ›**: the Reasoning view in the panel, with the full cards and the matrix.
- The "Nothing" evidence row: the absence inspector (the search, its window, the sources that
  answered).
- **✓ Checked: … It held.**: the check's inspector.

## c. An action waiting for approval before any verdict

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       3 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────────────┬─────────────────────────────────┤
│ [Conversation] Thread                                          │ VERDICT                         │
│                                                                │ None recorded yet      Report › │
│                                                    You · 15:04 │ Active · opened 14:41           │
│                     Isolate WIN-FILE01 now. Don't wait for the │                                 │
│                                                   backup team. │ OPEN ITEMS · 2                  │
│                                                                │ ◆ Did anyone on the backup team │
│ reckon · 15:04                                                 │   work by hand at 14:32?        │
│   Requested. It needs your approval. The explanation is not    │   needs a person                │
│   settled: authorised admin work stays open until the backup   │                 [Record answer] │
│   team answers.                                                │ ○ What is 10.0.4.20, and who    │
│                                                                │   was at it?                    │
│   ┌──────────────────────────────────────────────────────────┐ │   not checked yet       [Check] │
│   │ CONTAINMENT · Isolate host                    14:21 left │ │ ✕ VPN records for 10.0.4.20     │
│   │ WIN-FILE01 · finance file server                         │ │   could not check: no VPN       │
│   │                                                          │ │   source is connected           │
│   │ Will do: cut WIN-FILE01 off the network. Open sessions   │ │                                 │
│   │ on it drop.                                              │ │ PENDING APPROVALS · 1           │
│   │ Based on: sign-in 14:32:47 · PowerShell 14:33:10 ·       │ │ ● Isolate host WIN-FILE01       │
│   │ outbound 14:33:25                                        │ │   14:21 left       [Go to card] │
│   │ Can be undone: yes, by releasing the host.               │ │                                 │
│   │ Needs your approval. One host affected.                  │ │ ▸ WHY · 2 of 4 checks done      │
│   │                                                          │ │   Someone used svc_backup by    │
│   │ [ Approve ]  [ Edit… ]  [ Reject… ]            Details ▸ │ │   hand. One other explanation   │
│   └──────────────────────────────────────────────────────────┘ │   is still open.                │
│                                                                │                     Reasoning › │
│   ▸ Activity · 1 tool call, 1 record                           │                                 │
│                                                                │ WHAT HAPPENED                   │
│ ────────────────────────────────────────────────────────────── │ 5 events · 1 action  Timeline › │
│ Ask, pivot, or tell reckon what to do…                [ Send ] │                                 │
│                                                                │ KEY ENTITIES                    │
│                                                                │ svc_backup    account · subject │
│                                                                │ WIN-FILE01   host · file server │
│                                                                │ 10.0.4.20           IP · source │
│                                                                │ 185.220.101.5     IP · outbound │
│                                                                │                                 │
│                                                                │ PINNED EVIDENCE · 2             │
│                                                                │ · Desktop sign-in outside the   │
│                                                                │   backup pattern                │
│                                                                │ · Encoded PowerShell 23s after  │
│                                                                │   the sign-in                   │
└────────────────────────────────────────────────────────────────┴─────────────────────────────────┘
```

**Principles shown.** 3: containment is requested and can be approved while the verdict is
still "none recorded yet". Nothing on screen says the case is in the wrong stage. 4: the
approval is one of the few things that asks for a click. 5: the gate is on the action. 9: the
action card sits in the conversation at full width, where it was asked for. 6: the card is the
one place to decide. The panel carries one line that leads to it.

**Collapsed by default.** The card's **Details** (the evidence in full, the policy fact, the
targets, the history) are folded. The turn's Activity is folded.

**What a click opens.**

- **Approve**: approves, with the typed challenge first when the action's tier needs one.
  **Edit…**: the parameters before approval. **Reject…**: asks for a reason.
- **Details ▸**: unfolds in place. The card's title opens the action inspector.
- **sign-in 14:32:47**, **PowerShell 14:33:10**, **outbound 14:33:25**: each opens its evidence
  inspector.
- **[Go to card]** on the panel: scrolls the conversation to the card.
- **3 open** in the header counts the approval with the two open items. The panel keeps the
  approval in its own section.

## d. An inspector in the right panel

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       2 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├───────────────────────────────────────────────────────────┬──────────────────────────────────────┤
│ [Conversation] Thread                                     │ ‹ Case state                       ✕ │
│                                                           │                                      │
│ reckon · 14:53                                            │ EVIDENCE · SIGN-IN                   │
│   This does not look like the backup job. svc_backup      │ CONTOSO\svc_backup RDP logon to      │
│   normally signs in over the network from BKP-01 at       │ WIN-FILE01 from 10.0.4.20            │
│   02:00. This was a desktop session from 10.0.4.20 in the │ 20 Apr 2026 14:32:47 UTC             │
│   afternoon, and PowerShell started 23 seconds later with │                                      │
│   an encoded command.                                     │ In this case                         │
│                                                           │ · Check: the sign-in is far outside  │
│   Evidence · 4 rows                                       │   the account's normal pattern       │
│  02:00 ×7  Sign-in svc_backup network logon to WIN-FILE01 │   (held)                             │
│                    from BKP-01 · 7 nights, 13 to 19 Apr · │ · Pinned: desktop sign-in outside    │
│                    expected                               │   the backup pattern                 │
│ ▶14:32:47  Sign-in CONTOSO\svc_backup RDP logon to        │                                      │
│                    WIN-FILE01 from 10.0.4.20              │ [Details]  Provenance  Raw           │
│  14:33:10  Process CONTOSO\svc_backup ran powershell.exe  │                                      │
│                    on WIN-FILE01, child of                │ Account       CONTOSO\svc_backup     │
│                    mstsc-hijack.exe                       │ Sign-in type  Remote desktop (10)    │
│  14:33:10  Alert   Suspicious lateral movement via RDP    │ From          10.0.4.20              │
│                    followed by encoded PowerShell         │ To            WIN-FILE01             │
│                                                           │ Result        Success                │
│   ▸ Activity · 4 tool calls, 7 records                    │                                      │
│                                                           │ Around it on WIN-FILE01              │
│ ───────────────────────────────────────────────────────── │ 14:32:47  this sign-in               │
│ Ask, pivot, or tell reckon what to do…           [ Send ] │ 14:33:10  powershell.exe started     │
│                                                           │ 14:33:25  outbound to 185.220.101.5  │
│                                                           │                                      │
│                                                           │ Retrieved by enumerate_logons        │
│                                                           │ Answered in full                     │
└───────────────────────────────────────────────────────────┴──────────────────────────────────────┘
```

**Principles shown.** 8: the row's record, the call that produced it and its raw form are one
click from the sentence that made the claim. 7: the inspector keeps the frame and the tabs of
`08 §8.6`. 6: the panel shows either the case state or one inspector.

The inspector is a detail view of one thing, opened when it is wanted. One is open at a time,
so the pin control of `08 §8.6` is not here. The first record stays readable as its row in
the conversation while the inspector shows a second.

**Collapsed by default.** The Provenance and Raw tabs. The conversation keeps its place, and
the row that was clicked is marked.

**What a click opens.**

- **‹ Case state**: closes the inspector and returns the panel to the case state. `Esc` does
  the same. When one inspector was reached from another, the back control names that one, and
  Case state is the last step back.
- **✕**: closes to the case state.
- A line under **In this case**: the check or the pin it names.
- **Provenance**: the verb, the adapter, the query sent, coverage, ingest time and content
  hash. **Raw**: the stored record, with the engine's own field and type names.
- A row under **Around it**: that event's inspector, with back to this one.
- **Retrieved by enumerate_logons**: the capability call inspector.
- The panel is wider while an inspector is open: 38 of 100 columns, about 495px, against 33
  for the case state. The conversation gives up the difference.

## e. The timeline, with the action history in it

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       3 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────┬─────────────────────────────────────────┤
│ [Conversation] Thread                                  │ ‹ Case state                          ✕ │
│                                                        │                                         │
│                                            You · 15:10 │ TIMELINE                                │
│                             What do we still not know? │ [x] Telemetry [x] Actions [ ] Queries   │
│                                                        │ All · Sign-in · Process · Network ·     │
│ reckon · 15:10                                         │ Containment · Message                   │
│   Two things.                                          │ 5 events · 2 actions          [x] Group │
│   What 10.0.4.20 is and who was at it. Not checked     │                                         │
│   yet.                                                 │ 13 to 19 Apr · 02:00  Sign-in ×7        │
│   Whether the backup team worked by hand at 14:32.     │   svc_backup network logon to           │
│   That needs a person.                                 │   WIN-FILE01 from BKP-01 · expected     │
│                                                        │ 20 Apr 14:32:47  Sign-in                │
│   WIN-FILE01 is isolated. The platform team was asked  │   CONTOSO\svc_backup RDP logon to       │
│   at 15:08 to rotate the svc_backup password and has   │   WIN-FILE01 from 10.0.4.20             │
│   not answered.                                        │ 14:33:10  Process                       │
│                                                        │   CONTOSO\svc_backup ran powershell.exe │
│   Could not check: VPN records for 10.0.4.20. No VPN   │   on WIN-FILE01, child of               │
│   source is connected.                                 │   mstsc-hijack.exe                      │
│                                                        │ 14:33:10  Alert                         │
│   ▸ Activity · no tool calls                           │   Suspicious lateral movement via RDP   │
│                                                        │   followed by encoded PowerShell        │
│ ────────────────────────────────────────────────────── │ 14:33:25  Network                       │
│ Ask, pivot, or tell reckon what to do…        [ Send ] │   WIN-FILE01 connected to 185.220.101.5 │
│                                                        │   port 443 (exfil.example.net) over tcp │
│                                                        │ ── investigation opened 14:41 ───────── │
│                                                        │ 15:04:10  Action                        │
│                                                        │   Requested: isolate host WIN-FILE01    │
│                                                        │ 15:06:32  Action                        │
│                                                        │   Approved by you, run, succeeded:      │
│                                                        │   isolate host WIN-FILE01 · 3 steps in  │
│                                                        │   one minute                            │
│                                                        │ 15:08:05  Action                        │
│                                                        │   Sent: message to the platform team,   │
│                                                        │   asking them to rotate the svc_backup  │
│                                                        │   password                              │
└────────────────────────────────────────────────────────┴─────────────────────────────────────────┘
```

**Principles shown.** 1: the conversation keeps its place and its composer while the timeline
is open. The analyst asks about a row and reads the answer without leaving it. 6: what
happened and what was done about it are on one time axis. The action history has no list of
its own. 7: the timeline is the Story lens of `08 §8.4` with nothing taken out, and the
action history of `adr/0361` joins it. 9: in the panel a row puts the time and the kind on
one line and the sentence below, so the sentence gets the panel's full width.

**How the rows work.**

- A telemetry row sits at the time the event happened. An action row sits at the time the
  step happened. The line "investigation opened" divides what happened from what was done.
- Each step of an action is its own row at its own time: requested, edited, approved (or
  rejected, or expired), run, the result, reversed. Steps of one action that fall in the
  same minute share a row that names each step. Here the approval, the run and the result
  came within a minute, so they are one row. The request came two minutes earlier, so it is
  its own row.
- **Queries** is off by default. Turned on, it adds one row for each query at the time it
  ran, with what was asked and what came back. A query that found nothing and a query that
  could not run each get a row.
- Grouping of repeats and the merge of an event that several queries returned apply to
  telemetry rows only.
- Actions waiting for approval stay on the panel, with their buttons. The timeline shows the
  request as a row.

**Where it opens.** In the right panel, in place of the case state. The same holds for
Reasoning and Report, at every window width. The panel is 41 of 100 columns here, about
530px, and can be dragged wider until the conversation reaches its smallest width. While a
view is open, the header still shows the verdict status and the open count, and a new
approval still arrives as a card in the conversation.

**Collapsed by default.** Query rows. The evidence graph and the grouping panel are folded
inside the timeline, as they are today.

**What a click opens.**

- **‹ Case state** or **✕**: closes the timeline. The panel shows the case state again.
- **[x] Telemetry**, **[x] Actions**, **[ ] Queries**: turn a kind of row on or off. The
  next line filters inside the kinds: the telemetry sources and the action groups.
- A telemetry row: the evidence inspector, in the panel, with **‹ Timeline** to go back.
- An action row: the action inspector, with every step, the evidence and Reverse. The other
  steps of the same action are marked on the timeline.
- A query row: the capability call inspector.
- The folded row (**×7**): its seven members in place.
- **[x] Group**: turns the fold of repeats on or off.

## f. The same case in a narrow window

```
┌────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by      │
│ svc_backup                                             │
│ INC0010042 · active           [ Verdict… ] ( Conclude )│
├────────────────────────────────────────────────────────┤
│ No verdict · 3 open                             Case ▾ │
│ ────────────────────────────────────────────────────── │
│ [Conversation] Thread                                  │
│                                                        │
│                                            You · 14:52 │
│           Is this the backup job, or someone using the │
│                                       account by hand? │
│                                                        │
│ reckon · 14:53                                         │
│ This does not look like the backup job. svc_backup     │
│ normally signs in over the network from BKP-01 at      │
│ 02:00. This was a desktop session from 10.0.4.20 in    │
│ the afternoon, and PowerShell started 23 seconds later │
│ with an encoded command.                               │
│                                                        │
│ Evidence · 4 rows                                      │
│ 13–19 Apr  Sign-in ×7                                  │
│   svc_backup network logon to WIN-FILE01 from BKP-01,  │
│   nightly at 02:00 · expected                          │
│ 14:32:47  Sign-in                                      │
│   CONTOSO\svc_backup RDP logon to WIN-FILE01 from      │
│   10.0.4.20                                            │
│ 14:33:10  Process                                      │
│   CONTOSO\svc_backup ran powershell.exe on WIN-FILE01, │
│   child of mstsc-hijack.exe                            │
│ 14:33:10  Alert                                        │
│   Suspicious lateral movement via RDP followed by      │
│   encoded PowerShell                                   │
│                                                        │
│ Needs a person: the backup team, to say whether anyone │
│ worked by hand at 14:32.                               │
│                                                        │
│ ▸ Activity · 4 tool calls, 7 records                   │
│                                                        │
│ ────────────────────────────────────────────────────── │
│ Ask, pivot, or tell reckon…                   [ Send ] │
└────────────────────────────────────────────────────────┘
```

The diagram is 58 columns, about 750px.

**Principles shown.** 1: when there is room for one column, the conversation keeps it. 9: the
evidence rows put the time and kind on one line and the sentence below, so the sentence gets
the full width. 8: the case line under the header keeps the verdict and the open count in
view.

**Collapsed by default.** The whole panel. It becomes a drawer. Timeline, Reasoning and Report
open inside it, as they do in the panel.

**What a click opens.**

- **Case ▾**, or the case line itself: the case-state panel as a drawer over the conversation,
  with the same sections in the same order. An inspector or a view opens in that drawer, with
  back to the case state. The drawer covers the conversation while it is open and closes back
  to it. This is the one width at which the conversation is covered.
- Everything else opens what it opens on screen a.

## Settled in review

- The header shows one number: the open items plus the pending approvals. "Could not check"
  is not counted. The panel keeps the approvals in their own section.
- One inspector is open at a time. There is no pin control.
- The labels are "Leading explanation" and "Other explanations". The supported explanation
  leads when there is one. Otherwise the one proposed first leads.
- Nothing takes the conversation's place. Timeline, Reasoning and Report open in the right
  panel, from the section each is the longer form of, with a step back to the case state.
  There is no tab strip. Nothing sends the analyst to a view, and no stage switches one.
- The action history is part of the timeline, with each step of an action at its own time.
  Query rows are off by default.
- The narrative, the handoff note, the export and the procedure draft share the Report view.
- Adopting an explanation is not an open item. It stays on the full cards in the Reasoning
  view.
- The sidebar row of an open investigation shows the verdict status and the open count, for
  example "no verdict · 3 open". It no longer shows a stage step. This changes one point of
  `adr/0342`. The rest of that decision stands.
- Work that waits on someone outside shows as one kind of open item, "waiting on others". It
  is fed by the records that exist today. A change to how outside work is tracked would
  change what feeds it and would not change the screen.
