// The Knowledge section's nudge (09 §9.6, impl §7.9): the items the `auto`
// posture would include that are not included — every procedure, and every
// past case banded NEAR_DUPLICATE or RELATED (06 §5.1's rule, restated as
// data). The nudge informs; it never includes anything. Pure.
import type { KnowledgeItem } from "../../../src/protocol";

export function wouldAutoInclude(k: KnowledgeItem): boolean {
  return k.kind === "sop" || k.band === "NEAR_DUPLICATE" || k.band === "RELATED";
}

export interface Nudge {
  procedures: number;
  cases: number;
  text: string;
}

export function knowledgeNudge(items: KnowledgeItem[], posture: string): Nudge | null {
  if (posture !== "opt_in") return null;
  const missing = items.filter((k) => wouldAutoInclude(k) && !k.included);
  const procedures = missing.filter((k) => k.kind === "sop").length;
  const cases = missing.length - procedures;
  if (!missing.length) return null;
  const parts: string[] = [];
  if (procedures) parts.push(`${procedures} procedure${procedures === 1 ? "" : "s"}`);
  if (cases) parts.push(`${cases} past case${cases === 1 ? "" : "s"}`);
  return { procedures, cases, text: `${parts.join(" and ")} the auto posture would include ${missing.length === 1 ? "is" : "are"} not included` };
}
