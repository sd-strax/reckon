// The header (08 §8.2): title with inline rename, the case reference, who
// opened it, the verdict pill, the stage indicator, the primary action, the
// nearest approval expiry, and the lifecycle controls — every legal move a
// button, never a menu. Export lives on the Summary lens.
import { useEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { caseNoteStatus } from "../derive/actions";
import { openOwnerItems } from "../derive/summary";
import { concludeAllowed, concludePreflight, preflightClean } from "../derive/preflight";
import { STEPS, STEP_LABELS, nearestExpiry, primaryAction, stageOf } from "../derive/stage";
import { Countdown } from "./shared";

export function Header() {
  const { state, send, openPrompt, goTo, setVerdictOpen } = useApp();
  const snap = state.snap;
  const engine = snap ? String(snap.investigation.state).toUpperCase() : "";
  const closed = engine === "ARCHIVED" || engine === "CONCLUDED";
  const actions = snap?.actions || [];
  const verdict = snap?.investigation.verdict || null;
  const comms = snap?.comms || [];
  const owners = openOwnerItems(snap?.ownerItems).length;
  const inputs = { status: engine, verdict, actions, comms, openOwnerItems: owners };
  const stage = stageOf(inputs);
  const preflight = concludePreflight({ verdict, pins: snap?.pins || [], actions, comms, openOwnerItems: owners });
  const primary = snap ? primaryAction(inputs, stage, preflightClean(preflight)) : null;
  const expiry = nearestExpiry(actions);
  const seed = snap?.investigation.seed;

  // In-place rename: Enter commits, Escape cancels, blur commits. Guarded on
  // closed records for EVERY entry point (pencil, dblclick, tree).
  const [editing, setEditing] = useState<string | null>(null);
  const edit = useRef<HTMLInputElement>(null);
  useEffect(() => { if (state.renameRequest && !closed) setEditing(state.renameRequest.title || state.title); }, [state.renameRequest, closed, state.title]);
  useEffect(() => { if (editing !== null) { edit.current?.focus(); edit.current?.select(); } }, [editing]);
  const start = () => { if (!closed && editing === null) setEditing(state.title); };
  const end = (commit: boolean) => {
    if (editing === null) return;
    const v = editing.trim();
    setEditing(null);
    if (commit && v && v !== state.title) send({ type: "rename", title: v });
  };

  const conclude = () => openPrompt({
    title: "Conclude investigation",
    label: "Summary of record — what did this investigation determine?",
    placeholder: "e.g. Confirmed C2 beaconing from the file server; host isolated, IOCs blocked.",
    // The latest narrative prefills the conclusion summary (adr/0103).
    value: snap?.summary?.narrative?.body || undefined,
    helper: preflight.map((x) => (x.ok ? "☑ " : "☐ ") + x.text).join(" · ") + " · Concluding consumes the verdict of record and files the final report reference; you can reopen later.",
    confirm: "Conclude",
    onConfirm: (summary) => send({ type: "lifecycle", transition: "conclude", summary }),
  });

  const controls: React.ReactNode[] = [];
  const mk = (text: string, title: string, onClick: () => void, disabledReason?: string) => controls.push(
    <button key={text} title={disabledReason || title} disabled={!!disabledReason} onClick={disabledReason ? undefined : onClick}>{text}</button>,
  );
  switch (engine) {
    case "ACTIVE":
      mk("Pause", "A reversible hold — resume any time", () => send({ type: "lifecycle", transition: "pause" }));
      if (primary?.kind !== "conclude") mk("Conclude…", "Close with the verdict of record and a summary", conclude, concludeAllowed(preflight) ? "" : "Record a verdict first — a conclusion consumes the verdict of record");
      break;
    case "PAUSED":
      mk("Resume", "Back to ACTIVE", () => send({ type: "lifecycle", transition: "resume" }));
      break;
    case "CONCLUDED":
      mk("Reopen…", "Back to ACTIVE — the prior conclusion stays on the record", () => openPrompt({
        title: "Reopen investigation", label: "Why does this need to be reopened?", placeholder: "e.g. New alert on the same host — the conclusion may not hold",
        helper: "Reopening clears the conclusion slot; the prior report stays referenced from the thread.", confirm: "Reopen",
        onConfirm: (reason) => send({ type: "lifecycle", transition: "reopen", reason }),
      }));
      mk("Archive…", "Terminal — accepts no further changes", () => openPrompt({
        title: "Archive investigation", label: "Archiving is TERMINAL — the record accepts no further changes, ever. Note why (optional):", placeholder: "e.g. Retention review complete",
        helper: "Unlike pause or conclude, there is no way back from archived.", confirm: "Archive permanently", require: false,
        onConfirm: (reason) => send({ type: "lifecycle", transition: "archive", reason }),
      }));
      break;
  }

  return (
    <header id="header">
      <div className="titlerow">
        <span className="titlename">
          {editing === null ? (
            <>
              <h1 id="title" title="Double-click to rename" onDoubleClick={start}>{state.title || "Investigation"}</h1>
              {snap && !closed ? <button className="iconbtn" title="Rename this investigation" onClick={start}>✎</button> : null}
            </>
          ) : (
            <input ref={edit} className="titleedit" spellCheck={false} value={editing} onChange={(e) => setEditing(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); end(true); } else if (e.key === "Escape") { e.preventDefault(); end(false); } }}
              onBlur={() => end(true)} />
          )}
        </span>
        {verdict ? <span className={"state verdictbadge " + verdict.disposition} title={verdict.rationale || ""}>{verdict.disposition}</span> : null}
        {engine === "PAUSED" || engine === "ARCHIVED" || engine === "DRAFT" ? <span className="state" data-engine={engine}>{engine}</span> : null}
        <span className="headcontrols">
          {snap && !closed ? <button title="Record the disposition of record" onClick={() => setVerdictOpen(true)}>Verdict…</button> : null}
          {controls}
          <button title="Reload from the backend" onClick={() => send({ type: "refresh" })}>Refresh</button>
        </span>
      </div>
      <div id="meta">
        {snap ? (
          <>
            <code title={snap.investigation.id}>{snap.investigation.id.slice(0, 8)}</code>
            {seed?.type === "case" ? <> · from case <a onClick={() => seed.caseRef ? goTo({ kind: "ref", ref: seed.caseRef }) : undefined}>{seed.caseId}</a> <CaseNoteControl /></> : null}
            {seed?.type === "entity" ? <> · investigating <span className="mono">{seed.entityIdentifier || seed.entityRef}</span></> : null}
            {seed?.type === "question" ? <> · hunting: {seed.hypothesisStatement}</> : null}
            {seed?.type === "alert" ? <> · from alert <span className="mono">{seed.source ? seed.source + ": " : ""}{seed.alertId}</span></> : null}
            {snap.investigation.createdAt ? <> · <span title={snap.investigation.createdAt}>{new Date(snap.investigation.createdAt).toLocaleString()}</span></> : null}
            {snap.investigation.openedByName ? <> · opened by {snap.investigation.openedByName}</> : null}
          </>
        ) : null}
      </div>
      {snap ? (
        <div className="stagerow">
          <ol className="stage" aria-label="Stage">
            {STEPS.map((s) => (
              <li key={s} className={"step" + (stage.done[s] ? " done" : "") + (stage.current === s ? " current" : "")}>
                <span className="stepmark" aria-hidden="true">{stage.done[s] ? "✓" : stage.current === s ? "●" : "○"}</span>{STEP_LABELS[s]}
              </li>
            ))}
          </ol>
          <span className="primaryaction">
            {primary?.kind === "review" ? <button className="primary" onClick={() => goTo({ kind: "lens", lens: "response" })}>Review {primary.count} proposed action{primary.count === 1 ? "" : "s"}</button> : null}
            {primary?.kind === "notify" ? <button className="primary" onClick={() => goTo({ kind: "lens", lens: "response", anchor: "notification" })}>Send notification</button> : null}
            {primary?.kind === "conclude" ? <button className="primary" onClick={conclude}>Conclude</button> : null}
            {expiry ? <span className="expiry">⏳ first approval <Countdown expiresAt={expiry} className="inline" /></span> : null}
          </span>
        </div>
      ) : null}
    </header>
  );
}

/** The single-use "Note the case" action on a case seed (adr/0032): on demand,
 * never a creation-time choice. Once a comment action on the seed case exists
 * the line shows its status instead, read from the action record. */
function CaseNoteControl() {
  const { state, send, openPrompt } = useApp();
  const snap = state.snap;
  const seed = snap?.investigation.seed;
  if (!snap || !seed || seed.type !== "case") return null;
  const word = caseNoteStatus(seed, snap.actions || []);
  if (word) return <span className="seedNote" title="A comment on this case is on the action record">· noted · {word}</span>;
  const caseNote = snap.investigation.caseNote;
  if (!caseNote) return null;
  return (
    <button className="linklike seedNoteBtn" title="Post a comment on the case that an investigation was opened here — a ticket.comment action, approved like any other" onClick={() => openPrompt({
      title: "Add note to " + (caseNote.systemName || "the case system"),
      label: "Comment to post on " + caseNote.caseId + " — requested as a ticket.comment action in this investigation",
      helper: "Cancel posts nothing. The request goes through approval like any action.",
      value: caseNote.body, require: false, confirm: "Post note",
      onConfirm: (v) => send({ type: "caseNote.post", body: v }),
    })}>Note the case</button>
  );
}

