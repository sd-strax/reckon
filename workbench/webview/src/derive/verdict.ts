// The verdict dialog's preflight + residual (02 §2.10): the engine's gates
// rendered BEFORE the attempt, and what the analyst is signing over. Pure.
import type { Capability, Hypothesis, PinRow, Verdict } from "../../../src/viewTypes";
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
