// Story — event time (08 §8.4): everything here is in event time;
// investigation time appears once, as the divider where it started. The
// attack path and the generated timeline rows need gaps §6.12–§6.14; until
// they land the lens shows what the record backs: the cited events in event
// time with their citing predictions, the ATT&CK labels, the evidence graph
// and the open questions.
import { useMemo, useState } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { EventStrip, EvidenceGraph } from "../DerivedViews";
import { Empty, RefChip, fmtDateTime } from "../shared";

const CATEGORY: Record<string, string> = { Authentication: "auth", "Process Activity": "process", "Network Activity": "network", "DNS Activity": "network", "Detection Finding": "detection", "Security Finding": "detection", "observed-data": "observed" };

export function StoryLens() {
  const { state, openInspector } = useApp();
  const snap = state.snap;
  const [filter, setFilter] = useState<"all" | "auth" | "process" | "network" | "detection">("all");
  const hs = snap?.hypotheses || [];
  const rows = useMemo(() => {
    const strip = snap?.derived?.strip || [];
    // Citing predictions: whose test results include the ref.
    return strip.map((it) => {
      const citing = hs.flatMap((h) => h.predictions.filter((p) => (p.testResultRefs || []).includes(it.ref)).map((p) => ({ h, p })));
      const emphasis = citing.some(({ h, p }) => p.status === "CONFIRMED" && h.status === "SUPPORTED") ? "emphasized" : citing.some(({ p }) => p.status === "DISCONFIRMED") ? "against" : "plain";
      return { ...it, category: CATEGORY[it.label] || "other", citing, emphasis };
    });
  }, [snap?.derived?.strip, hs]);
  if (!snap) return null;
  const techniques = Array.from(new Set(hs.flatMap((h) => h.labels || []).filter((l) => /^T\d{4}/.test(l))));
  const supported = hs.some((h) => h.status === "SUPPORTED");
  const open = hs.flatMap((h) => h.status === "ABANDONED" ? [] : h.predictions.filter((p) => p.status === "UNTESTED" || p.status === "INCONCLUSIVE").map((p) => ({ h, p })));
  const created = snap.investigation.createdAt ? Date.parse(snap.investigation.createdAt) : NaN;
  const shown = rows.filter((r) => filter === "all" || r.category === filter);
  let dividerDone = false;
  return (
    <div className="lens story">
      <section className="lsection">
        <h3>Attack path</h3>
        {supported ? <Empty>The attack path (entities as nodes, typed relations between them) needs the entity-scoped event query — gap 00 §6.13. The evidence graph below shows what grounds what.</Empty> : <Empty>No hypothesis is SUPPORTED yet, so there is no attack path to show.</Empty>}
        {snap.derived ? <EvidenceGraph graph={snap.derived.graph} /> : null}
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
        {!shown.length ? <Empty>No cited events carry an event time yet.</Empty> : (
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
                  <span className="tsentence"><RefChip refId={r.ref} /> <span className="imeta">{r.label} (the per-class sentence is gap 00 §6.12)</span></span>
                  <span className="tciting">{r.citing.map(({ h, p }) => <a key={p.id} className={"pcite " + (p.status === "CONFIRMED" ? "ok" : p.status === "DISCONFIRMED" ? "bad" : "")} title={`${p.status} under ${hypName(h, hs)}`} onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a>)}</span>
                </li>
              );
            })}
            {!dividerDone && Number.isFinite(created) ? <li className="trow"><div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div></li> : null}
          </ol>
        )}
        <p className="imeta">Baseline rows (collapsed repetitions) need gap 00 §6.14; the full entity-scoped record needs gap 00 §6.13.</p>
      </section>
      <section className="lsection">
        <h3>Not yet known</h3>
        {open.length ? open.map(({ h, p }) => <div key={p.id}>? <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a> under {hypName(h, hs)}: {p.statement} <span className="imeta">({p.status.toLowerCase()})</span></div>) : <Empty>every prediction is decided</Empty>}
      </section>
    </div>
  );
}
