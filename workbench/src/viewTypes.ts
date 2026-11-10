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
  /** Why an answer could not be read: the backend responded, not in the shape of /status. */
  unreadable?: string;
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
/** Who recorded a narrative revision (adr/0103): a person, or the model under a person. */
export interface NarrativeAuthor {
  kind: string; // HUMAN | AI_DELEGATED
  name?: string;
  /** The drafting model, on an AI_DELEGATED revision. */
  model?: string;
}

/**
 * The investigation's narrative (mirrors server.NarrativeView, adr/0103): the
 * current revision, who recorded it and who drafted the first, the event it
 * was written at, and the history newest first.
 */
export interface NarrativeView {
  revision: number;
  body: string;
  sequenceNo: number;
  occurredAt: string;
  author: NarrativeAuthor;
  draftedBy: NarrativeAuthor;
  revisions: { revision: number; sequenceNo: number; occurredAt: string; author: NarrativeAuthor; body: string }[];
}

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
  /** The investigation's start (the Story lens's investigation-time divider). */
  createdAt?: string;
  /** Who opened it: the creating act's actor by display name (adr/0077). */
  openedByName?: string;
  /** The written account of the record with its authorship (adr/0103); absent until one is recorded. */
  narrative?: NarrativeView;
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
/** One piece of an event's sentence (adr/0090): text, or an entity the chip opens. */
export interface SentenceToken {
  t: "entity" | "text";
  ref?: string;
  text: string;
}

/** One curated field of an event (adr/0090). */
export interface EventField {
  label: string;
  value: string;
  ref?: string;
  mono?: boolean;
  tone?: "ok" | "warn" | "bad";
}

/** An event rendered for reading (mirrors ocsfview.View, adr/0090): the
 * sentence with its entities, the curated fields, the category word. The
 * sentence is absent for a class without a template. */
export interface EventView {
  sentence?: SentenceToken[];
  fields: EventField[];
  category: string;
  /** An encoded command line read back (adr/0300); absent unless a known encoding decoded
   * cleanly. `encoding` is one of the view package's closed list ("base64-utf16le"), carried as
   * the wire id. */
  decoded?: { field: string; encoding: string; text: string };
}

export interface EvidenceDoc {
  ref: string;
  kind: string; // "stix" | "ocsf"
  type: string;
  payload: unknown;
  classUid?: number;
  time?: string;
  recordedAt?: string;
  sourceTool?: string;
  /** On an OCSF event: its rendering (adr/0090). */
  view?: EventView;
  /** On an `indicator--` ref: its sightings, vendor identity and recursed evidence (08 §8.6.1). */
  related?: { sightings: EvidenceDoc[]; identity: EvidenceDoc | null; evidence: EvidenceDoc[] };
  /** On an `x-host--` ref: its identity view (adr/0211). */
  identity?: HostIdentity;
}

/**
 * One aliases or distinct-from edge touching a host (adr/0211): "observed" is
 * a source's record (`source` the tool), "asserted" a person's (who, when, the
 * note). A withdrawn edge is listed, marked.
 */
export interface IdentityEdge {
  edgeId: string;
  otherRef: string;
  kind: "observed" | "asserted";
  source?: string;
  by?: string;
  byName?: string;
  at: string;
  note?: string;
  withdrawn: boolean;
}

/** A host's identity as the record holds it: its edges, and what it has been seen as. */
export interface HostIdentity {
  aliases: IdentityEdge[];
  distinct: IdentityEdge[];
  seenAs: { domains: string[]; agents: { tool: string; uid: string }[]; ips: string[] };
}

/** One action's record (mirrors server.ActionRecordView): the card is the
 * summary; this is the history, the authorization, the results, the lineage. */
export interface ActionRecord {
  action: ActionRow;
  history: {
    sequenceNo: number;
    eventType: string;
    occurredAt: string;
    actor: ThreadActor;
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
  /** Procedures accepted into the library from this case (source reckon://investigation/<id>, adr/0109). */
  learnedProcedures: { id: string; title: string }[];
}

/**
 * Where a procedure or a past case was consulted across investigations
 * (mirrors server.KnowledgeCitationsResponse, adr/0109): one row per
 * investigation, "cited" when any act there named it by ref (adr/0236).
 */
export interface KnowledgeCitations {
  ref: string;
  citedIn: number;
  recalledIn: number;
  investigations: {
    investigationId: string;
    title: string;
    status: string;
    cited: boolean;
    lastAt: string;
    acts: number;
    /** The SOP revisions the acts had in front of them; empty for a case. */
    revisions: number[];
  }[];
}

/**
 * One past case as the Knowledge view lists it (mirrors server.CaseSummaryRow,
 * adr/0107): the structured summary's fields, typed rather than folded into a
 * line of text.
 */
export interface PastCaseRow {
  ref: string;
  title: string;
  concludedAt?: string;
  /** The verdict's word: BENIGN, SUSPICIOUS or MALICIOUS. */
  disposition?: string;
  techniques: string[];
  entities: string[];
  /** Each action taken, with its recorded outcome word verbatim. */
  actions: { actionType: string; targetCount: number; outcome: string }[];
  durationHours?: number;
  citedIn: number;
  /** A narrative was written over the deterministic baseline. */
  narrative: boolean;
}

/**
 * What the past-case listing is narrowed by (adr/0107). Techniques are ANDed;
 * the dates are calendar days, both inclusive, as the analyst picks them.
 */
export interface PastCaseFilter {
  techniques: string[];
  disposition?: string;
  /** YYYY-MM-DD: concluded on or after this day. */
  from?: string;
  /** YYYY-MM-DD: concluded on or before this day. */
  to?: string;
}

/** The techniques and dispositions present in the corpus, most common first. */
export interface PastCaseFacets {
  techniques: { value: string; count: number }[];
  dispositions: { value: string; count: number }[];
}

/** One revision of a procedure (mirrors knowledge.SOPRevision, adr/0097). */
export interface SOPRevisionRow {
  revision: number;
  contentHash: string;
  createdAt: string;
  supersededAt?: string;
  authoredBy?: string;
  importedBy?: string;
  importedAt?: string;
  current: boolean;
}

/** A section of one revision of a procedure, read singly (mirrors knowledge.SOPSectionRead). */
export interface SOPSectionView {
  revision: number;
  cut: string;
  idx: number;
  of: number;
  location: string;
  headingPath: string[];
  /** Set when the section is part `part` of `parts` under its heading (a long section cut on paragraphs). */
  part?: number;
  parts?: number;
  text: string;
  /** Every section's location in that revision under that cut. */
  outline: string[];
}

/** One revision's outline (mirrors knowledge.SOPOutline). */
export interface SOPOutlineView {
  revision: number;
  cut: string;
  sections: { idx: number; location: string; headingPath: string[]; part?: number; parts?: number }[];
}

/**
 * What the agent read of a procedure in this investigation, as it was
 * (adr/0097): for each act that recorded sections, the sections under the
 * revision and cut it recorded, read back from the library, and beside each
 * what the current revision holds under the same heading. `current` is not
 * read when the act saw the current revision; it is null when no section of
 * the current revision sits under that heading any more.
 */
export interface ProcedureReads {
  currentRevision: number;
  turns: ProcedureTurnRead[];
}

export interface ProcedureTurnRead {
  interpretationId: string;
  revision: number;
  cut: string;
  sections: ProcedureSectionRead[];
}

export interface ProcedureSectionRead {
  idx: number;
  location: string;
  text: string;
  current?: { revision: number; idx: number; location: string; text: string } | null;
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
  /** The revision this body is (adr/0107: the outline shown beside it is read for the same one). */
  revision?: number;
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
  /** The short name the record holds (adr/0090): authored, or derived once at record time. */
  title: string;
  /** The engine derived the title from the statement because none was authored. */
  titleDerived?: boolean;
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
  /** Where it happened (#139): a link, ticket or message id, or "sha256:…" of one. */
  reference?: string;
  /** Where the evidence itself is kept (adr/0139); reckon holds the location, never the file. */
  artifact?: ArtifactLocation;
}

/** Where an artifact is kept, so someone else in the organization can fetch it
 * (mirrors server.ArtifactBody): the system, the location in it, its SHA-256,
 * and how to get access. */
export interface ArtifactLocation {
  system?: string;
  location: string;
  sha256?: string;
  access?: string;
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
  /** True only for the fixture class: the form does not enable plugin classes, so it offers no affordance. */
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
  /** The engine-minted id of the call (adr/0092); absent from an older backend. */
  callId?: string;
  verb: string;
  coverage: string;
  ocsfEventRefs: string[];
  observedDataRefs: string[];
  entityRefs: string[];
  degradationNotes: string[];
  events: unknown[];
  normalized: unknown[];
}

/**
 * One event retrieved for the investigation, without its payload (adr/0092;
 * mirrors server.EventRowView): the record, the call that retrieved it, and
 * the interpretation-layer refs derived from it.
 */
export interface EventRow {
  id: string;
  classUid: number;
  className: string;
  time: string;
  recordedAt: string;
  sourceTool: string;
  callId: string;
  verb: string;
  coverage: string;
  observedDataRefs: string[];
  entityRefs: string[];
  /** The event's rendering (adr/0090). */
  view: EventView;
}

/** One slot of a group's key with the value every member shares (adr/0095). */
export interface EventKeyPart {
  slot: string;
  label: string;
  text: string;
  /** The object the record minted for the slot, when it did; a chip. */
  ref?: string;
}

/**
 * Retrieved events of one class sharing a key (mirrors ocsfview.Group): how
 * many, the span, the distinct UTC days, and the members newest first. The
 * fold counts; whether a group collapses in the timeline is display.
 */
export interface EventGroup {
  classUid: number;
  key: EventKeyPart[];
  count: number;
  first: string;
  last: string;
  days: number;
  eventIds: string[];
}

/** What a class present in a grouped read can key on (mirrors server.EventClassKeysView). */
export interface EventClassKeys {
  classUid: number;
  className: string;
  slots: { name: string; label: string }[];
  default: string[];
  /** The key the read used. */
  key: string[];
}

/**
 * One entity's retrieved events folded by the default key (adr/0095), for the
 * Entity inspector: the rows the fold covered (the newest, bounded), the
 * groups, the entity's unpaged total and how many of it the fold covered.
 */
export interface EventRepeats {
  events: EventRow[];
  groups: EventGroup[];
  total: number;
  foldedOver: number;
}

/** What one binding did on a call (mirrors server.BindingOutcomeView): the
 * parameters as rendered, and whether it answered or why it was skipped. */
export interface BindingOutcome {
  adapter: string;
  operation: string;
  priority: number;
  params?: unknown;
  answered: boolean;
  skipped?: string;
}

/** One configured binding of a verb (mirrors server.VerbBindingView). */
export interface VerbBinding {
  adapter: string;
  operation: string;
  priority: number;
  enabled: boolean;
}

/**
 * One capability call as the inspector opens it (adr/0092; mirrors
 * server.CapabilityCallView): which binding answered, with what, how it went,
 * and what it produced. `produced` and `bindings` come with the single read.
 */
export interface CapabilityCall {
  callId: string;
  investigationId?: string;
  verb: string;
  adapter?: string;
  operation?: string;
  priority: number;
  paramsSent?: unknown;
  coverage: string;
  bindingsAnswered: number;
  bindingsTotal: number;
  nullObservationRef?: string;
  degradationNotes: string[];
  startedAt: string;
  durationMs: number;
  /** Every binding the resolver walked, in priority order; adapter/operation/priority/paramsSent summarize the first that answered. */
  outcomes: BindingOutcome[];
  produced?: { eventIds: string[]; observedDataRefs: string[]; entityRefs: string[] };
  bindings?: VerbBinding[];
}

/** One tool call to record in an interpretation's side store. */
export interface ToolCallRecord {
  callId: string;
  toolName: string;
  args: unknown;
}

/** The actor of an act as the record keeps it (mirrors server.ThreadActorView). */
export interface ThreadActor {
  principal: string;
  principalName?: string;
  /** What the identity provider asserted in the token at the time of the act (adr/0105). */
  roles?: string[];
  /** The role set reckon required of the act, any of (adr/0105). */
  requiredRoles?: string[];
  kind: string;
  model?: string;
}

/** One reasoning act in the thread (mirrors server.ThreadEntryView). */
export interface ThreadEntry {
  sequenceNo: number;
  occurredAt: string;
  actor: ThreadActor;
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
  /**
   * The procedures the act consulted, each with the revision, cut and section
   * indexes it had in front of it (adr/0303); the three are absent on acts
   * recorded before sections existed.
   */
  consultedSops: { sopId: string; title?: string; cited: boolean; revision?: number; cut?: string; sections?: number[] }[];
  consultedSimilar: { investigationRef: string; title?: string; band?: string; cited: boolean }[];
}

/** One row of the durable action queue (mirrors server.ActionView). */
export interface ActionRow {
  actionId: string;
  actionType: string;
  tier: string;
  /** Raw engine lifecycle status (REQUESTED, PENDING_SECONDARY, APPROVED, …). */
  status: string;
  requiredMode: string;
  /** Who may complete a two-party approval, frozen at request time (adr/0099); absent for manual. */
  secondaryApproverPool?: string[];
  /** Who gave the first approval, once one is in; left out of the second approval's candidates. */
  primaryApproverRef?: string;
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
  /** The catalog's human name ("Isolate host") and category (#123). */
  name?: string;
  category?: string;
  /** When the request was edited before approval (adr/0127). */
  editedAt?: string;
  /** The parameters an edit may set: the type's declared non-entity inputs. */
  inputs?: { name: string; required: boolean }[];
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

/** One event on a dependency (mirrors server.DependencyHistoryView). */
export interface DependencyHistoryEntry {
  at: string;
  by: string;
  byName?: string;
  event: string; // created | attached | detached | sent | confirmed | waived
  note?: string;
  ref?: string;
}

/**
 * One dependency (mirrors server.DependencyView, adr/0101): work the
 * conclusion depends on that only someone outside reckon can do. OPEN when
 * recorded, SENT when the notify action it rides on succeeded, CONFIRMED or
 * WAIVED by the analyst with a note. A reply never moves it.
 */
export interface Dependency {
  dependencyId: string;
  title: string;
  owner: string;
  detail?: string;
  status: string; // OPEN | SENT | CONFIRMED | WAIVED
  /** The notify.* action the ask rides on, when attached. */
  notifyActionId?: string;
  /** Where the work lives outside reckon (a ticket, a thread permalink). */
  externalRef?: string;
  createdBy: string;
  createdByName?: string;
  createdAt: string;
  resolvedBy?: string;
  resolvedByName?: string;
  resolvedAt?: string;
  resolutionNote?: string;
  history: DependencyHistoryEntry[];
}

/** Where an approve/reject left the action (mirrors server.ActionDecisionResponse). */
export interface ActionDecision {
  actionId: string;
  status: string;
  stage?: string;
}
