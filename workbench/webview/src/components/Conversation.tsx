// The companion's Conversation mode (08 §8.5): the analyst's turns and the
// agent's prose; what a turn records renders as one link line each; consulted
// knowledge as chips; the activity disclosure (tool calls + records) collapsed;
// proposed actions as compact cards. No sequence numbers, no "committed"
// lines here — they stay in Thread mode and the export.
import { useMemo } from "react";
import { useApp } from "../app";
import { groupTurns, type Turn } from "../derive/turns";
import { parseTranscript } from "../transcript";
import { ActionCard } from "./ActionCard";
import { KnowledgeChips } from "./Chronicle";
import { LiveConversation } from "./LiveConversation";
import { Markdown, fmtDateTime, fmtTime } from "./shared";
import { ToolRow } from "./ToolRow";

function AgentTurn({ t }: { t: Turn }) {
  const { state, goTo, openInspector } = useApp();
  const parsed = useMemo(() => (t.transcript ? parseTranscript(t.transcript.body) : null), [t.transcript]);
  const asks = parsed?.items.filter((i) => i.kind === "user") || [];
  const answers = parsed?.items.filter((i) => i.kind === "assistant") || [];
  const tools = parsed?.items.filter((i) => i.kind === "tool") || [];
  const requested = (state.snap?.actions || []).filter((a) => t.records.some((r) => r.entry.interpretationType === "action-request" && (a.requestingInterpretationId === r.entry.interpretationId || (r.entry.outputRefs || []).includes(a.actionId))));
  const who = t.entry.actor.principalName || t.entry.actor.principal;
  const body = (
    <>
      {asks.map((a, i) => a.kind === "user" ? <div key={"u" + i} className="msg user"><span className="bubble">{a.text}</span></div> : null)}
      <div className="msg assistant">
        <div className="turnhead"><span className="who">reckon{t.entry.actor.model ? " · " + t.entry.actor.model : ""}</span><span className="imeta" title={t.entry.occurredAt}>{fmtTime(t.entry.occurredAt)}{t.entry.superseded ? " · superseded" : ""}</span></div>
        {answers.length ? answers.map((a, i) => a.kind === "assistant" ? <Markdown key={i} text={a.text} /> : null) : <p className="imeta">{t.entry.summary || "(no prose in this turn)"}</p>}
        {t.records.length ? (
          <ul className="records">
            {t.records.map((r, i) => (
              <li key={i} className={r.tone}>
                <span className="rglyph" aria-hidden="true">{r.tone === "ok" ? "✓" : r.tone === "bad" ? "✕" : "→"}</span>
                {r.target ? <a title={r.title} onClick={() => r.target!.kind === "ref" ? goTo({ kind: "ref", ref: r.target!.ref }) : goTo({ kind: "lens", lens: r.target!.lens })}>{r.text}</a> : <span title={r.title}>{r.text}</span>}
              </li>
            ))}
          </ul>
        ) : null}
        {requested.length ? <div className="proposed">{requested.map((a) => <ActionCard key={a.actionId} a={a} variant="compact" />)}</div> : null}
        <KnowledgeChips e={t.entry} />
        {(tools.length || t.records.length) ? (
          <details className="activity">
            <summary>Activity · {tools.length || t.entry.toolCalls} tool call{(tools.length || t.entry.toolCalls) === 1 ? "" : "s"}, {t.records.length} record{t.records.length === 1 ? "" : "s"}</summary>
            {tools.map((x, i) => x.kind === "tool" ? <ToolRow key={i} name={x.name} argsHint={x.argsRaw} argsPretty={x.argsPretty} result={x.result} /> : null)}
            {t.records.map((r, i) => <div key={"r" + i} className="imeta">{r.entry.interpretationType}: {r.text}</div>)}
            <div className="imeta"><a onClick={() => openInspector({ kind: "turn", interpretationId: t.entry.interpretationId })}>Open the turn</a></div>
          </details>
        ) : !parsed && t.entry.hasTranscript ? <div className="imeta"><a onClick={() => openInspector({ kind: "turn", interpretationId: t.entry.interpretationId })}>Open the turn</a> — its transcript is not loaded here</div> : null}
        <div className="imeta">asked by {who}</div>
      </div>
    </>
  );
  if (t.aside) {
    return <details className="aside"><summary>{parsed?.asideLabel || "⚡ aside"} · committed · {fmtDateTime(t.entry.occurredAt)}</summary><div className="asidebody">{body}</div></details>;
  }
  return <div className="turn">{body}</div>;
}

export function Conversation() {
  const { state } = useApp();
  const snap = state.snap;
  const turns = useMemo(() => groupTurns(snap?.thread || [], snap?.turns || [], snap?.hypotheses || [], snap?.actions || []), [snap?.thread, snap?.turns, snap?.hypotheses, snap?.actions]);
  return (
    <div id="conversationMode">
      {turns.map((t) => (
        <div key={t.entry.sequenceNo}>
          {t.resetBefore ? <div className="ctxreset">⟲ context reset — agent re-grounded from the investigation record · {fmtTime(t.resetBefore.occurredAt)}</div> : null}
          <AgentTurn t={t} />
        </div>
      ))}
      <LiveConversation />
    </div>
  );
}
