# 10 — Realignment wireframes: the conversation in the centre, the case state beside it

Status: proposal for review (issue #433). This sheet is not yet in force. Until a decision
record accepts it, `08-investigation-layout.md` describes the investigation document, and
where the two disagree, `08` stands.

The wireframes show one change of emphasis. The conversation takes the centre and the width.
One case-state panel sits on the right and says where the case stands. The lenses of `08 §8.4`
stop being tabs. Their content stays, one step away. Nothing here changes the engine, the
record, or anything the agent reads.

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
  `✓` was checked. `✕` could not be checked.
- The diagrams follow one case through an afternoon. Screen a has one check done. Screens b
  to e come after a second check.

## Words on screen

The screen uses these words. The stored record keeps the engine's words, and every inspector's
Raw tab and the Thread show them unchanged.

| On the record | On screen |
|---|---|
| The hypothesis the evidence favours | What I think happened |
| A rival hypothesis | What else it could be |
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
│ [Conversation] Thread         Open: Timeline · Matrix · More ▾ │ VERDICT                         │
│                                                                │ None recorded yet               │
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
│   worked by hand at 14:32.                                     │                                 │
│                                                                │ KEY ENTITIES                    │
│   → Noted 2 explanations and 4 checks · 1 check done           │ svc_backup    account · subject │
│   ▸ Activity · 4 tool calls, 7 records                         │ WIN-FILE01   host · file server │
│                                                                │ 10.0.4.20           IP · source │
│ ────────────────────────────────────────────────────────────── │                                 │
│ Ask, pivot, or tell reckon what to do…                [ Send ] │ PINNED EVIDENCE · 2             │
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
**Activity** (tool calls and records written) is folded. Timeline, matrix, full explanation
cards and the action history are closed.

**What a click opens.**

- Header title: rename, as today. **3 open**: scrolls the panel to Open items. **Verdict…**:
  the verdict dialog with its checklist, available now. **( Conclude )**: disabled until a
  verdict is recorded, because a conclusion uses the verdict on record. Its tooltip says so.
- **Conversation / Thread**: Thread shows the same turns as the full audit record, with every
  act, sequence and tool call.
- **Open: Timeline · Matrix · More**: opens that view in the centre beside the conversation
  (screen e). **More** lists Explanations (the full cards), Actions (the action history),
  Summary and export.
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
│ [Conversation] Thread         Open: Timeline · Matrix · More ▾ │ VERDICT                         │
│                                                                │ None recorded yet               │
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
│   14:33. It held.                                              │ What I think happened           │
│   Two checks are left. Both would tell the two explanations    │   Someone used the svc_backup   │
│   apart.                                                       │   password by hand to open a    │
│                                                                │   desktop session on WIN-FILE01 │
│   ▸ Activity · 2 tool calls, 2 records                         │   and run PowerShell.           │
│                                                                │ What else it could be           │
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
│                                                                │   Open: Matrix · Full cards     │
│                                                                │                                 │
│                                                                │ ▸ KEY ENTITIES · 4              │
│                                                                │                                 │
│                                                                │ ▸ PINNED EVIDENCE · 2           │
└────────────────────────────────────────────────────────────────┴─────────────────────────────────┘
```

**Principles shown.** 7: the whole competing set is one fold away, and the matrix and the full
cards are one more step. 10: this case has a second explanation that is still open, so the
panel shows it and the checks that would tell the two apart. A case with one explanation and
every check done shows the one-line basis and nothing else. 8: the "Nothing" row is a cited
absence. It names the search and its window, and it opens like any other evidence. 6: the
conversation tells what was just found. The panel holds the current state of every check, once.

**Collapsed by default.** **Why** is closed until the analyst opens it, and **Show the
working** is closed inside it. Both remember their last state for the investigation. Key
entities and pinned evidence fold here only to keep the diagram short.

**What a click opens.**

- The text under **What I think happened** or **What else it could be**: the explanation's
  inspector, with its record and its wording history. Editing the wording is offered there.
- A check row: the check's inspector, with its evidence and what it decides against.
  **9 evidence** opens the list of evidence for that check.
- **Open: Matrix · Full cards**: the matrix or the full cards in the centre, beside the
  conversation.
- The "Nothing" evidence row: the absence inspector (the search, its window, the sources that
  answered).
- **✓ Checked: … It held.**: the check's inspector.

## c. An action waiting for approval before any verdict

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       3 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────────────┬─────────────────────────────────┤
│ [Conversation] Thread         Open: Timeline · Matrix · More ▾ │ VERDICT                         │
│                                                                │ None recorded yet               │
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
│                                                                │                                 │
│   ▸ Activity · 1 tool call, 1 record                           │ KEY ENTITIES                    │
│                                                                │ svc_backup    account · subject │
│ ────────────────────────────────────────────────────────────── │ WIN-FILE01   host · file server │
│ Ask, pivot, or tell reckon what to do…                [ Send ] │ 10.0.4.20           IP · source │
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
- **3 open** in the header now counts the approval with the two open items.

## d. An inspector in the right panel

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       2 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├────────────────────────────────────────────────────────┬─────────────────────────────────────────┤
│ [Conversation] Thread Open: Timeline · Matrix · More ▾ │ ‹ Case state                    Pin   ✕ │
│                                                        │                                         │
│ reckon · 14:53                                         │ EVIDENCE · SIGN-IN                      │
│   This does not look like the backup job. svc_backup   │ CONTOSO\svc_backup RDP logon to         │
│   normally signs in over the network from BKP-01 at    │ WIN-FILE01 from 10.0.4.20               │
│   02:00. This was a desktop session from 10.0.4.20 in  │ 20 Apr 2026 14:32:47 UTC                │
│   the afternoon, and PowerShell started 23 seconds     │                                         │
│   later with an encoded command.                       │ In this case                            │
│                                                        │ · Check: the sign-in is far outside the │
│   Evidence · 4 rows                                    │   account's normal pattern (held)       │
│  02:00 ×7  Sign-in svc_backup network logon to         │ · Pinned: desktop sign-in outside the   │
│                    WIN-FILE01 from BKP-01 · 7 nights,  │   backup pattern                        │
│                    13 to 19 Apr · expected             │                                         │
│ ▶14:32:47  Sign-in CONTOSO\svc_backup RDP logon to     │ [Details]  Provenance  Raw              │
│                    WIN-FILE01 from 10.0.4.20           │                                         │
│  14:33:10  Process CONTOSO\svc_backup ran              │ Account       CONTOSO\svc_backup        │
│                    powershell.exe on WIN-FILE01, child │ Sign-in type  Remote desktop (10)       │
│                    of mstsc-hijack.exe                 │ From          10.0.4.20                 │
│  14:33:10  Alert   Suspicious lateral movement via RDP │ To            WIN-FILE01                │
│                    followed by encoded PowerShell      │ Result        Success                   │
│                                                        │                                         │
│   ▸ Activity · 4 tool calls, 7 records                 │ Around it on WIN-FILE01                 │
│                                                        │ 14:32:47  this sign-in                  │
│ ────────────────────────────────────────────────────── │ 14:33:10  powershell.exe started        │
│ Ask, pivot, or tell reckon what to do…        [ Send ] │ 14:33:25  outbound to 185.220.101.5     │
│                                                        │                                         │
│                                                        │ Retrieved by enumerate_logons           │
│                                                        │ Answered in full                        │
└────────────────────────────────────────────────────────┴─────────────────────────────────────────┘
```

**Principles shown.** 8: the row's record, the call that produced it and its raw form are one
click from the sentence that made the claim. 7: nothing was removed from the inspector. It has
the same frame and the same tabs as `08 §8.6`. 6: the panel shows either the case state or one
inspector.

**Collapsed by default.** The Provenance and Raw tabs. The conversation keeps its place, and
the row that was clicked is marked.

**What a click opens.**

- **‹ Case state**: closes the inspector and returns the panel to the case state. `Esc` does
  the same. When one inspector was reached from another, the back control names that one, and
  Case state is the last step back.
- **Pin**: keeps this inspector while another opens. Where the second one goes is an open
  question (see below).
- **✕**: closes to the case state.
- A line under **In this case**: the check or the pin it names.
- **Provenance**: the verb, the adapter, the query sent, coverage, ingest time and content
  hash. **Raw**: the stored record, with the engine's own field and type names.
- A row under **Around it**: that event's inspector, with back to this one.
- **Retrieved by enumerate_logons**: the capability call inspector.
- The panel is wider while an inspector is open: 41 of 100 columns, about 530px, against 33
  for the case state. The conversation gives up the difference.

## e. The timeline opened beside the conversation

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by svc_backup       2 open   [Verdict…] (Conclude)│
│ from INC0010042 · active · no verdict yet                                                        │
├──────────────────────────────┬──────────────────────────────────────┬────────────────────────────┤
│ [Conversation] Thread        │ TIMELINE · event time        ✕ Close │ VERDICT                    │
│                              │ All · Sign-in · Process · Network    │ None recorded yet          │
│                  You · 14:52 │ 5 events · 5 lookups       [x] Group │ Active · opened 14:41      │
│      Is this the backup job, │                                      │                            │
│         or someone using the │ 13–19 Apr · 02:00 · ×7               │ OPEN ITEMS · 2             │
│             account by hand? │   Sign-in  svc_backup network logon  │ ◆ Did anyone on the backup │
│                              │   to WIN-FILE01 from BKP-01 ·        │   team work by hand at     │
│ reckon · 14:53               │   expected                           │   14:32?                   │
│ This does not look like the  │ 20 Apr                               │   needs a person           │
│ backup job. svc_backup       │ 14:32:47  Sign-in                    │            [Record answer] │
│ normally signs in over the   │   CONTOSO\svc_backup RDP logon to    │ ○ What is 10.0.4.20, and   │
│ network from BKP-01 at       │   WIN-FILE01 from 10.0.4.20          │   who was at it?           │
│ 02:00. This was a desktop    │ 14:33:10  Process                    │   not checked yet  [Check] │
│ session from 10.0.4.20 in    │   CONTOSO\svc_backup ran             │ ✕ VPN records for          │
│ the afternoon.               │   powershell.exe on WIN-FILE01,      │   10.0.4.20                │
│                              │   child of mstsc-hijack.exe          │   could not check: no VPN  │
│ Evidence · 4 rows ▸          │ 14:33:10  Alert                      │   source is connected      │
│ ▸ Activity · 4 tool calls    │   Suspicious lateral movement via    │                            │
│                              │   RDP followed by encoded PowerShell │ PENDING APPROVALS · 0      │
│ ──────────────────────────── │ 14:33:25  Network                    │                            │
│ Ask or tell reckon…   [Send] │   WIN-FILE01 connected to            │ ▸ WHY · 2 of 4 checks done │
│                              │   185.220.101.5 port 443             │   Someone used svc_backup  │
│                              │   (exfil.example.net) over tcp       │   by hand. One other       │
│                              │ ── investigation opened 14:41 ──     │   explanation is still     │
│                              │ Not yet known: what 10.0.4.20 is and │   open.                    │
│                              │ who was at it.                       │                            │
│                              │                                      │ ▸ KEY ENTITIES · 4         │
│                              │                                      │                            │
│                              │                                      │ ▸ PINNED EVIDENCE · 2      │
└──────────────────────────────┴──────────────────────────────────────┴────────────────────────────┘
```

**Principles shown.** 7: the timeline is the Story lens of `08 §8.4` with nothing taken out.
It opens on demand and closes back to the conversation. 9: the timeline takes the widest
column while it is open. 1: the conversation stays on screen and the composer stays usable, so
a question about a row can be asked while looking at it.

**Collapsed by default.** In the narrowed conversation, a turn's evidence rows fold to one
line ("Evidence · 4 rows ▸") because the same events are in the timeline beside it. Key
entities and pinned evidence fold on the panel. The evidence graph and the grouping controls
are folded inside the timeline, as they are today.

**What a click opens.**

- **✕ Close**: closes the timeline. The conversation takes the centre again.
- A timeline row: the evidence inspector in the right panel.
- The folded row (**×7**): its seven members in place.
- **All · Sign-in · Process · Network**: filters the rows. **[x] Group**: turns the fold of
  repeats on or off.
- **Not yet known**: the open check it names.
- Matrix, Explanations and Actions open in this same middle column, one at a time.

This screen is the tightest. At 1300px three columns leave the conversation about 390px. See
the open questions.

## f. The same case in a narrow window

```
┌────────────────────────────────────────────────────────┐
│ Suspicious interactive RDP logon to WIN-FILE01 by      │
│ svc_backup                                             │
│ INC0010042 · active           [ Verdict… ] ( Conclude )│
├────────────────────────────────────────────────────────┤
│ No verdict · 3 open                             Case ▾ │
│ ────────────────────────────────────────────────────── │
│ [Conversation] Thread                           Open ▾ │
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

**Collapsed by default.** The whole case-state panel. It becomes a drawer. Timeline, matrix
and the other views are behind **Open ▾**.

**What a click opens.**

- **Case ▾**, or the case line itself: the case-state panel as a drawer over the conversation,
  with the same sections in the same order. An inspector opens in that drawer, with back to
  the case state.
- **Open ▾**: Timeline, Matrix, Explanations, Actions, Summary. At this width the chosen view
  replaces the conversation, with a control that returns to it.
- Everything else opens what it opens on screen a.

## Open questions for review

1. **What the header count counts.** The diagrams count open items and pending approvals
   together, and leave out "could not check". The other choice is two numbers.
2. **A home for what the target shape does not name.** The action history, conversations with
   others, dependencies on others, the "what happened" narrative, the handoff note, the
   export, the procedure draft and the evidence graph all exist today. The diagrams put the
   action history and the narrative under **More**, replies awaited and dependencies in Open
   items, and the graph inside the timeline. None of that is settled.
3. **Comparing two inspectors.** Today a pinned inspector stays while a second opens beside
   it. The right panel has room for one. The pinned one could move to the centre column.
4. **The width below which an opened view replaces the conversation.** Screen e keeps three
   columns at 1300px. Screen f replaces at 750px. The switch point between them is not chosen.
5. **Whose voice "What I think happened" is.** The words read as the agent's. Once the analyst
   has edited the wording, "I" may be the wrong word. "Leading explanation" and "Other
   explanations" are the neutral pair.
6. **Which explanation leads.** The record holds a set and no leader. The panel needs a rule:
   the supported one when there is one, otherwise the one proposed first.
