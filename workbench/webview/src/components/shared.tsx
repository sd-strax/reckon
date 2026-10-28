// Shared primitives (impl §2): the citation chip, badges, the markdown renderer,
// the live countdown, the empty line. One implementation each, used everywhere.
import { useEffect, useState } from "react";
import { useApp } from "../app";
import { md } from "../markdown";
import { chipClass, chipText } from "../refs";
import { fmtRemaining } from "../derive/actions";

/** A clickable citation (02 §2.8, 08 §8.6): every ref opens the inspector.
 * Reads as evidence — a type label immediately, enriched with the value once
 * the host resolves it; the full id is in the tooltip. */
export function RefChip({ refId: r, pinned, className }: { refId: string; pinned?: boolean; className?: string }) {
  const { state, resolve, openInspector } = useApp();
  const label = state.labels[r];
  useEffect(() => { if (label === undefined) resolve(r); }, [r, label, resolve]);
  return (
    <span
      className={chipClass(r) + (pinned ? " pinned" : "") + (className ? " " + className : "")}
      data-ref={r}
      title={"Open in the inspector: " + r}
      onClick={(e) => { e.stopPropagation(); openInspector({ kind: "ref", ref: r }); }}
    >
      {chipText(r, label)}
    </span>
  );
}

/** A status / tier pill. tone: ok | warn | bad | info | muted | T1 | T2 | T3 | "". */
export function Badge({ tone, title, children, className }: { tone?: string; title?: string; className?: string; children: React.ReactNode }) {
  return <span className={"badge" + (tone ? " " + tone : "") + (className ? " " + className : "")} title={title}>{children}</span>;
}

/** The escape-first markdown renderer — the only consumer of md()'s HTML. */
export function Markdown({ text, live, className }: { text: string; live?: boolean; className?: string }) {
  return <div className={"md" + (live ? " live" : "") + (className ? " " + className : "")} dangerouslySetInnerHTML={{ __html: md(text) }} />;
}

/** Live approval-window countdown (03 §3.3): the analyst's only warning for lazy expiry. */
export function Countdown({ expiresAt, className }: { expiresAt: string; className?: string }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const left = Date.parse(expiresAt) - now;
  return (
    <div className={"countdown" + (left <= 0 ? " due" : left < 5 * 60000 ? " warn" : "") + (className ? " " + className : "")}>
      {left <= 0 ? "window expired — refresh" : "expires in " + fmtRemaining(left)}
    </div>
  );
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="empty">{children}</div>;
}

/** The micro-label eyebrow (01 §Scale). */
export function Eyebrow({ children, className }: { children: React.ReactNode; className?: string }) {
  return <h2 className={className}>{children}</h2>;
}

export function fmtTime(iso: string | undefined): string {
  return iso ? new Date(iso).toLocaleTimeString() : "";
}
export function fmtDateTime(iso: string | undefined): string {
  return iso ? new Date(iso).toLocaleString() : "";
}
