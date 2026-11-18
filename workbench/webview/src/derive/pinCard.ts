// A pinned evidence card (adr/0352): what happened, from the events the pin
// cites that the workbench holds, and the pinned refs behind one disclosure,
// an account the record joins by an alias (adr/0324) shown once. Display
// only: the pin's refs are unchanged.
import type { AccountAlias } from "../../../src/protocol";
import type { EventRow } from "../../../src/viewTypes";
import { eventOfCitation, eventOfRecord, mergeSameEvents } from "./sameEvent";

/**
 * The events a pin cites that the workbench holds, each once: those it names
 * by a record, in pin order, then those it names by an observation. A ref to
 * any record of a merged event (adr/0348) stands for that event, and so does
 * an observation made from it (adr/0364), unless the pin already names a
 * record that observation was made from.
 */
export function pinnedEvents(refs: string[], events: EventRow[] | null | undefined): EventRow[] {
  const merged = mergeSameEvents(events);
  const ofRecord = eventOfRecord(merged);
  const ofCitation = eventOfCitation(merged);
  const out: EventRow[] = [];
  const add = (e: EventRow | undefined) => { if (e && !out.includes(e)) out.push(e); };
  for (const r of refs) add(ofRecord.get(r)?.row);
  const shown = new Set(out.flatMap((e) => e.observedDataRefs ?? []));
  for (const r of refs) if (!ofRecord.has(r) && !shown.has(r)) add(ofCitation.get(r)?.row);
  return out;
}

/** The alias sets that join accounts within this pin: the login's account and the others the pin also cites. */
export function pinAliases(refs: string[], aliases: AccountAlias[] | null | undefined): AccountAlias[] {
  const held = new Set(refs);
  return (aliases ?? []).flatMap((a) => {
    const also = a.also.filter((r) => held.has(r));
    return held.has(a.ref) && also.length ? [{ ref: a.ref, also }] : [];
  });
}

/**
 * Per account of a joined set, the identifiers read beside its chip in a
 * sentence: the set's others (the SID's), whichever of the set the sentence
 * bound, since the sentence's words for an account are its login.
 */
export function besideOf(sets: AccountAlias[]): Map<string, string[]> {
  const m = new Map<string, string[]>();
  for (const s of sets) for (const r of [s.ref, ...s.also]) m.set(r, s.also);
  return m;
}

/** The pin's parts as chips: each ref once, a joined set as its login's chip with the others beside it. */
export function partChips(refs: string[], sets: AccountAlias[]): { ref: string; also: string[] }[] {
  const folded = new Set(sets.flatMap((s) => s.also));
  return [...new Set(refs)].filter((r) => !folded.has(r)).map((r) => ({ ref: r, also: sets.find((s) => s.ref === r)?.also ?? [] }));
}
