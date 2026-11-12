// The extension-host ↔ webview message protocol for the investigation document
// (implementation/workbench-react.md §4). Types only: this module is imported
// by the host (tsc) and by the webview bundle (esbuild) and must stay free of
// `vscode` and of runtime code.
//
// The host owns a per-investigation cache of the engine's projections and
// posts it as a whole snapshot, then as whole re-posts of the slices an act
// touched (§3.1). Every projection message carries the investigation's event
// sequence so the webview can drop a stale patch (§3.2). Streaming and the
// one-object answers (labels, appearances) ride beside it unchanged.

import type {
  ActionRecord,
  ActionRow,
  Appearance,
  ArtifactLocation,
  Capability,
  CapabilityCall,
  CaseRow,
  CaseSummaryView,
  CommsThread,
  Dependency,
  Enablement,
  EventClassKeys,
  EventGroup,
  EventRepeats,
  EventRow,
  EvidenceDoc,
  Hypothesis,
  IdentityViews,
  InvestigationDetail,
  KnowledgeCitations,
  PastCaseFacets,
  PastCaseFilter,
  PastCaseRow,
  PinRow,
  ProcedureReads,
  SOPDoc,
  SOPOutlineView,
  ThreadEntry,
} from "./viewTypes";
import type { EnablementField } from "./enablement";

/** The four lenses (08 §8.4). */
export type Lens = "summary" | "story" | "reasoning" | "response";

/**
 * What an inspector opens (impl §2.4, §4.2). A `ref` is any STIX / OCSF id:
 * the host and the webview resolve its renderer from the id's shape and the
 * snapshot; the other kinds name records that are not STIX objects.
 */
export type InspectorTarget =
  | { kind: "ref"; ref: string }
  | { kind: "sop"; sopId: string }
  | { kind: "turn"; interpretationId: string }
  | { kind: "action"; actionId: string }
  | { kind: "thread"; threadId: string }
  | { kind: "case"; investigationRef: string }
  | { kind: "person"; principalId: string }
  // A capability call (adr/0092): the record behind a tool row or a timeline row.
  | { kind: "call"; callId: string }
  // A dependency (adr/0101): rendered from the snapshot alone.
  | { kind: "dependency"; dependencyId: string };

/**
 * One entry of the knowledge library (adr/0129): a procedure or a past case,
 * browsed or found by search. Relevance is in words and only on search hits;
 * nothing here is in the agent's context — the agent reads knowledge only
 * through its recall tools.
 */
export interface LibraryItem {
  kind: "sop" | "case";
  ref: string;
  title: string;
  /** A line under the title: tags for a procedure, the verdict and when for a case. */
  detail?: string;
  /** On a search hit: "close match" | "related" | "loosely related". */
  relevance?: string;
  /** On a browse row: distinct investigations with an act that cited it (adr/0109). */
  citedIn?: number;
}

/** An action type the analyst may request (#146): its name and its own fields. */
export interface ActionTypeView {
  actionType: string;
  name?: string;
  category?: string;
  inputs: { name: string; required: boolean }[];
}

/** Where a candidate procedure draft is (adr/0156). */
export type SopDraftState = "drafting" | "ready" | "declined" | "failed" | "added" | "discarded";

/** A committed turn's line-framed transcript body, keyed by its thread position. */
export interface ChronicleTurn {
  sequenceNo: number;
  occurredAt: string;
  body: string;
}

/** The event-time strip item: a cited ref that carries an EVENT time (binding §7). */
export interface StripItem {
  ref: string;
  time: string;
  label: string;
}

/** The minimal evidence graph (13 §7 step 7): the two-layer join as data. */
export interface GraphView {
  refs: { id: string; kind: string; type: string }[];
  edges: { from: string; to: string; ai: boolean; join?: boolean }[];
}

/** Lenses the host derives over the thread's citations with cached evidence
 * fetches (strip + graph). Null until computed; failures leave gaps. */
export interface DerivedViews {
  strip: StripItem[];
  graph: GraphView;
  /** The entities the cited evidence observed, most cited first (Summary key entities). */
  entities?: { ref: string; citations: number }[];
}

/**
 * The host's cache of one investigation, posted whole (§3.1). Slices that come
 * from an optional layer are null when the layer is off or unreachable so the
 * webview can render that honestly instead of an empty list.
 */
export interface InvestigationSnapshot {
  /** investigation.last_event_sequence when fetched. */
  seq: number;
  investigation: InvestigationDetail;
  hypotheses: Hypothesis[];
  /** The durable action queue + ledger; null when the action layer is off. */
  actions: ActionRow[] | null;
  pins: PinRow[];
  /** Comms / external-work threads; null when the layer is off. */
  comms: CommsThread[] | null;
  /** The work the conclusion depends on that only someone outside reckon can do (adr/0101); null when the layer is off. */
  dependencies: Dependency[] | null;
  /** The committed reasoning thread; null when its fetch failed. */
  thread: ThreadEntry[] | null;
  /** Transcript bodies for the most recent transcript-bearing entries. */
  turns: ChronicleTurn[];
  capabilities: Capability[] | null;
  enablement: Enablement | null;
  derived: DerivedViews | null;
  /** The events retrieved for the investigation (adr/0092), in event time;
   * null until read, or when the backend has no record of membership. */
  events?: EventRow[] | null;
  /** The unpaged count of retrieved events; `events` holds the newest page of them. */
  eventsTotal?: number;
  /** Why the retrieved record could not be read, when it could not; null once a read succeeds. */
  eventsError?: string | null;
  /** The cursor of the next older page of retrieved events; null when `events` holds the whole record. */
  eventsCursor?: string | null;
  /** The events folded by class and key (adr/0095); null when the read is the flat page, undefined until read. */
  eventGroups?: EventGroup[] | null;
  /** What each class present can key on, and the key the fold used. */
  eventKeys?: EventClassKeys[];
  /** How many of `eventsTotal` the fold covered (the newest). */
  eventsFoldedOver?: number;
}

/**
 * A live rival hypothesis as the webview briefs the scoped ⚡ turns about it
 * (02 §2.9: propose-tests / decide read the WHOLE live set, not the tab).
 */
export interface RivalBrief {
  id: string;
  handle?: string;
  statement: string;
  /** Untested predictions on the rival that discriminate against the hypothesis in view. */
  untestedDiscriminators: number;
}

/** An account the record holds under a name (adr/0324), as a refusal lists it. */
export interface KnownAccount {
  ref: string;
  accountType: string;
  accountLogin: string;
}

/** The kind of entity a seed names (adr/0324). */
export type SeedEntityKind = "host" | "account" | "person";

/** The other end of a join or a split: by ref, by hostname (a host), or by a hint (an account, a person). */
export type IdentityOther = { ref: string } | { hostname: string } | { hint: string };

/** What the extension host posts to the webview to render. */
export type RenderMessage =
  | { type: "loading" }
  // The unseeded state (design/ui/02 §2.7): render the "root this investigation"
  // surface instead of a document. "seeded" flips back once the seed persists;
  // "draft.error" reports a failed create without leaving the draft.
  | { type: "draft"; mode?: "case"; caseQueue?: string }
  | { type: "seeded" }
  // knownAccounts: on an account seed the record does not hold, the accounts
  // it does hold under the name (adr/0324), for the analyst to seed on one.
  | { type: "draft.error"; message: string; unknownEntity?: boolean; knownAccounts?: KnownAccount[] }
  // The "from a case" seed surface (13 §3): the SoR search results, and a
  // search failure rendered inline in the case sub-form.
  | { type: "draft.cases"; cases: { number: string; title: string; status: string }[] }
  | { type: "draft.caseError"; message: string }
  // The whole cache, and whole re-posts of the slices an act touched (§3.1).
  | { type: "state.snapshot"; seq: number; snapshot: InvestigationSnapshot }
  | { type: "state.patch"; seq: number; patch: Partial<InvestigationSnapshot> }
  // Who the signed-in principal is, for "edited by you" and attestations.
  | { type: "me"; name: string; principal: string }
  | { type: "error"; message: string }
  // Open the in-document rename card (never the top-of-window quick input).
  | { type: "rename.begin"; title: string }
  // Applied rename: update the title in place without a full reload.
  | { type: "renamed"; title: string }
  // A resolved human label for an evidence ref (e.g. "host · WIN-FILE01").
  | { type: "evidence.label"; ref: string; label: string }
  // The read failed: the webview may ask again after the next refresh.
  | { type: "evidence.unresolved"; ref: string }
  // The inspector's one-object answers (impl §4.1): the webview opened a pane
  // from the snapshot and asked for what the record holds beyond it.
  // `around` is the window of retrieved events on the same entity (adr/0092); empty when the record has none.
  | { type: "inspector.evidence"; ref: string; doc: EvidenceDoc; appearances: Appearance[]; around?: EventRow[]; repeats?: EventRepeats }
  | { type: "inspector.call"; callId: string; call: CapabilityCall }
  // citations is where the ref was consulted across investigations (adr/0109);
  // absent when that read failed, so the document still opens. reads is what
  // the agent read of it here, as it was (adr/0097): absent when no act
  // recorded sections, and when that read failed.
  | { type: "inspector.procedure"; sopId: string; sop: SOPDoc; citations?: KnowledgeCitations; reads?: ProcedureReads; outline?: SOPOutlineView }
  | { type: "inspector.turn"; interpretationId: string; body: string }
  | { type: "inspector.action"; actionId: string; record: ActionRecord }
  // summary is null when the corpus holds none for the ref (an honest gap).
  | { type: "inspector.case"; investigationRef: string; summary: CaseSummaryView | null; citations?: KnowledgeCitations }
  | { type: "inspector.error"; target: string; message: string }
  // One identity act's outcome (adr/0211, adr/0324), by the request id the
  // webview gave it: recorded, with the view of ref after it in the field of
  // its kind (null when the view could not be read: the last view stands),
  // or refused, for its form to say. A refusal over a hint that named
  // several accounts or persons lists them, for the analyst to pick one.
  | { type: "inspector.identity"; ref: string; requestId: string; views: IdentityViews | null }
  | { type: "identity.error"; ref: string; requestId: string; message: string; accounts?: KnownAccount[]; persons?: string[] }
  // An identity act on ref landed: every host, account and person a panel
  // holds may read differently now (the alias closure), so each is stale and
  // a shown one is read again once. Posted to every open panel; `acted` on
  // the one that sent the act, whose view of ref came with the act's answer.
  | { type: "identity.changed"; ref: string; acted?: true }
  // The action types the analyst can request directly (#146): available ones only.
  | { type: "actionTypes"; types: ActionTypeView[] }
  // Drafting a procedure from a concluded case (adr/0156): where the draft is,
  // and whether the case's knowledge summary was written.
  | { type: "sop.status"; state: SopDraftState; title?: string; message?: string }
  | { type: "summary.status"; state: "written" | "baseline" | "failed"; detail?: string }
  // The narrative (adr/0103): a draft in flight, written (the refresh carries
  // the text on the investigation) or failed; an edit saving, saved (after
  // the re-read) or refused, the text still in the editor.
  | { type: "narrative.status"; state: "drafting" | "written" | "failed" | "saving" | "saved" | "save-failed"; message?: string }
  // The knowledge library (adr/0129): one search's hits.
  | { type: "library.results"; query: string; sops: LibraryItem[]; cases: LibraryItem[] }
  // The Knowledge view's past cases under a filter (adr/0107), with how many
  // matched; error is set, and rows empty, when the read failed.
  | { type: "pastCases"; filter: PastCaseFilter; rows: PastCaseRow[]; total: number; error?: string }
  // Every past case in the corpus, beyond the listing's page (adr/0107): the
  // "m" of "n of m match". Not sent when it could not be read.
  | { type: "pastCases.corpus"; total: number }
  | { type: "pastCases.facets"; facets: PastCaseFacets }
  // The host finished handling a decision on this action (approve, reject,
  // re-request, reversal), whatever the outcome, after refreshing the record.
  | { type: "action.settled"; actionId: string }
  | { type: "turn.start" }
  | { type: "turn.user"; text: string }
  // An aside (the "btw" lens): a scoped agent turn rendered collapsed.
  | { type: "aside.start"; label: string }
  | { type: "turn.reasoning"; delta: string }
  | { type: "turn.text"; delta: string }
  | { type: "turn.step"; round: number }
  // The history was condensed to fit the model's context window (adr/0230).
  | { type: "turn.notice"; text: string }
  | { type: "turn.tool"; name: string; input: unknown }
  | { type: "turn.toolResult"; name: string; isError: boolean; coverage?: string; events?: number; refs?: string[]; callId?: string }
  | { type: "turn.committed"; interpretationId: string }
  | { type: "turn.error"; message: string }
  | { type: "turn.end"; usage?: { input: number; output: number; cacheRead: number }; rounds?: number }
  // Stage a request in the composer, never sent (adr/0344): "Ask reckon to
  // use this" from the sidebar's Knowledge view.
  | { type: "composer.stage"; text: string };

/** What the webview posts back to the extension host. */
export type InboundMessage =
  | { type: "refresh" | "ready" }
  // Draft a procedure from this concluded case, then add or discard it (adr/0156).
  | { type: "sop.draft"; force?: boolean }
  | { type: "sop.accept" }
  | { type: "sop.discard" }
  // The investigation's narrative (adr/0103): ask the model for a draft, or
  // record the analyst's own revision.
  | { type: "narrative.draft" }
  | { type: "narrative.edit"; body: string }
  // The Knowledge tab's (adr/0107, adr/0346): search the library, and its
  // past cases under a filter. An investigation document sends neither
  // (adr/0344): its host has no handler for them.
  | { type: "library.search"; text: string }
  // The page the Knowledge tab now shows (adr/0346): an item, or with no
  // target the search page. The host reads only what the shown page uses.
  | { type: "knowledge.showing"; target?: KnowledgeTarget }
  | { type: "pastCases.get"; filter: PastCaseFilter }
  // The lens and companion mode the analyst chose, mirrored to globalState so
  // the tree can deep-link (view state, never investigation state).
  | { type: "view.state"; lens?: Lens; companion?: "conversation" | "thread" }
  // The seed surface (design/ui/02 §2.7): the analyst's confirmed root.
  // entityKind is the analyst's kind for an entity token (adr/0324); absent
  // for a question, an address and a hash.
  // accountRef seeds on a persisted account the server listed (knownAccounts),
  // by its ref: value is then only what the analyst sees.
  | { type: "seed.submit"; value: string; kind: string; allowUnknown?: boolean; entityKind?: SeedEntityKind; accountRef?: string }
  | { type: "seed.alert"; alertId: string; source: string }
  | { type: "seed.caseSearch"; filter: string; queue?: string }
  | { type: "seed.case"; caseNumber: string }
  // The analyst confirmed (and possibly edited) the case note (adr/0032).
  | { type: "caseNote.post"; body: string }
  | { type: "send"; text: string }
  // Decisions name the action only; the host reads its tier and type from the record.
  | { type: "action.approve"; actionId: string; challenge?: string }
  | { type: "action.reject"; actionId: string; reason: string }
  | { type: "action.rerequest"; actionId: string; rationale: string }
  // Resolve an action whose outcome is unknown (adr/0216): what the tool shows.
  | { type: "action.resolve"; actionId: string; outcome: "SUCCEEDED" | "FAILED"; note: string; reference?: string; artifact?: ArtifactLocation }
  // Change a pending request's parameters before approving it (adr/0127).
  | { type: "action.edit"; actionId: string; parameters: Record<string, unknown>; reason?: string }
  // The analyst requests an action from a pinned finding, without the agent (#146).
  | { type: "action.request"; actionType: string; target: string; parameters: Record<string, unknown>; rationale: string; evidenceRefs: string[] }
  // Request the inverse action of a SUCCEEDED reversible one (04 §7); lands in the queue like any action.
  | { type: "action.reverse"; actionId: string; rationale: string }
  | { type: "pin.add"; refs: string[]; finding: string }
  | { type: "pin.unpin"; interpretationId: string; reason: string }
  | { type: "verdict.submit"; disposition: string; rationale: string; refs: string[] }
  | { type: "hyp.ack"; hypothesisRef: string; restatement?: string; handle?: string }
  | { type: "hyp.revise"; hypothesisRef: string; restatement?: string; handle?: string }
  | { type: "hyp.abandon"; hypothesisRef: string; reason: string }
  | { type: "hyp.new"; statement: string; handle?: string }
  | { type: "pred.new"; hypothesisRef: string; statement: string; discriminates: string[]; testKind?: string; title?: string }
  | { type: "pred.withdraw"; predictionRef: string; reason: string }
  // The analyst records a prediction's result with an attestation (adr/0054).
  | { type: "pred.result"; predictionRef: string; status: string; content: string; source?: string; channel?: string; reference?: string; hashReference?: boolean; artifact?: ArtifactLocation }
  | { type: "hyp.proposeTests"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  | { type: "hyp.runTests"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  // One prediction's test, run as a scoped turn from its row (the ⚡ family).
  | { type: "pred.test"; predictionRef: string; predictionName: string; statement: string; queryText?: string; tool?: string; hypothesisRef: string; hypStatement: string; handle?: string; rivals?: RivalBrief[] }
  | { type: "hyp.decide"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  // `ask`: the fields the native flow still asks for — every secret, and any
  // required field the form left empty — with the schema's rules (adr/0342).
  | { type: "enablement.apply"; adapter: string; enabled: boolean; config: Record<string, string>; ask: EnablementField[] }
  | { type: "comms.act"; threadId: string; verb: "ack" | "done" }
  | { type: "comms.snooze"; threadId: string; hours: number }
  | { type: "comms.followup"; threadId: string; actionType: string; target: string; subject: string; message: string; followUpHours: number }
  // Dependencies (adr/0101): the analyst records, rides on a message, and resolves them.
  | { type: "dependency.create"; title: string; owner: string; detail?: string; externalRef?: string; notifyActionId?: string }
  | { type: "dependency.attach"; dependencyId: string; notifyActionId: string }
  | { type: "dependency.resolve"; dependencyId: string; verb: "confirm" | "waive"; note: string; externalRef?: string }
  | { type: "lifecycle"; transition: string; reason?: string; summary?: string }
  // The analyst stopped the running turn.
  | { type: "turn.stop" }
  | { type: "evidence.resolve"; ref: string }
  // The Story timeline asks for the next older page of retrieved events (adr/0092).
  | { type: "events.older"; cursor: string }
  // The Story timeline changes the fold (adr/0095): a key spec ("default" or
  // "class:slot,slot;…"), or null for the flat newest page.
  | { type: "events.fold"; keys: string | null }
  // The webview opened an inspector pane (view state) and needs the record
  // behind it; the host answers with the matching inspector.* message.
  | { type: "inspector.open"; target: InspectorTarget }
  // The analyst's acts on an entity's identity (adr/0211, adr/0324): "same
  // as" (aliases) or "not the same" (distinct) on a host, an account or a
  // person, the other end named by ref, by a hostname, or by a hint the
  // server looks an account or a person up by (a bare string is a host's
  // name or ref, as before); an account's owner, by ref or by hint; and
  // withdrawing an asserted edge. Human acts, each carrying the request id
  // its outcome is answered under.
  | { type: "identity.assert"; ref: string; requestId: string; relation: "aliases" | "distinct"; other: string | IdentityOther; note?: string }
  | { type: "identity.owner"; ref: string; requestId: string; person: { ref: string } | { hint: string }; note?: string }
  | { type: "identity.withdraw"; ref: string; requestId: string; edgeId: string; note?: string }
  | { type: "export.open" }
  | { type: "copy"; text: string }
  | { type: "investigation.open"; id: string; title?: string }
  | { type: "rename"; title: string };

// ---- the other surfaces -------------------------------------------------------
//
// Every reckon panel is a root of the one bundle (adr/0248); the host names
// the surface on the root element and the page mounts that surface's app.
// Each surface has its own pair of unions, as narrow as the panel.

/** The surfaces the bundle can mount. */
export type Surface = "investigation" | "case" | "import" | "knowledge";

/**
 * The Knowledge tab (adr/0107, adr/0346, ui/09 §9.1): knowledge only, outside
 * any investigation. It mounts the investigation document's store with no
 * investigation loaded, so its pair is a subset of that document's: search,
 * the past-case listing, the procedure and case inspectors, and opening an
 * investigation a case names. Nothing in it acts on an investigation.
 */
export type KnowledgeRenderMessage = Extract<RenderMessage, {
  type: "library.results" | "pastCases" | "pastCases.corpus" | "pastCases.facets" | "inspector.procedure" | "inspector.case" | "inspector.error" | "error";
}> | KnowledgeCue;

/** A procedure or a past case: what the Knowledge tab's item page shows. */
export type KnowledgeTarget = Extract<InspectorTarget, { kind: "sop" | "case" }>;

/**
 * What the sidebar asks of the Knowledge tab (adr/0344, adr/0346): open on one
 * item, or on the search page with its query bar focused. The page's own,
 * never the store's.
 */
export type KnowledgeCue =
  | { type: "knowledge.show"; target: KnowledgeTarget }
  | { type: "knowledge.search" };

/** The intents only the Knowledge view sends (adr/0344). */
export type KnowledgeOnlyIntent = "library.search" | "pastCases.get" | "knowledge.showing";

/** What an investigation document's host answers: every intent but the Knowledge view's own. */
export type InvestigationInboundMessage = Exclude<InboundMessage, { type: KnowledgeOnlyIntent }>;

export type KnowledgeInboundMessage = Extract<InboundMessage, {
  type: "ready" | "refresh" | "library.search" | "pastCases.get" | "knowledge.showing" | "inspector.open" | "investigation.open" | "copy";
}>;

/** The case document (adr/0030): one system-of-record case, read-only. */
export type CaseRenderMessage =
  | { type: "case"; case: CaseRow }
  | { type: "error"; message: string }
  // A start is in flight: the button waits for the outcome.
  | { type: "starting" };

export type CaseInboundMessage =
  | { type: "ready" }
  | { type: "refresh" }
  // Root a new investigation on the case.
  | { type: "start" }
  // Open the investigation already rooted on it.
  | { type: "open"; id: string; title?: string };

/** One candidate document of the Import SOPs panel (design/06 §2.4), as posted. */
/** What one import did, as the server says it, plus the panel's own "failed". An
 * `unchanged` re-import brought what the library already holds: nothing was written. */
export type ImportOutcome = "created" | "revised" | "unchanged" | "failed";

export interface ImportRow {
  /** The document's URI, the row's identity. */
  id: string;
  name: string;
  dir: string;
  /** Added through the file dialog and read from disk; otherwise an open editor's live buffer. */
  fromDisk: boolean;
  dirty: boolean;
  checked: boolean;
  result?: { outcome: ImportOutcome; title: string; message?: string };
}

export type ImportRenderMessage =
  | { type: "state"; rows: ImportRow[]; busy: boolean; done?: { ok: number; failed: number } };

export type ImportInboundMessage =
  | { type: "ready" }
  // Rescan the open editors.
  | { type: "refresh" }
  | { type: "toggle"; id: string; checked: boolean }
  // The native file dialog, host-side.
  | { type: "addFiles" }
  | { type: "import" };
