// What an action will do, in the analyst's words (#124): one sentence naming
// the action and its targets, then each parameter the request carries under a
// plain label. Shown on every pending card before Approve — the approver sees
// exactly what will be written or sent. Pure.
import type { ActionRow } from "../../../src/viewTypes";

export interface PreviewField { label: string; value: string; long: boolean }
export interface ActionPreview { sentence: string; fields: PreviewField[]; followUp: boolean }

const LABELS: Record<string, string> = {
  summary: "Summary", description: "Description", issue_type: "Type", assignee: "Assignee",
  to_status: "New state", resolution: "Resolution", subject: "Subject", message: "Message",
};

function label(actionType: string, key: string): string {
  if (key === "body") return actionType.startsWith("ticket.") ? "Comment" : "Message";
  return LABELS[key] || key.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

export function actionPreview(a: Pick<ActionRow, "actionType" | "name" | "targets" | "parameters">): ActionPreview {
  const on = a.targets.length ? " — " + a.targets.join(", ") : "";
  const sentence = (a.name || a.actionType) + on;
  const p = (a.parameters || {}) as Record<string, unknown>;
  const fields: PreviewField[] = [];
  for (const [k, v] of Object.entries(p)) {
    if (k === "thread_ref" || v === null || v === undefined || v === "") continue;
    const value = k === "follow_up_hours" ? `${v} hours if there is no reply` : typeof v === "string" ? v : JSON.stringify(v);
    fields.push({ label: k === "follow_up_hours" ? "Follow up after" : label(a.actionType, k), value, long: value.length > 80 || value.includes("\n") });
  }
  return { sentence, fields, followUp: typeof p.thread_ref === "string" && p.thread_ref !== "" };
}

/** The undo fact in words. */
export function undoWords(reversibility: string | undefined): string {
  switch (reversibility) {
    case "REVERSIBLE": return "yes";
    case "BEST_EFFORT": return "partly — treat as permanent";
    case "IRREVERSIBLE": return "no";
    default: return "not stated";
  }
}
