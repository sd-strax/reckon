// Story — event time (08 §8.4): everything here is in event time;
// investigation time appears once, as the divider where it started. The
// attack path and the generated timeline rows need gaps §6.13–§6.14; until
// they land the lens shows what the record backs: the cited events in event
// time as their per-class sentences (adr/0090) with their citing predictions,
// the ATT&CK labels, the evidence graph and the open questions.
import { useMemo, useState } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { EventStrip, EvidenceGraph } from "../DerivedViews";
import { Empty, RefChip, fmtDateTime } from "../shared";
import { EventSentence } from "../EventSentence";
import { attackPath } from "../../derive/attackPath";
import { collapsePatterns, patternSentence } from "../../derive/baseline";

/** The category of a STIX object cited in event time (an OCSF record carries its own, adr/0090). */
const CATEGORY: Record<string, string> = { "observed-data": "observed" };

function fmtSpanMs(ms: number): string {
  if (ms < 60_000) return Math.round(ms / 1000) + " s";
  if (ms < 3_600_000) return Math.round(ms / 60_000) + " min";
  return (ms / 3_600_000).toFixed(1) + " h";
}

export function StoryLens() {
  const { state, openInspector } = useApp();
  const snap = state.snap;
  const [filter, setFilter] = useState<"all" | "auth" | "process" | "network" | "detection">("all");
  const hs = snap?.hypotheses || [];
  const rows = useMemo(() => {
    // The investigation's retrieved events (adr/0092) are the rows; the
    // cited-refs strip stands in when the events read failed.
    const events = snap?.events;
    // A repeated pattern (adr/0095) collapses into one baseline row at its first event.
    const items = events
      ? collapsePatterns(events).map((it) => it.kind === "pattern"
        ? { ref: it.events[0].id, time: it.time, label: it.className, sentence: it.events[0].view?.sentence, category: it.events[0].view?.category, odRefs: it.events.flatMap((e) => e.observedDataRefs), pattern: it }
        : { ref: it.event.id, time: it.event.time, label: it.event.className, sentence: it.event.view?.sentence, category: it.event.view?.category, odRefs: it.event.observedDataRefs, pattern: undefined })
      : (snap?.derived?.strip || []).map((it) => ({ ...it, odRefs: [] as string[], pattern: undefined }));
    // Citing predictions: whose test results include the event or its observed-data.
    return items.map((it) => {
      const citing = hs.flatMap((h) => h.predictions.filter((p) => (p.testResultRefs || []).some((r) => r === it.ref || it.odRefs.includes(r))).map((p) => ({ h, p })));
      const emphasis = citing.some(({ h, p }) => p.status === "CONFIRMED" && h.status === "SUPPORTED") ? "emphasized" : citing.some(({ p }) => p.status === "DISCONFIRMED") ? "against" : "plain";
      return { ...it, category: it.category || CATEGORY[it.label] || "other", citing, emphasis };
    });
  }, [snap?.events, snap?.derived?.strip, hs]);
  const path = useMemo(() => attackPath(hs, snap?.events), [hs, snap?.events]);
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
        <h3>Attack path{path && path.nodes.length ? <span className="imeta">{path.nodes.length} entities over {fmtSpanMs(path.spanMs)} · cited by {path.citedBy.map((c) => "P" + (hs.flatMap((h) => h.predictions).find((p) => p.id === c.predictionId)?.ordinal ?? "?")).join(", ")}</span> : null}</h3>
        {!supported ? <Empty>No hypothesis is SUPPORTED yet, so there is no attack path to show.</Empty>
          : !path ? <Empty>The investigation's events could not be read, so the path cannot be drawn.</Empty>
          : !path.nodes.length ? <Empty>The SUPPORTED hypothesis cites no observed-data among the retrieved events, so there is no path to draw.</Empty>
          : (
            <ol className="apath">
              {path.nodes.map((n, i) => {
                const out = path.edges.filter((e) => e.from === n.ref);
                return (
                  <li key={n.ref} className="apnode">
                    <span className="apidx">{i + 1}</span>
                    <RefChip refId={n.ref} />
                    <span className="imeta">first {new Date(n.firstAt).toLocaleTimeString()} · {n.eventIds.length} event{n.eventIds.length === 1 ? "" : "s"}</span>
                    {out.map((e) => <span key={e.type + e.to} className="apedge">— {e.type} → <RefChip refId={e.to} /></span>)}
                  </li>
                );
              })}
            </ol>
          )}
        <details className="railfold"><summary>Open full graph</summary>{snap.derived ? <EvidenceGraph graph={snap.derived.graph} /> : <Empty>not derived yet</Empty>}</details>
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
                <li key={r.ref + i} className={"trow " + r.emphasis + (r.pattern ? " baseline" : "")}>
                  {divider ? <div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div> : null}
                  <span className="ttime mono">{r.pattern ? new Date(r.time).toLocaleDateString() : new Date(r.time).toLocaleTimeString()}</span>
                  <span className={"tcat " + r.category}>{r.category.toUpperCase()}</span>
                  {r.pattern ? (
                    <details className="tsentence tpattern">
                      <summary><b>{patternSentence(r.pattern)}</b> · {r.sentence?.length ? <EventSentence spans={r.sentence} className="inline" /> : r.label}</summary>
                      {r.pattern.events.map((e) => <div key={e.id} className="irow"><span className="imeta mono">{fmtDateTime(e.time)}</span> <RefChip refId={e.id} className="topen" /></div>)}
                    </details>
                  ) : <span className="tsentence">{r.sentence?.length ? <><EventSentence spans={r.sentence} /> <RefChip refId={r.ref} className="topen" /></> : <><RefChip refId={r.ref} /> <span className="imeta">{r.label}</span></>}</span>}
                  <span className="tciting">{r.citing.map(({ h, p }) => <a key={p.id} className={"pcite " + (p.status === "CONFIRMED" ? "ok" : p.status === "DISCONFIRMED" ? "bad" : "")} title={`${p.status} under ${hypName(h, hs)}`} onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a>)}</span>
                </li>
              );
            })}
            {!dividerDone && Number.isFinite(created) ? <li className="trow"><div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div></li> : null}
          </ol>
        )}
        <p className="imeta">{snap.events ? `${snap.events.length} retrieved event${snap.events.length === 1 ? "" : "s"} in this investigation; a pattern repeated on three or more days collapses into one row.` : "The investigation's events could not be read; the cited refs stand in."}</p>
      </section>
      <section className="lsection">
        <h3>Not yet known</h3>
        {open.length ? open.map(({ h, p }) => <div key={p.id}>? <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a> under {hypName(h, hs)}: {p.statement} <span className="imeta">({p.status.toLowerCase()})</span></div>) : <Empty>every prediction is decided</Empty>}
      </section>
    </div>
  );
}
