// The app context: the reducer state, the transport, and the in-document
// dialogs every component may open (the prompt card, the pin dialog, the
// enablement form, the entity popover). Dialogs are view state; the host is
// told only when the analyst confirms.
import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, useState } from "react";
import type { InboundMessage, InspectorTarget, Lens } from "../../src/protocol";
import { defaultLens } from "./derive/lens";
import { targetKey } from "./derive/inspect";
import { initialState, reducer, type Action, type State } from "./store";
import type { WebviewTransport } from "./transport";
import type { PromptSpec } from "./components/PromptCard";

/** One open inspector pane: its target plus the back stack it was reached through (08 §8.6). */
export interface InspectorPane {
  stack: InspectorTarget[];
}

export interface InspectorState {
  /** The active pane; Esc closes it, opening a citation replaces its target and pushes the previous one onto its stack. */
  active: InspectorPane | null;
  /** A pinned pane stays while another opens beside it for comparison (at most two). */
  pinned: InspectorPane | null;
}

export interface AppContextValue {
  state: State;
  dispatch: (a: Action) => void;
  send: (m: InboundMessage) => void;
  transport: WebviewTransport;
  /** Resolve a ref's label through the host once; the store caches hits. */
  resolve: (ref: string) => void;
  prompt: PromptSpec | null;
  openPrompt: (spec: PromptSpec) => void;
  closePrompt: () => void;
  enableAdapter: string | null;
  openEnableForm: (adapter: string) => void;
  closeEnableForm: () => void;
  verdictOpen: boolean;
  setVerdictOpen: (on: boolean) => void;
  inspector: InspectorState;
  /** Open a citation in the active pane (pushing the previous target onto its back stack). */
  openInspector: (target: InspectorTarget) => void;
  inspectorBack: () => void;
  closeInspector: (which: "active" | "pinned") => void;
  pinInspector: () => void;
  /** Stage text in the composer — the pivot pattern: staged, never fired. */
  stage: (text: string) => void;
  staged: { text: string; nonce: number } | null;
  /** A turn is in flight (the composer and the ⚡ buttons are disabled). */
  busy: boolean;
  setBusy: (on: boolean) => void;
  /** The active lens: the analyst's choice for this investigation, else the stage's default (08 §8.4). */
  lens: Lens;
  setLens: (l: Lens) => void;
  companionMode: "conversation" | "thread";
  setCompanionMode: (m: "conversation" | "thread") => void;
  /** The companion column's width in px; undefined keeps the layout's default. */
  companionWidth: number | undefined;
  /** Resize the companion; `remember` keeps the width across reloads (view state). */
  setCompanionWidth: (w: number | undefined, remember: boolean) => void;
  /** Go to an object: switch lens if needed and scroll to it; a ref opens the inspector too. */
  goTo: (target: { kind: "lens"; lens: Lens; anchor?: string } | { kind: "ref"; ref: string }) => void;
}

interface PersistedView {
  /** The companion column's width in px, as the analyst dragged it. */
  companionW?: number;
  lens?: Record<string, Lens>;
  companion?: "conversation" | "thread";
}

const Ctx = createContext<AppContextValue | null>(null);

export function useApp(): AppContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp outside <AppProvider>");
  return v;
}

export function AppProvider({ transport, children }: { transport: WebviewTransport; children: React.ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const [prompt, setPrompt] = useState<PromptSpec | null>(null);
  const [enableAdapter, setEnableAdapter] = useState<string | null>(null);
  const [verdictOpen, setVerdictOpen] = useState(false);
  const [inspector, setInspector] = useState<InspectorState>({ active: null, pinned: null });
  const [staged, setStaged] = useState<{ text: string; nonce: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const requested = useRef(new Set<string>());
  const [chosenLens, setChosenLens] = useState<Lens | null>(null);
  const [companionMode, setCompanionModeState] = useState<"conversation" | "thread">(() => transport.getState<PersistedView>()?.companion || "conversation");
  const investigationId = state.snap?.investigation.id;
  // The analyst's last lens choice per investigation (view state) wins; else the stage's default.
  useEffect(() => {
    if (!investigationId) return;
    const saved = transport.getState<PersistedView>()?.lens?.[investigationId];
    setChosenLens(saved || null);
  }, [investigationId, transport]);
  const lens: Lens = chosenLens || (state.snap ? defaultLens(state.snap.investigation.state, !!state.snap.investigation.verdict) : "reasoning");
  const setLens = useCallback((l: Lens) => {
    setChosenLens(l);
    if (investigationId) {
      const prev = transport.getState<PersistedView>() || {};
      transport.setState({ ...prev, lens: { ...(prev.lens || {}), [investigationId]: l } });
    }
    transport.send({ type: "view.state", lens: l });
  }, [investigationId, transport]);
  const setCompanionMode = useCallback((m: "conversation" | "thread") => {
    setCompanionModeState(m);
    transport.setState({ ...(transport.getState<PersistedView>() || {}), companion: m });
    transport.send({ type: "view.state", companion: m });
  }, [transport]);
  const [companionWidth, setCompanionWidthState] = useState<number | undefined>(() => transport.getState<PersistedView>()?.companionW);
  const setCompanionWidth = useCallback((w: number | undefined, remember: boolean) => {
    setCompanionWidthState(w);
    if (remember) transport.setState({ ...(transport.getState<PersistedView>() || {}), companionW: w });
  }, [transport]);

  useEffect(() => {
    const off = transport.onMessage((msg) => {
      dispatch({ type: "msg", msg });
      // The composer re-enables when a turn ends; the lifecycle may keep it
      // parked (closed records), which the Composer decides from the state.
      if (msg.type === "turn.end") setBusy(false);
      if (msg.type === "turn.start" || msg.type === "aside.start") setBusy(true);
    });
    transport.send({ type: "ready" });
    return off;
  }, [transport]);

  const send = useCallback((m: InboundMessage) => transport.send(m), [transport]);
  const openInspector = useCallback((target: InspectorTarget) => {
    setInspector((s) => {
      const key = targetKey(target);
      const cur = s.active?.stack[s.active.stack.length - 1];
      if (cur && targetKey(cur) === key) return s;
      // Re-opening the pinned object just focuses it: no duplicate pane.
      const pinnedTop = s.pinned?.stack[s.pinned.stack.length - 1];
      if (pinnedTop && targetKey(pinnedTop) === key) return s;
      const stack = s.active ? [...s.active.stack.filter((x) => targetKey(x) !== key), target] : [target];
      return { ...s, active: { stack } };
    });
    // Every pane asks the host once per target; the store caches the answer,
    // and the host's own caches make a repeat ask cheap.
    transport.send({ type: "inspector.open", target });
  }, [transport]);
  const inspectorBack = useCallback(() => setInspector((s) => {
    if (!s.active || s.active.stack.length < 2) return s;
    return { ...s, active: { stack: s.active.stack.slice(0, -1) } };
  }), []);
  const closeInspector = useCallback((which: "active" | "pinned") => setInspector((s) => ({ ...s, [which]: null })), []);
  const pinInspector = useCallback(() => setInspector((s) => s.active ? { active: null, pinned: s.active } : s), []);
  const goTo = useCallback((target: { kind: "lens"; lens: Lens; anchor?: string } | { kind: "ref"; ref: string }) => {
    if (target.kind === "ref") {
      openInspector({ kind: "ref", ref: target.ref });
      const el = document.getElementById("obj-" + target.ref);
      if (el) el.scrollIntoView({ block: "center" });
      return;
    }
    setLens(target.lens);
    const anchor = target.anchor;
    requestAnimationFrame(() => {
      const el = anchor ? document.getElementById("obj-" + anchor) : null;
      if (el) el.scrollIntoView({ block: "start" });
    });
  }, [openInspector, setLens]);
  const resolve = useCallback((ref: string) => {
    if (requested.current.has(ref)) return;
    requested.current.add(ref);
    transport.send({ type: "evidence.resolve", ref });
  }, [transport]);

  const value = useMemo<AppContextValue>(() => ({
    state, dispatch, send, transport, resolve,
    prompt, openPrompt: setPrompt, closePrompt: () => setPrompt(null),
    enableAdapter, openEnableForm: setEnableAdapter, closeEnableForm: () => setEnableAdapter(null),
    verdictOpen, setVerdictOpen,
    inspector, openInspector, inspectorBack, closeInspector, pinInspector,
    stage: (text) => setStaged((s) => ({ text, nonce: (s?.nonce ?? 0) + 1 })), staged,
    busy, setBusy,
    lens, setLens, companionMode, setCompanionMode, companionWidth, setCompanionWidth, goTo,
  }), [state, send, transport, resolve, prompt, enableAdapter, verdictOpen, inspector, openInspector, inspectorBack, closeInspector, pinInspector, staged, busy, lens, setLens, companionMode, setCompanionMode, companionWidth, setCompanionWidth, goTo]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
