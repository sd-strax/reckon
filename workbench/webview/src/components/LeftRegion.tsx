// The left region (08 §8.3, 09 §9.1; impl §2.6): the Work | Knowledge toggle,
// the work queue (investigations needs-me-first with their stage step, then
// the case system's queue filtered to cases with no investigation), the
// capability health fold at the bottom (13 §4 puts it in the container; v1),
// and — in the Knowledge document — the four sections with their counts.
//
// The toggle is a navigation act, never a content swap: in an investigation
// document, Knowledge reveals the Knowledge document (`knowledge.open`); in
// the Knowledge document, Work lists the queue here and a row opens its
// investigation. Each document's main area is fixed.
import { useState } from "react";
import { useApp } from "../app";
import { STEP_LABELS, rowStep } from "../derive/stage";
import { Countdown, Empty } from "./shared";
import type { InvestigationSummary } from "../../../src/backend";
import type { KnowledgeSection } from "../../../src/protocol";

/** Needs-me-first (ui/06): soonest expiry, more pending, then recency. */
export function orderQueue(rows: InvestigationSummary[]): InvestigationSummary[] {
  return rows.slice().sort((a, b) => {
    const ax = a.nearestExpiry ? Date.parse(a.nearestExpiry) : Infinity;
    const bx = b.nearestExpiry ? Date.parse(b.nearestExpiry) : Infinity;
    if (ax !== bx) return ax - bx;
    if (a.pendingActions !== b.pendingActions) return b.pendingActions - a.pendingActions;
    return (b.updatedAt ? Date.parse(b.updatedAt) : 0) - (a.updatedAt ? Date.parse(a.updatedAt) : 0);
  });
}

function CapabilityHealth() {
  const { state, openEnableForm } = useApp();
  const caps = state.snap ? state.snap.capabilities : undefined;
  const enablement = state.snap?.enablement;
  let head = "Capabilities";
  let body: React.ReactNode;
  if (caps === undefined) body = <Empty>…</Empty>;
  else if (caps === null) { head = "Capabilities · off"; body = <Empty>capability layer off</Empty>; }
  else if (!caps.length) body = <Empty>none configured</Empty>;
  else {
    const avail = caps.filter((c) => c.status === "available").length;
    head = `Capabilities · ${avail}/${caps.length} available`;
    const order: Record<string, number> = { available: 0, degraded: 1, unavailable: 2 };
    body = caps.slice().sort((a, b) => (order[a.status] ?? 3) - (order[b.status] ?? 3) || a.verb.localeCompare(b.verb)).map((c) => {
      let setup: React.ReactNode = null;
      if (c.status === "unavailable" && enablement) {
        const ev = (enablement.verbs || []).find((x) => x.verb === c.verb);
        if (ev && !ev.enabled && (ev.closableBy || []).length) {
          setup = <button className="setup" title={ev.closableBy[0] + " is installed but not enabled — enable it to serve " + c.verb} onClick={() => openEnableForm(ev.closableBy[0])}>set up</button>;
        }
      }
      return <div key={c.verb} className={"caprow " + c.status}><span className={"dot " + c.status} title={c.status} /><span className="verb">{c.verb}</span>{setup}</div>;
    });
  }
  return <details className="railfold" id="capsFold"><summary>{head}</summary><div id="caps">{body}</div></details>;
}

/** The work queue (08 §8.3): the analyst's open investigations and the case
 * system's queue, as the host posts them. */
function WorkQueue() {
  const { surface, state, send } = useApp();
  const q = state.queue;
  const current = state.snap?.investigation.id;
  const investigations = orderQueue((q?.investigations || []).filter((r) => !["CONCLUDED", "ARCHIVED"].includes(String(r.state).toUpperCase())));
  const cases = q?.cases || [];
  return (
    <>
      <section className="queue">
        <h2>Investigations{q ? <span className="count">{investigations.length}</span> : null}</h2>
        {!q ? <Empty>loading…</Empty> : !investigations.length ? <Empty>none open</Empty> : investigations.map((r) => {
          const step = rowStep(r);
          return (
            <div key={r.id} className={"qrow" + (r.id === current ? " current" : "")} onClick={() => { if (r.id !== current) send({ type: "investigation.open", id: r.id, title: r.title }); }} title={r.seedSummary || r.title}>
              <div className="qtop">
                {step ? <span className={"qstep " + step}>{STEP_LABELS[step]}</span> : null}
                {r.nearestExpiry ? <Countdown expiresAt={r.nearestExpiry} className="qexpiry" /> : r.pendingActions ? <span className="qexpiry">{r.pendingActions} to approve</span> : null}
              </div>
              <div className="qtitle">{r.title}</div>
              {r.seed?.type === "case" && r.seed.caseId ? <div className="qcase">from {r.seed.caseId}</div> : null}
            </div>
          );
        })}
      </section>
      <section className="queue">
        <h2>Cases{q?.caseSource ? <span className="qsource"> · {q.caseSource}</span> : null}{q ? <span className="count">{cases.length}</span> : null}</h2>
        {!q ? null : !cases.length ? <Empty>no cases without an investigation</Empty> : cases.map((c) => (
          <div key={c.number} className="qrow case">
            <div className="qtop"><span className="mono">{c.number}</span>{c.priority ? <span className="qexpiry">{c.priority}</span> : null}</div>
            <div className="qtitle">{c.title || c.summary || ""}</div>
            <button className="qstart" onClick={() => send({ type: "queue.start", caseNumber: c.number })}>Start investigation</button>
          </div>
        ))}
        {q && cases.length ? <div className="imeta">Cases already under investigation are listed above, not here.</div> : null}
      </section>
      {surface === "investigation" ? <CapabilityHealth /> : null}
    </>
  );
}

export interface KnowledgeNav {
  section: KnowledgeSection;
  setSection: (s: KnowledgeSection) => void;
}

const SECTION_LABELS: Record<KnowledgeSection, string> = { procedures: "Procedures", cases: "Past cases", import: "Import", candidates: "Candidates" };

/** The Knowledge document's sections with their counts (09 §9.1): listed
 * once, here; a count the record cannot give yet shows as a gap mark. */
function KnowledgeSections({ nav }: { nav: KnowledgeNav }) {
  const { state } = useApp();
  const k = state.kdoc;
  const counts: Record<KnowledgeSection, string> = {
    procedures: k ? String(k.counts.procedures) : "…",
    cases: k?.counts.cases === null || k?.counts.cases === undefined ? "—" : String(k.counts.cases),
    import: k ? String(k.importRows.length) : "…",
    candidates: k?.counts.candidates === null || k?.counts.candidates === undefined ? "—" : String(k.counts.candidates),
  };
  const titles: Record<KnowledgeSection, string> = {
    procedures: "The procedure corpus",
    cases: "Concluded investigations' summaries; the listing is gap 00 §6.21",
    import: "Documents chosen for import",
    candidates: "Procedures drafted at conclusion; gap 00 §6.24",
  };
  return (
    <section className="queue ksections" aria-label="Knowledge sections">
      <h2>Knowledge</h2>
      {(Object.keys(SECTION_LABELS) as KnowledgeSection[]).map((s) => (
        <div key={s} className={"qrow ksec" + (nav.section === s ? " current" : "")} role="button" aria-pressed={nav.section === s} title={titles[s]} onClick={() => nav.setSection(s)}>
          <span className="qtitle">{SECTION_LABELS[s]}</span><span className="count">{counts[s]}</span>
        </div>
      ))}
    </section>
  );
}

export function LeftRegion({ knowledge }: { knowledge?: KnowledgeNav }) {
  const { surface, state, send } = useApp();
  const [collapsed, setCollapsed] = useState(false);
  // In the Knowledge document the region lists the sections by default and
  // the queue on Work; in an investigation it always lists the queue.
  const [region, setRegionState] = useState<"work" | "knowledge">(surface === "knowledge" ? "knowledge" : "work");
  const setRegion = (r: "work" | "knowledge") => {
    if (surface === "investigation") {
      if (r === "knowledge") send({ type: "knowledge.open" });
      return;
    }
    setRegionState(r);
    if (r === "work" && !state.queue) send({ type: "queue.get" });
  };
  const showing = surface === "investigation" ? "work" : region;
  return (
    <nav id="leftRegion" className={collapsed ? "collapsed" : ""} aria-label={showing === "work" ? "Work queue" : "Knowledge sections"}>
      <div className="regionhead">
        <div className="modetoggle" role="tablist">
          <button role="tab" aria-selected={showing === "work"} className={showing === "work" ? "on" : ""} onClick={() => setRegion("work")}>Work</button>
          <button role="tab" aria-selected={showing === "knowledge"} className={showing === "knowledge" ? "on" : ""} title={surface === "investigation" ? "Open the Knowledge view" : "The Knowledge sections"} onClick={() => setRegion("knowledge")}>Knowledge</button>
        </div>
        <button className="iconbtn" title={collapsed ? "Expand" : "Collapse"} onClick={() => setCollapsed((c) => !c)}>{collapsed ? "»" : "«"}</button>
      </div>
      {!collapsed ? (showing === "knowledge" && knowledge ? <KnowledgeSections nav={knowledge} /> : <WorkQueue />) : null}
    </nav>
  );
}
