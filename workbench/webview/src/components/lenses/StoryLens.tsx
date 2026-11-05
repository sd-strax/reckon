// Story — event time (08 §8.4): everything here is in event time;
// investigation time appears once, as the divider where it started. The
// timeline rows are the events retrieved for the investigation (adr/0092),
// each naming the call that retrieved it, with the cited refs that carry an
// event time joined in; on a record without membership (events persisted
// before it) the cited events alone remain. A retrieved row reads as the
// server's sentence with its entities as chips (adr/0090); a cited ref the
// record holds no row for reads as its class. The record is read folded by
// default (adr/0095): events of one class sharing a key are one group, and a
// group that repeats (3 events on 3 days) is one row that opens to its
// members; the analyst changes the key per class or turns the fold off. The
// attack path is gap §6.12.
import { useMemo, useState } from "react";
import { useApp } from "../../app";
import { collapses, fmtDay, keySpec, spanText } from "../../derive/events";
import { hypName } from "../../derive/hyps";
import type { EventClassKeys, EventGroup, EventRow, SentenceToken } from "../../../../src/viewTypes";
import { EventStrip, EvidenceGraph } from "../DerivedViews";
import { Empty, RefChip, Sentence, fmtDateTime } from "../shared";
import type { Hypothesis } from "../../../../src/viewTypes";

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
  /** The server's sentence (adr/0090); absent when the class has no template or the row is a cited ref alone. */
  sentence?: SentenceToken[];
  /** A collapsed group (adr/0095): the members newest first, `ref` being the newest. The row sits at the group's first event. */
  group?: EventGroup;
}

function rowItem(e: EventRow): TimelineItem {
  const it: TimelineItem = { ref: e.id, time: e.time, label: e.className, category: e.view.category, callId: e.callId, verb: e.verb };
  if (e.view.sentence?.length) it.sentence = e.view.sentence;
  return it;
}

/**
 * The retrieved events joined with the cited refs that carry an event time, in
 * event time, each ref once. With groups, a group that repeats is one item at
 * its first event, its members folded under it.
 */
export function timelineItems(events: EventRow[] | null | undefined, strip: { ref: string; time: string; label: string }[], groups?: EventGroup[] | null): TimelineItem[] {
  const out = new Map<string, TimelineItem>();
  const byId = new Map((events ?? []).map((e) => [e.id, e] as const));
  const folded = new Set<string>();
  for (const g of groups ?? []) {
    if (!collapses(g)) continue;
    const newest = g.eventIds.map((id) => byId.get(id)).find((e): e is EventRow => !!e);
    if (!newest) continue;
    const it = rowItem(newest);
    it.time = g.first;
    it.group = g;
    out.set(newest.id, it);
    for (const id of g.eventIds) folded.add(id);
  }
  for (const e of events ?? []) {
    if (!folded.has(e.id)) out.set(e.id, rowItem(e));
  }
  for (const it of strip) {
    if (!out.has(it.ref) && !folded.has(it.ref)) out.set(it.ref, { ref: it.ref, time: it.time, label: it.label, category: CATEGORY[it.label] || "other" });
  }
  return [...out.values()].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
}

/** "N retrieved events in the record", and how many of them the page or the fold covers when the record holds more. */
export function retrievedCount(shown: number, total: number | undefined, foldedOver?: number): string {
  const n = total ?? shown;
  const head = `${n} retrieved event${n === 1 ? "" : "s"} in the record`;
  if (foldedOver) return n > foldedOver ? `${head} · folded over the newest ${foldedOver}` : head;
  return n > shown ? `${head} · showing the newest ${shown}` : head;
}

// A stable empty, so the memo below does not recompute on every render.
const NO_HYPOTHESES: Hypothesis[] = [];

/** One class's key chooser: the slots it can key on, the key in use ticked. */
function KeyChooser({ keys, k, onChange }: { keys: EventClassKeys[]; k: EventClassKeys; onChange: (spec: string) => void }) {
  const [open, setOpen] = useState(false);
  const labels = k.key.map((n) => k.slots.find((s) => s.name === n)?.label ?? n);
  return (
    <span className="keychooser">
      <button className="linklike" aria-expanded={open} title={`What ${k.className} events fold on`} onClick={() => setOpen(!open)}>{k.className}: {labels.length ? labels.join(" · ") : "nothing"} {open ? "▴" : "▾"}</button>
      {open ? (
        <div className="keymenu" role="group" aria-label={`${k.className} key`}>
          {k.slots.map((s) => (
            <label key={s.name}>
              <input type="checkbox" checked={k.key.includes(s.name)} onChange={(e) => onChange(keySpec(keys, k.classUid, e.target.checked ? [...k.key, s.name] : k.key.filter((n) => n !== s.name)))} /> {s.label}
            </label>
          ))}
          {k.key.join(",") !== k.default.join(",") ? <button className="linklike" onClick={() => onChange(keySpec(keys, k.classUid, k.default))}>Reset to default</button> : null}
        </div>
      ) : null}
    </span>
  );
}

export function StoryLens() {
  const { state, openInspector, send } = useApp();
  const snap = state.snap;
  const [filter, setFilter] = useState<"all" | "auth" | "process" | "network" | "detection">("all");
  const [expanded, setExpanded] = useState<Record<string, true>>({});
  const hs = snap?.hypotheses ?? NO_HYPOTHESES;
  const folded = (snap?.eventGroups ?? null) !== null;
  const rows = useMemo(() => {
    // Citing predictions: whose test results include the ref, or any member of a group.
    return timelineItems(snap?.events, snap?.derived?.strip || [], snap?.eventGroups).map((it) => {
      const refs = it.group ? it.group.eventIds : [it.ref];
      const citing = hs.flatMap((h) => h.predictions.filter((p) => (p.testResultRefs || []).some((r) => refs.includes(r))).map((p) => ({ h, p })));
      const emphasis = citing.some(({ h, p }) => p.status === "CONFIRMED" && h.status === "SUPPORTED") ? "emphasized" : citing.some(({ p }) => p.status === "DISCONFIRMED") ? "against" : "plain";
      return { ...it, citing, emphasis };
    });
  }, [snap?.events, snap?.eventGroups, snap?.derived?.strip, hs]);
  if (!snap) return null;
  const byId = new Map((snap.events ?? []).map((e) => [e.id, e] as const));
  const techniques = Array.from(new Set(hs.flatMap((h) => h.labels || []).filter((l) => /^T\d{4}/.test(l))));
  const open = hs.flatMap((h) => h.status === "ABANDONED" ? [] : h.predictions.filter((p) => p.status === "UNTESTED" || p.status === "INCONCLUSIVE").map((p) => ({ h, p })));
  const created = snap.investigation.createdAt ? Date.parse(snap.investigation.createdAt) : NaN;
  const shown = rows.filter((r) => filter === "all" || r.category === filter);
  // The "investigation starts" divider sits above the first event at or after creation.
  const dividerAt = Number.isFinite(created) ? shown.findIndex((r) => Date.parse(r.time) >= created) : -1;
  const toggle = (ref: string) => setExpanded((x) => { const next = { ...x }; if (next[ref]) delete next[ref]; else next[ref] = true; return next; });
  const sentenceOf = (r: { ref: string; sentence?: SentenceToken[]; label: string }) => r.sentence ? <Sentence tokens={r.sentence} /> : <span className="imeta">{r.label}</span>;
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
          {(["all", "auth", "process", "network", "detection"] as const).map((f) => <button key={f} role="tab" aria-selected={filter === f} className={"itab" + (filter === f ? " on" : "")} onClick={() => setFilter(f)}>{f === "all" ? "All" : f.charAt(0).toUpperCase() + f.slice(1)}</button>)}
        </div>
        {snap.derived ? <EventStrip items={snap.derived.strip} /> : null}
        {snap.eventsError ? <div className="error">{snap.events ? "The next page of the retrieved record could not be read" : "The retrieved record could not be read"}: {snap.eventsError}.{snap.events ? "" : " Only the cited events are shown."}</div> : null}
        {snap.events?.length || snap.eventGroups ? (
          <div className="foldbar imeta">
            {snap.events?.length ? <span>{retrievedCount(snap.events.length, snap.eventsTotal, folded ? snap.eventsFoldedOver : undefined)}</span> : null}
            <label title="Fold the events of one class that share a key into one row when they repeat on 3 or more days (adr/0095)"><input type="checkbox" checked={folded} onChange={() => send({ type: "events.fold", keys: folded ? null : "default" })} /> Fold repeated events</label>
            {folded ? (snap.eventKeys ?? []).map((k) => <KeyChooser key={k.classUid} keys={snap.eventKeys ?? []} k={k} onChange={(spec) => send({ type: "events.fold", keys: spec })} />) : null}
          </div>
        ) : null}
        {!shown.length ? <Empty>{snap.events?.length ? "No retrieved or cited event in this category." : "No cited events carry an event time yet."}</Empty> : (
          <ol className="timeline">
            {shown.map((r, i) => {
              const divider = i === dividerAt;
              const isOpen = !!r.group && !!expanded[r.ref];
              return (
                <li key={r.ref + i} className={"trow " + r.emphasis + (r.group ? " group" : "")}>
                  {divider ? <div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div> : null}
                  <span className="ttime mono">{r.group ? fmtDay(r.group.first) : new Date(r.time).toLocaleTimeString()}</span>
                  <span className={"tcat " + r.category}>{r.category.toUpperCase()}</span>
                  <span className="tsentence">
                    {r.group ? null : <><RefChip refId={r.ref} /> </>}
                    {sentenceOf(r)}
                    {r.group ? <button className="linklike tfold" aria-expanded={isOpen} onClick={() => toggle(r.ref)}>{spanText(r.group)} · {fmtDay(r.group.first)} – {fmtDay(r.group.last)} {isOpen ? "▴" : "▾"}</button> : null}
                    {!r.group && r.callId ? <> <a className="imeta tvia" title="Open the capability call that retrieved this event" onClick={() => openInspector({ kind: "call", callId: r.callId! })}>via {r.verb}</a></> : null}
                  </span>
                  <span className="tciting">{r.citing.map(({ h, p }) => <a key={p.id} className={"pcite " + (p.status === "CONFIRMED" ? "ok" : p.status === "DISCONFIRMED" ? "bad" : "")} title={`${p.status} under ${hypName(h, hs)}`} onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a>)}</span>
                  {isOpen && r.group ? (
                    <ol className="members">
                      {r.group.eventIds.map((id) => byId.get(id)).filter((e): e is EventRow => !!e).map((e) => (
                        <li key={e.id} className="trow member">
                          <span className="ttime mono">{fmtDay(e.time)} {new Date(e.time).toLocaleTimeString()}</span>
                          <span className="tsentence"><RefChip refId={e.id} /> {sentenceOf({ ref: e.id, sentence: e.view.sentence?.length ? e.view.sentence : undefined, label: e.className })} <a className="imeta tvia" title="Open the capability call that retrieved this event" onClick={() => openInspector({ kind: "call", callId: e.callId })}>via {e.verb}</a></span>
                        </li>
                      ))}
                    </ol>
                  ) : null}
                </li>
              );
            })}
            {dividerAt < 0 && Number.isFinite(created) ? <li className="trow"><div className="tdivider">investigation starts · {fmtDateTime(snap.investigation.createdAt)}</div></li> : null}
          </ol>
        )}
        {snap.eventsCursor ? <button className="linklike older" onClick={() => send({ type: "events.older", cursor: snap.eventsCursor! })}>Show older retrieved events</button> : null}
      </section>
      <section className="lsection">
        <h3>Not yet known</h3>
        {open.length ? open.map(({ h, p }) => <div key={p.id}>? <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>P{p.ordinal ?? "?"}</a> under {hypName(h, hs)}: {p.statement} <span className="imeta">({p.status.toLowerCase()})</span></div>) : <Empty>every prediction is decided</Empty>}
      </section>
    </div>
  );
}
