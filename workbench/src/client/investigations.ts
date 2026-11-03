// The investigation aggregate over HTTP: the list and the record, the
// reasoning thread and its writes (hypotheses, predictions, verdict, pins),
// the lifecycle, creation and rename, and the exports. Every write here is the
// analyst's own act on the human token. Answers are decoded through the wire
// shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawInvestigation } from "../wire/backendShapes";
import type { ArtifactLocation, Hypothesis, InvestigationDetail, InvestigationSummary, PinRow, Seed, ThreadEntry, ToolCallRecord } from "../viewTypes";

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

function mapCreated(r: RawInvestigation): InvestigationDetail {
  return {
    id: r.aggregate_id,
    title: r.title || "(untitled)",
    state: r.status,
    lastEventSequence: r.last_event_sequence,
    seed: mapSeed(r.seed),
    seedSummary: r.seed_summary,
    caseNoteOnSeed: r.case_note_on_seed,
    caseNote: r.case_note?.case_id ? { caseId: r.case_note.case_id, body: r.case_note.body, systemName: r.case_note.system_name } : undefined,
  };
}

export class InvestigationsClient {
  constructor(private readonly http: BackendHttp) {}

  /** GET /api/investigations — the investigation list (server.InvestigationView). Authenticated. */
  async list(): Promise<InvestigationSummary[]> {
    const body = await this.http.get("/api/investigations", shapes.investigations);
    return body.investigations.map((r) => ({
      id: r.aggregate_id,
      title: r.title || "(untitled)",
      state: r.status,
      seedSummary: r.seed_summary,
      lastEventSequence: r.last_event_sequence,
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
  async get(id: string): Promise<InvestigationDetail> {
    const r = await this.http.get(`/api/investigations/${encodeURIComponent(id)}`, shapes.investigation);
    return {
      id: r.aggregate_id,
      title: r.title || "(untitled)",
      state: r.status,
      lastEventSequence: r.last_event_sequence,
      verdict: r.verdict?.disposition ? {
        disposition: r.verdict.disposition,
        rationale: r.verdict.rationale,
        verdictAt: r.verdict.verdict_at,
      } : undefined,
      seed: mapSeed(r.seed),
      seedSummary: r.seed_summary,
      caseNoteOnSeed: r.case_note_on_seed,
      caseNote: r.case_note?.case_id ? { caseId: r.case_note.case_id, body: r.case_note.body, systemName: r.case_note.system_name } : undefined,
      createdAt: r.created_at,
      openedByName: r.opened_by_name,
    };
  }

  /** POST /api/investigations — seed a new investigation. Analyst role. */
  async create(title: string, seed?: Seed): Promise<InvestigationDetail> {
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
    return mapCreated(await this.http.post("/api/investigations", body, shapes.created));
  }

  /**
   * POST /api/investigations from a raw analyst-typed seed (design/ui/02 §2.7):
   * the value the analyst rooted on plus their confirmed kind ("entity" |
   * "question"). The server classifies, mints the STIX id for an entity, and
   * derives the title — the workbench never handles ids. Title is optional; the
   * server derives it from the seed's display line.
   */
  async createFromInput(value: string, kind: string, title?: string, allowUnknown?: boolean): Promise<InvestigationDetail> {
    const body: Record<string, unknown> = { seed_input: { value, kind, allow_unknown: allowUnknown || undefined } };
    if (title) body.title = title;
    return mapCreated(await this.http.post("/api/investigations", body, shapes.created));
  }

  /**
   * POST /api/investigations/{id}/rename — change the title (analyst). Human
   * curation; the aggregate bars an AI delegate. Returns the applied title.
   */
  async rename(id: string, title: string): Promise<string> {
    const r = await this.http.post(`/api/investigations/${encodeURIComponent(id)}/rename`, { title }, shapes.renamed);
    return r.title;
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
    const r = await this.http.post(
      `/api/investigations/${encodeURIComponent(investigationId)}/lifecycle`,
      {
        transition: body.transition,
        reason: body.reason,
        report_ref: body.reportRef,
        summary: body.summary,
      },
      shapes.lifecycle,
    );
    return { status: r.status, exportWorkflowId: r.export_workflow_id };
  }

  /**
   * POST /api/investigations/{id}/case_note — request the "opened in reckon"
   * note on the seed case as a ticket.comment action inside the investigation
   * (adr/0032). body overrides the default text. Returns the action's id and
   * authorization status, exactly like any action request.
   */
  async requestCaseNote(id: string, body?: string): Promise<{ actionId: string; status: string }> {
    const r = await this.http.post(
      `/api/investigations/${encodeURIComponent(id)}/case_note`,
      body ? { body } : {},
      shapes.actionRequested,
    );
    return { actionId: r.action_id, status: r.status };
  }

  /**
   * GET /api/investigations/{id}/hypotheses — the reasoning nodes, predictions
   * nested under the hypothesis they test. Authenticated.
   */
  async hypotheses(id: string): Promise<Hypothesis[]> {
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(id)}/hypotheses`, shapes.hypotheses);
    return body.hypotheses.map((h) => ({
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
              reference: p.attestation.reference || undefined,
              artifact: p.attestation.artifact?.location ? p.attestation.artifact : undefined,
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
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(id)}/thread`, shapes.thread);
    return body.thread.map((e) => ({
      sequenceNo: e.sequence_no,
      occurredAt: e.occurred_at,
      actor: {
        principal: e.actor.principal,
        principalName: e.actor.principal_name,
        kind: e.actor.kind,
        model: e.actor.model,
      },
      interpretationId: e.interpretation_id,
      interpretationType: e.interpretation_type,
      summary: e.summary,
      confidence: e.confidence,
      inputRefs: e.input_refs ?? [],
      outputRefs: e.output_refs ?? [],
      toolCalls: e.tool_calls ?? 0,
      hasTranscript: e.has_transcript ?? false,
      superseded: e.superseded ?? false,
      disposition: e.disposition || undefined,
      consultedSops: (e.consulted_sops ?? []).map((c) => ({
        sopId: c.sop_id, title: c.title, cited: c.cited,
      })),
      consultedSimilar: (e.consulted_similar_investigations ?? []).map((c) => ({
        investigationRef: c.investigation_ref, title: c.title, band: c.band, cited: c.cited,
      })),
    }));
  }

  /** GET /api/investigations/{id}/pins — the pinned-evidence fold. */
  async pins(id: string): Promise<PinRow[]> {
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(id)}/pins`, shapes.pins);
    return body.pins.map((p) => ({
      interpretationId: p.interpretation_id,
      finding: p.finding,
      inputRefs: p.input_refs,
      actor: p.actor,
      pinnedAt: p.pinned_at,
      superseded: p.superseded ?? false,
    }));
  }

  /**
   * Pin evidence: an evidence-pin interpretation on the HUMAN token — the
   * analyst's curation act (01 §Pinned evidence).
   */
  async pinEvidence(investigationId: string, finding: string, refs: string[]): Promise<void> {
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
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
    attestation: { content: string; source?: string; channel?: string; reference?: string; hashReference?: boolean; artifact?: ArtifactLocation },
  ): Promise<void> {
    await this.http.post("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "prediction",
      prediction_ref: predictionRef,
      prediction_status: status,
      rationale,
      attestation: {
        content: attestation.content,
        ...(attestation.source ? { source: attestation.source } : {}),
        ...(attestation.channel ? { channel: attestation.channel } : {}),
        ...(attestation.reference ? { reference: attestation.reference, hash_reference: attestation.hashReference || undefined } : {}),
        ...(attestation.artifact ? { artifact: attestation.artifact } : {}),
      },
    });
  }

  /**
   * Withdraw an UNTESTED prediction (→ WITHDRAWN, 01 §x-prediction): the
   * analyst discarding a bad test without a deletion. Not an evidential
   * outcome — no evidence refs. Human token; the aggregate refuses it from the AI.
   */
  async withdrawPrediction(investigationId: string, predictionRef: string, reason: string): Promise<void> {
    await this.http.post("/api/interpretations", {
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
    await this.http.post("/api/interpretations", {
      investigation_ref: investigationId,
      interpretation_type: "verdict",
      verdict: { disposition },
      input_refs: refs,
      rationale,
    });
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
    const r = await this.http.post("/api/interpretations", body, shapes.interpretation);
    return { interpretationId: r.interpretation_id };
  }

  /** POST /api/interpretations/{id}/supersede — un-pin / retraction. */
  async supersedeInterpretation(interpretationId: string, investigationId: string, reason: string): Promise<void> {
    await this.http.post(
      `/api/interpretations/${encodeURIComponent(interpretationId)}/supersede`,
      { investigation_ref: investigationId, reason },
    );
  }

  /**
   * GET /api/interpretations/{id}/transcript — the committed turn record
   * behind a thread step (the content-addressed side store).
   */
  async transcript(interpretationId: string): Promise<{ turnId?: string; body: string }> {
    const r = await this.http.get(`/api/interpretations/${encodeURIComponent(interpretationId)}/transcript`, shapes.transcript);
    return { turnId: r.turn_id, body: r.body };
  }

  /**
   * GET /api/investigations/{id}/export.md — the live markdown projection
   * (binding §6 item 10): the portable, any-time snapshot. "Paste into a
   * ticket unedited" is this call, not a file on disk.
   */
  async exportMarkdown(investigationId: string): Promise<string> {
    const res = await this.http.withBearer((base, bearer) => fetch(
      `${base}/api/investigations/${encodeURIComponent(investigationId)}/export.md`,
      { headers: { Authorization: `Bearer ${bearer}` }, signal: AbortSignal.timeout(15_000) },
    ));
    if (!res.ok) {
      throw new Error(`export.md → ${res.status}`);
    }
    return await res.text();
  }
}
