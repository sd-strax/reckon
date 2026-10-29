// Requesting an action from a pinned finding (#146): the analyst picks the
// type from the catalog and a target the pin's evidence observed, gives a
// reason, then fills the type's own fields. The pin's evidence is the citation.
import type { ActionTypeView } from "../../../src/protocol";
import type { PinRow } from "../../../src/viewTypes";
import type { GraphView } from "../../../src/protocol";

/** The entities the pin's evidence observed: the derived graph's join edges. */
export function pinTargets(pin: PinRow, graph: GraphView | undefined, labels: Record<string, string>): { value: string; label: string }[] {
  const out: { value: string; label: string }[] = [];
  const refs = new Set(pin.inputRefs || []);
  for (const e of graph?.edges || []) {
    if (!e.join || !refs.has(e.from)) continue;
    const label = labels[e.to];
    if (label && !out.some((o) => o.value === label)) out.push({ value: label, label: label + " · " + e.to.split("--")[0].replace(/^x-/, "") });
  }
  return out;
}

/** The order the type list reads in: containment first, case-record writes last. */
const ORDER = ["containment", "eradication", "recovery", "notification", "case_record"];
export function typeOptions(types: ActionTypeView[]): { value: string; label: string }[] {
  return types.slice().sort((a, b) => ORDER.indexOf(a.category || "") - ORDER.indexOf(b.category || ""))
    .map((t) => ({ value: t.actionType, label: (t.name || t.actionType) + " (" + t.actionType + ")" }));
}
