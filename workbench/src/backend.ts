// Thin client for the reckon backend's public HTTP surface.
//
// Mirrors server.StatusResponse (server/backend.go), including the API contract
// version that drives the design/13 §2 handshake: the extension pins a set of
// backend API versions it supports and fails closed with a diagnostic on
// mismatch, rather than dispatching against an incompatible surface.

import type {
  ComponentStatus,
  BackendStatus,
  AuthConfig,
  Me,
  InvestigationSummary,
  Seed,
  CaseSummary,
  CaseRow,
  CaseQueue,
  InvestigationDetail,
  PinRow,
  Appearance,
  EvidenceDoc,
  ActionRecord,
  CaseSummaryView,
  SOPDoc,
  Hypothesis,
  ConfigField,
  Enablement,
  CapabilityInput,
  Capability,
  CapabilityResult,
  ToolCallRecord,
  ThreadEntry,
  ActionRow,
  CommsThread,
  ActionDecision,
} from "./viewTypes";
// The view types moved to viewTypes.ts (types only); re-exported so host modules keep one import.
export type * from "./viewTypes";

/**
 * SUPPORTED_API_VERSIONS is the set of backend API contract versions
 * (server.APIVersion) this build of the workbench speaks. Add a version here
 * when the extension is updated to handle a new backend contract; a backend
 * outside this set is a hard, diagnosable incompatibility — never a silent
 * best-effort call.
 */
export const SUPPORTED_API_VERSIONS: readonly number[] = [1];

/** A refused request, with its status and the server's JSON body when it sent one. */
export class BackendRequestError extends Error {
  constructor(message: string, readonly status: number, readonly body?: Record<string, unknown>) {
    super(message);
  }
}

/** The investigation a refused create points at — a case already investigated (#158) — or undefined. */
export function existingInvestigation(err: unknown): string | undefined {
  if (err instanceof BackendRequestError && err.status === 409 && typeof err.body?.investigation_id === "string") {
    return err.body.investigation_id;
  }
  return undefined;
}

interface RawCase {
  number: string;
  title?: string;
  status?: string;
  priority?: string;
  assignment_group?: string;
  summary?: string;
  link?: string;
  source?: string;
  opened_at?: string;
  investigation?: { id: string; title: string; status: string };
}

function mapCase(r: RawCase): CaseRow {
  return {
    number: r.number,
    title: r.title,
    status: r.status,
    priority: r.priority,
    assignmentGroup: r.assignment_group,
    summary: r.summary,
    link: r.link,
    source: r.source,
    openedAt: r.opened_at,
    investigation: r.investigation,
  };
}

/** Raw server.InvestigationView row, as served under {investigations: [...]}. */
interface RawInvestigation {
  aggregate_id: string;
  title: string;
  status: string;
  last_event_sequence?: number;
  updated_at?: string;
  pending_actions?: number;
  nearest_expiry?: string;
  knowledge_injection?: string;
  has_verdict?: boolean;
  open_actions?: number;
  open_notifications?: number;
  created_at?: string;
  opened_by_name?: string;
  verdict?: { disposition?: string; rationale?: string; verdict_at?: string };
  seed?: {
    type?: string; alert_id?: string; source?: string;
    detection_finding_ref?: string; entity_ref?: string;
    entity_identifier?: string; hypothesis_statement?: string;
    case_id?: string; case_ref?: string;
  };
  seed_summary?: string;
  // The case write-back posture and, on create, the note offer (adr/0032).
  case_note_on_seed?: string;
  case_note?: { case_id?: string; body?: string; system_name?: string };
}

/** Map a raw seed body to the client Seed shape (shared by list/get/create). */
function mapSeed(s: RawInvestigation["seed"]): Seed | undefined {
  if (!s || !s.type) return undefined;
  return {
    type: s.type,
    alertId: s.alert_id,
    source: s.source,
    detectionFindingRef: s.detection_finding_ref,
    entityRef: s.entity_ref,
    entityIdentifier: s.entity_identifier,
    hypothesisStatement: s.hypothesis_statement,
    caseId: s.case_id,
    caseRef: s.case_ref,
  };
}

/** Raw server.HypothesisView, as served under {hypotheses: [...]}. */
interface RawHypothesis {
  id: string;
  statement: string;
  handle?: string;
  status: string;
  parent_ref?: string;
  rooted_at_ref?: string;
  labels?: string[];
  predictions?: RawPrediction[];
}

/** Raw server.PredictionView. */
interface RawPrediction {
  id: string;
  statement: string;
  status: string;
  ordinal?: number;
  test_query?: { tool?: string; query_text?: string };
  discriminates?: string[];
  test_result_refs?: string[];
  test_kind?: string;
  attestation?: { note_ref: string; author: string; author_name?: string; content: string; source?: string; channel?: string; observed_at?: string };
}

export class BackendClient {
  /**
   * @param baseUrl read per-call so a settings change needs no reload.
   * @param token   supplies a bearer token for authenticated calls; omitted for
   *                the public /status and /auth-config probes.
   */
  constructor(
    private readonly baseUrl: () => string,
    private readonly token?: () => Promise<string>,
  ) {}

  async status(): Promise<BackendStatus> {
    try {
      const res = await fetch(`${this.baseUrl()}/status`, {
        signal: AbortSignal.timeout(3_000),
      });
      const body = (await res.json()) as {
        overall?: string;
        components?: Record<string, ComponentStatus>;
        api_version?: number;
      };
      const apiVersion = typeof body.api_version === "number" ? body.api_version : null;
      return {
        reachable: true,
        overall: body.overall ?? "unknown",
        components: body.components ?? {},
        apiVersion,
        compatible: apiVersion !== null && SUPPORTED_API_VERSIONS.includes(apiVersion),
      };
    } catch {
      return {
        reachable: false,
        overall: "unreachable",
        components: {},
        apiVersion: null,
        compatible: false,
      };
    }
  }

  /** GET /api/auth-config — public; the login prerequisites. Throws on 503/unreachable. */
  async authConfig(): Promise<AuthConfig> {
    const res = await fetch(`${this.baseUrl()}/api/auth-config`, {
      signal: AbortSignal.timeout(5_000),
    });
    if (!res.ok) {
      throw new Error(`auth-config ${res.status} (interactive login may be unconfigured)`);
    }
    const body = (await res.json()) as { issuer?: string; client_id?: string; agent_client_id?: string };
    if (!body.issuer || !body.client_id) {
      throw new Error("auth-config missing issuer/client_id");
    }
    return { issuer: body.issuer, clientId: body.client_id, agentClientId: body.agent_client_id };
  }

  /** GET /api/me — the signed-in identity. Authenticated. */
  async me(): Promise<Me> {
    const body = await this.authedGet<{
      subject?: string;
      preferred_username?: string;
      tenant_id?: string;
      roles?: string[];
    }>("/api/me");
    return {
      subject: body.subject ?? "",
      username: body.preferred_username ?? "",
      tenantId: body.tenant_id ?? "",
      roles: body.roles ?? [],
    };
  }

  /**
   * GET /api/capabilities — the tenant's configured case queue
   * (`policies.case_queue`, 14 §4.2), or undefined when none is set or the
   * capability layer is off. The "From a case…" picker opens on it.
   */
  async caseQueue(): Promise<string | undefined> {
    const body = await this.authedGet<{ case_queue?: string }>("/api/capabilities");
    return typeof body.case_queue === "string" && body.case_queue ? body.case_queue : undefined;
  }

  /** GET /api/capabilities — count of resolvable verbs. Authenticated. */
  async capabilityCount(): Promise<number> {
    const body = await this.authedGet<{ capabilities?: unknown[] } | unknown[]>("/api/capabilities");
    if (Array.isArray(body)) {
      return body.length;
    }
    return body.capabilities?.length ?? 0;
  }

  /**
   * GET /api/capabilities — the verb catalog with per-tenant availability
   * (03 §2.8). The agent loop trims this to `available` verbs before turning
   * them into LLM tool definitions (03 §6.3). Authenticated.
   */
  async capabilities(): Promise<Capability[]> {
    interface Raw {
      descriptor?: { verb?: string; intent?: string; inputs?: CapabilityInput[]; output?: string };
      status?: string;
    }
    const body = await this.authedGet<{ capabilities?: Raw[] }>("/api/capabilities");
    return (body.capabilities ?? []).map((c) => ({
      verb: c.descriptor?.verb ?? "",
      intent: c.descriptor?.intent ?? "",
      inputs: c.descriptor?.inputs ?? [],
      output: c.descriptor?.output ?? "",
      status: c.status ?? "unavailable",
    }));
  }

  /**
   * GET /api/enablement — the operator view of installed adapters and the
   * gaps a disabled one could close (11 §5.1, §6.2). Analyst role.
   */
  async enablement(): Promise<Enablement> {
    interface RawAdapter {
      name?: string; class?: string; enabled?: boolean; scenario?: string;
      config_schema?: { properties?: Record<string, { type?: string; description?: string; "x-secret"?: boolean }>; required?: string[] };
      supportable?: boolean;
    }
    interface RawVerb {
      verb?: string; adapters?: string[]; enabled?: boolean; closable_by?: string[];
    }
    const body = await this.authedGet<{ adapters?: RawAdapter[]; verbs?: RawVerb[] }>("/api/enablement");
    return {
      adapters: (body.adapters ?? []).map((a) => {
        const fields: Record<string, ConfigField> = {};
        const props = a.config_schema?.properties ?? {};
        for (const [k, p] of Object.entries(props)) {
          fields[k] = { type: p.type, description: p.description, secret: p["x-secret"] === true };
        }
        return {
          name: a.name ?? "",
          class: a.class ?? "",
          enabled: a.enabled ?? false,
          scenario: a.scenario,
          configFields: fields,
          requiredFields: a.config_schema?.required ?? [],
          supportable: a.supportable ?? false,
        };
      }),
      verbs: (body.verbs ?? []).map((v) => ({
        verb: v.verb ?? "",
        adapters: v.adapters ?? [],
        enabled: v.enabled ?? false,
        closableBy: v.closable_by ?? [],
      })),
    };
  }

  /**
   * POST /api/enablement/adapters/{name} — the human-confirmed apply
   * (11 §5.1). Always the HUMAN token: the backend 403s a delegate token, and
   * no sidecar tool reaches this path — the tool gap is the guarantee.
   */
  async applyEnablement(adapter: string, enabled: boolean, config: Record<string, string>): Promise<void> {
    await this.authedPost(`/api/enablement/adapters/${encodeURIComponent(adapter)}`, { enabled, config });
  }

  /**
   * POST /api/capability/{verb} — the agent loop's read-tool dispatch target
   * (03 §3.4). The body carries the entity to resolve templates against, an
   * optional window, and extra template roots. Analyst role.
   */
  async invokeCapability(
    verb: string,
    body: { entity?: unknown; window?: { from: string; to: string }; extra?: Record<string, unknown> },
  ): Promise<CapabilityResult> {
    interface Raw {
      verb?: string;
      coverage?: string;
      ocsf_event_refs?: string[];
      observed_data_refs?: string[];
      entity_refs?: string[];
      degradation_notes?: string[];
      events?: unknown[];
      normalized?: unknown[];
    }
    const r = await this.authedPost<Raw>(`/api/capability/${encodeURIComponent(verb)}`, body);
    return {
      verb: r.verb ?? verb,
      coverage: r.coverage ?? "UNKNOWN",
      ocsfEventRefs: r.ocsf_event_refs ?? [],
      observedDataRefs: r.observed_data_refs ?? [],
      entityRefs: r.entity_refs ?? [],
      degradationNotes: r.degradation_notes ?? [],
      events: r.events ?? [],
      normalized: r.normalized ?? [],
    };
  }

  /**
   * POST /api/interpretations — commit one reasoning act plus the turn's
   * transcript and tool-call side store (05 §3.4). The AI authors as the
   * analyst's delegate; the backend stamps Actor.Kind from the JWT. Returns the
   * minted interpretation id. Analyst role.
   */
  async recordInterpretation(rec: {
    investigationRef: string;
    interpretationType: string;
    rationale: string;
    inputRefs?: string[];
    outputRefs?: string[];
    transcript?: { transcriptId?: string; turnId?: string; body: string };
    toolCalls?: ToolCallRecord[];
  }): Promise<{ interpretationId: string }> {
    const body = {
      investigation_ref: rec.investigationRef,
      interpretation_type: rec.interpretationType,
      rationale: rec.rationale,
      input_refs: rec.inputRefs,
      output_refs: rec.outputRefs,
      transcript: rec.transcript
        ? { transcript_id: rec.transcript.transcriptId, turn_id: rec.transcript.turnId, body: rec.transcript.body }
        : undefined,
      tool_calls: rec.toolCalls?.map((t) => ({ call_id: t.callId, tool_name: t.toolName, args: t.args })),
    };
    const r = await this.authedPost<{ interpretation_id?: string }>("/api/interpretations", body);
    return { interpretationId: r.interpretation_id ?? "" };
  }

  /** GET /api/investigations — the investigation list (server.InvestigationView). Authenticated. */
  async investigations(): Promise<InvestigationSummary[]> {
    const body = await this.authedGet<{ investigations?: RawInvestigation[] }>("/api/investigations");
    return (body.investigations ?? []).map((r) => ({
      id: r.aggregate_id,
      title: r.title || "(untitled)",
      state: r.status,
      seedSummary: r.seed_summary,
      lastEventSequence: r.last_event_sequence ?? 0,
      updatedAt: r.updated_at,
      pendingActions: r.pending_actions ?? 0,
      nearestExpiry: r.nearest_expiry,
      hasVerdict: !!r.has_verdict,
      openActions: r.open_actions ?? 0,
      openNotifications: r.open_notifications ?? 0,
      seed: mapSeed(r.seed),
    }));
  }

  /** GET /api/investigations/{id} — one investigation's detail. Authenticated. */
  async getInvestigation(id: string): Promise<InvestigationDetail> {
    const r = await this.authedGet<RawInvestigation>(`/api/investigations/${encodeURIComponent(id)}`);
    return {
      id: r.aggregate_id,
      title: r.title || "(untitled)",
      state: r.status,
      lastEventSequence: r.last_event_sequence ?? 0,
      verdict: r.verdict?.disposition ? {
        disposition: r.verdict.disposition,
        rationale: r.verdict.rationale,
        verdictAt: r.verdict.verdict_at ?? "",
      } : undefined,
      seed: mapSeed(r.seed),
      seedSummary: r.seed_summary,
      caseNoteOnSeed: r.case_note_on_seed,
      caseNote: r.case_note?.case_id ? { caseId: r.case_note.case_id, body: r.case_note.body ?? "", systemName: r.case_note.system_name } : undefined,
      knowledgeInjection: r.knowledge_injection,
      createdAt: r.created_at,
      openedByName: r.opened_by_name,
    };
  }

  /**
   * GET /api/investigations/{id}/hypotheses — the reasoning nodes, predictions
   * nested under the hypothesis they test (D.2). Authenticated.
   */
  async hypotheses(id: string): Promise<Hypothesis[]> {
    const body = await this.authedGet<{ hypotheses?: RawHypothesis[] }>(
      `/api/investigations/${encodeURIComponent(id)}/hypotheses`,
    );
    return (body.hypotheses ?? []).map((h) => ({
      id: h.id,
      statement: h.statement,
      handle: h.handle || undefined,
      status: h.status,
      parentRef: h.parent_ref,
      rootedAtRef: h.rooted_at_ref,
      labels: h.labels ?? [],
      predictions: (h.predictions ?? []).map((p) => ({
        id: p.id,
        statement: p.statement,
        status: p.status,
        ordinal: p.ordinal || undefined,
        testQuery: p.test_query
          ? { tool: p.test_query.tool, queryText: p.test_query.query_text }
          : undefined,
        discriminates: p.discriminates ?? [],
        testResultRefs: p.test_result_refs ?? [],
        testKind: p.test_kind || undefined,
        attestation: p.attestation
          ? {
              noteRef: p.attestation.note_ref,
              author: p.attestation.author,
              authorName: p.attestation.author_name || undefined,
              content: p.attestation.content,
              source: p.attestation.source,
              channel: p.attestation.channel,
              observedAt: p.attestation.observed_at,
            }
          : undefined,
      })),
    }));
  }

  /**
   * GET /api/investigations/{id}/thread — the chronological reasoning thread
   * (13 §4): every recorded reasoning act, including the interpretations
   * paired with lifecycle/action transitions, in aggregate sequence order.
   * This is the "how did it get here" surface.
   */
  async thread(id: string): Promise<ThreadEntry[]> {
    interface Raw {
      sequence_no?: number;
      occurred_at?: string;
      actor?: { principal?: string; principal_name?: string; kind?: string; model?: string };
      interpretation_id?: string;
      interpretation_type?: string;
      summary?: string;
      confidence?: string;
      input_refs?: string[];
      output_refs?: string[];
      tool_calls?: number;
      has_transcript?: boolean;
      superseded?: boolean;
      disposition?: string;
      consulted_sops?: { sop_id?: string; title?: string; used?: boolean }[];
      consulted_similar_investigations?: { investigation_ref?: string; title?: string; band?: string; used?: boolean }[];
    }
    const body = await this.authedGet<{ thread?: Raw[] }>(
      `/api/investigations/${encodeURIComponent(id)}/thread`,
    );
    return (body.thread ?? []).map((e) => ({
      sequenceNo: e.sequence_no ?? 0,
      occurredAt: e.occurred_at ?? "",
      actor: {
        principal: e.actor?.principal ?? "",
        principalName: e.actor?.principal_name,
        kind: e.actor?.kind ?? "HUMAN",
        model: e.actor?.model,
      },
      interpretationId: e.interpretation_id ?? "",
      interpretationType: e.interpretation_type ?? "",
      summary: e.summary ?? "",
      confidence: e.confidence,
      inputRefs: e.input_refs ?? [],
      outputRefs: e.output_refs ?? [],
      toolCalls: e.tool_calls ?? 0,
      hasTranscript: e.has_transcript ?? false,
      superseded: e.superseded ?? false,
      disposition: e.disposition || undefined,
      consultedSops: (e.consulted_sops ?? []).map((c) => ({
        sopId: c.sop_id ?? "", title: c.title, used: c.used ?? false,
      })),
      consultedSimilar: (e.consulted_similar_investigations ?? []).map((c) => ({
        investigationRef: c.investigation_ref ?? "", title: c.title, band: c.band, used: c.used ?? false,
      })),
    }));
  }

  /**
   * GET /api/investigations/{id}/actions — the durable action queue
   * (design/13 §7 step 4). The pending flag/label mirror the Go client's
   * ActionStatus.Pending/PendingLabel so every surface speaks one vocabulary.
   */
  async actions(investigationId: string): Promise<ActionRow[]> {
    const body = await this.authedGet<{ actions?: RawAction[] }>(
      `/api/investigations/${encodeURIComponent(investigationId)}/actions`,
    );
    return (body.actions ?? []).map(mapAction);
  }

  /**
   * GET /api/investigations/{id}/comms — the external-work threads (Phase F,
   * binding §4): what was sent, who replied, what still awaits.
   */
  async comms(investigationId: string): Promise<CommsThread[]> {
    interface Raw {
      thread_id?: string; action_id?: string; action_type?: string; target?: string;
      subject?: string; status?: string; follow_up_hours?: number; follow_ups?: number;
      unacked_reply?: boolean; sent_at?: string; next_followup_at?: string;
      trail?: { direction?: string; author?: string; at?: string; body?: string }[];
      follow_up_due?: boolean; escalation_triggered?: boolean; escalation_policy?: string;
    }
    const body = await this.authedGet<{ threads?: Raw[] }>(
      `/api/investigations/${encodeURIComponent(investigationId)}/comms`,
    );
    return (body.threads ?? []).map((t) => ({
      threadId: t.thread_id ?? "",
      actionId: t.action_id ?? "",
      actionType: t.action_type ?? "",
      target: t.target ?? "",
      subject: t.subject,
      status: t.status ?? "",
      followUpHours: t.follow_up_hours ?? 0,
      followUps: t.follow_ups ?? 0,
      unackedReply: t.unacked_reply ?? false,
      sentAt: t.sent_at ?? "",
      nextFollowUpAt: t.next_followup_at,
      trail: (t.trail ?? []).map((e) => ({
        direction: e.direction ?? "", author: e.author ?? "", at: e.at ?? "", body: e.body ?? "",
      })),
      followUpDue: t.follow_up_due ?? false,
      escalationTriggered: t.escalation_triggered ?? false,
      escalationPolicy: t.escalation_policy,
    }));
  }

  /** POST /api/comms/{id}/ack|done — the analyst's act on a thread. */
  async commsAct(threadId: string, verb: "ack" | "done"): Promise<void> {
    await this.authedPost(`/api/comms/${encodeURIComponent(threadId)}/${verb}`, {});
  }

  /** POST /api/comms/{id}/snooze — push the follow-up clock. */
  async commsSnooze(threadId: string, hours: number): Promise<void> {
    await this.authedPost(`/api/comms/${encodeURIComponent(threadId)}/snooze`, { hours });
  }

  /**
   * POST /api/actions — request one action on the HUMAN token (the workbench's
   * own write path — e.g. a comms follow-up). Same endpoint the agent uses via
   * the sidecar; Gate 2 and the tiers apply identically.
   */
  async requestAction(body: {
    investigationRef: string;
    actionType: string;
    targets: { resolvedIdentifier: string; entityRef?: string }[];
    parameters?: Record<string, unknown>;
    evidenceRefs?: string[];
    rationale: string;
    /** A reversal of a SUCCEEDED action (04 §7): the inverse type at the same tier. */
    reversalOfRef?: string;
  }): Promise<{ actionId: string; status: string }> {
    const r = await this.authedPost<{ action_id?: string; status?: string }>("/api/actions", {
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
    });
    return { actionId: r.action_id ?? "", status: r.status ?? "" };
  }

  /** GET /api/action-types — the write-side catalog (08 §3): each type's declared inverse. */
  async actionTypes(): Promise<{ actionType: string; reversibleBy?: string; tier?: string }[]> {
    // Each entry nests the type's declaration under `descriptor`, beside its availability.
    const body = await this.authedGet<{ action_types?: { descriptor?: { action_type?: string; reversible_by?: string; default_tier?: string } }[] }>("/api/action-types");
    return (body.action_types ?? []).map(({ descriptor: d = {} }) => ({ actionType: d.action_type ?? "", reversibleBy: d.reversible_by || undefined, tier: d.default_tier }));
  }

  /** GET /api/investigations/{id}/pins — the pinned-evidence fold. */
  async pins(id: string): Promise<PinRow[]> {
    interface Raw {
      interpretation_id?: string;
      finding?: string;
      input_refs?: string[];
      actor?: string;
      pinned_at?: string;
      superseded?: boolean;
    }
    const body = await this.authedGet<{ pins?: Raw[] }>(
      `/api/investigations/${encodeURIComponent(id)}/pins`,
    );
    return (body.pins ?? []).map((p) => ({
      interpretationId: p.interpretation_id ?? "",
      finding: p.finding ?? "",
      inputRefs: p.input_refs ?? [],
      actor: p.actor ?? "HUMAN",
      pinnedAt: p.pinned_at ?? "",
      superseded: p.superseded ?? false,
    }));
  }

  /**
   * Pin evidence: an evidence-pin interpretation on the HUMAN token — the
   * analyst's curation act (01 §Pinned evidence).
   */
  async pinEvidence(investigationId: string, finding: string, refs: string[]): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "evidence-pin",
      input_refs: refs,
      rationale: finding,
    });
  }

  /**
   * Record an analyst-authored hypothesis (01 §x-hypothesis): lands OPEN
   * immediately (acknowledgment is only for AI proposals). An alternate
   * explanation is simply a sibling hypothesis — the tracker scores them
   * against each other. Human token; the node id is minted server-side.
   */
  async recordHypothesis(investigationId: string, statement: string, handle?: string): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "hypothesis",
      hypothesis: handle ? { statement, handle } : { statement },
      rationale: statement,
    });
  }

  /**
   * Acknowledge (adopt) an AI-PROPOSED hypothesis into OPEN — the human taking
   * ownership of the line of inquiry (01 §Interpretation types). The adopt may
   * RESTATE the claim and/or name it (acknowledge-with-restatement, legal while
   * nothing under it is tested — the aggregate enforces). The aggregate refuses
   * this from an AI delegate; always the human token.
   */
  async acknowledgeHypothesis(
    investigationId: string,
    hypothesisRef: string,
    opts?: { restatement?: string; handle?: string },
  ): Promise<void> {
    const restated = !!(opts?.restatement || opts?.handle);
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "hypothesis",
      hypothesis_ref: hypothesisRef,
      ...(opts?.restatement ? { restatement: opts.restatement } : {}),
      ...(opts?.handle ? { handle: opts.handle } : {}),
      rationale: restated
        ? "adopted from the workbench with a restatement — taking ownership of this line of inquiry"
        : "acknowledged from the workbench — taking ownership of this line of inquiry",
    });
  }

  /**
   * Revise an OPEN hypothesis's handle and/or statement (01 §x-hypothesis:
   * human-only, statement only while untested). Same act as the adopt, on an
   * already-owned hypothesis. Human token.
   */
  async reviseHypothesis(
    investigationId: string,
    hypothesisRef: string,
    opts: { restatement?: string; handle?: string },
  ): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "hypothesis",
      hypothesis_ref: hypothesisRef,
      ...(opts.restatement ? { restatement: opts.restatement } : {}),
      ...(opts.handle ? { handle: opts.handle } : {}),
      rationale: opts.restatement ? "restated from the workbench" : "renamed from the workbench",
    });
  }

  /**
   * Abandon a live hypothesis the analyst is not pursuing (→ ABANDONED,
   * 01 §x-hypothesis). Terminal; the thread keeps it. Human token.
   */
  async abandonHypothesis(investigationId: string, hypothesisRef: string, reason: string): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "inconclusive",
      hypothesis_ref: hypothesisRef,
      abandoned: true,
      rationale: reason,
    });
  }

  /**
   * Record an analyst-authored prediction under a hypothesis (01
   * §x-prediction: either actor authors predictions) — the test only
   * environmental knowledge can supply. discriminates names the rivals whose
   * expectation is the opposite. Lands UNTESTED. Human token.
   */
  async recordPrediction(
    investigationId: string,
    hypothesisRef: string,
    statement: string,
    discriminates: string[],
    testKind?: string,
  ): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "prediction",
      prediction: {
        hypothesis_ref: hypothesisRef,
        statement,
        ...(discriminates.length ? { discriminates } : {}),
        ...(testKind === "manual" ? { test_kind: "manual" } : {}),
      },
      rationale: testKind === "manual" ? "analyst-authored test — a person tests it" : "analyst-authored test",
    });
  }

  /**
   * Record a prediction's result as the analyst (adr/0054): the outcome plus
   * an attestation — what a person said, or what was checked by hand — which
   * the engine materializes as a STIX note the outcome cites. This is both the
   * way a manual prediction is decided and the human's way through the
   * decide gate (adr/0051). Human token; the engine refuses it from the agent.
   */
  async recordPredictionResult(
    investigationId: string,
    predictionRef: string,
    status: string,
    rationale: string,
    attestation: { content: string; source?: string; channel?: string },
  ): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "prediction",
      prediction_ref: predictionRef,
      prediction_status: status,
      rationale,
      attestation: {
        content: attestation.content,
        ...(attestation.source ? { source: attestation.source } : {}),
        ...(attestation.channel ? { channel: attestation.channel } : {}),
      },
    });
  }

  /**
   * Withdraw an UNTESTED prediction (→ WITHDRAWN, 01 §x-prediction): the
   * analyst discarding a bad test without a deletion. Not an evidential
   * outcome — no evidence refs. Human token; the aggregate refuses it from the AI.
   */
  async withdrawPrediction(investigationId: string, predictionRef: string, reason: string): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "prediction",
      prediction_ref: predictionRef,
      prediction_status: "WITHDRAWN",
      rationale: reason,
    });
  }

  /** Record/revise the verdict of record (01 §Verdict). Human token. */
  async recordVerdict(
    investigationId: string,
    disposition: string,
    rationale: string,
    refs: string[],
  ): Promise<void> {
    await this.authedPost("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "verdict",
      verdict: { disposition },
      input_refs: refs,
      rationale,
    });
  }

  /** POST /api/interpretations/{id}/supersede — un-pin / retraction. */
  async supersedeInterpretation(interpretationId: string, investigationId: string, reason: string): Promise<void> {
    await this.authedPost(
      `/api/interpretations/${encodeURIComponent(interpretationId)}/supersede`,
      { investigation_ref: investigationId, reason },
    );
  }

  /**
   * GET /api/interpretations/{id}/transcript — the committed turn record
   * behind a thread step (the content-addressed side store).
   */
  async transcript(interpretationId: string): Promise<{ turnId?: string; body: string }> {
    const r = await this.authedGet<{ turn_id?: string; body?: string }>(
      `/api/interpretations/${encodeURIComponent(interpretationId)}/transcript`,
    );
    return { turnId: r.turn_id, body: r.body ?? "" };
  }

  /**
   * GET /api/entities/{ref}/appearances — cross-investigation memory
   * (binding §6.1): every investigation whose thread cites the ref, powered by
   * deterministic identity. The entity popover's "appears in N other
   * investigations" list.
   */
  async appearances(ref: string): Promise<Appearance[]> {
    interface Raw {
      investigation_id?: string; title?: string; status?: string;
      seed_summary?: string; first_seen?: string; last_seen?: string; mentions?: number;
    }
    const body = await this.authedGet<{ appearances?: Raw[] }>(
      `/api/entities/${encodeURIComponent(ref)}/appearances`,
    );
    return (body.appearances ?? []).map((a) => ({
      investigationId: a.investigation_id ?? "",
      title: a.title ?? "",
      status: a.status ?? "",
      seedSummary: a.seed_summary,
      firstSeen: a.first_seen,
      lastSeen: a.last_seen,
      mentions: a.mentions ?? 0,
    }));
  }

  /** GET /api/evidence/{ref} — citation-open (02 §2.8). */
  async evidence(ref: string): Promise<EvidenceDoc> {
    const r = await this.authedGet<RawEvidence>(`/api/evidence/${encodeURIComponent(ref)}`);
    return mapEvidence(r, ref);
  }

  /** GET /api/actions/{id} — the action's record (08 §8.6.1). */
  async actionRecord(actionId: string): Promise<ActionRecord> {
    interface RawApprover { principal?: string; principal_name?: string; at?: string }
    interface Raw {
      action?: RawAction;
      history?: { sequence_no?: number; event_type?: string; occurred_at?: string; actor?: { principal?: string; principal_name?: string; kind?: string; model?: string }; status?: string; detail?: string }[];
      authorization?: {
        required_mode?: string; mode?: string; stage?: string;
        primary_approver?: RawApprover; secondary_approver?: RawApprover; secondary_approver_pool?: string[];
        policy_ref?: string; policy_version?: string; challenge_answered?: boolean; matched_policy_ref?: string;
        evaluations?: { policy_ref?: string; policy_version?: string; would_have_fired?: boolean; effect?: string; shadow?: boolean }[];
      };
      result?: { final_outcome?: string; attempts?: number; adapter?: string; error_detail?: string; raw_response_ref?: string; per_target?: { target?: string; outcome?: string }[] };
      links?: { retry_of?: string; retried_by?: string[]; reversal_of_ref?: string; reversed_by_ref?: string; reversal_attempted_by_ref?: string; requesting_interpretation_id?: string };
    }
    const r = await this.authedGet<Raw>(`/api/actions/${encodeURIComponent(actionId)}`);
    const approver = (a?: RawApprover) => a ? { principal: a.principal ?? "", principalName: a.principal_name, at: a.at ?? "" } : undefined;
    const auth = r.authorization ?? {};
    return {
      action: mapAction(r.action ?? {}),
      history: (r.history ?? []).map((h) => ({
        sequenceNo: h.sequence_no ?? 0, eventType: h.event_type ?? "", occurredAt: h.occurred_at ?? "",
        actor: { principal: h.actor?.principal ?? "", principalName: h.actor?.principal_name, kind: h.actor?.kind ?? "HUMAN", model: h.actor?.model },
        status: h.status, detail: h.detail,
      })),
      authorization: {
        requiredMode: auth.required_mode, mode: auth.mode, stage: auth.stage,
        primaryApprover: approver(auth.primary_approver), secondaryApprover: approver(auth.secondary_approver),
        secondaryApproverPool: auth.secondary_approver_pool, policyRef: auth.policy_ref, policyVersion: auth.policy_version,
        challengeAnswered: auth.challenge_answered, matchedPolicyRef: auth.matched_policy_ref,
        evaluations: (auth.evaluations ?? []).map((e) => ({ policyRef: e.policy_ref ?? "", policyVersion: e.policy_version ?? "", wouldHaveFired: !!e.would_have_fired, effect: e.effect ?? "", shadow: !!e.shadow })),
      },
      result: r.result ? {
        finalOutcome: r.result.final_outcome ?? "", attempts: r.result.attempts, adapter: r.result.adapter, errorDetail: r.result.error_detail,
        rawResponseRef: r.result.raw_response_ref, perTarget: (r.result.per_target ?? []).map((p) => ({ target: p.target ?? "", outcome: p.outcome ?? "" })),
      } : undefined,
      links: {
        retryOf: r.links?.retry_of, retriedBy: r.links?.retried_by, reversalOfRef: r.links?.reversal_of_ref,
        reversedByRef: r.links?.reversed_by_ref, reversalAttemptedByRef: r.links?.reversal_attempted_by_ref,
        requestingInterpretationId: r.links?.requesting_interpretation_id,
      },
    };
  }

  /** GET /api/knowledge/case_summaries/{ref} — one concluded case's summary; null when none exists. */
  async caseSummary(investigationRef: string): Promise<CaseSummaryView | null> {
    interface Raw {
      investigation_ref?: string; title?: string; body?: string; generator_model?: string; conclusion_at?: string;
      seed_kind?: string; seed_value?: string; techniques?: string[]; primary_entities?: string[];
      actions_taken?: { action_type?: string; target_count?: number; outcome?: string }[];
      conclusion_outcome?: string; duration_hours?: number; verdict_disposition?: string; verdict_rationale?: string;
      tags?: string[]; revision?: number; content_hash?: string;
    }
    let r: Raw;
    try {
      r = await this.authedGet<Raw>(`/api/knowledge/case_summaries/${encodeURIComponent(investigationRef)}`);
    } catch (err) {
      if (err instanceof Error && /\b404\b/.test(err.message)) return null;
      throw err;
    }
    return {
      investigationRef: r.investigation_ref ?? investigationRef, title: r.title ?? "", body: r.body ?? "",
      generatorModel: r.generator_model, conclusionAt: r.conclusion_at, seedKind: r.seed_kind, seedValue: r.seed_value,
      techniques: r.techniques ?? [], primaryEntities: r.primary_entities ?? [],
      actionsTaken: (r.actions_taken ?? []).map((a) => ({ actionType: a.action_type ?? "", targetCount: a.target_count ?? 0, outcome: a.outcome ?? "" })),
      conclusionOutcome: r.conclusion_outcome, durationHours: r.duration_hours, verdictDisposition: r.verdict_disposition,
      verdictRationale: r.verdict_rationale, tags: r.tags ?? [], revision: r.revision ?? 0, contentHash: r.content_hash,
    };
  }

  /** GET /api/sops/{id} — one procedure, whole (08 §8.6 "Whole document"). */
  async sop(id: string): Promise<SOPDoc> {
    interface Raw {
      id?: string; title?: string; body?: string; tags?: string[]; status?: string; recommendation?: string;
      author_id?: string; source?: { system?: string; url?: string; version?: string }; imported_by?: string;
      imported_at?: string; created_at?: string; updated_at?: string;
    }
    const r = await this.authedGet<Raw>(`/api/sops/${encodeURIComponent(id)}`);
    return {
      id: r.id ?? id, title: r.title ?? "", body: r.body ?? "", tags: r.tags ?? [], status: r.status ?? "",
      recommendation: r.recommendation, authorId: r.author_id,
      source: r.source ? { system: r.source.system, url: r.source.url ?? "", version: r.source.version } : undefined,
      importedBy: r.imported_by, importedAt: r.imported_at, createdAt: r.created_at, updatedAt: r.updated_at,
    };
  }

  /**
   * POST /api/actions/{id}/approve — the analyst's own act, always on the
   * human token (a delegate token is 403'd server-side). challengeResponse is
   * the typed challenge a T3 approval requires (04 §5.5).
   */
  async approveAction(actionId: string, challengeResponse?: string): Promise<ActionDecision> {
    const r = await this.authedPost<{ action_id?: string; status?: string; stage?: string }>(
      `/api/actions/${encodeURIComponent(actionId)}/approve`,
      challengeResponse ? { challenge_response: challengeResponse } : {},
    );
    return { actionId: r.action_id ?? actionId, status: r.status ?? "", stage: r.stage };
  }

  /**
   * POST /api/actions/{id}/rerequest — re-request an expired action (human
   * token). The backend rebuilds it from the original's frozen fields with a
   * fresh window and retry_of lineage; the analyst then approves the new one.
   */
  /** POST /api/actions/{id}/edit — change a pending request's parameters before approval (adr/0127). */
  async editAction(actionId: string, parameters: Record<string, unknown>, reason?: string): Promise<void> {
    await this.authedPost(`/api/actions/${encodeURIComponent(actionId)}/edit`, { parameters, reason });
  }

  async rerequestAction(actionId: string, rationale: string): Promise<void> {
    await this.authedPost(`/api/actions/${encodeURIComponent(actionId)}/rerequest`, { rationale });
  }

  /**
   * POST /api/investigations/{id}/lifecycle — the analyst-driven state machine
   * (01 §Extension 2): activate | pause | resume | conclude | reopen | archive.
   * Conclude requires report_ref (minted by the caller — the STIX Report object
   * itself is v1) and is refused by the aggregate without a verdict of record.
   * Human token only; the aggregate's actor allowlist bars an AI delegate.
   */
  async lifecycle(
    investigationId: string,
    body: { transition: string; reason?: string; reportRef?: string; summary?: string },
  ): Promise<{ status: string; exportWorkflowId?: string }> {
    const r = await this.authedPost<{ status?: string; export_workflow_id?: string }>(
      `/api/investigations/${encodeURIComponent(investigationId)}/lifecycle`,
      {
        transition: body.transition,
        reason: body.reason,
        report_ref: body.reportRef,
        summary: body.summary,
      },
    );
    return { status: r.status ?? "", exportWorkflowId: r.export_workflow_id };
  }

  /** POST /api/actions/{id}/reject — ditto, human token only. */
  async rejectAction(actionId: string, reason: string): Promise<ActionDecision> {
    const r = await this.authedPost<{ action_id?: string; status?: string; stage?: string }>(
      `/api/actions/${encodeURIComponent(actionId)}/reject`,
      { reason },
    );
    return { actionId: r.action_id ?? actionId, status: r.status ?? "", stage: r.stage };
  }

  /** POST /api/investigations — seed a new investigation. Analyst role. */
  async createInvestigation(title: string, seed?: Seed): Promise<InvestigationDetail> {
    const body: Record<string, unknown> = { title };
    if (seed) {
      body.seed = {
        type: seed.type,
        alert_id: seed.alertId,
        source: seed.source,
        detection_finding_ref: seed.detectionFindingRef,
        entity_ref: seed.entityRef,
        entity_identifier: seed.entityIdentifier,
        hypothesis_statement: seed.hypothesisStatement,
      };
    }
    return this.mapCreated(await this.authedPost<RawInvestigation>("/api/investigations", body));
  }

  /**
   * POST /api/investigations/{id}/rename — change the title (analyst). Human
   * curation; the aggregate bars an AI delegate. Returns the applied title.
   */
  async renameInvestigation(id: string, title: string): Promise<string> {
    const r = await this.authedPost<{ title?: string }>(
      `/api/investigations/${encodeURIComponent(id)}/rename`,
      { title },
    );
    return r.title ?? title;
  }

  /**
   * POST /api/investigations from a raw analyst-typed seed (design/ui/02 §2.7):
   * the value the analyst rooted on plus their confirmed kind ("entity" |
   * "question"). The server classifies, mints the STIX id for an entity, and
   * derives the title — the workbench never handles ids. Title is optional; the
   * server derives it from the seed's display line.
   */
  async createInvestigationFromInput(value: string, kind: string, title?: string): Promise<InvestigationDetail> {
    const body: Record<string, unknown> = { seed_input: { value, kind } };
    if (title) body.title = title;
    return this.mapCreated(await this.authedPost<RawInvestigation>("/api/investigations", body));
  }

  /**
   * Import one institutional-knowledge document into the SOP corpus (design/06
   * §2.4): the raw markdown + its filename go to the server, which parses
   * frontmatter and upserts (re-import of the same source revises in place).
   * Parsing stays server-side — one robust parser, never a TS reimplementation.
   */
  async importSOPMarkdown(filename: string, content: string): Promise<{ title: string; outcome: string }> {
    const r = await this.authedPost<{ title?: string; outcome?: string }>(
      "/api/sops/import_markdown",
      { filename, content },
    );
    return { title: r.title ?? filename, outcome: r.outcome ?? "ok" };
  }

  /**
   * Query the case system of record for the seed picker (14-case-seed.md §4.1):
   * runs query_external_cases and pulls each case's metadata out of the
   * class-2005 ObservedData extensions the case normalizer wrote (03 §2.9).
   */
  async queryExternalCases(filter?: string, _status?: string, queue?: string): Promise<CaseSummary[]> {
    // The picker reads the same non-persisting queue surface the Cases view
    // does (adr/0030): browsing is not evidence; the case is promoted once,
    // when it seeds an investigation.
    const res = await this.cases({ filter, queue });
    return res.cases.map((c) => ({ number: c.number, title: c.title ?? "", status: c.status ?? "" }));
  }

  /**
   * GET /api/cases — the case queue, read-only and non-persisting
   * (adr/0030-cases-pane.md). queue defaults server-side to the tenant's
   * configured one (14 §4.2); filter narrows within it. Each row carries the
   * open investigation already rooted on it, if any.
   */
  async cases(opts: { filter?: string; queue?: string } = {}): Promise<CaseQueue> {
    const q = new URLSearchParams();
    if (opts.filter) q.set("filter", opts.filter);
    if (opts.queue) q.set("queue", opts.queue);
    const qs = q.toString();
    const body = await this.authedGet<{ queue?: string; cases?: RawCase[] }>("/api/cases" + (qs ? "?" + qs : ""));
    return { queue: body.queue ?? "", cases: (body.cases ?? []).map(mapCase) };
  }

  /** GET /api/cases/{number} — one case, read-only and non-persisting. */
  async caseDetail(number: string): Promise<CaseRow> {
    return mapCase(await this.authedGet<RawCase>("/api/cases/" + encodeURIComponent(number)));
  }

  private mapCreated(r: RawInvestigation): InvestigationDetail {
    return {
      id: r.aggregate_id,
      title: r.title || "(untitled)",
      state: r.status,
      lastEventSequence: r.last_event_sequence ?? 0,
      seed: mapSeed(r.seed),
      seedSummary: r.seed_summary,
      caseNoteOnSeed: r.case_note_on_seed,
      caseNote: r.case_note?.case_id ? { caseId: r.case_note.case_id, body: r.case_note.body ?? "", systemName: r.case_note.system_name } : undefined,
    };
  }

  /**
   * POST /api/investigations/{id}/case_note — request the "opened in reckon"
   * note on the seed case as a ticket.comment action inside the investigation
   * (adr/0032). body overrides the default text. Returns the action's id and
   * authorization status, exactly like any action request.
   */
  async requestCaseNote(id: string, body?: string): Promise<{ actionId: string; status: string }> {
    const r = await this.authedPost<{ action_id?: string; status?: string }>(
      `/api/investigations/${encodeURIComponent(id)}/case_note`,
      body ? { body } : {},
    );
    return { actionId: r.action_id ?? "", status: r.status ?? "" };
  }

  /**
   * GET /api/investigations/{id}/export.md — the live markdown projection
   * (binding §6 item 10): the portable, any-time snapshot. "Paste into a
   * ticket unedited" is this call, not a file on disk.
   */
  async exportMarkdown(investigationId: string): Promise<string> {
    if (!this.token) {
      throw new Error("no token source configured");
    }
    const bearer = await this.token();
    const res = await fetch(
      `${this.baseUrl()}/api/investigations/${encodeURIComponent(investigationId)}/export.md`,
      { headers: { Authorization: `Bearer ${bearer}` }, signal: AbortSignal.timeout(15_000) },
    );
    if (!res.ok) {
      throw new Error(`export.md → ${res.status}`);
    }
    return await res.text();
  }

  private async authedGet<T>(path: string): Promise<T> {
    return this.authed<T>("GET", path);
  }

  private async authedPost<T>(path: string, body: unknown): Promise<T> {
    return this.authed<T>("POST", path, body);
  }

  private async authed<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
    if (!this.token) {
      throw new Error("no token source configured");
    }
    const bearer = await this.token();
    const res = await fetch(`${this.baseUrl()}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${bearer}`,
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(10_000),
    });
    if (!res.ok) {
      // Surface the server's explanation when it sent one — a Gate 2 denial
      // or a guarded transition is information, not noise (approve/reject
      // rejections carry their reason in the error body).
      let detail = "";
      let parsed: Record<string, unknown> | undefined;
      try {
        parsed = (await res.json()) as Record<string, unknown>;
        detail = typeof parsed.error === "string" && parsed.error ? `: ${parsed.error}` : "";
      } catch {
        // non-JSON error body — the status alone will have to do
      }
      throw new BackendRequestError(`${method} ${path} → ${res.status}${detail}`, res.status, parsed);
    }
    return (await res.json()) as T;
  }

  /** A one-line diagnostic for an incompatible/unreachable backend, or null when compatible. */
  incompatibilityReason(status: BackendStatus): string | null {
    if (!status.reachable) {
      return "backend not reachable — run `reckon start`";
    }
    if (status.apiVersion === null) {
      return "backend predates the version handshake — upgrade the backend";
    }
    if (!status.compatible) {
      return `backend API v${status.apiVersion} unsupported (this workbench speaks v${SUPPORTED_API_VERSIONS.join(", v")}) — align versions`;
    }
    return null;
  }
}

interface RawEvidence {
  ref?: string; kind?: string; type?: string; payload?: unknown;
  class_uid?: number; time?: string; recorded_at?: string; source_tool?: string;
  related?: { sightings?: RawEvidence[]; identity?: RawEvidence | null; evidence?: RawEvidence[] };
}

function mapEvidence(r: RawEvidence, ref: string): EvidenceDoc {
  const doc: EvidenceDoc = {
    ref: r.ref ?? ref,
    kind: r.kind ?? "",
    type: r.type ?? "",
    payload: r.payload,
    classUid: r.class_uid,
    time: r.time,
    recordedAt: r.recorded_at,
    sourceTool: r.source_tool,
  };
  if (r.related) {
    doc.related = {
      sightings: (r.related.sightings ?? []).map((s) => mapEvidence(s, s.ref ?? "")),
      identity: r.related.identity ? mapEvidence(r.related.identity, r.related.identity.ref ?? "") : null,
      evidence: (r.related.evidence ?? []).map((e) => mapEvidence(e, e.ref ?? "")),
    };
  }
  return doc;
}

/** Raw server.ActionView row, as served under {actions: [...]} and on the action record. */
interface RawAction {
  action_id?: string;
  action_type?: string;
  tier?: string;
  status?: string;
  required_mode?: string;
  expires_at?: string;
  targets?: { entity_ref?: string; resolved_identifier?: string }[];
  is_reversal?: boolean;
  reversibility?: string;
  tier_escalated?: boolean;
  evidence_refs?: string[];
  edited_at?: string;
  inputs?: { name?: string; required?: boolean }[];
  name?: string;
  category?: string;
  retry_of?: string;
  reversal_of_ref?: string;
  requesting_interpretation_id?: string;
  adapter?: string;
  planned_adapter?: string;
  error_detail?: string;
  result_ref?: string;
}

/** Map one raw action row to the queue/ledger view: pending vocabulary, lazy expiry. */
function mapAction(a: RawAction): ActionRow {
  const status = a.status ?? "";
  const mode = a.required_mode ?? "";
  const pending = status === "REQUESTED" || status === "PENDING_SECONDARY"
    || status === "PENDING_MANUAL" || status === "PENDING_TWO_PARTY";
  const pendingLabel =
    status === "REQUESTED" && mode === "TWO_PARTY" ? "PENDING_TWO_PARTY"
    : status === "REQUESTED" ? "PENDING_MANUAL"
    : status === "PENDING_SECONDARY" ? "PENDING_TWO_PARTY"
    : status;
  const params = (a as { parameters?: Record<string, unknown> }).parameters;
  const expiry = a.expires_at ? Date.parse(a.expires_at) : NaN;
  return {
    actionId: a.action_id ?? "",
    actionType: a.action_type ?? "",
    tier: a.tier ?? "",
    status,
    requiredMode: mode,
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
    inputs: (a.inputs ?? []).map((x) => ({ name: x.name ?? "", required: x.required ?? false })).filter((x) => x.name),
    retryOf: a.retry_of,
    reversalOfRef: a.reversal_of_ref,
    requestingInterpretationId: a.requesting_interpretation_id,
    adapter: a.adapter,
    plannedAdapter: a.planned_adapter,
    errorDetail: a.error_detail,
    resultRef: a.result_ref,
    parameters: params,
  };
}
