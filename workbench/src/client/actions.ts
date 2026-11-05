// The write side over HTTP (design/04, 08): the action queue and ledger, the
// catalog, requests and decisions, the action record, and the comms threads
// (outbound messages are actions through Gate 2, so they live here). Every
// decision is the analyst's own act on the human token. Answers are decoded
// through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawAction } from "../wire/backendShapes";
import type { ActionDecision, ActionRecord, ActionRow, ArtifactLocation, CommsThread } from "../viewTypes";

/** An action type the analyst may request (08 §3), with its declared inverse. */
export interface ActionTypeRow {
  actionType: string;
  reversibleBy?: string;
  tier?: string;
  name?: string;
  category?: string;
  available: boolean;
  inputs: { name: string; required: boolean }[];
}

/** Map one raw action row to the queue/ledger view: pending vocabulary, lazy expiry. */
function mapAction(a: RawAction): ActionRow {
  const status = a.status;
  const mode = a.required_mode ?? "";
  const pending = status === "REQUESTED" || status === "PENDING_SECONDARY"
    || status === "PENDING_MANUAL" || status === "PENDING_TWO_PARTY";
  const pendingLabel =
    status === "REQUESTED" && mode === "TWO_PARTY" ? "PENDING_TWO_PARTY"
    : status === "REQUESTED" ? "PENDING_MANUAL"
    : status === "PENDING_SECONDARY" ? "PENDING_TWO_PARTY"
    : status;
  const expiry = a.expires_at ? Date.parse(a.expires_at) : NaN;
  return {
    actionId: a.action_id,
    actionType: a.action_type,
    tier: a.tier,
    status,
    requiredMode: mode,
    secondaryApproverPool: a.secondary_approver_pool?.length ? a.secondary_approver_pool : undefined,
    targets: (a.targets ?? []).map((t) => t.resolved_identifier || t.entity_ref || ""),
    pending,
    pendingLabel,
    // The stored status is authoritative (the durable expiry timer owns the
    // transition); the deadline comparison covers the moments it lags.
    expired: status === "EXPIRED" || (pending && Number.isFinite(expiry) && Date.now() > expiry),
    expiresAt: a.expires_at,
    isReversal: a.is_reversal ?? false,
    // The engine sends the classification lowercase ("best_effort"); the row
    // carries it in the documented upper-case form every surface compares against.
    reversibility: a.reversibility ? a.reversibility.toUpperCase() : undefined,
    tierEscalated: a.tier_escalated ?? false,
    evidenceRefs: a.evidence_refs ?? [],
    name: a.name || undefined,
    category: a.category || undefined,
    editedAt: a.edited_at || undefined,
    inputs: (a.inputs ?? []).map((x) => ({ name: x.name, required: x.required ?? false })).filter((x) => x.name),
    retryOf: a.retry_of,
    reversalOfRef: a.reversal_of_ref,
    requestingInterpretationId: a.requesting_interpretation_id,
    adapter: a.adapter,
    plannedAdapter: a.planned_adapter,
    errorDetail: a.error_detail,
    resultRef: a.result_ref,
    parameters: a.parameters,
  };
}

export class ActionsClient {
  constructor(private readonly http: BackendHttp) {}

  /**
   * GET /api/investigations/{id}/actions — the durable action queue
   * (design/13 §7 step 4). The pending flag/label mirror the Go client's
   * ActionStatus.Pending/PendingLabel so every surface speaks one vocabulary.
   */
  async list(investigationId: string): Promise<ActionRow[]> {
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(investigationId)}/actions`, shapes.actions);
    return body.actions.map(mapAction);
  }

  /** GET /api/action-types — the write-side catalog (08 §3): each type's declared inverse. */
  async types(): Promise<ActionTypeRow[]> {
    // Each entry nests the type's declaration under `descriptor`, beside its availability.
    const body = await this.http.get("/api/action-types", shapes.actionTypes);
    return body.action_types.map(({ descriptor: d, status }) => ({
      actionType: d.action_type, reversibleBy: d.reversible_by || undefined, tier: d.default_tier,
      name: d.name || undefined, category: d.category || undefined, available: status === "available",
      inputs: d.inputs.filter((i) => i.type !== "entity" && i.name).map((i) => ({ name: i.name, required: i.required })),
    }));
  }

  /**
   * POST /api/actions — request one action on the HUMAN token (the workbench's
   * own write path — e.g. a comms follow-up). Same endpoint the agent uses via
   * the sidecar; Gate 2 and the tiers apply identically.
   */
  async request(body: {
    investigationRef: string;
    actionType: string;
    targets: { resolvedIdentifier: string; entityRef?: string }[];
    parameters?: Record<string, unknown>;
    evidenceRefs?: string[];
    rationale: string;
    /** A reversal of a SUCCEEDED action (04 §7): the inverse type at the same tier. */
    reversalOfRef?: string;
  }): Promise<{ actionId: string; status: string }> {
    const r = await this.http.post("/api/actions", {
      investigation_ref: body.investigationRef,
      action_type: body.actionType,
      targets: body.targets.map((t) => ({
        resolved_identifier: t.resolvedIdentifier,
        entity_ref: t.entityRef,
      })),
      parameters: body.parameters,
      evidence_refs: body.evidenceRefs,
      rationale: body.rationale,
      reversal_of_ref: body.reversalOfRef,
    }, shapes.actionRequested);
    return { actionId: r.action_id, status: r.status };
  }

  /**
   * POST /api/actions/{id}/approve — the analyst's own act, always on the
   * human token (a delegate token is 403'd server-side). challengeResponse is
   * the typed challenge a T3 approval requires (04 §5.5).
   */
  async approve(actionId: string, challengeResponse?: string): Promise<ActionDecision> {
    const r = await this.http.post(
      `/api/actions/${encodeURIComponent(actionId)}/approve`,
      challengeResponse ? { challenge_response: challengeResponse } : {},
      shapes.actionDecision,
    );
    return { actionId: r.action_id, status: r.status, stage: r.stage };
  }

  /** POST /api/actions/{id}/reject — ditto, human token only. */
  async reject(actionId: string, reason: string): Promise<ActionDecision> {
    const r = await this.http.post(`/api/actions/${encodeURIComponent(actionId)}/reject`, { reason }, shapes.actionDecision);
    return { actionId: r.action_id, status: r.status, stage: r.stage };
  }

  /** POST /api/actions/{id}/edit — change a pending request's parameters before approval (adr/0127). */
  async edit(actionId: string, parameters: Record<string, unknown>, reason?: string): Promise<void> {
    await this.http.post(`/api/actions/${encodeURIComponent(actionId)}/edit`, { parameters, reason });
  }

  /**
   * POST /api/actions/{id}/rerequest — re-request an expired action (human
   * token). The backend rebuilds it from the original's frozen fields with a
   * fresh window and retry_of lineage; the analyst then approves the new one.
   */
  async rerequest(actionId: string, rationale: string): Promise<void> {
    await this.http.post(`/api/actions/${encodeURIComponent(actionId)}/rerequest`, { rationale });
  }

  /**
   * POST /api/actions/{id}/resolve — a person who checked the tool records what
   * it shows for an action whose outcome is unknown (adr/0216). Human token only.
   */
  async resolve(
    actionId: string,
    body: { outcome: "SUCCEEDED" | "FAILED"; note: string; reference?: string; artifact?: ArtifactLocation },
  ): Promise<ActionDecision> {
    const r = await this.http.post(
      `/api/actions/${encodeURIComponent(actionId)}/resolve`,
      { outcome: body.outcome, note: body.note, reference: body.reference, artifact: body.artifact },
      shapes.actionDecision,
    );
    return { actionId: r.action_id, status: r.status };
  }

  /** GET /api/actions/{id} — the action's record (08 §8.6.1). */
  async record(actionId: string): Promise<ActionRecord> {
    const r = await this.http.get(`/api/actions/${encodeURIComponent(actionId)}`, shapes.actionRecord);
    const approver = (a?: { principal: string; principal_name?: string; at: string }) => a ? { principal: a.principal, principalName: a.principal_name, at: a.at } : undefined;
    const auth = r.authorization;
    return {
      action: mapAction(r.action),
      history: r.history.map((h) => ({
        sequenceNo: h.sequence_no, eventType: h.event_type, occurredAt: h.occurred_at,
        actor: { principal: h.actor.principal, principalName: h.actor.principal_name, kind: h.actor.kind, model: h.actor.model },
        status: h.status, detail: h.detail,
      })),
      authorization: {
        requiredMode: auth.required_mode, mode: auth.mode, stage: auth.stage,
        primaryApprover: approver(auth.primary_approver), secondaryApprover: approver(auth.secondary_approver),
        secondaryApproverPool: auth.secondary_approver_pool, policyRef: auth.policy_ref, policyVersion: auth.policy_version,
        challengeAnswered: auth.challenge_answered, matchedPolicyRef: auth.matched_policy_ref,
        evaluations: (auth.evaluations ?? []).map((e) => ({ policyRef: e.policy_ref, policyVersion: e.policy_version, wouldHaveFired: e.would_have_fired, effect: e.effect, shadow: e.shadow })),
      },
      result: r.result ? {
        finalOutcome: r.result.final_outcome, attempts: r.result.attempts, adapter: r.result.adapter, errorDetail: r.result.error_detail,
        rawResponseRef: r.result.raw_response_ref, perTarget: (r.result.per_target ?? []).map((p) => ({ target: p.target, outcome: p.outcome })),
      } : undefined,
      links: {
        retryOf: r.links.retry_of, retriedBy: r.links.retried_by, reversalOfRef: r.links.reversal_of_ref,
        reversedByRef: r.links.reversed_by_ref, reversalAttemptedByRef: r.links.reversal_attempted_by_ref,
        requestingInterpretationId: r.links.requesting_interpretation_id,
      },
    };
  }

  /**
   * GET /api/investigations/{id}/comms — the external-work threads (binding
   * §4): what was sent, who replied, what still awaits.
   */
  async comms(investigationId: string): Promise<CommsThread[]> {
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(investigationId)}/comms`, shapes.comms);
    return body.threads.map((t) => ({
      threadId: t.thread_id,
      actionId: t.action_id,
      actionType: t.action_type,
      target: t.target,
      subject: t.subject,
      status: t.status,
      followUpHours: t.follow_up_hours ?? 0,
      followUps: t.follow_ups ?? 0,
      unackedReply: t.unacked_reply ?? false,
      sentAt: t.sent_at,
      nextFollowUpAt: t.next_followup_at,
      trail: (t.trail ?? []).map((e) => ({
        direction: e.direction, author: e.author, at: e.at, body: e.body,
      })),
      followUpDue: t.follow_up_due ?? false,
      escalationTriggered: t.escalation_triggered ?? false,
      escalationPolicy: t.escalation_policy,
    }));
  }

  /** POST /api/comms/{id}/ack|done — the analyst's act on a thread. */
  async commsAct(threadId: string, verb: "ack" | "done"): Promise<void> {
    await this.http.post(`/api/comms/${encodeURIComponent(threadId)}/${verb}`, {});
  }

  /** POST /api/comms/{id}/snooze — push the follow-up clock. */
  async commsSnooze(threadId: string, hours: number): Promise<void> {
    await this.http.post(`/api/comms/${encodeURIComponent(threadId)}/snooze`, { hours });
  }
}
