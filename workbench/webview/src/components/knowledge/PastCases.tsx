// Past cases (09 §9.4): one row per concluded investigation summary. The
// listing is gap 00 §6.21 (recall needs a query; listing does not exist), so
// the section frame carries the gap line until it lands; Search finds cases
// by similarity today, and the detail is the Similar-case inspector.
import { useApp } from "../../app";
import { Badge, Empty, fmtDateTime } from "../shared";

export function PastCases() {
  const { state, openInspector, send } = useApp();
  const cases = state.kdoc?.cases;
  return (
    <section className="lsection kcases" aria-label="Past cases">
      <h3>Past cases <span className="imeta">{cases ? cases.length : "—"}</span></h3>
      {cases === null || cases === undefined ? (
        <Empty>Listing concluded investigations lands with gap 00 §6.21 (recall needs a query; a listing does not exist yet). Search above finds them by similarity today; each opens as a similar-case inspector.</Empty>
      ) : !cases.length ? <Empty>no concluded investigations have been summarized yet</Empty> : (
        <table className="ktable" aria-label="Past cases">
          <thead><tr><th>Title</th><th>Concluded</th><th>Disposition</th><th>Techniques</th><th>Entities</th><th>Actions</th><th>Duration</th></tr></thead>
          <tbody>
            {cases.map((c) => (
              <tr key={c.investigationRef} className="krow" onClick={() => openInspector({ kind: "case", investigationRef: c.investigationRef })}>
                <td className="ktitle">{c.title}</td>
                <td className="imeta">{fmtDateTime(c.conclusionAt)}</td>
                <td>{c.verdictDisposition ? <Badge>{c.verdictDisposition}</Badge> : null}</td>
                <td className="mono">{c.techniques.join(", ")}</td>
                <td className="mono">{c.primaryEntities.join(", ")}</td>
                <td className="mono">{c.actionsTaken.map((a) => `${a.actionType} ${a.outcome.toLowerCase()}`).join(" · ")}</td>
                <td className="mono">{c.durationHours ? `${c.durationHours} h` : ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="imeta">A concluded investigation's summary joins here on its own; <a onClick={() => send({ type: "cases.list" })}>ask the host</a> re-reads the listing.</div>
    </section>
  );
}
