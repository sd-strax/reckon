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
  ArtifactLocation,
  ActionRow,
  Appearance,
  Capability,
  CaseRow,
  CaseSummaryView,
  CommsThread,
  Enablement,
  EvidenceDoc,
  Hypothesis,
  InvestigationDetail,
  InvestigationSummary,
  PinRow,
  SOPDoc,
  ThreadEntry,
} from "./viewTypes";

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
  | { kind: "person"; principalId: string };

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
  /** The committed reasoning thread; null when its fetch failed. */
  thread: ThreadEntry[] | null;
  /** Transcript bodies for the most recent transcript-bearing entries. */
  turns: ChronicleTurn[];
  capabilities: Capability[] | null;
  enablement: Enablement | null;
  derived: DerivedViews | null;
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

/** What the extension host posts to the webview to render. */
export type RenderMessage =
  | { type: "loading" }
  // The unseeded state (design/ui/02 §2.7): render the "root this investigation"
  // surface instead of a document. "seeded" flips back once the seed persists;
  // "draft.error" reports a failed create without leaving the draft.
  | { type: "draft"; mode?: "case"; caseQueue?: string }
  | { type: "seeded" }
  | { type: "draft.error"; message: string; unknownEntity?: boolean }
  // The "from a case" seed surface (13 §3): the SoR search results, and a
  // search failure rendered inline in the case sub-form.
  | { type: "draft.cases"; cases: { number: string; title: string; status: string }[] }
  | { type: "draft.caseError"; message: string }
  // The whole cache, and whole re-posts of the slices an act touched (§3.1).
  | { type: "state.snapshot"; seq: number; snapshot: InvestigationSnapshot }
  | { type: "state.patch"; seq: number; patch: Partial<InvestigationSnapshot> }
  // The work queue (08 §8.3): the analyst's investigations and the case
  // system's queue, posted on load and on refresh; `queue.get` re-asks.
  | { type: "queue"; investigations: InvestigationSummary[]; cases: CaseRow[]; caseSource?: string }
  // Who the signed-in principal is, for "edited by you" and attestations.
  | { type: "me"; name: string; principal: string }
  | { type: "error"; message: string }
  // Open the in-document rename card (never the top-of-window quick input).
  | { type: "rename.begin"; title: string }
  // Applied rename: update the title in place without a full reload.
  | { type: "renamed"; title: string }
  // A resolved human label for an evidence ref (e.g. "host · WIN-FILE01").
  | { type: "evidence.label"; ref: string; label: string }
  // The inspector's one-object answers (impl §4.1): the webview opened a pane
  // from the snapshot and asked for what the record holds beyond it.
  | { type: "inspector.evidence"; ref: string; doc: EvidenceDoc; appearances: Appearance[] }
  | { type: "inspector.procedure"; sopId: string; sop: SOPDoc }
  | { type: "inspector.turn"; interpretationId: string; body: string }
  | { type: "inspector.action"; actionId: string; record: ActionRecord }
  // summary is null when the corpus holds none for the ref (an honest gap).
  | { type: "inspector.case"; investigationRef: string; summary: CaseSummaryView | null }
  | { type: "inspector.error"; target: string; message: string }
  // The action types the analyst can request directly (#146): available ones only.
  | { type: "actionTypes"; types: ActionTypeView[] }
  // Drafting a procedure from a concluded case (adr/0156): where the draft is,
  // and whether the case's knowledge summary was written.
  | { type: "sop.status"; state: SopDraftState; title?: string; message?: string }
  | { type: "summary.status"; state: "written" | "baseline" | "failed"; detail?: string }
  // The knowledge library (adr/0129): the browse lists, and one search's hits.
  | { type: "library"; sops: LibraryItem[]; cases: LibraryItem[] }
  | { type: "library.results"; query: string; sops: LibraryItem[]; cases: LibraryItem[] }
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
  | { type: "turn.toolResult"; name: string; isError: boolean; coverage?: string; events?: number; refs?: string[] }
  | { type: "turn.committed"; interpretationId: string }
  | { type: "turn.error"; message: string }
  | { type: "turn.end"; usage?: { input: number; output: number; cacheRead: number }; rounds?: number };

/** What the webview posts back to the extension host. */
export type InboundMessage =
  | { type: "refresh" | "ready" }
  | { type: "queue.get" }
  // Start an investigation from a case row in the work queue (08 §8.3).
  | { type: "queue.start"; caseNumber: string }
  // Draft a procedure from this concluded case, then add or discard it (adr/0156).
  | { type: "sop.draft"; force?: boolean }
  | { type: "sop.accept" }
  | { type: "sop.discard" }
  // The knowledge library (adr/0129): load it, search it, import into it.
  | { type: "library.get" }
  | { type: "library.search"; text: string }
  | { type: "library.import" }
  // The lens and companion mode the analyst chose, mirrored to globalState so
  // the tree can deep-link (view state, never investigation state).
  | { type: "view.state"; lens?: Lens; companion?: "conversation" | "thread" }
  // The seed surface (design/ui/02 §2.7): the analyst's confirmed root.
  | { type: "seed.submit"; value: string; kind: string; allowUnknown?: boolean }
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
  | { type: "pred.new"; hypothesisRef: string; statement: string; discriminates: string[]; testKind?: string }
  | { type: "pred.withdraw"; predictionRef: string; reason: string }
  // The analyst records a prediction's result with an attestation (adr/0054).
  | { type: "pred.result"; predictionRef: string; status: string; content: string; source?: string; channel?: string; reference?: string; hashReference?: boolean; artifact?: ArtifactLocation }
  | { type: "hyp.proposeTests"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  | { type: "hyp.runTests"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  // One prediction's test, run as a scoped turn from its row (the ⚡ family).
  | { type: "pred.test"; predictionRef: string; predictionName: string; statement: string; queryText?: string; tool?: string; hypothesisRef: string; hypStatement: string; handle?: string; rivals?: RivalBrief[] }
  | { type: "hyp.decide"; hypothesisRef: string; statement: string; handle?: string; rivals?: RivalBrief[] }
  | { type: "enablement.apply"; adapter: string; enabled: boolean; config: Record<string, string>; secretFields: string[] }
  | { type: "comms.act"; threadId: string; verb: "ack" | "done" }
  | { type: "comms.snooze"; threadId: string; hours: number }
  | { type: "comms.followup"; threadId: string; actionType: string; target: string; subject: string; message: string; followUpHours: number }
  | { type: "lifecycle"; transition: string; reason?: string; summary?: string }
  | { type: "evidence.resolve"; ref: string }
  // The webview opened an inspector pane (view state) and needs the record
  // behind it; the host answers with the matching inspector.* message.
  | { type: "inspector.open"; target: InspectorTarget }
  | { type: "export.open" }
  | { type: "copy"; text: string }
  | { type: "investigation.open"; id: string; title?: string }
  // Start a new investigation: the seed surface, as the command opens it (#162).
  | { type: "investigation.new" }
  | { type: "rename"; title: string };
