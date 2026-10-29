// The view types the workbench renders: the engine's records as the host maps
// them off the wire (backend.ts) and as the webview reads them. Types only —
// no runtime code and no vscode — so the webview bundle and the host share them
// without the webview depending on the HTTP client.

/** Mirrors server.ComponentStatus. */
export interface ComponentStatus {
  ready: boolean;
  message: string;
}

/** Mirrors server.StatusResponse, plus reachability and derived compatibility. */
export interface BackendStatus {
  reachable: boolean;
  overall: string;
  components: Record<string, ComponentStatus>;
  /** Backend's advertised API version, or null if it predates the field. */
  apiVersion: number | null;
  /** True only when reachable AND apiVersion is in SUPPORTED_API_VERSIONS. */
  compatible: boolean;
}

/** Login config from GET /api/auth-config (mirrors server.AuthConfigResponse). */
export interface AuthConfig {
  issuer: string;
  clientId: string;
  /**
   * The delegate-path OIDC client (stamps delegate_kind issuer-side). The
   * second, silent PKCE flow runs against it (implementation/agent-sidecar.md
   * §5); absent when the deployment did not configure one.
   */
  agentClientId?: string;
}

/** Mirrors server.MeResponse (the fields the UI uses). */
export interface Me {
  subject: string;
  username: string;
  tenantId: string;
  roles: string[];
}

/** One investigation summary row (subset of the /api/investigations item). */
export interface InvestigationSummary {
  id: string;
  title: string;
  state: string;
  /** The triage line: what this case is about (01 §Extension 1). */
  seedSummary?: string;
  /** Last committed event — drives the unseen-changes cue (ui/02 §2.12). */
  lastEventSequence: number;
  updatedAt?: string;
  /** Actions still awaiting a human decision (triage cue, ui/06). */
  pendingActions: number;
  /** Soonest approval-window deadline among them (ISO) — the countdown cue. */
  nearestExpiry?: string;
  /** Stage inputs for the work queue (08 §8.3): list rows only. */
  hasVerdict: boolean;
  openActions: number;
  openNotifications: number;
  /** The seed, when the row carries one (the case reference in the queue). */
  seed?: Seed;
}

/** The investigation's root (mirrors server.SeedBody, 01 §Extension 1). */
export interface Seed {
  type: string; // alert | entity | question | case
  alertId?: string;
  source?: string;
  detectionFindingRef?: string;
  entityRef?: string;
  /** The human identifier an entity seed was rooted on (hostname/IP/hash). */
  entityIdentifier?: string;
  hypothesisStatement?: string;
  caseId?: string;
  caseRef?: string;
}

/** One case from query_external_cases, for the seed picker (14-case-seed.md §4.1). */
export interface CaseSummary {
  number: string;
  title: string;
  status: string;
}

/** One row of the case queue (GET /api/cases; adr/0030-cases-pane.md). */
export interface CaseRow {
  number: string;
  title?: string;
  status?: string;
  priority?: string;
  assignmentGroup?: string;
  summary?: string;
  link?: string;
  /** provenance.tool — the SoR as the binding labels it. */
  source?: string;
  openedAt?: string;
  /** The open investigation already rooted on this case, if any. */
  investigation?: { id: string; title: string; status: string };
}

/** The queue as served: which queue the rows came from, and the rows. */
export interface CaseQueue {
  queue: string;
  cases: CaseRow[];
}

/** The verdict of record (mirrors server.VerdictView) — absent when none. */
export interface Verdict {
  disposition: string; // BENIGN | SUSPICIOUS | MALICIOUS
  rationale?: string;
  verdictAt: string;
}

/** A single investigation's detail (mirrors server.InvestigationView). */
export interface InvestigationDetail {
  id: string;
  title: string;
  state: string;
  lastEventSequence: number;
  verdict?: Verdict;
  seed?: Seed;
  seedSummary?: string;
  /** Tenant case write-back posture: "confirm" | "auto" | "off" (adr/0032). */
  caseNoteOnSeed?: string;
  /** On create of a case-rooted investigation: the default note to offer. */
  caseNote?: { caseId: string; body: string; systemName?: string };
  /** The knowledge-injection posture (06 §5.1): "opt_in" | "auto". Detail responses. */
  knowledgeInjection?: string;
  /** The investigation's start (the Story lens's investigation-time divider). */
  createdAt?: string;
  /** Who opened it: the creating act's actor by display name (adr/0077). */
  openedByName?: string;
}

/** One pinned-evidence row (mirrors server.PinView). */
export interface PinRow {
  interpretationId: string;
  finding: string;
  inputRefs: string[];
  actor: string; // HUMAN | AI_DELEGATED | SYSTEM
  pinnedAt: string;
  superseded: boolean;
}

/** One investigation a ref appears in (mirrors server.AppearanceView). */
export interface Appearance {
  investigationId: string;
  title: string;
  status: string;
  seedSummary?: string;
  firstSeen?: string;
  lastSeen?: string;
  mentions: number;
}

/** One opened citation (mirrors server.EvidenceView). */
export interface EvidenceDoc {
  ref: string;
  kind: string; // "stix" | "ocsf"
  type: string;
  payload: unknown;
  classUid?: number;
  time?: string;
  recordedAt?: string;
  sourceTool?: string;
  /** On an `indicator--` ref: its sightings, vendor identity and recursed evidence (08 §8.6.1). */
  related?: { sightings: EvidenceDoc[]; identity: EvidenceDoc | null; evidence: EvidenceDoc[] };
}

/** One action's record (mirrors server.ActionRecordView): the card is the
 * summary; this is the history, the authorization, the results, the lineage. */
export interface ActionRecord {
  action: ActionRow;
  history: {
    sequenceNo: number;
    eventType: string;
    occurredAt: string;
    actor: { principal: string; principalName?: string; kind: string; model?: string };
    status?: string;
    detail?: string;
  }[];
  authorization: {
    requiredMode?: string;
    mode?: string;
    stage?: string;
    primaryApprover?: { principal: string; principalName?: string; at: string };
    secondaryApprover?: { principal: string; principalName?: string; at: string };
    secondaryApproverPool?: string[];
    policyRef?: string;
    policyVersion?: string;
    challengeAnswered?: boolean;
    matchedPolicyRef?: string;
    evaluations?: { policyRef: string; policyVersion: string; wouldHaveFired: boolean; effect: string; shadow: boolean }[];
  };
  result?: {
    finalOutcome: string;
    attempts?: number;
    adapter?: string;
    errorDetail?: string;
    rawResponseRef?: string;
    perTarget?: { target: string; outcome: string }[];
  };
  links: {
    retryOf?: string;
    retriedBy?: string[];
    reversalOfRef?: string;
    reversedByRef?: string;
    reversalAttemptedByRef?: string;
    requestingInterpretationId?: string;
  };
}

/** One concluded investigation's summary (mirrors knowledge.CaseSummaryView). */
export interface CaseSummaryView {
  investigationRef: string;
  title: string;
  body: string;
  generatorModel?: string;
  conclusionAt?: string;
  seedKind?: string;
  seedValue?: string;
  techniques: string[];
  primaryEntities: string[];
  actionsTaken: { actionType: string; targetCount: number; outcome: string }[];
  conclusionOutcome?: string;
  durationHours?: number;
  verdictDisposition?: string;
  verdictRationale?: string;
  tags: string[];
  revision: number;
  contentHash?: string;
}

/** One procedure (mirrors knowledge.SOP, the fields the inspector shows). */
export interface SOPDoc {
  id: string;
  title: string;
  body: string;
  tags: string[];
  status: string;
  recommendation?: string;
  authorId?: string;
  source?: { system?: string; url: string; version?: string };
  importedBy?: string;
  importedAt?: string;
  createdAt?: string;
  updatedAt?: string;
}

/** A prediction's declared falsification test (mirrors aggregate.QuerySpec). */
export interface TestQuery {
  tool?: string;
  queryText?: string;
}

/** One prediction under a hypothesis (mirrors server.PredictionView). */
export interface Prediction {
  id: string;
  statement: string;
  status: string;
  /** P<n>: the server view's ordinal in record order across the investigation (adr/0047); the name the agent uses too. */
  ordinal?: number;
  /** The declared test — ⚡ Test P<n> runs just it as a scoped agent turn. */
  testQuery?: TestQuery;
  /**
   * Rival hypothesis ids whose expectation for this prediction is the OPPOSITE
   * of the owner's (01 §x-prediction). Empty = merely consistent with its owner.
   */
  discriminates: string[];
  testResultRefs: string[];
  /**
   * "tool" (default) or "manual" — a person's answer decides it (adr/0054):
   * the analyst records the result; the agent cannot.
   */
  testKind?: string;
  /** The human observation the outcome cites, when it has one. */
  attestation?: PredictionAttestation;
}

/** A human attestation on a prediction outcome (mirrors server.AttestationView). */
export interface PredictionAttestation {
  noteRef: string;
  author: string;
  /** The attesting principal's display name at the time, when the record has it (adr/0077). */
  authorName?: string;
  content: string;
  source?: string;
  channel?: string;
  observedAt?: string;
}

/** One reasoning node with its predictions nested (mirrors server.HypothesisView). */
export interface Hypothesis {
  id: string;
  statement: string;
  /** Short mnemonic ("H1 rdp-lateral") — the tab / composer / thread name. */
  handle?: string;
  status: string;
  parentRef?: string;
  rootedAtRef?: string;
  labels: string[];
  predictions: Prediction[];
}

/** One JSON-schema property of an adapter's config form (11 §4.3). */
export interface ConfigField {
  type?: string;
  description?: string;
  /** x-secret: the value must be a secret REFERENCE, captured out-of-band. */
  secret?: boolean;
}

/** One installed adapter instance (mirrors server.EnablementAdapterView). */
export interface EnablementAdapter {
  name: string;
  class: string;
  enabled: boolean;
  scenario?: string;
  /** Schema-derived form source; the extension renders it, never the model. */
  configFields: Record<string, ConfigField>;
  requiredFields: string[];
  /** False for classes v0 cannot spawn — no enable affordance, honestly. */
  supportable: boolean;
}

/** One verb's enablement state (mirrors server.EnablementVerbView). */
export interface EnablementVerb {
  verb: string;
  adapters: string[];
  enabled: boolean;
  /** Disabled-but-supportable adapters that would serve this verb. */
  closableBy: string[];
}

/** The operator enablement surface (11 §5.1). */
export interface Enablement {
  adapters: EnablementAdapter[];
  verbs: EnablementVerb[];
}

/** One typed input of a capability verb (mirrors capability.InputParam). */
export interface CapabilityInput {
  name: string;
  type: string; // "entity" | "time_window" | "string" | "int" | "enum" | ...
  required: boolean;
  desc?: string;
}

/** A capability verb + its current tenant availability (mirrors capability.CapabilitySummary). */
export interface Capability {
  verb: string;
  intent: string;
  inputs: CapabilityInput[];
  output: string;
  /** "available" | "degraded" | "unavailable". */
  status: string;
}

/** The envelope a verb invocation returns (subset of server.CapabilityInvokeResponse). */
export interface CapabilityResult {
  verb: string;
  coverage: string;
  ocsfEventRefs: string[];
  observedDataRefs: string[];
  entityRefs: string[];
  degradationNotes: string[];
  events: unknown[];
  normalized: unknown[];
}

/** One tool call to record in an interpretation's side store. */
export interface ToolCallRecord {
  callId: string;
  toolName: string;
  args: unknown;
}

/** One reasoning act in the thread (mirrors server.ThreadEntryView). */
export interface ThreadEntry {
  sequenceNo: number;
  occurredAt: string;
  actor: { principal: string; principalName?: string; kind: string; model?: string };
  interpretationId: string;
  interpretationType: string;
  summary: string;
  confidence?: string;
  inputRefs: string[];
  outputRefs: string[];
  toolCalls: number;
  hasTranscript: boolean;
  superseded: boolean;
  /** A verdict act's disposition (BENIGN | SUSPICIOUS | MALICIOUS). */
  disposition?: string;
  consultedSops: { sopId: string; title?: string; used: boolean }[];
  consultedSimilar: { investigationRef: string; title?: string; band?: string; used: boolean }[];
}

/** One row of the durable action queue (mirrors server.ActionView). */
export interface ActionRow {
  actionId: string;
  actionType: string;
  tier: string;
  /** Raw engine lifecycle status (REQUESTED, PENDING_SECONDARY, APPROVED, …). */
  status: string;
  requiredMode: string;
  targets: string[];
  /** True while the action still awaits a human decision. */
  pending: boolean;
  /** The awaiting state in approval vocabulary (PENDING_MANUAL / PENDING_TWO_PARTY). */
  pendingLabel: string;
  /**
   * True when the request's approval deadline (expires_at, frozen at request
   * time) has passed: the engine will refuse an approve, so the UI must not
   * offer one. The stored status (converged by the durable expiry timer) is
   * authoritative; the deadline comparison covers the moments it lags.
   */
  expired: boolean;
  /** The approval deadline (ISO), for the live countdown. */
  expiresAt?: string;
  /** True when this action IS a reversal of another (drives the ledger marker). */
  isReversal: boolean;
  /** REVERSIBLE | BEST_EFFORT | IRREVERSIBLE — frozen at request time. */
  reversibility?: string;
  /** The blast-radius escalator raised this above the type's default tier. */
  tierEscalated: boolean;
  /** Cited evidence — each ref opens (02 §2.8). */
  evidenceRefs: string[];
  /** Lineage: the FAILED/EXPIRED action this request replaces. */
  retryOf?: string;
  /** Lineage: the action this one reverses (set when isReversal). */
  reversalOfRef?: string;
  /** The act that requested it (#153): the thread's request line names this action. */
  requestingInterpretationId?: string;
  /**
   * The tool that dispatched the action (write-side provenance) — which of
   * several possible adapters actually acted. Empty until dispatched.
   */
  adapter?: string;
  /**
   * The tool the resolver WOULD dispatch to if approved now — the pre-approval
   * preview so the analyst sees which system of record an external action will
   * hit before committing. Present only while pending.
   */
  plannedAdapter?: string;
  /** Why a FAILED action failed (adapter/dispatch reason). Empty on success. */
  errorDetail?: string;
  /** The operational reference the dispatch returned (e.g. the INC number). */
  resultRef?: string;
  /**
   * The request's frozen parameters (raw JSON) — the pre-send preview for
   * notify.* actions renders the exact message from here.
   */
  parameters?: Record<string, unknown>;
}

/** One comms trail entry (mirrors server.CommsTrailView). */
export interface CommsTrailEntry {
  direction: string; // outbound | inbound | note
  author: string;
  at: string;
  body: string;
}

/** One comms/external-work thread (mirrors server.CommsThreadView). */
export interface CommsThread {
  threadId: string;
  actionId: string;
  actionType: string;
  target: string;
  subject?: string;
  status: string; // awaiting_reply | replied | followed_up | closed
  followUpHours: number;
  followUps: number;
  unackedReply: boolean;
  sentAt: string;
  nextFollowUpAt?: string;
  trail: CommsTrailEntry[];
  followUpDue: boolean;
  escalationTriggered: boolean;
  escalationPolicy?: string;
}

/** Where an approve/reject left the action (mirrors server.ActionDecisionResponse). */
export interface ActionDecision {
  actionId: string;
  status: string;
  stage?: string;
}
