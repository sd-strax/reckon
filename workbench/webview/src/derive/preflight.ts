// The conclude preflight (02 §2.10, impl §7.8): the engine's gates rendered
// BEFORE the attempt, each unmet item naming the surface that satisfies it.
// The verdict preflight stays in derive/verdict.ts. Pure.
import type { ActionRow, CommsThread, PinRow, Verdict } from "../../../src/viewTypes";
import type { Lens } from "../../../src/protocol";
import { terminal } from "./stage";

export interface PreflightItem {
  ok: boolean;
  text: string;
  /** Where to go to satisfy it. */
  lens?: Lens;
}

export interface ConcludeInputs {
  verdict: Verdict | null | undefined;
  pins: PinRow[];
  actions: ActionRow[];
  comms: CommsThread[];
  /** OPEN + SENT dependencies; null when the slice could not be read. */
  openDependencies: number | null;
}

export function concludePreflight(i: ConcludeInputs): PreflightItem[] {
  const activePins = i.pins.filter((p) => !p.superseded).length;
  const open = i.actions.filter((a) => !terminal(a));
  const awaiting = i.comms.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
  return [
    { ok: !!i.verdict, text: i.verdict ? `verdict of record: ${i.verdict.disposition}` : "no verdict of record — the engine refuses to conclude without one", lens: "reasoning" },
    { ok: activePins > 0, text: activePins > 0 ? `${activePins} evidence item(s) pinned` : "no pinned evidence", lens: "reasoning" },
    { ok: open.length === 0, text: open.length === 0 ? "every action is terminal" : `${open.length} action(s) not yet terminal`, lens: "response" },
    { ok: awaiting === 0, text: awaiting === 0 ? "no comms thread awaits a reply" : `${awaiting} comms thread(s) still await replies — mark them done or accept leaving them open`, lens: "response" },
    dependencyItem(i.openDependencies),
  ];
}

function dependencyItem(open: number | null): PreflightItem {
  if (open === null) return { ok: false, text: "dependencies on others could not be read — the count is unchecked", lens: "response" };
  if (open === 0) return { ok: true, text: "no dependency on others outstanding", lens: "response" };
  return { ok: false, text: `${open} dependenc${open === 1 ? "y" : "ies"} on others not confirmed or waived`, lens: "response" };
}

/** The engine's hard gate: a verdict. Everything else is decision support at v0. */
export function concludeAllowed(items: PreflightItem[]): boolean {
  return items[0]?.ok === true;
}

/** Every item met (the primary action becomes Conclude). */
export function preflightClean(items: PreflightItem[]): boolean {
  return items.every((x) => x.ok);
}
