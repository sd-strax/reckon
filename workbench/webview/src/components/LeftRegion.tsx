// The left region (08 §8.3; impl §2.6): the work queue (investigations
// needs-me-first with their stage step, then the case system's queue filtered
// to cases with no investigation), and the capability health fold at the
// bottom (13 §4 puts it in the container; v1).
import { useState } from "react";
import { useApp } from "../app";
import { STEP_LABELS, rowStep } from "../derive/stage";
import { Countdown, Empty } from "./shared";
import type { InvestigationSummary } from "../../../src/viewTypes";

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

export function LeftRegion() {
  const { state, send } = useApp();
  const [collapsed, setCollapsed] = useState(false);
  const q = state.queue;
  const current = state.snap?.investigation.id;
  const investigations = orderQueue((q?.investigations || []).filter((r) => !["CONCLUDED", "ARCHIVED", "DISCARDED"].includes(String(r.state).toUpperCase())));
  // Concluded investigations stay findable (#158): reopening one is how a case comes back.
  const concluded = (q?.investigations || []).filter((r) => String(r.state).toUpperCase() === "CONCLUDED");
  const cases = q?.cases || [];
  return (
    <nav id="leftRegion" className={collapsed ? "collapsed" : ""} aria-label="Work queue">
      <div className="regionhead">
        <button className="iconbtn" title={collapsed ? "Expand the work queue" : "Collapse the work queue"} onClick={() => setCollapsed((c) => !c)}>{collapsed ? "»" : "«"}</button>
      </div>
      {!collapsed ? (
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
            {concluded.length ? (
              <details className="concludedfold">
                <summary>Concluded<span className="count">{concluded.length}</span></summary>
                {concluded.map((r) => (
                  <div key={r.id} className={"qrow concluded" + (r.id === current ? " current" : "")} onClick={() => { if (r.id !== current) send({ type: "investigation.open", id: r.id, title: r.title }); }} title={r.seedSummary || r.title}>
                    <div className="qtitle">{r.title}</div>
                    {r.seed?.type === "case" && r.seed.caseId ? <div className="qcase">from {r.seed.caseId}</div> : null}
                  </div>
                ))}
              </details>
            ) : null}
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
            {q && cases.length ? <div className="imeta">Cases with an investigation — open or concluded — are listed above, not here.</div> : null}
          </section>
          <CapabilityHealth />
        </>
      ) : null}
    </nav>
  );
}
