// Procedures (09 §9.3): one row per procedure with filters; the detail beside
// the list with the body, the frontmatter block, Versions and Where used;
// Edit (an in-document markdown editor with preview; Save is a new revision
// through PUT /api/sops/{id}), Retire with a reason, Copy link. Retired
// procedures are hidden by default and shown marked when included.
import { useEffect, useMemo, useState } from "react";
import { useApp } from "../../app";
import type { SOPDoc } from "../../../../src/backend";
import { KV, Section, Tabs } from "../inspectors/primitives";
import { Badge, Empty, Markdown, fmtDateTime } from "../shared";
import { useKnowledgeView } from "./KnowledgeApp";

/** "v1.0" when the source system recorded a version, else "rev N" (impl §8.0). */
export function versionLabel(s: SOPDoc): string {
  if (s.source?.version) return "v" + s.source.version;
  return s.revisionCount ? "rev " + s.revisionCount : "rev ?";
}

/** The investigation an accepted candidate came from (09 §9.3), from the source URL. */
export function fromInvestigation(s: SOPDoc): string | null {
  const m = /^reckon:\/\/investigation\/([^/?#]+)/.exec(s.source?.url || "");
  return m ? m[1] : null;
}

function matches(s: SOPDoc, text: string): boolean {
  if (!text) return true;
  const t = text.toLowerCase();
  return s.title.toLowerCase().includes(t) || s.tags.some((x) => x.toLowerCase().includes(t)) || (s.authorId || "").toLowerCase().includes(t);
}

export function Procedures() {
  const { state } = useApp();
  const view = useKnowledgeView();
  const k = state.kdoc;
  const [filter, setFilter] = useState("");
  const [rec, setRec] = useState("");
  const [author, setAuthor] = useState("");
  const [showRetired, setShowRetired] = useState(false);
  const all = k?.sops || [];
  const recs = useMemo(() => Array.from(new Set(all.map((s) => s.recommendation).filter(Boolean))).sort() as string[], [all]);
  const authors = useMemo(() => Array.from(new Set(all.map((s) => s.authorId).filter(Boolean))).sort() as string[], [all]);
  const rows = all.filter((s) => (showRetired || s.status !== "retired") && matches(s, filter) && (!rec || s.recommendation === rec) && (!author || s.authorId === author));
  const selected = view.selected ? all.find((s) => s.id === view.selected) || null : null;
  // A selected row that no longer exists in the list (retired and hidden, or gone) stays open from the detail read.
  const detail = k?.detail && k.detail.id === view.selected ? k.detail : selected;
  return (
    <div className={"ksplit" + (view.selected ? " open" : "")}>
      <section className="lsection kprocs" aria-label="Procedure list">
        <h3>Procedures <span className="imeta">{rows.length} of {all.length}</span></h3>
        <div className="kfilters">
          <input type="text" placeholder="Filter by title, tag, technique or author" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <select value={rec} onChange={(e) => setRec(e.target.value)} title="Recommendation"><option value="">any recommendation</option>{recs.map((r) => <option key={r} value={r}>{r}</option>)}</select>
          <select value={author} onChange={(e) => setAuthor(e.target.value)} title="Author"><option value="">any author</option>{authors.map((a) => <option key={a} value={a}>{a}</option>)}</select>
          <label className="imeta"><input type="checkbox" checked={showRetired} onChange={(e) => setShowRetired(e.target.checked)} /> show retired</label>
        </div>
        {!k ? <Empty>loading…</Empty> : !rows.length ? <Empty>{all.length ? "no procedure matches the filter" : "no procedures yet — import some, or accept a candidate"}</Empty> : (
          <table className="ktable" aria-label="Procedures">
            <thead><tr><th>Title</th><th>Tags</th><th>Recommends</th><th>Version</th><th>Status</th><th>Author</th><th>Updated</th></tr></thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id} className={"krow" + (s.id === view.selected ? " current" : "") + (s.status === "retired" ? " retired" : "")} onClick={() => view.select(s.id === view.selected ? null : s.id)}>
                  <td className="ktitle">{s.title}{fromInvestigation(s) ? <Badge tone="info" title="Accepted from a candidate drafted at that investigation's conclusion">from an investigation</Badge> : null}</td>
                  <td className="mono ktags">{s.tags.join(", ")}</td>
                  <td className="mono">{s.recommendation || ""}</td>
                  <td className="mono">{versionLabel(s)}</td>
                  <td><Badge tone={s.status === "published" ? "ok" : s.status === "retired" ? "muted" : "warn"}>{s.status}</Badge></td>
                  <td>{s.authorId || ""}</td>
                  <td className="imeta">{fmtDateTime(s.updatedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
      {view.selected ? (
        <section className="lsection kdetail" aria-label="Procedure detail">
          {detail ? (view.editing ? <ProcedureEditor sop={detail} /> : <ProcedureDetail sop={detail} />) : <Empty>reading the procedure…</Empty>}
        </section>
      ) : null}
    </div>
  );
}

function ProcedureDetail({ sop }: { sop: SOPDoc }) {
  const { send, openPrompt, openInspector } = useApp();
  const view = useKnowledgeView();
  const inv = fromInvestigation(sop);
  const retire = () => openPrompt({
    title: "Retire procedure",
    label: `Why is “${sop.title}” retired? The reason is recorded with the retirement; the content stays readable with “show retired”.`,
    placeholder: "e.g. superseded by the new containment runbook",
    confirm: "Retire",
    onConfirm: (reason) => send({ type: "sop.retire", sopId: sop.id, reason }),
  });
  return (
    <>
      <div className="kdhead">
        <h3 className="kdtitle">{sop.title}</h3>
        <span className="headcontrols">
          {sop.status !== "retired" ? <button onClick={() => view.setEditing(true)}>Edit</button> : null}
          {sop.status !== "retired" ? <button onClick={retire}>Retire…</button> : null}
          <button title="Copy a link to this procedure" onClick={() => send({ type: "copy", text: "reckon://procedure/" + sop.id })}>Copy link</button>
          <button className="iconbtn" title="Close" onClick={() => view.select(null)}>✕</button>
        </span>
      </div>
      <div className="isub">
        <Badge tone={sop.status === "published" ? "ok" : sop.status === "retired" ? "muted" : "warn"}>{sop.status}</Badge>
        {" "}{versionLabel(sop)}{sop.recommendation ? <> · recommends <span className="mono">{sop.recommendation}</span></> : null}
        {sop.updatedAt ? <> · updated {fmtDateTime(sop.updatedAt)}</> : null}
        {sop.status === "retired" ? <div className="kretired">Retired{sop.retiredAt ? " " + fmtDateTime(sop.retiredAt) : ""}{sop.retiredBy ? " by " + sop.retiredBy : ""}{sop.retiredReason ? <>: <em>{sop.retiredReason}</em></> : " · no reason recorded"}</div> : null}
      </div>
      <Tabs tabs={[
        { id: "doc", label: "Document", body: () => (
          <>
            <Section title="Frontmatter">
              <KV rows={[
                { k: "Author", v: sop.authorId },
                { k: "Tags", v: sop.tags.join(", ") || undefined, mono: true },
                { k: "Recommendation", v: sop.recommendation, mono: true },
                { k: "Source", v: sop.source ? <>{sop.source.system ? sop.source.system + " · " : ""}{inv ? <a onClick={() => send({ type: "investigation.open", id: inv })}>{sop.source.url}</a> : <span className="mono">{sop.source.url}</span>}</> : undefined },
                { k: "Version", v: sop.source?.version, mono: true },
                { k: "Imported", v: sop.importedBy ? `by ${sop.importedBy}${sop.importedAt ? " · " + fmtDateTime(sop.importedAt) : ""}` : undefined },
                { k: "Id", v: sop.id, mono: true },
              ]} />
            </Section>
            <Markdown text={sop.body} className="idoc kbody" />
          </>
        ) },
        { id: "versions", label: `Versions · ${sop.revisionCount ?? "?"}`, body: () => (
          <>
            <div className="irow">rev {sop.revisionCount ?? "?"} · current{sop.updatedAt ? " · " + fmtDateTime(sop.updatedAt) : ""}{sop.source?.version ? ` · the source's v${sop.source.version}` : ""}</div>
            <Empty>Each revision with its time and who made it, and comparing two, land with gap 00 §6.15 (the revisions endpoint).</Empty>
          </>
        ) },
        { id: "used", label: "Where used", body: () => <Empty>Which investigations consulted or applied this procedure is gap 00 §6.22 (the citation projector).</Empty> },
      ]} />
      <div className="decide"><button onClick={() => openInspector({ kind: "sop", sopId: sop.id })}>Open in the inspector</button></div>
    </>
  );
}

/** The in-document markdown editor with preview (09 §9.3; 13 §3: never the
 * quick input). Save is a new revision under the same id. */
function ProcedureEditor({ sop }: { sop: SOPDoc }) {
  const { send } = useApp();
  const view = useKnowledgeView();
  const [title, setTitle] = useState(sop.title);
  const [tags, setTags] = useState(sop.tags.join(", "));
  const [rec, setRec] = useState(sop.recommendation || "");
  const [body, setBody] = useState(sop.body);
  const [saving, setSaving] = useState(false);
  // A fresh detail after the save closes the editor.
  useEffect(() => { if (saving) { setSaving(false); view.setEditing(false); } /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [sop]);
  const dirty = title !== sop.title || body !== sop.body || tags !== sop.tags.join(", ") || rec !== (sop.recommendation || "");
  const save = () => {
    if (!title.trim() || !body.trim()) return;
    setSaving(true);
    send({ type: "sop.save", sopId: sop.id, title: title.trim(), body, tags: tags.split(/[,\s]+/).map((t) => t.trim()).filter(Boolean), recommendation: rec.trim() || undefined });
  };
  return (
    <div className="keditor" aria-label="Procedure editor">
      <div className="kdhead">
        <input className="titleedit" value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" />
        <span className="headcontrols">
          <button className="primary" disabled={!dirty || saving || !title.trim() || !body.trim()} onClick={save}>{saving ? "Saving…" : "Save as a new revision"}</button>
          <button onClick={() => view.setEditing(false)}>Cancel</button>
        </span>
      </div>
      <div className="kfields">
        <label>Tags <input type="text" value={tags} onChange={(e) => setTags(e.target.value)} placeholder="comma-separated; techniques are tags" /></label>
        <label>Recommendation <input type="text" value={rec} onChange={(e) => setRec(e.target.value)} placeholder="e.g. isolate" /></label>
      </div>
      <div className="kpane">
        <textarea className="kmd" value={body} onChange={(e) => setBody(e.target.value)} spellCheck={false} aria-label="Markdown" />
        <div className="kpreview"><div className="kicker">Preview</div><Markdown text={body} className="idoc kbody" /></div>
      </div>
      <div className="imeta">Saving writes revision {(sop.revisionCount ?? 0) + 1}; the prior content stays hash-addressable. The source pointer and the import attribution are carried forward.</div>
    </div>
  );
}
