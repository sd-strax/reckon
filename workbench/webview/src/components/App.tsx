// The investigation document (08 §8.1): header; work queue · lens area ·
// companion column as the regions grid; the inspector overlaying the
// companion side of the lens area; the dialogs and the draft surface on top.
// Below 1280px the work queue collapses to an icon rail; below 1024px the
// companion becomes a toggled drawer (container queries on #root).
import { useEffect, useState } from "react";
import { useApp } from "../app";
import { Companion } from "./Companion";
import { EnableForm, VerdictDialog } from "./Dialogs";
import { DraftView } from "./DraftView";
import { Header } from "./Header";
import { InspectorDock } from "./inspectors/Inspector";
import { LeftRegion } from "./LeftRegion";
import { LensArea } from "./LensArea";
import { PromptCard } from "./PromptCard";

export function App() {
  const { state, prompt, enableAdapter, verdictOpen } = useApp();
  const [drawer, setDrawer] = useState(false);
  useEffect(() => {
    document.body.classList.toggle("loading", state.loading);
  }, [state.loading]);
  return (
    <>
      <div id="document" className={drawer ? "drawer-open" : ""}>
        <Header />
        <div id="regions">
          <LeftRegion />
          <div id="lensWrap">
            <LensArea />
            <InspectorDock />
          </div>
          <Companion />
          <button id="drawerToggle" className="iconbtn" title={drawer ? "Hide the companion" : "Show the companion"} onClick={() => setDrawer((d) => !d)}>{drawer ? "✕" : "💬"}</button>
        </div>
      </div>
      {prompt ? <PromptCard key={prompt.title + (prompt.value || "")} spec={prompt} /> : null}
      {enableAdapter ? <EnableForm adapter={enableAdapter} /> : null}
      {verdictOpen ? <VerdictDialog /> : null}
      {state.draft.visible ? <DraftView key={state.draft.caseQueue + String(state.draft.mode)} /> : null}
    </>
  );
}
