// The timeline's baseline rows (08 §8.4; adr/0095): the investigation's
// retrieved events grouped by class and the server's pattern key; a group
// with at least three events on at least three distinct days collapses into
// one row carrying the count, the days and the typical hour. The thresholds
// are the server fold's (capability/ocsfview.Baseline); this is the same
// counting rule applied for display, never a judgment.
import type { EventRow } from "../../../src/backend";

export const MIN_PATTERN_EVENTS = 3;
export const MIN_PATTERN_DAYS = 3;

export interface PatternRow {
  kind: "pattern";
  key: string;
  classUid: number;
  className: string;
  pattern: Record<string, string>;
  events: EventRow[];
  count: number;
  days: number;
  typicalHour: number;
  /** The first event's time: where the row sits in the timeline. */
  time: string;
}

export type TimelineItem = { kind: "event"; event: EventRow } | PatternRow;

function keyOf(e: EventRow): string | null {
  if (!e.pattern) return null;
  return e.classUid + "|" + Object.keys(e.pattern).sort().map((k) => k + "=" + e.pattern![k]).join("|");
}

/** Collapse repeated patterns; everything else stays an event row. Output in event time. */
export function collapsePatterns(events: EventRow[]): TimelineItem[] {
  const groups = new Map<string, EventRow[]>();
  for (const e of events) {
    const k = keyOf(e);
    if (!k) continue;
    const g = groups.get(k);
    if (g) g.push(e); else groups.set(k, [e]);
  }
  const collapsed = new Map<string, PatternRow>();
  for (const [k, g] of groups) {
    const days = new Set(g.map((e) => e.time.slice(0, 10)));
    if (g.length < MIN_PATTERN_EVENTS || days.size < MIN_PATTERN_DAYS) continue;
    const hours = new Map<number, number>();
    for (const e of g) { const h = new Date(e.time).getUTCHours(); hours.set(h, (hours.get(h) || 0) + 1); }
    let typical = 0, best = -1;
    for (const [h, n] of hours) if (n > best || (n === best && h < typical)) { typical = h; best = n; }
    const sorted = g.slice().sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
    collapsed.set(k, { kind: "pattern", key: k, classUid: g[0].classUid, className: g[0].className, pattern: g[0].pattern!, events: sorted, count: g.length, days: days.size, typicalHour: typical, time: sorted[0].time });
  }
  const out: TimelineItem[] = [];
  const emitted = new Set<string>();
  for (const e of events.slice().sort((a, b) => Date.parse(a.time) - Date.parse(b.time))) {
    const k = keyOf(e);
    const row = k ? collapsed.get(k) : undefined;
    if (row) {
      if (!emitted.has(k!)) { emitted.add(k!); out.push(row); }
      continue;
    }
    out.push({ kind: "event", event: e });
  }
  return out;
}

/** "N events over M days, typically around HH:00 UTC" — the body never says "normal". */
export function patternSentence(p: { count: number; days: number; typicalHour: number }): string {
  return `${p.count} events over ${p.days} days, typically around ${String(p.typicalHour).padStart(2, "0")}:00 UTC`;
}
