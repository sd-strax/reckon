// The committed chronicle (design/ui/00 §7): the whole thread in sequence
// order — transcript-bearing entries as full turns reconstructed from their
// line-framed bodies, act-typed entries as one-line act lines, a context reset
// as a boundary, and an entry whose body is not loaded as a stub that opens
// the transcript on demand. Early history must never silently vanish.
import { useMemo } from "react";
import { useApp } from "../app";
import { parseTranscript } from "../transcript";
import { Markdown, RefChip, fmtDateTime, fmtTime } from "./shared";
import { ToolRow } from "./ToolRow";
import { actLineId, verdictLinks, type VerdictLink } from "../derive/verdict";
import { actionRequestText } from "../derive/turns";
import type { ThreadEntry } from "../../../src/viewTypes";
import type { ChronicleTurn } from "../../../src/protocol";

/** The act types that render as compact chronicle lines. */
export const CHRONICLE_ACTS: Record<string, { icon: string; verb: string }> = {
  "action-request": { icon: "▹", verb: "requested" },
  "action-approval": { icon: "✔", verb: "approved" },
  "action-rejection": { icon: "✕", verb: "rejected" },
  "action-edit": { icon: "✎", verb: "edited before approval" },
  "action-dispatch": { icon: "→", verb: "dispatched" },
  "action-result": { icon: "●", verb: "" },
  "action-resolution": { icon: "✓", verb: "resolved after checking the tool" },
  "action-reversal": { icon: "↺", verb: "reversed" },
  "action-expiry": { icon: "⌛", verb: "expired" },
  "evidence-pin": { icon: "📌", verb: "pinned" },
  "verdict": { icon: "⚖", verb: "verdict" },
  "lifecycle": { icon: "◆", verb: "" },
  "conclusion": { icon: "■", verb: "concluded" },
};

/** Scroll the thread to an act's line. */
export function scrollToAct(interpretationId: string): void {
  document.getElementById(actLineId(interpretationId))?.scrollIntoView({ block: "center" });
}

function ActLine({ e, link }: { e: ThreadEntry; link?: VerdictLink }) {
  const { state, openInspector } = useApp();
  const actions = state.snap?.actions || [];
  const spec = CHRONICLE_ACTS[e.interpretationType];
  const refs: string[] = [];
  for (const r of (e.inputRefs || []).concat(e.outputRefs || [])) if (r && !refs.includes(r)) refs.push(r);
  const verdict = e.interpretationType === "verdict";
  // A verdict says what it was (#155): its disposition leads, the rationale follows.
  const text = verdict && e.disposition
    ? `Verdict ${e.disposition}` + (e.summary ? ` — ${e.summary}` : "")
    : e.interpretationType === "action-request" ? actionRequestText(e, actions)
    : e.summary || (spec ? spec.verb : e.interpretationType);
  // A changed verdict is the opposite of noise: both ends stand out, linked.
  const replaced = verdict && e.superseded;
  return (
    <div id={actLineId(e.interpretationId)} className={"actline"
      + (e.actor.kind === "AI_DELEGATED" ? " ai" : "")
      + (verdict || e.interpretationType === "conclusion" ? " strong" : "")
      + (replaced ? " replaced" : e.superseded ? " superseded" : "")
      + (link?.replaces ? " revised" : "")}>
      <span className="acticon">{spec ? spec.icon : "·"}</span>
      <span className="acttext" title={text + " — open"} style={{ cursor: "pointer" }} onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{text}</span>
      {replaced && link?.replacedBy ? <a className="actreplaced" onClick={() => scrollToAct(link.replacedBy!.interpretationId)}>replaced at {fmtTime(link.replacedBy.occurredAt)}</a>
        : e.superseded ? <span className="actsuperseded">superseded</span> : null}
      {link?.replaces ? <a className="actreplaced" onClick={() => scrollToAct(link.replaces!.interpretationId)}>replaces the {fmtTime(link.replaces.occurredAt)} verdict{link.replaces.disposition ? ` (${link.replaces.disposition})` : ""}</a> : null}
      {refs.slice(0, 2).map((r) => <RefChip key={r} refId={r} />)}
      <span className="acttime">{fmtTime(e.occurredAt)}</span>
    </div>
  );
}

function TurnStub({ e }: { e: ThreadEntry }) {
  const { openInspector } = useApp();
  return (
    <div
      className={"actline turnstub" + (e.actor.kind === "AI_DELEGATED" ? " ai" : "")}
      style={{ cursor: "pointer" }}
      onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}
    >
      <span className="acticon">…</span>
      <span className="acttext">{(e.summary || "committed turn") + " — open transcript"}</span>
      <span className="acttime">{fmtTime(e.occurredAt)}</span>
    </div>
  );
}

/** Knowledge-retrieval provenance (ui/02 §2.11): which SOPs / prior cases the
 * turn pulled, followed (solid) vs merely consulted (dashed). */
export function KnowledgeChips({ e }: { e: ThreadEntry }) {
  const { openInspector } = useApp();
  const sops = e.consultedSops || [];
  const similar = e.consultedSimilar || [];
  if (!sops.length && !similar.length) return null;
  return (
    <div className="usage">
      {sops.map((s, i) => (
        <span key={"s" + i} className={"sopchip" + (s.used ? "" : " consulted")} title="Open the procedure" onClick={() => openInspector({ kind: "sop", sopId: s.sopId })}>{(s.used ? "followed: " : "consulted: ") + (s.title || s.sopId)}</span>
      ))}
      {similar.map((s, i) => (
        <span key={"c" + i} className={"sopchip" + (s.used ? "" : " consulted")} title="Open the similar case" onClick={() => openInspector({ kind: "case", investigationRef: s.investigationRef })}>{(s.used ? "drew on case: " : "similar case: ") + (s.title || s.investigationRef)}</span>
      ))}
    </div>
  );
}

export function RestoredTurn({ turn }: { turn: ChronicleTurn }) {
  const parsed = useMemo(() => parseTranscript(turn.body), [turn.body]);
  const body = (
    <div>
      <div className="usage">{(turn.occurredAt ? fmtDateTime(turn.occurredAt) + " · " : "") + "committed turn"}</div>
      {parsed.items.map((it, i) => {
        if (it.kind === "user") return <div key={i} className="msg user"><span className="bubble">{it.text}</span></div>;
        if (it.kind === "assistant") return <Markdown key={i} className="msg assistant" text={it.text} />;
        if (it.kind === "loop") return <div key={i} className="committed">{it.text}</div>;
        return <ToolRow key={i} name={it.name} argsHint={it.argsRaw} argsPretty={it.argsPretty} result={it.result} />;
      })}
    </div>
  );
  if (parsed.aside) {
    // An aside turn reconstructs COLLAPSED — the same record, the same quieter lens it had live.
    return (
      <details className="aside">
        <summary>{parsed.asideLabel} · committed aside{turn.occurredAt ? " · " + fmtDateTime(turn.occurredAt) : ""}</summary>
        {body}
      </details>
    );
  }
  return body;
}

export function Chronicle() {
  const { state } = useApp();
  const thread = state.snap?.thread || [];
  const turns = state.snap?.turns || [];
  const bodies = useMemo(() => new Map(turns.map((t) => [t.sequenceNo, t])), [turns]);
  const entries = useMemo(() => thread.slice().sort((a, b) => a.sequenceNo - b.sequenceNo), [thread]);
  const links = useMemo(() => verdictLinks(thread), [thread]);
  return (
    <div id="restored">
      {entries.map((e) => {
        // A context reset is a BOUNDARY, not a line (prefix contract shared
        // with the agent's rehydrate marker — keep in lockstep).
        if (e.summary && e.summary.indexOf("context reset:") === 0) {
          return <div key={e.sequenceNo} className="ctxreset">⟲ context reset — agent re-grounded from the investigation record{e.occurredAt ? " · " + fmtTime(e.occurredAt) : ""}</div>;
        }
        const body = bodies.get(e.sequenceNo);
        if (body) {
          return <div key={e.sequenceNo}><RestoredTurn turn={body} /><KnowledgeChips e={e} /></div>;
        }
        if (CHRONICLE_ACTS[e.interpretationType]) return <ActLine key={e.sequenceNo} e={e} link={links.get(e.interpretationId)} />;
        if (e.hasTranscript) return <TurnStub key={e.sequenceNo} e={e} />;
        return null;
      })}
    </div>
  );
}
