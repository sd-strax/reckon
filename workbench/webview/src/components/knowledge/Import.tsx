// Import (09 §9.5; parity §N): the former Import SOPs panel moved into the
// document. Documents open in the editor are listed as shown (unsaved edits
// included); files or a folder are added through the native picker; each row
// shows the server's parsed frontmatter before import with the recommended
// fields it lacks flagged (the dry run, impl §8.0), then its outcome: added,
// revised (the same source URL as an existing procedure: a new version under
// the same id), or failed with the parser's reason, verbatim.
import { useApp } from "../../app";
import type { ImportRow } from "../../../../src/protocol";
import { Badge, Empty } from "../shared";

function Preview({ r }: { r: ImportRow }) {
  if (r.previewError) return <div className="kprev failed">{r.previewError}</div>;
  const p = r.preview;
  if (!p) return <div className="kprev imeta">reading…</div>;
  return (
    <div className="kprev">
      <span className="kpv"><b>{p.title}</b></span>
      {p.author ? <span className="kpv">by {p.author}</span> : null}
      {p.tags.length ? <span className="kpv mono">{p.tags.join(", ")}</span> : null}
      {p.recommendation ? <span className="kpv mono">recommends {p.recommendation}</span> : null}
      <span className="kpv">{p.sourceDefaulted ? <span className="imeta">source: the file path</span> : <span className="mono">{p.source.system ? p.source.system + " · " : ""}{p.source.url}{p.source.version ? " · v" + p.source.version : ""}</span>}</span>
      {p.missing.length ? <span className="kpv kmissing" title="Import proceeds without them">missing {p.missing.map((m) => m.replace("_", " ")).join(", ")}</span> : null}
      <span className="kpv">{p.outcome === "revised" ? <>will revise <em>{p.existingTitle || p.existingId}</em></> : "will be added"}</span>
    </div>
  );
}

function Row({ r, busy }: { r: ImportRow; busy: boolean }) {
  const { send } = useApp();
  return (
    <div className={"irowbox" + (r.result ? (r.result.outcome === "failed" ? " failed" : " done") : "")}>
      <input type="checkbox" checked={r.checked} disabled={busy} aria-label={"Import " + r.name} onChange={(e) => send({ type: "import.toggle", id: r.id, checked: e.target.checked })} />
      <div className="ibody">
        <div className="iname">{r.name}{r.dirty ? <Badge tone="warn" title="The live buffer is imported as shown">unsaved edits</Badge> : null}{r.fromEditor ? <Badge tone="muted">open in the editor</Badge> : null}</div>
        {r.dir ? <div className="idir mono">{r.dir}</div> : null}
        <Preview r={r} />
        {r.result ? (
          <div className={"iresult" + (r.result.outcome === "failed" ? " failed" : "")}>
            <Badge tone={r.result.outcome === "failed" ? "bad" : "ok"}>{r.result.outcome}</Badge>{" "}
            {r.result.outcome === "failed" ? (r.result.message || "import failed") : r.result.outcome === "added" ? <>Added to the library as “{r.result.title}”</> : <>Revised the existing procedure “{r.result.title}”</>}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function Import() {
  const { state, send } = useApp();
  const k = state.kdoc;
  const rows = k?.importRows || [];
  const busy = !!k?.importing;
  const editor = rows.filter((r) => r.fromEditor);
  const disk = rows.filter((r) => !r.fromEditor);
  const n = rows.filter((r) => r.checked).length;
  const done = rows.filter((r) => r.result);
  const ok = done.filter((r) => r.result!.outcome !== "failed").length;
  return (
    <div className="kimport">
      <section className="lsection">
        <h3>Import <span className="imeta">markdown with YAML frontmatter · title, author, tags, recommendation, source</span></h3>
        <p className="imeta">What is imported is each document as shown. Re-importing a document with the same source URL revises the procedure under the same id rather than duplicating it. Other formats convert to markdown first.</p>
        <div className="toolbar">
          <button disabled={busy} onClick={() => send({ type: "import.choose", folder: false })}>Add files…</button>
          <button disabled={busy} onClick={() => send({ type: "import.choose", folder: true })}>Add a folder…</button>
          <button disabled={busy} title="Re-read every document and run the preview again" onClick={() => send({ type: "import.refresh" })}>Refresh</button>
        </div>
      </section>
      <section className="lsection">
        <h3>Open in the editor <span className="imeta">{editor.length}</span></h3>
        {!editor.length ? <Empty>No markdown or text documents are open. Open one to import it as shown, or add files below.</Empty> : editor.map((r) => <Row key={r.id} r={r} busy={busy} />)}
      </section>
      <section className="lsection">
        <h3>From disk <span className="imeta">{disk.length}</span></h3>
        {!disk.length ? <Empty>No files added.</Empty> : disk.map((r) => <Row key={r.id} r={r} busy={busy} />)}
      </section>
      <div className="foot">
        <button className="primary" disabled={busy || n === 0} onClick={() => send({ type: "import.run" })}>{busy ? "Importing…" : n ? `Import ${n} document${n === 1 ? "" : "s"}` : "Import"}</button>
        <span className="imeta">
          {busy ? "" : done.length ? (done.length - ok ? `${ok} imported, ${done.length - ok} failed — see each row.` : `${ok} imported — consultable now in the Knowledge section of every investigation.`) : n ? "" : "Select the documents to import."}
        </span>
      </div>
    </div>
  );
}
