// The document header: title (inline rename, like a file rename), the engine
// state pill with its derived presentation label (binding §2.4), the verdict
// badge, and the lifecycle controls — every legal move a button, never a menu.
import { useEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { headerLabel } from "../derive/actions";

export function Header() {
  const { state, send, openPrompt, setVerdictOpen } = useApp();
  const snap = state.snap;
  const engine = snap ? String(snap.investigation.state).toUpperCase() : "";
  const closed = engine === "ARCHIVED" || engine === "CONCLUDED";
  const actions = snap?.actions || [];
  const verdict = snap?.investigation.verdict || null;
  const comms = snap?.comms || [];

  // In-place rename: Enter commits, Escape cancels, blur commits. Guarded on
  // closed records for EVERY entry point (pencil, dblclick, tree) — the engine
  // refuses a rename on concluded/archived, so no path may offer one.
  const [editing, setEditing] = useState<string | null>(null);
  const edit = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (state.renameRequest && !closed) setEditing(state.renameRequest.title || state.title);
  }, [state.renameRequest, closed, state.title]);
  useEffect(() => { if (editing !== null) { edit.current?.focus(); edit.current?.select(); } }, [editing]);
  const start = () => { if (!closed && editing === null) setEditing(state.title); };
  const end = (commit: boolean) => {
    if (editing === null) return;
    const v = editing.trim();
    setEditing(null);
    if (commit && v && v !== state.title) send({ type: "rename", title: v });
  };

  const lifecycle = (transition: string, reason?: string) => send({ type: "lifecycle", transition, reason });
  const controls: React.ReactNode[] = [];
  const mk = (text: string, title: string, onClick: () => void, disabledReason?: string) => controls.push(
    <button key={text} title={disabledReason || title} disabled={!!disabledReason} onClick={disabledReason ? undefined : onClick}>{text}</button>,
  );
  switch (engine) {
    case "ACTIVE":
      mk("Pause", "A reversible hold — resume any time", () => lifecycle("pause"));
      mk("Conclude…", "Close with the verdict of record and a summary", () => {
        // The comms conclude-gate input (binding §4, ui/04 §4.10): open external
        // work is decision support at the moment of closing — surfaced, never
        // silently ignored.
        const openComms = comms.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
        const commsWarn = openComms ? " ⚠ " + openComms + " comms thread(s) still await replies — mark them done or accept leaving them open." : "";
        openPrompt({
          title: "Conclude investigation",
          label: "Summary of record — what did this investigation determine?",
          placeholder: "e.g. Confirmed C2 beaconing from WIN-9; host isolated, IOCs blocked.",
          helper: "Concluding consumes the verdict of record (" + (verdict ? verdict.disposition : "none") + ") and files the final report reference. You can reopen later if the world disagrees." + commsWarn,
          confirm: "Conclude",
          onConfirm: (summary) => send({ type: "lifecycle", transition: "conclude", summary }),
        });
      }, verdict ? "" : "Record a verdict first — a conclusion consumes the verdict of record");
      break;
    case "PAUSED":
      mk("Resume", "Back to ACTIVE", () => lifecycle("resume"));
      break;
    case "CONCLUDED":
      mk("Reopen…", "Back to ACTIVE — the prior conclusion stays on the record", () => openPrompt({
        title: "Reopen investigation",
        label: "Why does this need to be reopened?",
        placeholder: "e.g. New alert on the same host — the conclusion may not hold",
        helper: "Reopening clears the conclusion slot; the prior report stays referenced from the thread.",
        confirm: "Reopen",
        onConfirm: (reason) => send({ type: "lifecycle", transition: "reopen", reason }),
      }));
      mk("Archive…", "Terminal — accepts no further changes", () => openPrompt({
        title: "Archive investigation",
        label: "Archiving is TERMINAL — the record accepts no further changes, ever. Note why (optional):",
        placeholder: "e.g. Retention review complete",
        helper: "Unlike pause or conclude, there is no way back from archived.",
        confirm: "Archive permanently",
        require: false,
        onConfirm: (reason) => send({ type: "lifecycle", transition: "archive", reason }),
      }));
      break;
    // DRAFT: no explicit Activate (01 §Extension 2, invisible activate).
  }

  return (
    <header>
      <div className="titlerow">
        <span className="titlename">
          {editing === null ? (
            <>
              <h1 id="title" title="Double-click to rename" onDoubleClick={start}>{state.title || "Investigation"}</h1>
              {snap && !closed ? <button className="iconbtn" title="Rename this investigation" onClick={start}>✎</button> : null}
            </>
          ) : (
            <input
              ref={edit}
              className="titleedit"
              spellCheck={false}
              value={editing}
              onChange={(e) => setEditing(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); end(true); }
                else if (e.key === "Escape") { e.preventDefault(); end(false); }
              }}
              onBlur={() => end(true)}
            />
          )}
        </span>
        <span className="state" data-engine={engine}>{snap ? headerLabel(engine, actions, verdict) : ""}</span>
        {verdict ? <span className={"state verdictbadge " + verdict.disposition} title={verdict.rationale || ""}>{verdict.disposition}</span> : null}
        {snap && !closed ? <button title="Record the disposition of record" onClick={() => setVerdictOpen(true)}>Verdict…</button> : null}
        <span>{controls}</span>
        <button title="The live markdown snapshot — paste into a ticket unedited" onClick={() => send({ type: "export.open" })}>Export</button>
        <button title="Reload from the backend" onClick={() => send({ type: "refresh" })}>Refresh</button>
      </div>
      <div id="meta">
        {snap ? <><code>{snap.investigation.id}</code> · seq {snap.investigation.lastEventSequence}</> : null}
      </div>
    </header>
  );
}
