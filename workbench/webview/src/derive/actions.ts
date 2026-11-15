// Derivations over the action queue (03 §3.3, binding §2.3/§2.4). Pure.
import type { ActionRow, Seed, Verdict } from "../../../src/viewTypes";

/** What happened to the action, in the word the analyst reads (adr/0361). The
 * lifecycle status folds a PARTIAL outcome into SUCCEEDED and a TIMEOUT into
 * FAILED, so on those two statuses the recorded outcome gives the word back.
 * Every other status is its own word: a later step such as REVERSED wins over
 * the outcome, and a row with no outcome reads its status. */
export function outcomeStatus(a: Pick<ActionRow, "status" | "outcome">): string {
  if (a.status === "SUCCEEDED" && a.outcome === "PARTIAL") return "PARTIAL";
  if (a.status === "FAILED" && a.outcome === "TIMEOUT") return "TIMEOUT";
  return a.status;
}

/** The status badge for a settled or in-flight action: label + tone. Pending
 * rows keep their approval vocabulary; the rest map what happened (the
 * lifecycle status, with the recorded outcome where it says more) to an
 * outcome colour. */
export function actionStatusBadge(a: ActionRow): { label: string; tone: string } {
  if (a.pending) return { label: a.pendingLabel || "PENDING", tone: "warn" };
  switch (outcomeStatus(a)) {
    case "SUCCEEDED": return { label: "SUCCEEDED", tone: "ok" };
    case "PARTIAL": return { label: "PARTIAL", tone: "warn" };
    case "FAILED": return { label: "FAILED", tone: "bad" };
    case "TIMEOUT": return { label: "TIMEOUT", tone: "bad" };
    case "REVERSED": return { label: "REVERSED", tone: "info" };
    case "REJECTED": return { label: "REJECTED", tone: "" };
    case "EXPIRED": return { label: "EXPIRED", tone: "warn" };
    case "APPROVED": return { label: "APPROVED", tone: "info" };
    case "EXECUTING": return { label: "DISPATCHING", tone: "info" };
    case "OUTCOME_UNKNOWN": return { label: "OUTCOME UNKNOWN", tone: "bad" };
    default: return { label: outcomeStatus(a) || "—", tone: "" };
  }
}

/** What came after an action (adr/0152): its re-request, if any — the engine
 * allows one — and its most recent reversal. Each chain is a line, so the
 * original shows its successor instead of offering the step again. */
export interface Successors { retry?: ActionRow; reversal?: ActionRow }

export function successors(a: ActionRow, actions: ActionRow[]): Successors {
  const retry = actions.find((x) => x.retryOf === a.actionId);
  const reversals = actions.filter((x) => x.reversalOfRef === a.actionId);
  return { retry, reversal: reversals[reversals.length - 1] };
}

/** A reversal that ended without taking effect; the original may be reversed again. */
export function reversalEnded(r: ActionRow): boolean {
  return r.status === "FAILED" || r.status === "REJECTED" || r.status === "EXPIRED";
}

/** Re-request is offered on an EXPIRED or FAILED action that has not been
 * re-requested. `next` is the action's successors when the caller has them. */
export function canRerequest(a: ActionRow, actions: ActionRow[], next: Successors = successors(a, actions)): boolean {
  return (a.status === "EXPIRED" || a.status === "FAILED") && !next.retry;
}

/** Reverse is offered on a SUCCEEDED action with a declared inverse — reversible
 * or best effort (#151) — that has no reversal pending or done. */
export function canReverse(a: ActionRow, actions: ActionRow[], next: Successors = successors(a, actions)): boolean {
  const { reversal } = next;
  const undoable = a.reversibility === "REVERSIBLE" || a.reversibility === "BEST_EFFORT";
  return a.status === "SUCCEEDED" && undoable && (!reversal || reversalEnded(reversal));
}

/** Resolve is offered on an action whose outcome is unknown (adr/0216): only a
 * person who checked the tool can say what happened, and nothing reruns it
 * until they do. */
export function canResolve(a: ActionRow): boolean {
  return a.status === "OUTCOME_UNKNOWN";
}

/** True while the action awaits a decision the analyst can still make: it sits
 * under "Needs your decision", never on the timeline (adr/0361). */
export function awaitsDecision(a: Pick<ActionRow, "pending" | "expired">): boolean {
  return a.pending && !a.expired;
}

/** The action was approved and went on to be dispatched, or is about to be:
 * every status past the approval. The one predicate the history line and the
 * approval fact read (adr/0361). */
export function wasApproved(status: string): boolean {
  return status === "APPROVED" || status === "EXECUTING" || status === "SUCCEEDED" || status === "FAILED"
    || status === "OUTCOME_UNKNOWN" || status === "REVERSED";
}

/** A pending request needs more than the reader's own approval: a two-party
 * one, or one awaiting its second approver. The pending card states the
 * requirement at rest when this holds (adr/0361). */
export function approvalNeedsMore(a: Pick<ActionRow, "pending" | "status" | "requiredMode">): boolean {
  return a.pending && (a.status === "PENDING_SECONDARY" || a.requiredMode === "TWO_PARTY");
}

/** A failed, timed-out or partial outcome, read from the recorded outcome
 * (adr/0361). A row with no outcome falls back to a FAILED status. A reversed
 * partial reads REVERSED and is handled. */
export function failedOutcome(a: Pick<ActionRow, "status" | "outcome">): boolean {
  const s = outcomeStatus(a);
  return s === "FAILED" || s === "TIMEOUT" || s === "PARTIAL";
}

/** Whether `x` was requested after `a`: by request time, else by row order. */
function requestedAfter(x: ActionRow, a: ActionRow, actions: ActionRow[]): boolean {
  const tx = at(x.requestedAt), ta = at(a.requestedAt);
  if (!Number.isNaN(tx) && !Number.isNaN(ta) && tx !== ta) return tx > ta;
  return actions.indexOf(x) > actions.indexOf(a);
}

/** A successor that can still take over a failure: not rejected, not expired,
 * and not a pending request whose window ran out. */
function live(x: ActionRow): boolean {
  return x.status !== "REJECTED" && x.status !== "EXPIRED" && !(x.pending && x.expired);
}

/** The rule that matched (adr/0361): a re-request, a reversal of a partial
 * result, or a later reversal of the same original as a failed reversal. */
export type Takeover = { by: ActionRow; how: "retry" | "undo-partial" | "re-reversal" } | undefined;

/** The later action that took over a failure (adr/0361), if one did: its
 * live re-request, for a partial result the first live reversal of it, or,
 * for a failed reversal, the next live reversal of the same original. Any of
 * these links makes the failure handled; a successor that was rejected or
 * left to expire took nothing over. */
export function takenOverBy(a: ActionRow, actions: ActionRow[], next: Successors = successors(a, actions)): Takeover {
  if (!failedOutcome(a)) return undefined;
  const retry = firstLiveRetry(next.retry, actions);
  if (retry) return { by: retry, how: "retry" };
  const undo = outcomeStatus(a) === "PARTIAL" ? firstLiveReversalOf(a.actionId, a, actions) : undefined;
  if (undo) return { by: undo, how: "undo-partial" };
  const again = a.isReversal && a.reversalOfRef ? firstLiveReversalOf(a.reversalOfRef, a, actions) : undefined;
  if (again) return { by: again, how: "re-reversal" };
  return undefined;
}

/** The first live link of a re-request chain starting at `r`: a re-request
 * that expired or was rejected was itself re-requested, and the later link
 * takes the failure over. A cycle in the links ends the walk. */
function firstLiveRetry(r: ActionRow | undefined, actions: ActionRow[]): ActionRow | undefined {
  const seen = new Set<string>();
  for (let x = r; x && !seen.has(x.actionId); x = successors(x, actions).retry) {
    if (live(x)) return x;
    seen.add(x.actionId);
  }
  return undefined;
}

/** The first live reversal of `original` requested after `a`, other than `a`. */
function firstLiveReversalOf(original: string, a: ActionRow, actions: ActionRow[]): ActionRow | undefined {
  const later = actions.filter((x) => x !== a && x.isReversal && x.reversalOfRef === original && live(x) && requestedAfter(x, a, actions));
  return byRequested(later, "oldest")[0];
}

/** Why a timeline item opens at rest (adr/0361), or null when it is one line:
 * the outcome failed or is partial and no later action has taken it over, the
 * outcome is unknown, or the request expired and can still be re-requested.
 * The one place the rule lives. */
export function attentionKind(a: ActionRow, actions: ActionRow[], next: Successors = successors(a, actions), takeover: Takeover = takenOverBy(a, actions, next)): "failed" | "unknown" | "rerequest" | null {
  if (failedOutcome(a)) return takeover ? null : "failed";
  if (canResolve(a)) return "unknown";
  if (a.expired && a.status === "EXPIRED" && canRerequest(a, actions, next)) return "rerequest";
  return null;
}

const at = (iso: string | undefined): number => (iso ? Date.parse(iso) : NaN);
const byId = (x: ActionRow, y: ActionRow): number => (x.actionId < y.actionId ? -1 : x.actionId > y.actionId ? 1 : 0);

/** Pending actions, the one whose window closes soonest first; a request with
 * no deadline goes last (adr/0361). */
export function byDeadline(rows: ActionRow[]): ActionRow[] {
  return rows.slice().sort((x, y) => {
    const a = at(x.expiresAt), b = at(y.expiresAt);
    if (Number.isNaN(a) !== Number.isNaN(b)) return Number.isNaN(a) ? 1 : -1;
    if (!Number.isNaN(a) && a !== b) return a - b;
    return byId(x, y);
  });
}

/** The timeline's order (adr/0361): by request time, ties on the action id;
 * newest first is the same order reversed. A row served without a request
 * time sorts as the oldest. */
export function byRequested(rows: ActionRow[], order: "oldest" | "newest"): ActionRow[] {
  const out = rows.slice().sort((x, y) => {
    const a = at(x.requestedAt), b = at(y.requestedAt);
    const ka = Number.isNaN(a) ? -Infinity : a, kb = Number.isNaN(b) ? -Infinity : b;
    if (ka !== kb) return ka < kb ? -1 : 1;
    return byId(x, y);
  });
  return order === "newest" ? out.reverse() : out;
}

/** The approval queue: live pending rows plus EXPIRED ones that carry the
 * re-request affordance — but not an EXPIRED action that already has a
 * successor: a dangling button would mint duplicates. */
export function pendingQueue(actions: ActionRow[]): ActionRow[] {
  return actions.filter((a) => a.pending || (a.status === "EXPIRED" && canRerequest(a, actions)));
}

/** The derived presentation label (binding §2.4): ACTIVE reads as remediating
 * or verdict-reached; derived, never stored. */
export function headerLabel(engine: string, actions: ActionRow[], verdict: Verdict | null | undefined): string {
  if (engine !== "ACTIVE") return engine;
  const open = actions.some((a) => a.pending && !a.expired) ||
    actions.some((a) => a.status === "APPROVED" || a.status === "EXECUTING" || a.status === "OUTCOME_UNKNOWN");
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
  // What happened to the comment: a partial or timed-out post reads its outcome word (adr/0361).
  const s = String(outcomeStatus(existing) || "").toUpperCase();
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
