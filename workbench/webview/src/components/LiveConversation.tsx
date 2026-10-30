// The in-flight conversation: ONLY the streaming turn (and a failed turn's
// preserved copy); once idle, the committed chronicle holds everything.
import { useApp } from "../app";
import type { LiveTurn } from "../store";
import { Markdown } from "./shared";
import { ToolRow } from "./ToolRow";

function Turn({ t, streaming }: { t: LiveTurn; streaming: boolean }) {
  const last = t.segments[t.segments.length - 1];
  const body = (
    <>
      {t.reasoning ? <div className="reasoning">{t.reasoning}</div> : null}
      {t.segments.map((s, i) => {
        if (s.kind === "text") return <Markdown key={i} text={s.text} live={streaming && s === last} />;
        if (s.kind === "step") return <div key={i} className="stepmark">· step {s.round} ·</div>;
        if (s.kind === "usage") return <div key={i} className="usage">{s.text}</div>;
        if (s.kind === "notice") return <div key={i} className="notice">{s.text}</div>;
        return (
          <ToolRow
            key={i}
            name={s.row.name}
            argsHint={JSON.stringify(s.row.input ?? {})}
            argsPretty={JSON.stringify(s.row.input ?? {}, null, 2)}
            result={s.row.result}
          />
        );
      })}
    </>
  );
  if (t.aside) {
    // The "btw" lens: the full exchange streams into a COLLAPSED disclosure —
    // expandable live for anyone watching; the working surface is the tracker.
    return (
      <details className="aside">
        <summary>{t.aside.label}{t.aside.done ? " · done — see the tracker" : " · working…"}</summary>
        <div className="asidebody">{body}</div>
      </details>
    );
  }
  return <div className="msg assistant">{body}</div>;
}

export function LiveConversation() {
  const { state } = useApp();
  return (
    <div id="conversation">
      {state.live.map((it, i) => {
        if (it.kind === "user") return <div key={i} className="msg user"><span className="bubble">{it.text}</span></div>;
        if (it.kind === "committed") return <div key={i} className="committed">{it.text}</div>;
        if (it.kind === "error") return <div key={i} className="error">{it.message}</div>;
        return <Turn key={i} t={it} streaming={state.streaming && i === state.live.length - 1} />;
      })}
    </div>
  );
}
