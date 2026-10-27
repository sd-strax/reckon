// The stage indicator and the primary action (08 §8.2, impl §7.1): five steps
// over the derived lifecycle — no new aggregate state. Owner items count
// under Notify once gap §6.17 exists; until then the input is empty. Pure.
import type { ActionRow, CommsThread, InvestigationSummary, Verdict } from "../../../src/backend";

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
  /** The step the investigation is at; null once closed. */
  current: Step | null;
}

export function stageOf(i: StageInputs): Stage {
  const status = String(i.status).toUpperCase();
  const hasVerdict = !!i.verdict;
  const nonNotify = i.actions.filter((a) => !isNotify(a));
  const notify = i.actions.filter(isNotify);
  const containDone = hasVerdict && nonNotify.every(terminal);
  const awaiting = i.comms.some((t) => t.status === "awaiting_reply" || t.status === "followed_up");
  const notifyDone = notify.every(terminal) && !awaiting && i.openOwnerItems === 0;
  const closed = status === "CONCLUDED" || status === "ARCHIVED";
  const done: Record<Step, boolean> = {
    investigate: hasVerdict,
    verdict: hasVerdict,
    contain: containDone,
    notify: containDone && notifyDone,
    close: closed,
  };
  let current: Step | null;
  if (closed) current = null;
  else if (!hasVerdict) current = "investigate";
  else if (!containDone) current = "contain";
  else if (!notifyDone) current = "notify";
  else current = "close";
  return { done, current };
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
