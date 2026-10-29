// The lens area (08 §8.4): one lens at a time behind four tabs with their
// counts, headed by a new investigation's first step.
import { useEffect } from "react";
import { useApp } from "../app";
import { LENSES, LENS_LABELS } from "../derive/lens";
import { isLive } from "../derive/hyps";
import { firstStep, isFresh } from "../derive/firstStep";
import { ReasoningLens } from "./lenses/ReasoningLens";
import { ResponseLens } from "./lenses/ResponseLens";
import { StoryLens } from "./lenses/StoryLens";
import { SummaryLens } from "./lenses/SummaryLens";

/** The first step of a new investigation (#160): what it is rooted on and a
 * request staged in the composer — sent only when the analyst sends it. */
function FirstStep() {
  const { state, stage, setCompanionMode } = useApp();
  const fresh = isFresh(state.snap);
  const id = state.snap?.investigation.id;
  // Opening a new investigation lands on the conversation, where it starts.
  useEffect(() => { if (fresh) setCompanionMode("conversation"); }, [fresh, id, setCompanionMode]);
  if (!state.snap || !fresh) return null;
  const step = firstStep(state.snap);
  return (
    <div className="card firststep" role="note">
      <div className="kicker">New investigation</div>
      <p>Rooted on <strong>{step.rootedOn}</strong>. Nothing has happened in it yet — start in the conversation.</p>
      <button className="primary" onClick={() => { setCompanionMode("conversation"); stage(step.ask); }}>Start investigating…</button>
      <div className="imeta">Puts a first request in the composer for you to edit and send.</div>
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
        <FirstStep />
        {!state.snap ? null : lens === "summary" ? <SummaryLens /> : lens === "story" ? <StoryLens /> : lens === "reasoning" ? <ReasoningLens /> : <ResponseLens />}
      </div>
    </main>
  );
}
