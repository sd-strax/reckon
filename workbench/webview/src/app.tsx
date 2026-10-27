// The app context: the reducer state, the transport, and the in-document
// dialogs every component may open (the prompt card, the pin dialog, the
// enablement form, the entity popover). Dialogs are view state; the host is
// told only when the analyst confirms.
import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, useState } from "react";
import type { InboundMessage } from "../../src/protocol";
import { initialState, reducer, type Action, type State } from "./store";
import type { WebviewTransport } from "./transport";
import type { PromptSpec } from "./components/PromptCard";

export interface PopoverSpec {
  ref: string;
  anchor: DOMRect;
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
  popover: PopoverSpec | null;
  openPopover: (ref: string, anchor: DOMRect) => void;
  closePopover: () => void;
  /** Stage text in the composer — the pivot pattern: staged, never fired. */
  stage: (text: string) => void;
  staged: { text: string; nonce: number } | null;
  /** A turn is in flight (the composer and the ⚡ buttons are disabled). */
  busy: boolean;
  setBusy: (on: boolean) => void;
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
  const [popover, setPopover] = useState<PopoverSpec | null>(null);
  const [staged, setStaged] = useState<{ text: string; nonce: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const requested = useRef(new Set<string>());

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
    popover, openPopover: (ref, anchor) => setPopover({ ref, anchor }), closePopover: () => setPopover(null),
    stage: (text) => setStaged((s) => ({ text, nonce: (s?.nonce ?? 0) + 1 })), staged,
    busy, setBusy,
  }), [state, send, transport, resolve, prompt, enableAdapter, verdictOpen, popover, staged, busy]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
