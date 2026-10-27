// The investigation document: two regions (conversation + the state rail),
// the resizable grip between them, the inspector dock overlaying the
// conversation side, and the overlays (dialogs, the draft surface). Region layout is the current one; ui/08's lenses land in
// the layout PR on top of these same components.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { PendingSection } from "./Approvals";
import { Chronicle } from "./Chronicle";
import { Composer } from "./Composer";
import { EventStrip, EvidenceGraph } from "./DerivedViews";
import { EnableForm, VerdictDialog } from "./Dialogs";
import { DraftView } from "./DraftView";
import { Header } from "./Header";
import { Hypotheses } from "./Hypotheses";
import { InspectorDock } from "./inspectors/Inspector";
import { LiveConversation } from "./LiveConversation";
import { PromptCard } from "./PromptCard";
import { ActionsLedger, CapabilitiesFold, Comms, KnowledgeRail, Pins } from "./Rail";
import { SeedChip } from "./SeedChip";

interface PersistedView { railW?: number }

function RailGrip() {
  const { transport } = useApp();
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    const saved = transport.getState<PersistedView>();
    if (saved && Number.isFinite(saved.railW)) {
      document.documentElement.style.setProperty("--rail-w", saved.railW + "px");
    }
  }, [transport]);
  useEffect(() => {
    document.body.classList.toggle("railDragging", dragging);
  }, [dragging]);
  return (
    <div
      id="railGrip"
      title="Drag to resize"
      onPointerDown={(e) => { setDragging(true); (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId); e.preventDefault(); }}
      onPointerMove={(e) => {
        if (!dragging) return;
        const w = Math.min(Math.max(window.innerWidth - e.clientX, 200), Math.floor(window.innerWidth * 0.6));
        document.documentElement.style.setProperty("--rail-w", w + "px");
      }}
      onPointerUp={(e) => {
        if (!dragging) return;
        setDragging(false);
        (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
        const w = parseInt(getComputedStyle(document.documentElement).getPropertyValue("--rail-w"), 10);
        if (Number.isFinite(w)) transport.setState({ ...(transport.getState<PersistedView>() || {}), railW: w });
      }}
    />
  );
}

export function App() {
  const { state, prompt, enableAdapter, verdictOpen } = useApp();
  const scroll = useRef<HTMLDivElement>(null);
  const wasAtBottom = useRef(true);
  // Stick to the bottom while the analyst is already there (streaming text,
  // new act lines); never yank the view when they scrolled up.
  useLayoutEffect(() => {
    const el = scroll.current;
    if (el && wasAtBottom.current) el.scrollTop = el.scrollHeight;
  });
  const onScroll = () => {
    const el = scroll.current;
    if (el) wasAtBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };
  useEffect(() => {
    document.body.classList.toggle("loading", state.loading);
  }, [state.loading]);
  const derived = state.snap?.derived;
  return (
    <>
      <div id="main">
        <Header />
        <div id="scroll" ref={scroll} onScroll={onScroll}>
          {state.error ? <div id="banner">Could not load: {state.error}</div> : null}
          <SeedChip />
          {derived ? <EventStrip items={derived.strip} /> : null}
          {derived ? <EvidenceGraph graph={derived.graph} /> : null}
          <Chronicle />
          <LiveConversation />
        </div>
        <Composer />
        <InspectorDock />
      </div>
      <RailGrip />
      <aside id="rail">
        {/* The rail mirrors the epistemic workflow: what we believe (hypotheses)
            → what grounds it (pinned evidence). Approvals are an INTERRUPT, not
            a section. Capability health is operator info — collapsed to a count. */}
        <PendingSection />
        <Hypotheses />
        <KnowledgeRail />
        <Pins />
        <ActionsLedger />
        <Comms />
        <CapabilitiesFold />
      </aside>
      {prompt ? <PromptCard key={prompt.title + (prompt.value || "")} spec={prompt} /> : null}
      {enableAdapter ? <EnableForm adapter={enableAdapter} /> : null}
      {verdictOpen ? <VerdictDialog /> : null}
      {state.draft.visible ? <DraftView key={state.draft.caseQueue + String(state.draft.mode)} /> : null}
    </>
  );
}
