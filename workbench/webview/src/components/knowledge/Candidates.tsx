// Candidates (09 §9.7, adr/0112): procedures the agent drafted at an
// investigation's conclusion, stored for review. The list carries each draft's
// status; the detail is the draft as an editable body with the closest existing
// procedure beside it; Add to library imports the edited body through the
// procedure import path with a reckon://investigation/<id> source URL (a second
// accept revises); Discard takes a reason. Nothing is added without an accept.
import { useEffect, useState } from "react";
import { useApp } from "../../app";
import { KV, Section } from "../inspectors/primitives";
import { Badge, Empty, Markdown, fmtDateTime } from "../shared";

export function Candidates() {
  const { state, send, openPrompt, openInspector } = useApp();
  const k = state.kdoc;
  const rows = k?.candidates;
  const [status, setStatus] = useState<string>("NEW");
  const [selected, setSelected] = useState<string | null>(null);
  const [draft, setDraft] = useState<string>("");
  const detail = k?.candidateDetail && k.candidateDetail.id === selected ? k.candidateDetail : null;
  useEffect(() => { if (detail) setDraft(detail.body); }, [detail?.id, detail?.body]);
  const shown = (rows || []).filter((c) => !status || c.status === status);
  const open = (id: string) => { setSelected(id); send({ type: "candidate.get", id }); };
  const total = k?.counts.candidates;
  return (
    <section className="lsection kcands" aria-label="Candidates">
      <h3>Candidates <span className="imeta">{total === null || total === undefined ? "—" : total}</span></h3>
      <div className="kfilters">
        {(["NEW", "ACCEPTED", "DISCARDED", ""] as const).map((st) => <button key={st || "all"} className={status === st ? "on" : undefined} onClick={() => setStatus(st)}>{st ? st.toLowerCase() : "all"}</button>)}
      </div>
      {rows === null || rows === undefined ? <Empty>The candidates could not be read.</Empty>
        : !shown.length ? <Empty>{status === "NEW" ? "no candidate is waiting for review; one is drafted when an investigation concludes and the record warrants a procedure" : "no candidate in this state"}</Empty> : (
          <table className="ktable" aria-label="Candidates">
            <thead><tr><th>Title</th><th>Drafted from</th><th>Drafted</th><th>Status</th><th>Decided</th></tr></thead>
            <tbody>
              {shown.map((c) => (
                <tr key={c.id} className={"krow" + (c.id === selected ? " current" : "")} onClick={() => open(c.id)}>
                  <td className="ktitle">{c.title}</td>
                  <td><a onClick={(e) => { e.stopPropagation(); send({ type: "investigation.open", id: c.investigationId }); }}>the investigation</a></td>
                  <td className="imeta">{fmtDateTime(c.draftedAt)}{c.draftedByName ? " · on behalf of " + c.draftedByName : ""}</td>
                  <td><Badge tone={c.status === "ACCEPTED" ? "ok" : c.status === "DISCARDED" ? "muted" : "info"}>{c.status}</Badge></td>
                  <td className="imeta">{c.decidedAt ? `${c.decidedByName || c.decidedBy || ""} · ${fmtDateTime(c.decidedAt)}${c.discardReason ? " · " + c.discardReason : ""}` : ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      {selected ? (
        <section className="lsection kdetail" aria-label="Candidate detail">
          {!detail ? <Empty>reading the candidate…</Empty> : (
            <>
              <div className="kdhead"><h3 className="kdtitle">{detail.title}</h3><Badge tone={detail.status === "ACCEPTED" ? "ok" : detail.status === "DISCARDED" ? "muted" : "info"}>{detail.status}</Badge></div>
              <Section title="Draft">
                <KV rows={[
                  { k: "Tags", v: detail.tags.join(", ") || undefined, mono: true },
                  { k: "Recommends", v: detail.recommendation, mono: true },
                  { k: "Why it was drafted", v: detail.rationale },
                  { k: "Drafted", v: `${fmtDateTime(detail.draftedAt)}${detail.draftedByName ? " · on behalf of " + detail.draftedByName : ""}${detail.generatorModel ? " · " + detail.generatorModel : ""}` },
                  ...(detail.acceptedSopId ? [{ k: "Procedure", v: "accepted into the library" }] : []),
                ]} />
                {detail.status === "NEW" || detail.status === "ACCEPTED"
                  ? <textarea aria-label="Candidate body" className="keditor" value={draft} onChange={(e) => setDraft(e.target.value)} rows={14} />
                  : <Markdown text={detail.body} className="idoc kbody" />}
              </Section>
              <Section title="Closest existing procedure">
                {detail.closest ? <div className="irow"><a onClick={() => openInspector({ kind: "sop", sopId: detail.closest!.sopId })}>{detail.closest.title}</a> <span className="imeta">· {detail.closest.score.toFixed(2)}{detail.closest.rationale ? " · " + detail.closest.rationale : ""}</span>{detail.closest.excerpt ? <blockquote className="iquote">{detail.closest.excerpt}</blockquote> : null}</div> : <div className="empty">none found: the library holds nothing like it</div>}
              </Section>
              <div className="decide">
                {detail.status !== "DISCARDED" ? <button className="primary" disabled={!draft.trim()} onClick={() => send({ type: "candidate.accept", id: detail.id, body: draft })}>{detail.status === "ACCEPTED" ? "Revise in library" : "Add to library"}</button> : null}
                {detail.status === "NEW" ? <button onClick={() => openPrompt({ title: "Discard the candidate", label: "Why: the reason stays on the record", confirm: "Discard", onConfirm: (reason) => send({ type: "candidate.discard", id: detail.id, reason }) })}>Discard…</button> : null}
                {detail.acceptedSopId ? <button onClick={() => openInspector({ kind: "sop", sopId: detail.acceptedSopId! })}>Open the procedure</button> : null}
              </div>
            </>
          )}
        </section>
      ) : null}
    </section>
  );
}
