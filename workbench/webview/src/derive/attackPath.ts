// The attack path (08 §8.4 Story; impl §7.6): entities = the object_refs of
// the observed-data cited by CONFIRMED predictions of SUPPORTED hypotheses,
// ordered by the first event time of the events those observed-data derive
// from; edges = the normalizer relationships among those entities; span =
// last − first event time. Absent when no hypothesis is SUPPORTED. Pure over
// the hypotheses and the investigation's events (adr/0092).
import type { EventRow, Hypothesis } from "../../../src/backend";

export interface AttackPathNode {
  ref: string;
  /** Event time of the first event this entity appears in among the cited ones. */
  firstAt: string;
  /** The cited events it appears in. */
  eventIds: string[];
}

export interface AttackPathEdge {
  type: string;
  from: string;
  to: string;
}

export interface AttackPath {
  nodes: AttackPathNode[];
  edges: AttackPathEdge[];
  /** Milliseconds between the first and the last cited event. */
  spanMs: number;
  /** The citing predictions, for the section's provenance line. */
  citedBy: { predictionId: string; hypothesisId: string }[];
}

export function attackPath(hs: Hypothesis[], events: EventRow[] | null | undefined): AttackPath | null {
  const supported = hs.filter((h) => h.status === "SUPPORTED");
  if (!supported.length || !events) return null;
  const citedOD = new Set<string>();
  const citedBy: AttackPath["citedBy"] = [];
  for (const h of supported) {
    for (const p of h.predictions) {
      if (p.status !== "CONFIRMED") continue;
      let cites = false;
      for (const r of p.testResultRefs || []) {
        if (r.startsWith("observed-data--")) { citedOD.add(r); cites = true; }
      }
      if (cites) citedBy.push({ predictionId: p.id, hypothesisId: h.id });
    }
  }
  if (!citedOD.size) return { nodes: [], edges: [], spanMs: 0, citedBy };
  const cited = events.filter((e) => (e.observedDataRefs || []).some((r) => citedOD.has(r)));
  const byRef = new Map<string, AttackPathNode>();
  const edgeKeys = new Set<string>();
  const edges: AttackPathEdge[] = [];
  let first = Infinity, last = -Infinity;
  for (const e of cited.slice().sort((a, b) => Date.parse(a.time) - Date.parse(b.time))) {
    const t = Date.parse(e.time);
    if (Number.isFinite(t)) { first = Math.min(first, t); last = Math.max(last, t); }
    for (const ref of e.entityRefs || []) {
      const n = byRef.get(ref);
      if (n) n.eventIds.push(e.id);
      else byRef.set(ref, { ref, firstAt: e.time, eventIds: [e.id] });
    }
    for (const r of e.relationships || []) {
      const k = r.type + "|" + r.sourceRef + "|" + r.targetRef;
      if (!edgeKeys.has(k)) { edgeKeys.add(k); edges.push({ type: r.type, from: r.sourceRef, to: r.targetRef }); }
    }
  }
  const nodes = Array.from(byRef.values()).sort((a, b) => Date.parse(a.firstAt) - Date.parse(b.firstAt));
  return { nodes, edges, spanMs: Number.isFinite(first) && Number.isFinite(last) ? last - first : 0, citedBy };
}

/** The events just before and after one event on the same host entity (08
 * §8.6.1 "Around it"): the window of ±n, the event itself excluded. The host
 * entity is the first x-host ref the event names; the first entity otherwise. */
export function aroundEvent(events: EventRow[] | null | undefined, eventId: string, n = 3): { before: EventRow[]; after: EventRow[]; key: string } | null {
  if (!events) return null;
  const idx = events.findIndex((e) => e.id === eventId);
  if (idx < 0) return null;
  const anchor = events[idx];
  const key = (anchor.entityRefs || []).find((r) => r.startsWith("x-host--")) || (anchor.entityRefs || [])[0] || "";
  const same = key ? events.filter((e) => (e.entityRefs || []).includes(key)) : events;
  const i = same.findIndex((e) => e.id === eventId);
  return { before: same.slice(Math.max(0, i - n), i), after: same.slice(i + 1, i + 1 + n), key };
}

/** The bundle view (08 §8.6.1): a call's events grouped by source tool, with
 * identical sentences collapsed into one row and a count. */
export function bundleGroups(events: EventRow[]): { source: string; rows: { key: string; text: string; ids: string[]; first: EventRow }[] }[] {
  const bySource = new Map<string, Map<string, { key: string; text: string; ids: string[]; first: EventRow }>>();
  for (const e of events) {
    const text = (e.view?.sentence || []).map((s) => s.text).join("") || e.className;
    const key = e.classUid + "|" + text;
    let g = bySource.get(e.sourceTool);
    if (!g) { g = new Map(); bySource.set(e.sourceTool, g); }
    const row = g.get(key);
    if (row) row.ids.push(e.id);
    else g.set(key, { key, text, ids: [e.id], first: e });
  }
  return Array.from(bySource.entries()).map(([source, rows]) => ({ source, rows: Array.from(rows.values()) }));
}
