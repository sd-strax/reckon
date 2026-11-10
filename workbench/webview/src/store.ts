// The webview's state: a reducer over the host's RenderMessages plus the few
// local acts (library search, draft submission guards). Projection slices
// arrive whole with a sequence (impl §3.1/§3.2); a snapshot or patch older than
// what the webview holds is dropped. Everything here is view state or a mirror of the
// host's cache — never a second source of truth.
import type { ActionRecord, Appearance, CapabilityCall, CaseRow, CaseSummary, CaseSummaryView, EventRepeats, EventRow, EvidenceDoc, InvestigationSummary, KnowledgeCitations, PastCaseFacets, PastCaseFilter, PastCaseRow, ProcedureReads, SOPDoc, SOPOutlineView } from "../../src/viewTypes";
import type { ActionTypeView, InvestigationSnapshot, LibraryItem, RenderMessage, SopDraftState } from "../../src/protocol";

export interface LiveToolResult {
  isError: boolean;
  /** "COMPLETE · 3 event(s)", "error", "done". */
  detail: string;
  coverage?: string;
  refs: string[];
  /** The capability call behind the result (adr/0092), when the envelope named one. */
  callId?: string;
}

export interface LiveToolRow {
  name: string;
  input: unknown;
  result?: LiveToolResult;
}

export type LiveSegment =
  | { kind: "text"; text: string }
  | { kind: "tool"; row: LiveToolRow }
  | { kind: "step"; round: number }
  | { kind: "usage"; text: string }
  | { kind: "notice"; text: string };

export interface LiveTurn {
  kind: "assistant";
  reasoning: string;
  segments: LiveSegment[];
  /** Set for a scoped ⚡ aside: rendered as a collapsed disclosure. */
  aside?: { label: string; done: boolean };
}

export type LiveItem =
  | { kind: "user"; text: string }
  | LiveTurn
  | { kind: "committed"; text: string }
  | { kind: "error"; message: string };

export interface DraftState {
  visible: boolean;
  mode?: "case";
  caseQueue: string;
  /** null = no search yet. */
  cases: CaseSummary[] | null;
  status: string;
  error: string;
  /** The error is "nothing knows this entity" (#159): the analyst may start anyway. */
  unknownEntity?: boolean;
  /** One submit per draft (both the Enter path and the buttons). */
  submitting: boolean;
}

export interface State {
  loading: boolean;
  error: string | null;
  seq: number;
  snap: InvestigationSnapshot | null;
  /** The title as last applied (rename echoes update it without a reload). */
  title: string;
  /** A tree-initiated rename request: the prefill, bumped so the same title re-triggers. */
  renameRequest: { title: string; nonce: number } | null;
  draft: DraftState;
  labels: Record<string, string>;
  appearances: Record<string, Appearance[]>;
  /** The inspector's fetched records, keyed by target key (derive/inspect.ts). null = the fetch failed; the error is in inspectorErrors. */
  docs: Record<string, EvidenceDoc | null>;
  sops: Record<string, SOPDoc>;
  transcripts: Record<string, string>;
  actionRecords: Record<string, ActionRecord>;
  caseSummaries: Record<string, CaseSummaryView | null>;
  /** Where a procedure (by sop id) or a past case (by ref) was consulted across investigations (adr/0109). */
  citations: Record<string, KnowledgeCitations>;
  /** What the agent read of a procedure here, as it was (adr/0097); absent when nothing was recorded or the read failed. */
  procedureReads: Record<string, ProcedureReads>;
  /** A procedure's current outline (adr/0303), by sop id; absent when that read failed. */
  outlines: Record<string, SOPOutlineView>;
  /** The Knowledge view's past cases under the last filter answered (adr/0107); null until the first answer. */
  pastCases: { filter: PastCaseFilter; rows: PastCaseRow[]; total: number; error?: string } | null;
  /** The techniques and dispositions present in the corpus, for the filters; null until read. */
  caseFacets: PastCaseFacets | null;
  /** Capability calls by id (adr/0092), and the retrieved events around an opened event. */
  calls: Record<string, CapabilityCall>;
  around: Record<string, EventRow[]>;
  /** An opened entity's retrieved events folded by the default key (adr/0095); absent when the read failed. */
  repeats: Record<string, EventRepeats>;
  inspectorErrors: Record<string, string>;
  /** The action types the analyst may request directly (#146). */
  actionTypes: ActionTypeView[];
  /** A procedure drafted from this concluded case (adr/0156), and the case summary's outcome. */
  sopDraft: { state: SopDraftState; title?: string; message?: string } | null;
  summaryStatus: { state: "written" | "baseline" | "failed"; detail?: string } | null;
  /** The narrative (adr/0103): a draft in flight or failed, or an edit saving or refused; cleared when the record carries it. */
  narrativeStatus: { state: "drafting" | "failed" | "saving" | "save-failed"; message?: string } | null;
  /** How many edits the record has taken in this session, so the editor closes once its own landed. */
  narrativeSaves: number;
  /** The knowledge library (adr/0129): what there is, and the last search's hits. */
  library: {
    sops: LibraryItem[]; cases: LibraryItem[]; casesTotal?: number;
    /** The last procedures read failed; the list is the one read before it. */
    sopsError?: string;
    loaded: boolean;
    results: { query: string; sops: LibraryItem[]; cases: LibraryItem[] } | null;
    /** The search asked last; an answer for any other, or after it was dismissed, is not shown. */
    pending?: string;
  };
  live: LiveItem[];
  streaming: boolean;
  /** A failed turn's live copy is the only record of it: keep it until a new turn. */
  preserveLive: boolean;
  /** The work queue (08 §8.3); null until the host posts it. */
  queue: { investigations: InvestigationSummary[]; cases: CaseRow[]; caseSource?: string } | null;
  /** The signed-in principal, for "edited by you" and attestations. */
  me: { name: string; principal: string } | null;
  /** Actions with a decision sent and not yet settled by the host: every card for one disables. */
  actionsInFlight: Record<string, true>;
  /**
   * The identity acts on a host (adr/0211), by its ref: one in flight, the
   * last refusal, and how many have landed, so an inline form closes once its
   * own act landed.
   */
  identityActs: Record<string, { pending: boolean; error?: string; landed: number }>;
}

export const initialState: State = {
  loading: true,
  error: null,
  seq: -1,
  snap: null,
  title: "",
  renameRequest: null,
  draft: { visible: false, caseQueue: "", cases: null, status: "", error: "", submitting: false },
  labels: {},
  appearances: {},
  docs: {},
  sops: {},
  transcripts: {},
  actionRecords: {},
  caseSummaries: {},
  citations: {},
  procedureReads: {},
  outlines: {},
  pastCases: null,
  caseFacets: null,
  calls: {},
  around: {},
  repeats: {},
  inspectorErrors: {},
  library: { sops: [], cases: [], loaded: false, results: null },
  actionTypes: [],
  sopDraft: null,
  summaryStatus: null,
  narrativeStatus: null,
  narrativeSaves: 0,
  live: [],
  streaming: false,
  preserveLive: false,
  queue: null,
  me: null,
  actionsInFlight: {},
  identityActs: {},
};

export type Action =
  | { type: "msg"; msg: RenderMessage }
  | { type: "library.clear" }
  /** A search was sent: only its answer is shown, and only until it is dismissed. */
  | { type: "library.searching"; query: string }
  /** The page asks again after a failed load (the Knowledge view's Retry). */
  | { type: "error.clear" }
  | { type: "draft.submitting" }
  | { type: "draft.searching"; status: string }
  /** A decision on this action was sent to the host. */
  | { type: "action.sent"; actionId: string }
  /** An identity act on this host was sent to the host (adr/0211). */
  | { type: "identity.sent"; ref: string };

function currentTurn(live: LiveItem[]): LiveTurn | null {
  const last = live[live.length - 1];
  return last && last.kind === "assistant" ? last : null;
}

/** Mutate-and-return on a shallow copy of the live list and its last turn. */
function withTurn(state: State, f: (t: LiveTurn) => void): State {
  const t = currentTurn(state.live);
  if (!t) return state;
  const copy: LiveTurn = { ...t, segments: t.segments.slice() };
  f(copy);
  return { ...state, live: [...state.live.slice(0, -1), copy] };
}

function appendText(t: LiveTurn, delta: string): void {
  const last = t.segments[t.segments.length - 1];
  if (last && last.kind === "text") {
    t.segments[t.segments.length - 1] = { kind: "text", text: last.text + delta };
  } else {
    t.segments.push({ kind: "text", text: delta });
  }
}

export function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "library.clear":
      return { ...state, library: { ...state.library, results: null, pending: undefined } };
    case "library.searching":
      return { ...state, library: { ...state.library, pending: action.query } };
    case "error.clear":
      return { ...state, error: null };
    case "draft.submitting":
      return { ...state, draft: { ...state.draft, submitting: true, error: "" } };
    case "draft.searching":
      return { ...state, draft: { ...state.draft, status: action.status, cases: [] } };
    case "action.sent":
      return { ...state, actionsInFlight: { ...state.actionsInFlight, [action.actionId]: true } };
    case "identity.sent":
      return { ...state, identityActs: { ...state.identityActs, [action.ref]: { pending: true, landed: state.identityActs[action.ref]?.landed ?? 0 } } };
    case "msg":
      return onMessage(state, action.msg);
  }
}

function onMessage(state: State, msg: RenderMessage): State {
  switch (msg.type) {
    case "loading":
      return { ...state, loading: true };
    case "error":
      return { ...state, loading: false, error: msg.message };
    case "draft":
      return {
        ...state,
        loading: false,
        draft: { visible: true, mode: msg.mode, caseQueue: msg.caseQueue || "", cases: null, status: "", error: "", submitting: false },
      };
    case "seeded":
      return { ...state, draft: { ...state.draft, visible: false } };
    case "draft.cases":
      return { ...state, draft: { ...state.draft, cases: msg.cases, status: "" } };
    case "draft.caseError":
      return { ...state, draft: { ...state.draft, submitting: false, status: msg.message, cases: [] } };
    case "actionTypes":
      return { ...state, actionTypes: msg.types };
    case "sop.status":
      return { ...state, sopDraft: { state: msg.state, title: msg.title, message: msg.message } };
    case "summary.status":
      return { ...state, summaryStatus: { state: msg.state, detail: msg.detail } };
    case "narrative.status":
      if (msg.state === "saved") return { ...state, narrativeStatus: null, narrativeSaves: state.narrativeSaves + 1 };
      if (msg.state === "written") return { ...state, narrativeStatus: null };
      return { ...state, narrativeStatus: { state: msg.state, message: msg.message } };
    case "library":
      return {
        ...state,
        library: msg.sopsError
          // A failed read keeps what was read before and says it failed.
          ? { ...state.library, cases: msg.cases, casesTotal: msg.casesTotal, sopsError: msg.sopsError, loaded: true }
          : { ...state.library, sops: msg.sops, cases: msg.cases, casesTotal: msg.casesTotal, sopsError: undefined, loaded: true },
      };
    case "library.results":
      if (state.library.pending !== msg.query) return state;
      return { ...state, library: { ...state.library, pending: undefined, results: { query: msg.query, sops: msg.sops, cases: msg.cases } } };
    case "pastCases":
      return { ...state, pastCases: { filter: msg.filter, rows: msg.rows, total: msg.total, error: msg.error } };
    case "pastCases.facets":
      return { ...state, caseFacets: msg.facets };
    case "draft.error":
      return { ...state, draft: { ...state.draft, submitting: false, error: msg.message, unknownEntity: !!msg.unknownEntity } };
    case "state.snapshot": {
      const snap = msg.snapshot;
      // A snapshot older than what the webview holds for the same
      // investigation lost a race with a newer read: keep the newer state.
      if (state.snap && state.snap.investigation.id === snap.investigation.id && msg.seq < state.seq) {
        return { ...state, loading: false };
      }
      return {
        ...state,
        loading: false,
        error: null,
        seq: msg.seq,
        snap,
        title: snap.investigation.title,
        // The committed thread now holds everything the live conversation
        // showed — unless a turn is streaming or a failed turn is preserved.
        live: state.streaming || state.preserveLive ? state.live : [],
      };
    }
    case "state.patch": {
      if (!state.snap || msg.seq < state.seq) return state; // stale
      const snap = { ...state.snap, ...msg.patch };
      const next: State = { ...state, seq: msg.seq, snap, title: msg.patch.investigation ? msg.patch.investigation.title : state.title };
      // A failed draft's note is about the narrative as it was; a re-read of
      // the investigation (an edit saved, a draft landed) replaces it. A draft
      // still in flight keeps its note.
      if (msg.patch.investigation && state.narrativeStatus?.state === "failed") next.narrativeStatus = null;
      if (msg.patch.thread !== undefined && !state.streaming && !state.preserveLive) next.live = [];
      return next;
    }
    case "queue":
      return { ...state, queue: { investigations: msg.investigations, cases: msg.cases, caseSource: msg.caseSource } };
    case "me":
      return { ...state, me: { name: msg.name, principal: msg.principal } };
    case "rename.begin":
      return { ...state, renameRequest: { title: msg.title, nonce: (state.renameRequest?.nonce ?? 0) + 1 } };
    case "renamed":
      return { ...state, title: msg.title, snap: state.snap ? { ...state.snap, investigation: { ...state.snap.investigation, title: msg.title } } : null };
    case "evidence.label":
      return { ...state, labels: { ...state.labels, [msg.ref]: msg.label } };
    case "evidence.unresolved":
      return state; // the app's own request bookkeeping; nothing renders it
    case "inspector.evidence": {
      return {
        ...state,
        docs: { ...state.docs, [msg.ref]: msg.doc },
        appearances: { ...state.appearances, [msg.ref]: msg.appearances },
        around: { ...state.around, [msg.ref]: msg.around ?? [] },
        repeats: msg.repeats ? { ...state.repeats, [msg.ref]: msg.repeats } : dropKey(state.repeats, msg.ref),
        inspectorErrors: dropKey(state.inspectorErrors, msg.ref),
      };
    }
    case "inspector.identity": {
      const doc = state.docs[msg.ref];
      return {
        ...state,
        docs: doc ? { ...state.docs, [msg.ref]: { ...doc, identity: msg.identity } } : state.docs,
        identityActs: { ...state.identityActs, [msg.ref]: { pending: false, landed: (state.identityActs[msg.ref]?.landed ?? 0) + 1 } },
      };
    }
    case "identity.error":
      return { ...state, identityActs: { ...state.identityActs, [msg.ref]: { pending: false, error: msg.message, landed: state.identityActs[msg.ref]?.landed ?? 0 } } };
    case "inspector.call":
      // The pane is named by the verb (derive/inspect.ts targetName).
      return { ...state, calls: { ...state.calls, [msg.callId]: msg.call }, labels: { ...state.labels, ["call:" + msg.callId]: msg.call.verb }, inspectorErrors: dropKey(state.inspectorErrors, msg.callId) };
    case "inspector.procedure":
      return {
        ...state,
        sops: { ...state.sops, [msg.sopId]: msg.sop },
        citations: withCitations(state.citations, msg.sopId, msg.citations),
        procedureReads: msg.reads ? { ...state.procedureReads, [msg.sopId]: msg.reads } : dropKey(state.procedureReads, msg.sopId),
        outlines: msg.outline ? { ...state.outlines, [msg.sopId]: msg.outline } : dropKey(state.outlines, msg.sopId),
        // The pane's name, where no investigation's thread names it (the Knowledge view).
        labels: { ...state.labels, ["sop:" + msg.sopId]: msg.sop.title },
        inspectorErrors: dropKey(state.inspectorErrors, msg.sopId),
      };
    case "inspector.turn":
      return { ...state, transcripts: { ...state.transcripts, [msg.interpretationId]: msg.body }, inspectorErrors: dropKey(state.inspectorErrors, msg.interpretationId) };
    case "inspector.action":
      return { ...state, actionRecords: { ...state.actionRecords, [msg.actionId]: msg.record }, inspectorErrors: dropKey(state.inspectorErrors, msg.actionId) };
    case "inspector.case":
      return {
        ...state,
        caseSummaries: { ...state.caseSummaries, [msg.investigationRef]: msg.summary },
        citations: withCitations(state.citations, msg.investigationRef, msg.citations),
        labels: msg.summary?.title ? { ...state.labels, ["case:" + msg.investigationRef]: msg.summary.title } : state.labels,
        inspectorErrors: dropKey(state.inspectorErrors, msg.investigationRef),
      };
    case "action.settled":
      return { ...state, actionsInFlight: dropKey(state.actionsInFlight, msg.actionId) };
    case "inspector.error":
      return { ...state, inspectorErrors: { ...state.inspectorErrors, [msg.target]: msg.message }, docs: msg.target in state.docs ? state.docs : { ...state.docs, [msg.target]: null } };
    case "turn.user":
      return { ...state, preserveLive: false, live: [...state.live, { kind: "user", text: msg.text }] };
    case "turn.start":
      return { ...state, streaming: true, preserveLive: false, live: [...state.live, { kind: "assistant", reasoning: "", segments: [] }] };
    case "aside.start":
      return {
        ...state, streaming: true, preserveLive: false,
        live: [...state.live, { kind: "assistant", reasoning: "", segments: [], aside: { label: msg.label, done: false } }],
      };
    case "turn.reasoning":
      return withTurn(state, (t) => { t.reasoning += msg.delta; });
    case "turn.text":
      return withTurn(state, (t) => appendText(t, msg.delta));
    case "turn.step":
      // A subtle step divider from round 2 on (round 1 is the turn start).
      return msg.round > 1 ? withTurn(state, (t) => { t.segments.push({ kind: "step", round: msg.round }); }) : state;
    case "turn.notice":
      return withTurn(state, (t) => { t.segments.push({ kind: "notice", text: msg.text }); });
    case "turn.tool":
      return withTurn(state, (t) => { t.segments.push({ kind: "tool", row: { name: msg.name, input: msg.input } }); });
    case "turn.toolResult": {
      const detail = msg.isError ? "error"
        : msg.coverage !== undefined ? msg.coverage + " · " + msg.events + " event(s)"
        : "done";
      const t = currentTurn(state.live);
      // The FIFO of tool rows awaiting their result: the first unsettled one.
      const idx = t ? t.segments.findIndex((s) => s.kind === "tool" && !s.row.result) : -1;
      if (!t || idx < 0) {
        return { ...state, live: [...state.live, { kind: "committed", text: (msg.isError ? "✗ " : "✓ ") + msg.name + " — " + detail }] };
      }
      return withTurn(state, (turn) => {
        const seg = turn.segments[idx] as { kind: "tool"; row: LiveToolRow };
        turn.segments[idx] = { kind: "tool", row: { ...seg.row, result: { isError: msg.isError, detail, coverage: msg.coverage, refs: msg.refs || [], callId: msg.callId } } };
      });
    }
    case "turn.committed":
      return { ...state, live: [...state.live, { kind: "committed", text: "saved to thread · " + String(msg.interpretationId).slice(0, 8) + "…" }] };
    case "turn.error":
      // The failed turn was never committed, so the next thread render won't
      // contain it — keep the live copy on screen instead of wiping it.
      return { ...state, streaming: false, preserveLive: true, live: [...state.live, { kind: "error", message: msg.message }] };
    case "turn.end": {
      let next = state;
      if (msg.usage) {
        const u = msg.usage;
        const parts: string[] = [];
        if (msg.rounds) parts.push(msg.rounds + " tool round" + (msg.rounds === 1 ? "" : "s"));
        parts.push(u.input.toLocaleString() + " in / " + u.output.toLocaleString() + " out");
        if (u.cacheRead) parts.push(u.cacheRead.toLocaleString() + " cached");
        next = withTurn(next, (t) => { t.segments.push({ kind: "usage", text: parts.join(" · ") }); });
      }
      next = withTurn(next, (t) => { if (t.aside) t.aside = { ...t.aside, done: true }; });
      return { ...next, streaming: false };
    }
  }
}

function dropKey<T>(m: Record<string, T>, k: string): Record<string, T> {
  if (!(k in m)) return m;
  const { [k]: _dropped, ...rest } = m;
  return rest;
}

/** The citations an inspector answer carried, kept under its key; an answer without them (the read failed) drops a stale entry. */
function withCitations(m: Record<string, KnowledgeCitations>, k: string, c: KnowledgeCitations | undefined): Record<string, KnowledgeCitations> {
  return c ? { ...m, [k]: c } : dropKey(m, k);
}

