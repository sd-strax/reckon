// The inspector's shared pieces (08 §8.6, impl §2.4): tabs, the generic
// depth-limited field table (informative for any record without per-type
// code), the raw JSON view (textContent, copy), key/value rows, and the
// "In this case" block every pane carries.
import { useState } from "react";
import { useApp } from "../../app";
import { inThisCase, isEmpty } from "../../derive/citations";
import { hypName, predName } from "../../derive/hyps";
import { Badge, fmtTime } from "../shared";

export function Tabs({ tabs, initial }: { tabs: { id: string; label: string; body: () => React.ReactNode }[]; initial?: string }) {
  const [active, setActive] = useState(initial || tabs[0]?.id);
  const cur = tabs.find((t) => t.id === active) || tabs[0];
  return (
    <div className="itabs">
      <div className="itabbar" role="tablist">
        {tabs.map((t) => (
          <button key={t.id} role="tab" aria-selected={t.id === cur?.id} className={"itab" + (t.id === cur?.id ? " on" : "")} onClick={() => setActive(t.id)}>{t.label}</button>
        ))}
      </div>
      <div className="itabbody" role="tabpanel">{cur?.body()}</div>
    </div>
  );
}

export function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="isection"><div className="kicker">{title}</div>{children}</section>;
}

/** A labelled fact row; values that look like data render in mono. */
export function KV({ rows }: { rows: { k: string; v: React.ReactNode; mono?: boolean }[] }) {
  return (
    <dl className="ikv">
      {rows.filter((r) => r.v !== undefined && r.v !== null && r.v !== "").map((r, i) => (
        <div key={i} className="ikvrow"><dt>{r.k}</dt><dd className={r.mono ? "mono" : undefined}>{r.v}</dd></div>
      ))}
    </dl>
  );
}

function human(k: string): string {
  return String(k).replace(/[_.]/g, " ");
}
function looksMono(v: unknown): boolean {
  const s = String(v);
  return (/--|[0-9a-f]{16,}|^\d+$|:|\\|\//.test(s) && !/\s/.test(s.trim())) || s.length > 40;
}

/** The generic field renderer ported from the evidence card: primitives as
 * rows, nested objects recursed two levels, deeper ones as JSON. */
export function Fields({ obj, depth = 0 }: { obj: unknown; depth?: number }) {
  const entries = Object.entries((obj && typeof obj === "object" ? obj : {}) as Record<string, unknown>)
    .filter(([, v]) => v !== null && v !== undefined && v !== "");
  if (!entries.length) return <div className="empty">—</div>;
  return (
    <dl className="ifields">
      {entries.map(([k, v]) => {
        if (Array.isArray(v)) {
          if (v.every((x) => typeof x !== "object")) {
            return <div key={k} className="ikvrow"><dt>{human(k)}</dt><dd className="mono">{v.join(", ")}</dd></div>;
          }
          return (
            <div key={k} className="nest">
              <div className="nlabel">{human(k)} ({v.length})</div>
              {v.map((x, i) => <Fields key={i} obj={typeof x === "object" ? x : { ["#" + i]: x }} depth={depth + 1} />)}
            </div>
          );
        }
        if (typeof v === "object") {
          if (depth >= 2) return <div key={k} className="ikvrow"><dt>{human(k)}</dt><dd className="mono">{JSON.stringify(v)}</dd></div>;
          return <div key={k} className="nest"><div className="nlabel">{human(k)}</div><Fields obj={v} depth={depth + 1} /></div>;
        }
        return <div key={k} className="ikvrow"><dt>{human(k)}</dt><dd className={looksMono(v) ? "mono" : undefined}>{String(v)}</dd></div>;
      })}
    </dl>
  );
}

/** Raw JSON, rendered through textContent, with copy. */
export function Json({ value }: { value: unknown }) {
  const { send } = useApp();
  const text = JSON.stringify(value, null, 2) ?? "";
  return (
    <div className="ijson">
      <button className="ghost icopy" title="Copy the JSON" onClick={() => send({ type: "copy", text })}>Copy</button>
      <pre>{text}</pre>
    </div>
  );
}

/** "In this case" (08 §8.6 frame item 3): what in this investigation cites the object. */
export function InThisCaseBlock({ refId }: { refId: string }) {
  const { state, openInspector } = useApp();
  const c = inThisCase(refId, state.snap);
  const hs = state.snap?.hypotheses || [];
  if (isEmpty(c)) return <div className="ithiscase"><span className="kicker">In this case:</span> <span className="empty">not cited yet</span></div>;
  return (
    <div className="ithiscase">
      <span className="kicker">In this case:</span>
      {c.seed ? <span className="icite">the seed</span> : null}
      {c.predictions.map(({ h, p }) => (
        <span key={p.id} className="icite">
          test result of <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>{predName(p)}</a> ({p.status}, {hypName(h, hs)})
        </span>
      ))}
      {c.actions.map(({ a, role }) => (
        <span key={a.actionId} className="icite">
          {role === "evidence" ? "grounds " : "target of "}<a onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{a.actionType}</a> <Badge tone={a.pending ? "warn" : ""}>{a.pending ? a.pendingLabel : a.status}</Badge>
        </span>
      ))}
      {c.pins.map((p) => <span key={p.interpretationId} className="icite">pinned{p.superseded ? " (superseded)" : ""}: “{p.finding}”</span>)}
      {c.acts.slice(0, 4).map(({ e, role }) => (
        <span key={e.sequenceNo} className="icite">
          {role === "produced" ? "produced by " : "read by "}
          <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || e.interpretationType}</a>
          <span className="imeta"> · {fmtTime(e.occurredAt)}</span>
        </span>
      ))}
      {c.acts.length > 4 ? <span className="icite imeta">+{c.acts.length - 4} more acts</span> : null}
    </div>
  );
}

export function Loading({ what }: { what: string }) {
  return <div className="empty">Loading {what}…</div>;
}

export function InspectorError({ message }: { message: string }) {
  return <div className="ierror"><div className="kicker">Could not open</div>{message}</div>;
}
