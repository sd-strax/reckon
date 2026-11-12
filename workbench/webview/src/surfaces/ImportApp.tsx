// Import SOPs (design/06 §2.4, design/13 §3): choosing WHAT becomes
// institutional knowledge is an operational decision, so it gets a visible
// surface the analyst can read, revise, and see the outcome of. The host owns
// the rows (the open editors, the files added from disk) and the import; this
// surface renders its state and posts the analyst's choices.
import { useEffect, useId, useState } from "react";
import type { ImportInboundMessage, ImportOutcome, ImportRenderMessage, ImportRow } from "../../../src/protocol";
import type { WebviewTransport } from "../transport";

export type ImportTransport = WebviewTransport<ImportInboundMessage, ImportRenderMessage>;

interface ImportState {
  rows: ImportRow[];
  busy: boolean;
  done?: { ok: number; failed: number };
}

export function ImportApp({ transport }: { transport: ImportTransport }) {
  const [state, setState] = useState<ImportState>({ rows: [], busy: false });
  useEffect(() => {
    const off = transport.onMessage((m) => {
      if (m.type === "state") {
        setState({ rows: m.rows, busy: m.busy, done: m.done });
      }
    });
    transport.send({ type: "ready" });
    return off;
  }, [transport]);

  const toggle = (row: ImportRow, checked: boolean) => {
    setState((s) => ({ ...s, rows: s.rows.map((r) => (r.id === row.id ? { ...r, checked } : r)) }));
    transport.send({ type: "toggle", id: row.id, checked });
  };
  const open = state.rows.filter((r) => !r.fromDisk);
  const disk = state.rows.filter((r) => r.fromDisk);
  const selected = state.rows.filter((r) => r.checked).length;
  const note = state.done
    ? (state.done.failed
        ? `${state.done.ok} imported, ${state.done.failed} failed — see each row.`
        : `${state.done.ok} imported — in the Knowledge view now, for the agent to recall.`)
    : (!state.busy && selected === 0 ? "Select the documents to import." : "");

  return (
    <div className="panel import">
      <div className="kicker">Knowledge · institutional SOPs</div>
      <h1>Import SOPs</h1>
      <p className="sub">Markdown or text, with optional YAML frontmatter (title, author, tags, source). Imported documents are in the Knowledge view at once, for the agent to recall. Re-importing a document with the same source revises it rather than duplicating it.</p>

      <section>
        <span className="kicker">Open in the editor{open.length ? ` · ${open.length}` : ""}</span>
        {open.length === 0
          ? <p className="empty">No markdown or text documents are open. Open one to import it as shown, or add files from disk below.</p>
          : open.map((r) => <Row key={r.id} row={r} busy={state.busy} onToggle={toggle} />)}
        <div className="toolbar"><button disabled={state.busy} onClick={() => transport.send({ type: "refresh" })}>Rescan open editors</button></div>
      </section>

      <section>
        <span className="kicker">From disk</span>
        {disk.length === 0
          ? <p className="empty">No files added.</p>
          : disk.map((r) => <Row key={r.id} row={r} busy={state.busy} onToggle={toggle} />)}
        <div className="toolbar"><button disabled={state.busy} onClick={() => transport.send({ type: "addFiles" })}>Add files…</button></div>
      </section>

      <div className="foot">
        <button className="primary" disabled={state.busy || selected === 0} onClick={() => transport.send({ type: "import" })}>
          {state.busy ? "Importing…" : selected ? `Import ${selected} document${selected === 1 ? "" : "s"}` : "Import"}
        </button>
        <span className="note">{note}</span>
      </div>
    </div>
  );
}

/** The result line under a row that was imported. */
function outcomeLine(outcome: ImportOutcome, title: string): string {
  switch (outcome) {
    case "created":
      return `Added to the SOP library as “${title}”`;
    case "unchanged":
      return `Already in the SOP library as “${title}”, nothing changed`;
    default:
      return `Revised existing SOP “${title}”`;
  }
}

function Row({ row: r, busy, onToggle }: { row: ImportRow; busy: boolean; onToggle: (row: ImportRow, checked: boolean) => void }) {
  // The checkbox's id is React's, unique per row: one derived from the URI
  // could collide (a-b.md and ab.md) and a label would toggle the wrong file.
  const id = useId();
  const outcome = r.result?.outcome;
  return (
    <div className={"row" + (outcome ? (outcome === "failed" ? " failed" : " done") : "")}>
      <input type="checkbox" id={id} checked={r.checked} disabled={busy} onChange={(e) => onToggle(r, e.target.checked)} />
      <label htmlFor={id}>
        <div className="name">{r.name}</div>
        {r.dir ? <div className="dir">{r.dir}</div> : null}
      </label>
      <div className="tags">
        {r.dirty ? <span className="badge unsaved">unsaved edits · imported as shown</span> : null}
        {outcome ? <span className={"badge " + outcome}>{outcome}</span> : null}
      </div>
      {r.result ? (
        <div className={"result" + (outcome === "failed" ? " failed" : "")}>
          {r.result.outcome === "failed" ? (r.result.message || "import failed") : outcomeLine(r.result.outcome, r.result.title)}
        </div>
      ) : null}
    </div>
  );
}
