// The companion column (08 §8.5; impl §2.5): Conversation | Thread modes, the
// Knowledge section at the top, the composer at the bottom. Thread is the
// audit lens (the interleaved chronicle with act lines, sequence numbers on
// hover, the context-reset boundary); Conversation is the reading path.
// Its left edge is a grip: drag (or arrow keys) to resize, double-click to
// return to the default width.
import { useRef, type KeyboardEvent, type PointerEvent } from "react";
import { useApp } from "../app";
import { Chronicle } from "./Chronicle";
import { Composer } from "./Composer";
import { Conversation } from "./Conversation";
import { KnowledgeSection } from "./KnowledgeSection";
import { LiveConversation } from "./LiveConversation";

/** The narrowest the companion goes, and the share of the regions it may take. */
const MIN_W = 280;
const MAX_SHARE = 0.6;
const KEY_STEP = 24;

export function Companion() {
  const { state, companionMode, setCompanionMode, companionWidth, setCompanionWidth } = useApp();
  const aside = useRef<HTMLElement>(null);
  const dragging = useRef(false);
  const clamp = (w: number) => {
    const regions = aside.current?.parentElement?.getBoundingClientRect().width || 0;
    const max = regions ? Math.max(MIN_W, regions * MAX_SHARE) : w;
    return Math.round(Math.min(Math.max(w, MIN_W), max));
  };
  const widthAt = (clientX: number) => clamp((aside.current?.parentElement?.getBoundingClientRect().right || 0) - clientX);
  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    dragging.current = true;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    document.body.classList.add("gripDragging");
  };
  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    if (dragging.current) setCompanionWidth(widthAt(e.clientX), false);
  };
  const onUp = (e: PointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    dragging.current = false;
    document.body.classList.remove("gripDragging");
    setCompanionWidth(widthAt(e.clientX), true);
  };
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const now = companionWidth || aside.current?.getBoundingClientRect().width || MIN_W;
    setCompanionWidth(clamp(now + (e.key === "ArrowLeft" ? KEY_STEP : -KEY_STEP)), true);
  };
  return (
    <aside id="companion" aria-label="Companion" ref={aside}>
      <div
        className="companiongrip" role="separator" aria-orientation="vertical" aria-label="Resize the companion" tabIndex={0}
        title="Drag to resize · double-click for the default width"
        onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp} onKeyDown={onKey}
        onDoubleClick={() => setCompanionWidth(undefined, true)}
      />
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
