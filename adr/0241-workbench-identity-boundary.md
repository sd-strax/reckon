# 0241 — Nothing read as one person outlives their session, and a failed read is not an answer

Issue: #241 · PR: · Status: accepted

## Context

The workbench host keeps reads in memory. Identity did not bound them: on sign-out or a sign-in as someone else the host only re-rendered. `docCache`, `labelCache` and the shared transcripts survived, so the next person was served the previous person's evidence and transcripts with no backend check. A panel the next person could not read kept the last snapshot on screen, and its tab the investigation's title. A case document did the same. `SeenTracker` kept one set of last-seen marks for everyone who ever signed in on the machine, so one analyst's activity set another's "● new" cue. adr/0237 ended the sidecar's sessions on a change of person and left this half.

Failures were cached too. `getDoc` stored `null` for any error, and `labelCache` stored `""`, so one transient error blanked a ref until reload. The webview also asks for each ref once per panel, so the host alone could not bring a label back.

`forget()` left state behind. A rename pending when a panel closed popped up the next time that investigation opened.

## Decision

**A change of person clears everything read as the previous one.** On `onDidChangeIdentity`, the host clears `docCache`, `labelCache`, all of `InvestigationCache` including the transcripts, `sopDrafts` and `pendingRename`, and ends the sidecar (adr/0237).

**A read in flight cannot refill it.** A generation counter is bumped at the clear; a read that began before it may answer its caller but is not cached.

**Everything reckon opened closes.** Investigation, case and import documents close, not blank: that removes the content, the tab title and the registry entry together, where a blanked panel would keep the title and stay bound to the previous person's investigation. SOP drafts close with their unsaved text reverted, so nobody is asked to save someone else's work. An exported report's preview closes and its scratch file is deleted. Only what reckon opened is touched, tracked as it opens; other editors stay. The Cases and Investigations views then come up for the new person. On the restore at window start nobody changed, so nothing is revealed.

**Last-seen marks are per person**, keyed by subject in `globalState`. They survive sign-out for that person. The old shared key is deleted, so cues reset once on upgrade. With nobody signed in, nothing is read or written.

**Failures are not cached.** The caches hold successes only, and concurrent reads of one ref share a fetch. The host posts `evidence.unresolved`, and the webview asks again after the next snapshot or patch. Retries follow refreshes, not renders, so a ref that never resolves is not fetched in a loop. An empty label from a doc that did read stays cached: that is an answer.

**`forget()` forgets all of it.** `InvestigationCache.forget` also drops the watermark, and clears again behind any read still running, which would otherwise write when it lands. `InvestigationDocuments.forget` also drops the investigation's `sopDrafts`, `pendingRename` and `draftMode`.

## Alternatives rejected

- Blanking panels: see above.
- Leaving SOP drafts and reports open: the previous person's text would stay in front of the next.
- Keying the caches by subject: the previous person's data would stay in memory for the session, and nothing would read it again.
- Retrying a failed ref on every render: an unresolvable ref would be fetched in a loop during a streaming turn.
- A blanket retry timer: it adds load with nothing to tell it the read might now succeed.

## Consequences

- A change of person closes every reckon document; the next person starts from their own views.
- Unsaved edits to a SOP draft are lost on a change of person. A draft or report the analyst saved to a file is theirs, and stays.
- The preview tab is found by its view type and label, which belong to VS Code's markdown extension and could change.
- `SeenTracker` no longer lives in `extension.ts`, so it is testable.
