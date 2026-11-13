// One row per event (adr/0348): each call's answer is stored as its own
// records, so an event that overlapping queries returned is several records.
// They are the same event when their class, their event time and their
// observed-data refs (as a set, non-empty) are equal and they came from
// different calls. Within one call, records stay separate; across calls, the
// merged rows number the most copies any single call returned. Display only:
// the records themselves are unchanged.
import type { EventGroup, EventRow } from "../../../src/viewTypes";

/** A call that returned an event: the call's id and its verb. */
export interface EventCall {
  callId: string;
  verb: string;
}

/** One event as the timeline shows it: the record that stands for it and every record it stands for. */
export interface SameEvent {
  /** The record that stands for the event: the first retrieved. Its id is the row's ref (chip, pin). */
  row: EventRow;
  /** Every record the row stands for, `row.id` first. */
  ids: string[];
  /** The calls that returned it, in retrieval order. */
  calls: EventCall[];
}

function sameEventKey(e: EventRow): string | null {
  if (!e.observedDataRefs?.length) return null;
  return `${e.classUid}|${e.time}|${[...new Set(e.observedDataRefs)].sort().join(",")}`;
}

/**
 * The records merged into events, in the order of each event's first record
 * in the input. A record with no observed-data refs is its own event.
 */
export function mergeSameEvents(events: EventRow[] | null | undefined): SameEvent[] {
  const list = events ?? [];
  // Retrieval order: when the record was written, ties in input order.
  const order = new Map(list.map((e, i) => [e, i] as const));
  const retrieved = (a: EventRow, b: EventRow) => (Date.parse(a.recordedAt) - Date.parse(b.recordedAt)) || (order.get(a)! - order.get(b)!);
  const buckets = new Map<string, EventRow[]>();
  const slots: (EventRow | string)[] = [];
  for (const e of list) {
    const k = sameEventKey(e);
    if (k === null) { slots.push(e); continue; }
    const b = buckets.get(k);
    if (b) b.push(e);
    else { buckets.set(k, [e]); slots.push(k); }
  }
  const out: SameEvent[] = [];
  for (const s of slots) {
    if (typeof s !== "string") { out.push({ row: s, ids: [s.id], calls: [{ callId: s.callId, verb: s.verb }] }); continue; }
    // Each call's copies, the calls in the order they first retrieved the event.
    const byCall = new Map<string, EventRow[]>();
    for (const e of [...buckets.get(s)!].sort(retrieved)) {
      const c = byCall.get(e.callId);
      if (c) c.push(e);
      else byCall.set(e.callId, [e]);
    }
    const copies = Math.max(...[...byCall.values()].map((c) => c.length));
    for (let n = 0; n < copies; n++) {
      const members = [...byCall.values()].map((c) => c[n]).filter((e): e is EventRow => !!e);
      out.push({ row: members[0]!, ids: members.map((e) => e.id), calls: members.map((e) => ({ callId: e.callId, verb: e.verb })) });
    }
  }
  return out;
}

/** Each record id to the event that stands for it. */
export function eventOfRecord(merged: SameEvent[]): Map<string, SameEvent> {
  const m = new Map<string, SameEvent>();
  for (const s of merged) for (const id of s.ids) m.set(id, s);
  return m;
}

/**
 * A group (adr/0095) over the merged rows: its members are the events its
 * records stand for, each once, so its count is events, not retrievals. The
 * count drops by the records that stood for an event already counted.
 */
export function groupOverEvents(g: EventGroup, of: Map<string, SameEvent>): EventGroup {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const id of g.eventIds) {
    const rep = of.get(id)?.row.id ?? id;
    if (seen.has(rep)) continue;
    seen.add(rep);
    ids.push(rep);
  }
  return { ...g, count: g.count - (g.eventIds.length - ids.length), eventIds: ids };
}

/** "a, b ×2": each verb once in the order it first ran, with how many calls of it returned the event. */
export function viaVerbs(calls: EventCall[]): { verb: string; calls: EventCall[] }[] {
  const out: { verb: string; calls: EventCall[] }[] = [];
  for (const c of calls) {
    const v = out.find((x) => x.verb === c.verb);
    if (v) v.calls.push(c);
    else out.push({ verb: c.verb, calls: [c] });
  }
  return out;
}
