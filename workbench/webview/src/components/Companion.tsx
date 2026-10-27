// The companion column (08 §8.5; impl §2.5): Conversation | Thread modes, the
// Knowledge section at the top, the composer at the bottom. Thread is the
// audit lens (the interleaved chronicle with act lines, sequence numbers on
// hover, the context-reset boundary); Conversation is the reading path.
import { useApp } from "../app";
import { Chronicle } from "./Chronicle";
import { Composer } from "./Composer";
import { Conversation } from "./Conversation";
import { KnowledgeSection } from "./KnowledgeSection";
import { LiveConversation } from "./LiveConversation";

export function Companion() {
  const { state, companionMode, setCompanionMode } = useApp();
  return (
    <aside id="companion" aria-label="Companion">
      <div className="regionhead">
        <div className="modetoggle" role="tablist">
          <button role="tab" aria-selected={companionMode === "conversation"} className={companionMode === "conversation" ? "on" : ""} onClick={() => setCompanionMode("conversation")}>Conversation</button>
          <button role="tab" aria-selected={companionMode === "thread"} className={companionMode === "thread" ? "on" : ""} onClick={() => setCompanionMode("thread")}>Thread</button>
        </div>
        {state.snap ? <span className="imeta">scoped to {state.snap.investigation.id.slice(0, 8)}</span> : null}
      </div>
      <div className="companionscroll">
        <KnowledgeSection />
        {companionMode === "conversation" ? <Conversation /> : <><Chronicle /><LiveConversation /></>}
      </div>
      <Composer />
    </aside>
  );
}
