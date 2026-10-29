// The stage indicator and the primary action (08 §8.2, impl §7.1): five steps
// over the derived lifecycle — no new aggregate state. Owner items count
// under Notify once gap §6.17 exists; until then the input is empty. Pure.
import type { ActionRow, CommsThread, InvestigationSummary, Verdict } from "../../../src/viewTypes";

export type Step = "investigate" | "verdict" | "contain" | "notify" | "close";
export const STEPS: Step[] = ["investigate", "verdict", "contain", "notify", "close"];
export const STEP_LABELS: Record<Step, string> = { investigate: "Investigate", verdict: "Verdict", contain: "Contain", notify: "Notify", close: "Close" };

const TERMINAL = new Set(["SUCCEEDED", "PARTIAL", "FAILED", "TIMEOUT", "REJECTED", "EXPIRED", "REVERSED"]);
export const terminal = (a: ActionRow): boolean => TERMINAL.has(a.status);
export const isNotify = (a: Pick<ActionRow, "actionType">): boolean => a.actionType.startsWith("notify.");

export interface StageInputs {
  status: string;
  verdict: Verdict | null | undefined;
  actions: ActionRow[];
  comms: CommsThread[];
  /** Gap §6.17: OPEN/SENT owner items; empty until the record carries them. */
  openOwnerItems: number;
}

export interface Stage {
  done: Record<Step, boolean>;
  /** A step that does not apply: a benign verdict with nothing contained or sent (#145). */
  notNeeded: Record<Step, boolean>;
  /** The step the investigation is at; null once closed. */
  current: Step | null;
}

/** An action on the world that contains or removes a threat: not a message, not
 * a write to the case record, not the undo of an earlier action. */
export const isContainment = (a: ActionRow): boolean =>
  !isNotify(a) && !a.actionType.startsWith("ticket.") && !a.isReversal;

const took = (a: ActionRow): boolean => a.status === "SUCCEEDED" || a.status === "PARTIAL" || a.status === "REVERSED";

/**
 * The progress row (#145). Investigate → Verdict is a sequence; Contain and
 * Notify are done only when something was done for them — a containment that
 * took effect, a message that was sent with no reply outstanding — and nothing
 * for them is still open. With a benign verdict and nothing done, they read as
 * not needed. An empty list is never "done".
 */
export function stageOf(i: StageInputs): Stage {
  const status = String(i.status).toUpperCase();
  const hasVerdict = !!i.verdict;
  const benign = String(i.verdict?.disposition || "").toUpperCase() === "BENIGN";
  const contain = i.actions.filter(isContainment);
  const notify = i.actions.filter(isNotify);
  const awaiting = i.comms.some((t) => t.status === "awaiting_reply" || t.status === "followed_up");
  const containDone = contain.some(took) && contain.every(terminal);
  const notifyDone = notify.some((a) => a.status === "SUCCEEDED") && notify.every(terminal) && !awaiting && i.openOwnerItems === 0;
  const closed = status === "CONCLUDED" || status === "ARCHIVED";
  const notNeeded: Record<Step, boolean> = {
    investigate: false, verdict: false, close: false,
    contain: hasVerdict && benign && contain.length === 0,
    notify: hasVerdict && benign && notify.length === 0 && !awaiting,
  };
  const done: Record<Step, boolean> = {
    investigate: hasVerdict,
    verdict: hasVerdict,
    contain: containDone,
    notify: notifyDone,
    close: closed,
  };
  let current: Step | null;
  if (closed) current = null;
  else if (!hasVerdict) current = "investigate";
  else if (!containDone && !notNeeded.contain) current = "contain";
  else if (!notifyDone && !notNeeded.notify) current = "notify";
  else current = "close";
  return { done, notNeeded, current };
}

/** The work queue's one-word step for a list row (08 §8.3), from the list view's stage inputs. */
export function rowStep(r: InvestigationSummary): Step | null {
  const status = String(r.state).toUpperCase();
  if (status === "CONCLUDED" || status === "ARCHIVED") return null;
  if (!r.hasVerdict) return "investigate";
  if (r.openActions > 0) return "contain";
  if (r.openNotifications > 0) return "notify";
  return "close";
}

export type PrimaryAction =
  | { kind: "review"; count: number }
  | { kind: "notify" }
  | { kind: "conclude" }
  | null;

/** One primary action per stage (08 §8.2). `preflightOk` is derive/preflight.ts's conclude verdict. */
export function primaryAction(i: StageInputs, stage: Stage, preflightOk: boolean): PrimaryAction {
  if (stage.current === null) return null;
  const pending = i.actions.filter((a) => a.pending && !a.expired).length;
  if (pending > 0) return { kind: "review", count: pending };
  if (stage.current === "notify" && !i.actions.some(isNotify)) return { kind: "notify" };
  if (preflightOk && String(i.status).toUpperCase() !== "CONCLUDED") return { kind: "conclude" };
  return null;
}

/** The soonest approval deadline among REQUESTED actions (08 §8.2). */
export function nearestExpiry(actions: ActionRow[]): string | null {
  let best: string | null = null;
  for (const a of actions) {
    if (!a.pending || a.expired || !a.expiresAt) continue;
    if (!best || Date.parse(a.expiresAt) < Date.parse(best)) best = a.expiresAt;
  }
  return best;
}
