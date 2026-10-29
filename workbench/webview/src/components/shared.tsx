// Shared primitives (impl §2): the citation chip, badges, the markdown renderer,
// the live countdown, the empty line. One implementation each, used everywhere.
import { useEffect, useState } from "react";
import { useApp } from "../app";
import { md } from "../markdown";
import { chipClass, chipText } from "../refs";
import { fmtRemaining } from "../derive/actions";
import { hypWords, shortTitle } from "../derive/hyps";
import type { InvestigationSnapshot } from "../../../src/protocol";

/** A clickable citation (02 §2.8, 08 §8.6): every ref opens the inspector.
 * Reads as evidence — a type label immediately, enriched with the value once
 * the host resolves it; the full id is in the tooltip. */
export function RefChip({ refId: r, pinned, className }: { refId: string; pinned?: boolean; className?: string }) {
  const { state, resolve, openInspector } = useApp();
  // A chip names its object (#153): hypotheses and predictions by what the
  // snapshot says they are, the seed entity by the identifier it was rooted on.
  const resolved = state.labels[r];
  const label = snapshotLabel(r, state.snap) ?? resolved;
  useEffect(() => { if (resolved === undefined) resolve(r); }, [r, resolved, resolve]);
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

/** What the snapshot already knows a ref to be, before any fetch. */
function snapshotLabel(r: string, snap: InvestigationSnapshot | null): string | undefined {
  if (!snap) return undefined;
  const hs = snap.hypotheses || [];
  if (r.startsWith("x-hypothesis--")) {
    const h = hs.find((x) => x.id === r);
    return h ? hypWords(h, hs) : undefined;
  }
  if (r.startsWith("x-prediction--")) {
    for (const h of hs) {
      const p = h.predictions.find((x) => x.id === r);
      if (p) return (p.ordinal ? "P" + p.ordinal + " " : "") + shortTitle(p);
    }
    return undefined;
  }
  const seed = snap.investigation.seed;
  return seed?.entityRef === r && seed.entityIdentifier ? seed.entityIdentifier : undefined;
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
