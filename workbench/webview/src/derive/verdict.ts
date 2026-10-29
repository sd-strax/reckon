// The verdict dialog's preflight + residual (02 §2.10): the engine's gates
// rendered BEFORE the attempt, and what the analyst is signing over. Pure.
import type { Capability, Hypothesis, PinRow, ThreadEntry, Verdict } from "../../../src/viewTypes";
import { hypName } from "./hyps";

export interface ChecklistItem { ok: boolean; text: string }

export function verdictChecklist(pins: PinRow[], verdict: Verdict | null | undefined): ChecklistItem[] {
  const active = pins.filter((p) => !p.superseded);
  const items: ChecklistItem[] = [
    { ok: active.length > 0, text: active.length > 0
      ? active.length + " evidence item(s) pinned"
      : "no pinned evidence — pin the key findings first (the engine will refuse)" },
    { ok: true, text: "cites the pinned evidence (attached automatically)" },
  ];
  if (verdict) items.push({ ok: true, text: "revises the current verdict (" + verdict.disposition + ") — history is preserved" });
  return items;
}

/** The residual lines: verbs never checkable, the rival nobody killed, untested predictions. */
export function verdictResidual(caps: Capability[] | null, hyps: Hypothesis[]): string[] {
  const lines: string[] = [];
  const unavailable = (caps || []).filter((c) => c.status === "unavailable").map((c) => c.verb);
  const untested: string[] = [];
  const openRivals: { h: Hypothesis; disc: number }[] = [];
  for (const h of hyps) {
    const live = h.status === "PROPOSED" || h.status === "OPEN";
    let disc = 0;
    for (const p of h.predictions || []) {
      if (p.status === "UNTESTED") {
        untested.push(p.statement);
        if ((p.discriminates || []).length) disc++;
      }
    }
    if (live) openRivals.push({ h, disc });
  }
  if (unavailable.length) lines.push("verbs never checkable in this tenant: " + unavailable.join(", "));
  for (const { h, disc } of openRivals) {
    const why = h.status === "PROPOSED" ? "never adopted" : "still open";
    lines.push("undecided hypothesis " + hypName(h, hyps) + " — " + why
      + (disc ? ", " + disc + " untested discriminating test" + (disc === 1 ? "" : "s") : "")
      + " (abandon it on the tracker if you are not pursuing it)");
  }
  for (const s of untested.slice(0, 5)) lines.push("untested prediction: " + s);
  if (untested.length > 5) lines.push("… and " + (untested.length - 5) + " more untested predictions");
  if (!unavailable.length && !untested.length && !openRivals.length) {
    lines.push("no known gaps — all configured verbs available, no undecided hypotheses, no untested predictions");
  }
  return lines;
}

/** The refs a verdict cites: every active pin's refs, deduplicated. */
export function verdictRefs(pins: PinRow[]): string[] {
  const refs: string[] = [];
  for (const p of pins.filter((x) => !x.superseded)) {
    for (const r of p.inputRefs || []) if (!refs.includes(r)) refs.push(r);
  }
  return refs;
}

/** How verdicts in the thread relate (#155): each replaced verdict and the one
 * that replaced it (the next verdict in order), in both directions. */
export interface VerdictLink { replacedBy?: ThreadEntry; replaces?: ThreadEntry }

export function verdictLinks(thread: ThreadEntry[]): Map<string, VerdictLink> {
  const verdicts = thread.filter((e) => e.interpretationType === "verdict").sort((a, b) => a.sequenceNo - b.sequenceNo);
  const out = new Map<string, VerdictLink>();
  verdicts.forEach((v, i) => {
    const next = verdicts[i + 1];
    if (!v.superseded || !next) return;
    out.set(v.interpretationId, { ...out.get(v.interpretationId), replacedBy: next });
    out.set(next.interpretationId, { ...out.get(next.interpretationId), replaces: v });
  });
  return out;
}

/** The thread line's id for an act, so a link can scroll to it. */
export const actLineId = (interpretationId: string): string => "act-" + interpretationId;
