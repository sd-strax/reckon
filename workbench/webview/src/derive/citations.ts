// "In this case" (08 §8.6 frame item 3, impl §7.4): what in this investigation
// cites an object — predictions whose results cite it, actions grounded on it
// or targeting it, pins that hold it, the acts that read or produced it, and
// the seed. Derived from the snapshot alone; the cross-investigation fold
// stays server-side. Pure.
import type { ActionRow, Hypothesis, PinRow, Prediction, ThreadEntry } from "../../../src/viewTypes";
import type { InvestigationSnapshot } from "../../../src/protocol";

export interface InThisCase {
  /** Predictions whose test results cite the ref, with their owner. */
  predictions: { h: Hypothesis; p: Prediction }[];
  /** Actions grounded on the ref (evidence) or acting on it (target). */
  actions: { a: ActionRow; role: "evidence" | "target" }[];
  pins: PinRow[];
  /** Acts whose inputs read the ref or whose outputs produced it. */
  acts: { e: ThreadEntry; role: "read" | "produced" }[];
  seed: boolean;
}

export function inThisCase(ref: string, snap: InvestigationSnapshot | null): InThisCase {
  const out: InThisCase = { predictions: [], actions: [], pins: [], acts: [], seed: false };
  if (!snap) return out;
  for (const h of snap.hypotheses) {
    for (const p of h.predictions) {
      if ((p.testResultRefs || []).includes(ref)) out.predictions.push({ h, p });
    }
  }
  for (const a of snap.actions || []) {
    if ((a.evidenceRefs || []).includes(ref)) out.actions.push({ a, role: "evidence" });
    else if ((a.targets || []).includes(ref) || (a.targets || []).some((t) => t === ref)) out.actions.push({ a, role: "target" });
  }
  for (const pin of snap.pins) {
    if ((pin.inputRefs || []).includes(ref)) out.pins.push(pin);
  }
  for (const e of snap.thread || []) {
    if ((e.outputRefs || []).includes(ref)) out.acts.push({ e, role: "produced" });
    else if ((e.inputRefs || []).includes(ref)) out.acts.push({ e, role: "read" });
  }
  const seed = snap.investigation.seed;
  out.seed = !!seed && (seed.entityRef === ref || seed.caseRef === ref || seed.detectionFindingRef === ref);
  return out;
}

export function isEmpty(c: InThisCase): boolean {
  return !c.predictions.length && !c.actions.length && !c.pins.length && !c.acts.length && !c.seed;
}
