// Derivations over the action queue (03 §3.3, binding §2.3/§2.4). Pure.
import type { ActionRow, Seed, Verdict } from "../../../src/backend";

/** The status badge for a settled or in-flight action: label + tone. Pending
 * rows keep their approval vocabulary; the rest map the engine's lifecycle
 * status to an outcome colour. */
export function actionStatusBadge(a: ActionRow): { label: string; tone: string } {
  if (a.pending) return { label: a.pendingLabel || "PENDING", tone: "warn" };
  switch (a.status) {
    case "SUCCEEDED": return { label: "SUCCEEDED", tone: "ok" };
    case "PARTIAL": return { label: "PARTIAL", tone: "warn" };
    case "FAILED": return { label: "FAILED", tone: "bad" };
    case "TIMEOUT": return { label: "TIMEOUT", tone: "bad" };
    case "REVERSED": return { label: "REVERSED", tone: "info" };
    case "REJECTED": return { label: "REJECTED", tone: "" };
    case "EXPIRED": return { label: "EXPIRED", tone: "warn" };
    case "APPROVED": return { label: "APPROVED", tone: "info" };
    case "EXECUTING": return { label: "DISPATCHING", tone: "info" };
    default: return { label: a.status || "—", tone: "" };
  }
}

/** The approval queue: live pending rows plus EXPIRED ones that carry the
 * re-request affordance — but not an EXPIRED action that already has a
 * successor (retry_of pointing at it): a dangling button would mint duplicates. */
export function pendingQueue(actions: ActionRow[]): ActionRow[] {
  const retried = new Set(actions.map((a) => a.retryOf).filter(Boolean));
  return actions.filter((a) => a.pending || (a.status === "EXPIRED" && !retried.has(a.actionId)));
}

/** The derived presentation label (binding §2.4): ACTIVE reads as remediating
 * or verdict-reached; derived, never stored. */
export function headerLabel(engine: string, actions: ActionRow[], verdict: Verdict | null | undefined): string {
  if (engine !== "ACTIVE") return engine;
  const open = actions.some((a) => a.pending && !a.expired) ||
    actions.some((a) => a.status === "APPROVED" || a.status === "EXECUTING");
  if (open) return "ACTIVE · REMEDIATING";
  if (verdict) return "ACTIVE · VERDICT REACHED";
  return engine;
}

/** The one-word status of the comment action on the seed case (adr/0032),
 * read from the investigation's own action record; null when none exists. */
export function caseNoteStatus(seed: Seed | null | undefined, actions: ActionRow[]): string | null {
  if (!seed?.caseId) return null;
  const existing = actions.find((a) => a.actionType === "ticket.comment" && (a.targets || []).includes(seed.caseId!));
  if (!existing) return null;
  const s = String(existing.status || "").toUpperCase();
  return existing.pending ? "awaiting approval"
    : s === "SUCCEEDED" ? "posted"
    : s === "FAILED" ? "failed"
    : s === "REJECTED" ? "rejected"
    : s === "EXPIRED" ? "expired"
    : s === "APPROVED" || s === "DISPATCHED" ? "posting"
    : s.toLowerCase().replace(/_/g, " ");
}

export function fmtRemaining(ms: number): string {
  if (ms <= 0) return "expired";
  const m = Math.floor(ms / 60000), s = Math.floor((ms % 60000) / 1000);
  return m > 0 ? m + "m " + s + "s" : s + "s";
}

export function fmtSpan(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 120) return s + "s";
  const m = Math.round(s / 60);
  if (m < 120) return m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return h + "h";
  return Math.round(h / 24) + "d";
}
