// The Knowledge document (09 §9.1; impl §2.6): the query bar above the
// content, the left region listing the four sections with their counts (or
// the work queue in Work mode), one section in the main area, and the
// inspector dock hosting the Procedure and Similar-case renderers. It shows
// knowledge only: no investigation state renders here and nothing here acts
// on an investigation — an investigation appears only as a link the record
// carries.
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useApp } from "../../app";
import type { KnowledgeSection } from "../../../../src/protocol";
import { InspectorDock } from "../inspectors/Inspector";
import { LeftRegion } from "../LeftRegion";
import { PromptCard } from "../PromptCard";
import { Candidates } from "./Candidates";
import { Import } from "./Import";
import { PastCases } from "./PastCases";
import { Procedures } from "./Procedures";
import { QueryBar } from "./QueryBar";
import { AnswerView, SearchResults } from "./Results";

/** The document's view state (impl §3): section, the selected row, the
 * editor, the query — persisted under one key, never investigation state. */
export interface KnowledgeView {
  section: KnowledgeSection;
  setSection: (s: KnowledgeSection) => void;
  selected: string | null;
  /** Select a procedure row and ask the host for it whole. */
  select: (sopId: string | null) => void;
  editing: boolean;
  setEditing: (on: boolean) => void;
  mode: "search" | "ask";
  setMode: (m: "search" | "ask") => void;
  /** The results view replaces the section until the analyst goes back (09 §9.1). */
  showingResults: "search" | "ask" | null;
  setShowingResults: (m: "search" | "ask" | null) => void;
  lastQuery: string;
  setLastQuery: (q: string) => void;
}

interface Persisted {
  knowledge?: { section?: KnowledgeSection; selected?: string | null; mode?: "search" | "ask" };
}

const Ctx = createContext<KnowledgeView | null>(null);

export function useKnowledgeView(): KnowledgeView {
  const v = useContext(Ctx);
  if (!v) throw new Error("useKnowledgeView outside <KnowledgeApp>");
  return v;
}

export const SECTION_TITLES: Record<KnowledgeSection, string> = { procedures: "Procedures", cases: "Past cases", import: "Import", candidates: "Candidates" };

export function KnowledgeApp() {
  const { state, transport, send, prompt } = useApp();
  const saved = useMemo(() => transport.getState<Persisted>()?.knowledge || {}, [transport]);
  const [section, setSectionState] = useState<KnowledgeSection>(saved.section || "procedures");
  const [selected, setSelected] = useState<string | null>(saved.selected || null);
  const [editing, setEditing] = useState(false);
  const [mode, setModeState] = useState<"search" | "ask">(saved.mode || "search");
  const [showingResults, setShowingResults] = useState<"search" | "ask" | null>(null);
  const [lastQuery, setLastQuery] = useState("");

  const persist = useCallback((patch: Persisted["knowledge"]) => {
    const prev = transport.getState<Persisted>() || {};
    transport.setState({ ...prev, knowledge: { ...(prev.knowledge || {}), ...patch } });
  }, [transport]);
  const setSection = useCallback((s: KnowledgeSection) => {
    setSectionState(s);
    setShowingResults(null);
    setEditing(false);
    persist({ section: s });
  }, [persist]);
  const select = useCallback((sopId: string | null) => {
    setSelected(sopId);
    setEditing(false);
    persist({ selected: sopId });
    if (sopId) send({ type: "sop.get", sopId });
  }, [persist, send]);
  const setMode = useCallback((m: "search" | "ask") => { setModeState(m); persist({ mode: m }); }, [persist]);

  useEffect(() => { document.body.classList.toggle("loading", state.loading); }, [state.loading]);

  // A reveal from the host (the import command, a link with an id) lands on
  // the section and the object.
  useEffect(() => {
    const r = state.kreveal;
    if (!r) return;
    setSection(r.section);
    if (r.section === "procedures" && r.id) select(r.id);
  }, [state.kreveal, setSection, select]);

  // The selected procedure is re-read on load so an edit elsewhere shows.
  useEffect(() => {
    if (state.kdoc && selected && !state.kdoc.detail) send({ type: "sop.get", sopId: selected });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.kdoc !== null]);

  const view = useMemo<KnowledgeView>(() => ({
    section, setSection, selected, select, editing, setEditing, mode, setMode, showingResults, setShowingResults, lastQuery, setLastQuery,
  }), [section, setSection, selected, select, editing, mode, setMode, showingResults, lastQuery]);

  const k = state.kdoc;
  let main: React.ReactNode;
  if (showingResults === "ask") main = <AnswerView />;
  else if (showingResults === "search") main = <SearchResults />;
  else if (section === "procedures") main = <Procedures />;
  else if (section === "cases") main = <PastCases />;
  else if (section === "import") main = <Import />;
  else main = <Candidates />;

  return (
    <Ctx.Provider value={view}>
      <div id="document" className="knowledge">
        <header id="kheader">
          <div className="titlerow">
            <span className="titlename"><h1 id="title">Knowledge</h1></span>
            <span className="imeta">{k ? `${k.counts.procedures} procedure${k.counts.procedures === 1 ? "" : "s"}` : "loading…"} · the record is the knowledge service; nothing here touches an investigation</span>
            <span className="headcontrols"><button title="Reload from the backend" onClick={() => send({ type: "refresh" })}>Refresh</button></span>
          </div>
          <QueryBar />
        </header>
        <div id="regions">
          <LeftRegion knowledge={{ section, setSection }} />
          <div id="lensWrap">
            <main id="kmain" className="lensscroll" aria-label={showingResults ? "Results" : SECTION_TITLES[section]}>
              {state.kerror ? <div className="banner error" role="alert">{state.kerror}</div> : null}
              {main}
            </main>
            <InspectorDock />
          </div>
        </div>
      </div>
      {prompt ? <PromptCard key={prompt.title + (prompt.value || "")} spec={prompt} /> : null}
    </Ctx.Provider>
  );
}

/** The results header's way back (09 §9.1). */
export function BackToSection() {
  const view = useKnowledgeView();
  return <button className="linklike" onClick={() => view.setShowingResults(null)}>‹ back to {SECTION_TITLES[view.section]}</button>;
}
