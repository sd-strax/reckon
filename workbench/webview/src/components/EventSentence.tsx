// The per-class event rendering (adr/0090; 08 §8.4 Story, §8.6.1 Evidence):
// the sentence with its entity slots as chips (a slot the record could not
// resolve renders as text), the curated field list, and the decoded form of
// an encoded field beside the original. All of it comes from the server's
// view; nothing is derived here.
import type { EventField, EventSpan, EventView } from "../../../src/backend";
import { useApp } from "../app";
import { KV } from "./inspectors/primitives";
import { RefChip } from "./shared";

export function EventSentence({ spans, className }: { spans: EventSpan[]; className?: string }) {
  const { openInspector } = useApp();
  return (
    <span className={"esentence" + (className ? " " + className : "")}>
      {spans.map((s, i) => s.t === "entity"
        ? (s.ref
          ? <RefChip key={i} refId={s.ref} className="eslot" />
          : <a key={i} className="eslot plain" title={s.type} onClick={s.ref ? () => openInspector({ kind: "ref", ref: s.ref! }) : undefined}>{s.text}</a>)
        : <span key={i}>{s.text}</span>)}
    </span>
  );
}

/** The curated fields as key/value rows; a field naming an entity links to it. */
export function EventFields({ fields }: { fields: EventField[] }) {
  const { openInspector } = useApp();
  return (
    <KV rows={fields.map((f) => ({
      k: f.label,
      v: f.ref ? <a onClick={() => openInspector({ kind: "ref", ref: f.ref! })}>{f.value}</a> : f.tone ? <span className={"etone " + f.tone}>{f.value}</span> : f.value,
      mono: f.mono,
    }))} />
  );
}

export function EventDecoded({ view }: { view: EventView }) {
  if (!view.decoded) return null;
  return (
    <div className="edecoded">
      <span className="kicker">Decoded</span> <span className="mono">{view.decoded.field}</span> · {view.decoded.encoding}
      <pre className="mono">{view.decoded.text}</pre>
      <div className="imeta">The original value stays above; this is its decoded form, shown beside it.</div>
    </div>
  );
}
