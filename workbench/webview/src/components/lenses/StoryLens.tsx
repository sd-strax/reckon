// Story — event time (08 §8.4): everything here is in event time;
// investigation time appears once, as the divider where it started. The
// timeline rows are the events retrieved for the investigation (adr/0092),
// each naming the call that retrieved it, with the cited refs that carry an
// event time joined in; on a record without membership (events persisted
// before it) the cited events alone remain. The attack path, the generated
// sentences and the baseline rows are gaps §6.12 and §6.14.
import { useMemo, useState } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import type { EventRow } from "../../../../src/viewTypes";
import { EventStrip, EvidenceGraph } from "../DerivedViews";
import { Empty, RefChip, fmtDateTime } from "../shared";

const CATEGORY: Record<string, string> = { Authentication: "auth", "Process Activity": "process", "Network Activity": "network", "DNS Activity": "network", "Detection Finding": "detection", "Security Finding": "detection", "observed-data": "observed" };

/** An OCSF category from the class: the thousands digit of class_uid (3002 Authentication is IAM, 3). */
export function categoryOfClass(classUid: number): string {
  switch (Math.floor(classUid / 1000)) {
    case 1: return "process";
    case 2: return "detection";
    case 3: return "auth";
    case 4: return "network";
    default: return "other";
  }
}

interface TimelineItem {
  ref: string;
  time: string;
  label: string;
  category: string;
  /** The call that retrieved the event (adr/0092); absent on a cited ref the record holds no membership for. */
  callId?: string;
  verb?: string;
}

/** The retrieved events joined with the cited refs that carry an event time, in event time, each ref once. */
export function timelineItems(events: EventRow[] | null | undefined, strip: { ref: string; time: string; label: string }[]): TimelineItem[] {
  const out = new Map<string, TimelineItem>();
  for (const e of events ?? []) {
    out.set(e.id, { ref: e.id, time: e.time, label: e.className, category: categoryOfClass(e.classUid), callId: e.callId, verb: e.verb });
  }
  for (const it of strip) {
    if (!out.has(it.ref)) out.set(it.ref, { ref: it.ref, time: it.time, label: it.label, category: CATEGORY[it.label] || "other" });
  }
  return [...out.values()].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
}

export function StoryLens() {
  const { state, openInspector } = useApp();
  const snap = state.snap;
  const [filter, setFilter] = useState<"all" | "auth" | "process" | "network" | "detection">("all");
  const hs = snap?.hypotheses || [];
  const rows = useMemo(() => {
    // Citing predictions: whose test results include the ref.
    return timelineItems(snap?.events, snap?.derived?.strip || []).map((it) => {
      const citing = hs.flatMap((h) => h.predictions.filter((p) => (p.testResultRefs || []).includes(it.ref)).map((p) => ({ h, p })));
      const emphasis = citing.some(({ h, p }) => p.status === "CONFIRMED" && h.status === "SUPPORTED") ? "emphasized" : citing.some(({ p }) => p.status === "DISCONFIRMED") ? "against" : "plain";
      return { ...it, citing, emphasis };
    });
  }, [snap?.events, snap?.derived?.strip, hs]);
  if (!snap) return null;
  const techniques = Array.from(new Set(hs.flatMap((h) => h.labels || []).filter((l) => /^T\d{4}/.test(l))));
  const open = hs.flatMap((h) => h.status === "ABANDONED" ? [] : h.predictions.filter((p) => p.status === "UNTESTED" || p.status === "INCONCLUSIVE").map((p) => ({ h, p })));
  const created = snap.investigation.createdAt ? Date.parse(snap.investigation.createdAt) : NaN;
  const shown = rows.filter((r) => filter === "all" || r.category === filter);
  let dividerDone = false;
  return (
    <div className="lens story">
      <section className="lsection">
        <h3>Evidence graph</h3>
        {snap.derived ? <EvidenceGraph graph={snap.derived.graph} /> : <Empty>Building the graph from the cited evidence…</Empty>}
      </section>
      <section className="lsection">
        <h3>ATT&CK</h3>
        {techniques.length ? techniques.map((t) => <span key={t} className="technique mono">{t}</span>) : <Empty>no technique labels on the hypotheses yet</Empty>}
      </section>
      <section className="lsection">
        <h3>Timeline <span className="imeta">event time</span></h3>
        <div className="filters" role="tablist">
          {(["all", "auth", "process", "network", "detection"] as const).map((f) => <button key={f} role="tab" aria-selected={filter === f} className={"itab" + (filter === f ? " on" : "")} onClick={() => setFilter(f)}>{f === "all" ? "All" : f[0].toUpperCase() + f.slice(1)}</button>)}
        </div>
        {snap.derived ? <EventStrip items={snap.derived.strip} /> : null}
        {snap.events?.length ? <div className="imeta">{snap.events.length} retrieved event{snap.events.length === 1 ? "" : "s"} in the record</div> : null}
        {!shown.length ? <Empty>{snap.events?.length ? "No retrieved or cited event in this category." : "No cited events carry an event time yet."}</Empty> : (
          <ol className="timeline">
            {shown.map((r, i) => {
              const t = Date.parse(r.time);
              const divider = !dividerDone && Number.isFinite(created) && t >= created;
              if (divider) dividerDone = true;
              return (
                <li key={r.ref + i} className={"trow " + r.emphasis}>
                  {divider ? <div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div> : null}
                  <span className="ttime mono">{new Date(r.time).toLocaleTimeString()}</span>
                  <span className={"tcat " + r.category}>{r.category.toUpperCase()}</span>
                  <span className="tsentence">
                    <RefChip refId={r.ref} /> <span className="imeta">{r.label}</span>
                    {r.callId ? <> <a className="imeta tvia" title="Open the capability call that retrieved this event" onClick={() => openInspector({ kind: "call", callId: r.callId! })}>via {r.verb}</a></> : null}
                  </span>
                  <span className="tciting">{r.citing.map(({ h, p }) => <a key={p.id} className={"pcite " + (p.status === "CONFIRMED" ? "ok" : p.status === "DISCONFIRMED" ? "bad" : "")} title={`${p.status} under ${hypName(h, hs)}`} onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a>)}</span>
                </li>
              );
            })}
            {!dividerDone && Number.isFinite(created) ? <li className="trow"><div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div></li> : null}
          </ol>
        )}
      </section>
      <section className="lsection">
        <h3>Not yet known</h3>
        {open.length ? open.map(({ h, p }) => <div key={p.id}>? <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a> under {hypName(h, hs)}: {p.statement} <span className="imeta">({p.status.toLowerCase()})</span></div>) : <Empty>every prediction is decided</Empty>}
      </section>
    </div>
  );
}
