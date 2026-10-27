// Past cases (09 §9.4, adr/0107): one row per concluded investigation summary,
// newest first, narrowed by technique, disposition and conclusion window. The
// listing is a read over the case-summaries corpus; the detail is the
// similar-case inspector at full width. A concluded investigation joins on its
// own once its summary is written.
import { useState } from "react";
import { useApp } from "../../app";
import { Badge, Empty, fmtDateTime } from "../shared";

export function PastCases() {
  const { state, openInspector, send } = useApp();
  const cases = state.kdoc?.cases;
  // The section's number is the corpus total the host counts, not the filtered rows.
  const total = state.kdoc?.counts.cases ?? (cases ? cases.length : null);
  const [technique, setTechnique] = useState("");
  const [disposition, setDisposition] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const filtered = !!(technique || disposition || from || to);
  const list = () => send({ type: "cases.list", filters: { technique: technique.trim() || undefined, disposition: disposition || undefined, from: from || undefined, to: to || undefined } });
  const clear = () => { setTechnique(""); setDisposition(""); setFrom(""); setTo(""); send({ type: "cases.list" }); };
  const dispositions = Array.from(new Set((cases || []).map((c) => c.verdictDisposition).filter(Boolean) as string[])).sort();
  if (disposition && !dispositions.includes(disposition)) dispositions.push(disposition);
  return (
    <section className="lsection kcases" aria-label="Past cases">
      <h3>Past cases <span className="imeta">{total === null ? "—" : total}</span></h3>
      <form className="kfilters" aria-label="Past case filters" onSubmit={(e) => { e.preventDefault(); list(); }}>
        <input id="caseTechnique" placeholder="technique (T1021.001)" value={technique} onChange={(e) => setTechnique(e.target.value)} aria-label="technique" />
        <select id="caseDisposition" value={disposition} onChange={(e) => setDisposition(e.target.value)} aria-label="disposition">
          <option value="">any disposition</option>
          {dispositions.map((d) => <option key={d} value={d}>{d.toLowerCase()}</option>)}
        </select>
        <input id="caseFrom" type="date" value={from} onChange={(e) => setFrom(e.target.value)} aria-label="concluded from" />
        <input id="caseTo" type="date" value={to} onChange={(e) => setTo(e.target.value)} aria-label="concluded to" />
        <button type="submit">Filter</button>
        {filtered ? <button type="button" onClick={clear}>Clear</button> : null}
      </form>
      {cases === null || cases === undefined ? (
        <Empty>The listing could not be read; Search above still finds cases by similarity.</Empty>
      ) : !cases.length ? <Empty>{filtered ? "no concluded investigation matches these filters" : "no concluded investigations have been summarized yet"}</Empty> : (
        <table className="ktable" aria-label="Past cases">
          <thead><tr><th>Title</th><th>Concluded</th><th>Disposition</th><th>Techniques</th><th>Entities</th><th>Actions</th><th>Duration</th><th>Narrative</th></tr></thead>
          <tbody>
            {cases.map((c) => (
              <tr key={c.investigationRef} className="krow" onClick={() => openInspector({ kind: "case", investigationRef: c.investigationRef })}>
                <td className="ktitle">{c.title}</td>
                <td className="imeta">{c.conclusionAt ? fmtDateTime(c.conclusionAt) : ""}</td>
                <td>{c.verdictDisposition ? <Badge>{c.verdictDisposition}</Badge> : null}</td>
                <td className="mono">{c.techniques.join(", ")}</td>
                <td className="mono">{(c.primaryEntities.length ? c.primaryEntities : [c.seedValue].filter(Boolean)).join(", ")}</td>
                <td className="mono">{c.actionsTaken.map((a) => `${a.actionType} ${a.outcome.toLowerCase()}`).join(" · ")}</td>
                <td className="mono">{c.durationHours ? `${c.durationHours} h` : ""}</td>
                <td className="imeta" title={c.generatorModel ? "a narrative was written at conclusion (" + c.generatorModel + ")" : "the deterministic baseline only"}>{c.generatorModel ? "written" : "baseline"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="imeta">A concluded investigation's summary joins here on its own once the post-conclusion pipeline writes it.</div>
    </section>
  );
}
