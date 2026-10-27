// The lens area (08 §8.4): one lens at a time behind four tabs with their
// counts; the knowledge nudge (09 §9.6) heads the Reasoning and Response
// lenses, beside the ⚡ buttons and the plan, and never includes anything.
import { useApp } from "../app";
import { LENSES, LENS_LABELS } from "../derive/lens";
import { isLive } from "../derive/hyps";
import { knowledgeNudge } from "../derive/knowledgeNudge";
import { ReasoningLens } from "./lenses/ReasoningLens";
import { ResponseLens } from "./lenses/ResponseLens";
import { StoryLens } from "./lenses/StoryLens";
import { SummaryLens } from "./lenses/SummaryLens";

function KnowledgeNudge() {
  const { state } = useApp();
  const posture = state.snap?.investigation.knowledgeInjection || "opt_in";
  const nudge = knowledgeNudge(state.knowledge, posture);
  if (!nudge) return null;
  return (
    <div className="nudge" role="note">
      <span className="dot" aria-hidden="true" />
      {nudge.text} · <a onClick={() => document.getElementById("knowledgeSection")?.scrollIntoView({ block: "start" })}>Review</a>
    </div>
  );
}

export function LensArea() {
  const { state, lens, setLens } = useApp();
  const hs = state.snap?.hypotheses || [];
  const pending = (state.snap?.actions || []).filter((a) => a.pending && !a.expired).length;
  const counts: Record<string, number> = { reasoning: hs.filter(isLive).length, response: pending };
  return (
    <main id="lensArea" aria-label="Lenses">
      <div className="lenstabs" role="tablist">
        {LENSES.map((l) => (
          <button key={l} role="tab" aria-selected={lens === l} className={"lenstab" + (lens === l ? " on" : "")} onClick={() => setLens(l)}>
            {LENS_LABELS[l]}{counts[l] ? <span className="count">{counts[l]}</span> : null}
          </button>
        ))}
      </div>
      <div className="lensscroll">
        {state.error ? <div id="banner">Could not load: {state.error}</div> : null}
        {(lens === "reasoning" || lens === "response") ? <KnowledgeNudge /> : null}
        {!state.snap ? null : lens === "summary" ? <SummaryLens /> : lens === "story" ? <StoryLens /> : lens === "reasoning" ? <ReasoningLens /> : <ResponseLens />}
      </div>
    </main>
  );
}
