// The investigation's stage steps (08 §8.2), named once (adr/0342): the
// document's progress strip and the sidebar's Investigations rows both read
// them, so the two never label a step differently. Pure, and types only from
// viewTypes, so the extension host and the webview bundle both compile it.
import type { InvestigationSummary } from "./viewTypes";

export type Step = "investigate" | "verdict" | "contain" | "notify" | "close";
export const STEPS: Step[] = ["investigate", "verdict", "contain", "notify", "close"];
export const STEP_LABELS: Record<Step, string> = { investigate: "Investigate", verdict: "Verdict", contain: "Contain", notify: "Notify", close: "Close" };

/** A list row's one-word step, from the list view's stage inputs; null once closed. */
export function rowStep(r: InvestigationSummary): Step | null {
  const status = String(r.state).toUpperCase();
  if (status === "CONCLUDED" || status === "ARCHIVED" || status === "DISCARDED") return null;
  if (!r.hasVerdict) return "investigate";
  if (r.openActions > 0) return "contain";
  if (r.openNotifications > 0) return "notify";
  return "close";
}
