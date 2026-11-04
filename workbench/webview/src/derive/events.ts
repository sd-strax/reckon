// The folded record (adr/0095): the engine groups the retrieved events by
// class and key and counts; whether a group collapses into one row, and how
// a key choice travels back, is the webview's.
import type { EventClassKeys, EventGroup } from "../../../src/viewTypes";

/**
 * A group collapses into one row when it repeats: 3 or more events on 3 or
 * more distinct days (implementation/workbench-react.md §8.3). Display only.
 */
export function collapses(g: EventGroup): boolean {
  return g.count >= 3 && g.days >= 3;
}

/**
 * The key spec the events read takes, with one class's key replaced. A class
 * with no slots (no template) has nothing to key on and is left out; an empty
 * key ("3002:") is a choice the read keeps, not the default.
 */
export function keySpec(keys: EventClassKeys[], classUid: number, next: string[]): string {
  return keys.filter((k) => k.slots.length > 0).map((k) => `${k.classUid}:${(k.classUid === classUid ? next : k.key).join(",")}`).join(";");
}

/** "N events over M days", the group's count and span as the record holds them. */
export function spanText(g: EventGroup): string {
  return `${g.count} events over ${g.days} day${g.days === 1 ? "" : "s"}`;
}

/** A day, short: "Apr 13". */
export function fmtDay(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
}
