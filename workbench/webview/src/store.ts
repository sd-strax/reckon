// The webview's state: a reducer over the host's RenderMessages plus the few
// local acts (knowledge toggles, draft submission guards). Projection slices
// arrive whole with a sequence (impl §3.1/§3.2); a patch older than what the
// webview holds is dropped. Everything here is view state or a mirror of the
// host's cache — never a second source of truth.
import type { ActionRecord, Appearance, BaselineView, CallDetail, CaseRow, CaseSummary, CaseSummaryView, EvidenceDoc, InvestigationSummary, RetrievalRow, SOPDoc, SOPRevision } from "../../src/backend";
import type { InvestigationSnapshot, KnowledgeItem, KnowledgeSection, KnowledgeState, RenderMessage } from "../../src/protocol";

export interface LiveToolResult {
  isError: boolean;
  /** "COMPLETE · 3 event(s)", "error", "done". */
  detail: string;
  coverage?: string;
  refs: string[];
  /** The capability call the result came from (adr/0092). */
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
  | { kind: "usage"; text: string };

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
  /** What this investigation retrieved of a procedure and its lineage (adr/0097); undefined = not answered. */
  sopExtras: Record<string, { retrievals?: RetrievalRow[]; revisions?: SOPRevision[] }>;
  transcripts: Record<string, string>;
  actionRecords: Record<string, ActionRecord>;
  caseSummaries: Record<string, CaseSummaryView | null>;
  calls: Record<string, CallDetail>;
  /** Entity baselines (adr/0095): null = the record holds none; absent = not asked yet. */
  baselines: Record<string, BaselineView | null>;
  inspectorErrors: Record<string, string>;
  /** The analyst's knowledge curation: the session's items with local toggles. */
  knowledge: KnowledgeItem[];
  live: LiveItem[];
  streaming: boolean;
  /** A failed turn's live copy is the only record of it: keep it until a new turn. */
  preserveLive: boolean;
  /** The work queue (08 §8.3); null until the host posts it. */
  queue: { investigations: InvestigationSummary[]; cases: CaseRow[]; caseSource?: string } | null;
  /** The signed-in principal, for "edited by you" and attestations. */
  /** The signed-in principal and the issuer that authenticated them (the Person inspector's issuer line, adr/0105). */
  me: { name: string; principal: string; issuer?: string } | null;
  /** The Knowledge document's cache (impl §2.6, §3.1): null on the investigation surface, whose host never posts it. */
  kdoc: KnowledgeState | null;
  /** The Knowledge host's last error, shown as a banner until the next successful slice. */
  kerror: string | null;
  /** A reveal request (the import command, a link): the section and the object to land on. */
  kreveal: { section: KnowledgeSection; id?: string; nonce: number } | null;
  /** The narrative draft in flight (adr/0103), and its last failure. */
  narrativeDrafting: boolean;
  narrativeError: string | null;
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
  sopExtras: {},
  transcripts: {},
  actionRecords: {},
  caseSummaries: {},
  calls: {},
  baselines: {},
  inspectorErrors: {},
  knowledge: [],
  live: [],
  streaming: false,
  preserveLive: false,
  queue: null,
  me: null,
  kdoc: null,
  kerror: null,
  kreveal: null,
  narrativeDrafting: false,
  narrativeError: null,
};

export type Action =
  | { type: "msg"; msg: RenderMessage }
  | { type: "knowledge.toggle"; ref: string; included: boolean }
  | { type: "draft.submitting" }
  | { type: "draft.searching"; status: string };

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
    case "knowledge.toggle":
      return { ...state, knowledge: state.knowledge.map((k) => k.ref === action.ref ? { ...k, included: action.included } : k) };
    case "draft.submitting":
      return { ...state, draft: { ...state.draft, submitting: true, error: "" } };
    case "draft.searching":
      return { ...state, draft: { ...state.draft, status: action.status, cases: [] } };
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
    case "draft.error":
      return { ...state, draft: { ...state.draft, submitting: false, error: msg.message } };
    case "state.snapshot": {
      const snap = msg.snapshot;
      return {
        ...state,
        loading: false,
        error: null,
        seq: msg.seq,
        snap,
        title: snap.investigation.title,
        knowledge: mergeKnowledge(state.knowledge, snap.knowledge),
        // The committed thread now holds everything the live conversation
        // showed — unless a turn is streaming or a failed turn is preserved.
        live: state.streaming || state.preserveLive ? state.live : [],
      };
    }
    case "state.patch": {
      if (!state.snap || msg.seq < state.seq) return state; // stale
      const snap = { ...state.snap, ...msg.patch };
      const next: State = { ...state, seq: msg.seq, snap, title: msg.patch.investigation ? msg.patch.investigation.title : state.title };
      if (msg.patch.knowledge) next.knowledge = mergeKnowledge(state.knowledge, msg.patch.knowledge);
      if (msg.patch.summary !== undefined) { next.narrativeDrafting = false; next.narrativeError = null; }
      if (msg.patch.thread !== undefined && !state.streaming && !state.preserveLive) next.live = [];
      return next;
    }
    case "queue":
      return { ...state, queue: { investigations: msg.investigations, cases: msg.cases, caseSource: msg.caseSource } };
    case "knowledge.state": {
      // Slices arrive whole; the first post carries every slice (the host's
      // load), later ones only what a read or a write touched.
      const base: KnowledgeState = state.kdoc ?? { counts: { procedures: 0, cases: null, candidates: null }, sops: [], cases: null, candidates: null, detail: null, detailRevisions: null, revisionBodies: {}, results: null, answer: null, importRows: [], importing: false };
      return { ...state, loading: false, kerror: null, kdoc: { ...base, ...msg.state } };
    }
    case "knowledge.error":
      return { ...state, loading: false, kerror: msg.message };
    case "narrative.drafting":
      return { ...state, narrativeDrafting: true, narrativeError: null };
    case "narrative.failed":
      return { ...state, narrativeDrafting: false, narrativeError: msg.message };
    case "knowledge.reveal":
      return { ...state, kreveal: { section: msg.section, id: msg.id, nonce: (state.kreveal?.nonce ?? 0) + 1 } };
    case "knowledge.answer.start":
      return state.kdoc ? { ...state, kdoc: { ...state.kdoc, answer: { requestId: msg.requestId, question: state.kdoc.answer?.question || "", text: "", citations: [], model: "", toolsUsed: [] } } } : state;
    case "knowledge.answer.delta":
      return state.kdoc?.answer && state.kdoc.answer.requestId === msg.requestId ? { ...state, kdoc: { ...state.kdoc, answer: { ...state.kdoc.answer, text: state.kdoc.answer.text + msg.text } } } : state;
    case "knowledge.answer.done":
      return state.kdoc ? { ...state, kdoc: { ...state.kdoc, answer: msg.answer } } : state;
    case "me":
      return { ...state, me: { name: msg.name, principal: msg.principal, issuer: msg.issuer } };
    case "rename.begin":
      return { ...state, renameRequest: { title: msg.title, nonce: (state.renameRequest?.nonce ?? 0) + 1 } };
    case "renamed":
      return { ...state, title: msg.title, snap: state.snap ? { ...state.snap, investigation: { ...state.snap.investigation, title: msg.title } } : null };
    case "evidence.label":
      return { ...state, labels: { ...state.labels, [msg.ref]: msg.label } };
    case "inspector.evidence": {
      return {
        ...state,
        docs: { ...state.docs, [msg.ref]: msg.doc },
        appearances: { ...state.appearances, [msg.ref]: msg.appearances },
        inspectorErrors: dropKey(state.inspectorErrors, msg.ref),
      };
    }
    case "inspector.procedure":
      return { ...state, sops: { ...state.sops, [msg.sopId]: msg.sop }, sopExtras: { ...state.sopExtras, [msg.sopId]: { retrievals: msg.retrievals, revisions: msg.revisions } }, inspectorErrors: dropKey(state.inspectorErrors, msg.sopId) };
    case "inspector.turn":
      return { ...state, transcripts: { ...state.transcripts, [msg.interpretationId]: msg.body }, inspectorErrors: dropKey(state.inspectorErrors, msg.interpretationId) };
    case "inspector.action":
      return { ...state, actionRecords: { ...state.actionRecords, [msg.actionId]: msg.record }, inspectorErrors: dropKey(state.inspectorErrors, msg.actionId) };
    case "inspector.case":
      return { ...state, caseSummaries: { ...state.caseSummaries, [msg.investigationRef]: msg.summary }, inspectorErrors: dropKey(state.inspectorErrors, msg.investigationRef) };
    case "inspector.baseline":
      return { ...state, baselines: { ...state.baselines, [msg.ref]: msg.baseline } };
    case "inspector.call":
      return { ...state, calls: { ...state.calls, [msg.callId]: msg.call }, inspectorErrors: dropKey(state.inspectorErrors, msg.callId) };
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

/** New knowledge items take the posture's initial inclusion; items the analyst
 * already toggled keep their toggle across re-posts of the slice. */
function mergeKnowledge(prev: KnowledgeItem[], next: KnowledgeItem[]): KnowledgeItem[] {
  const seen = new Map(prev.map((k) => [k.ref, k.included] as const));
  return next.map((k) => seen.has(k.ref) ? { ...k, included: seen.get(k.ref)! } : k);
}

function dropKey<T>(m: Record<string, T>, k: string): Record<string, T> {
  if (!(k in m)) return m;
  const { [k]: _dropped, ...rest } = m;
  return rest;
}

