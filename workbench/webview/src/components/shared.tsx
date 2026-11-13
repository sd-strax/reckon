// Shared primitives (impl §2): the citation chip, badges, the markdown renderer,
// the live countdown, the empty line. One implementation each, used everywhere.
import { useEffect, useState } from "react";
import { useApp } from "../app";
import { md } from "../markdown";
import { chipClass, chipText } from "../refs";
import { fmtRemaining } from "../derive/actions";
import { hypWords, titleOf } from "../derive/hyps";
import type { InvestigationSnapshot } from "../../../src/protocol";
import type { SentenceToken } from "../../../src/viewTypes";

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

/** An entity named inside a sentence (adr/0090): the chip shows the
 * sentence's own words for it and opens the inspector on the ref. */
export function EntityChip({ refId: r, text }: { refId: string; text: string }) {
  const { openInspector } = useApp();
  return (
    <span className={chipClass(r) + " inline"} data-ref={r} title={"Open in the inspector: " + r}
      onClick={(e) => { e.stopPropagation(); openInspector({ kind: "ref", ref: r }); }}>{text}</span>
  );
}

/** An event's sentence (adr/0090): the server's tokens, entities as chips.
 * `beside` names, per entity ref, the aliased accounts whose identifiers read
 * after its chip (adr/0352). */
export function Sentence({ tokens, className, beside }: { tokens: SentenceToken[]; className?: string; beside?: Map<string, string[]> }) {
  return (
    <span className={"sentence" + (className ? " " + className : "")}>
      {tokens.map((t, i) => t.t === "entity" && t.ref
        ? <span key={i}><EntityChip refId={t.ref} text={t.text} />{(beside?.get(t.ref) ?? []).map((o) => <AliasOf key={o} refId={o} />)}</span>
        : <span key={i}>{t.text}</span>)}
    </span>
  );
}

/** An event as one line (adr/0090): its sentence, or its class name when the class has no template. */
export function EventSentence({ sentence, label, beside }: { sentence?: SentenceToken[]; label: string; beside?: Map<string, string[]> }) {
  return sentence?.length ? <Sentence tokens={sentence} beside={beside} /> : <span className="imeta">{label}</span>;
}

/** An aliased account's other identifier (adr/0352), read beside the chip
 * of the account it is joined to; it opens the inspector on its own object. */
export function AliasOf({ refId: r }: { refId: string }) {
  const { state, resolve, openInspector } = useApp();
  const resolved = state.labels[r];
  useEffect(() => { if (resolved === undefined) resolve(r); }, [r, resolved, resolve]);
  return (
    <span className="aliasof mono" data-ref={r} title={"Also this account: open in the inspector: " + r}
      onClick={(e) => { e.stopPropagation(); openInspector({ kind: "ref", ref: r }); }}>{resolved || chipText(r, undefined)}</span>
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
      if (p) return (p.ordinal ? "P" + p.ordinal + " " : "") + titleOf(p);
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
  const { state, openInspector } = useApp();
  // A chip in the prose opens its evidence (#136). A shortened id resolves to
  // the one full id in the record it starts with; otherwise it stays a label.
  const onClick = (e: React.MouseEvent) => {
    const el = (e.target as HTMLElement).closest("[data-ref]") as HTMLElement | null;
    if (!el) return;
    const ref = fullRef(el.dataset.ref || "", state.snap);
    if (ref) { e.stopPropagation(); openInspector({ kind: "ref", ref }); }
  };
  return <div className={"md" + (live ? " live" : "") + (className ? " " + className : "")} onClick={onClick} dangerouslySetInnerHTML={{ __html: md(text) }} />;
}

/** The full id a (possibly shortened) ref in prose names, from the refs the record holds. */
export function fullRef(ref: string, snap: InvestigationSnapshot | null): string | undefined {
  if (/--[0-9a-f]{8}-[0-9a-f]{4}-/.test(ref)) return ref;
  if (!snap) return undefined;
  const known = new Set<string>();
  for (const e of snap.thread || []) for (const r of [...(e.inputRefs || []), ...(e.outputRefs || [])]) known.add(r);
  for (const p of snap.pins) for (const r of p.inputRefs || []) known.add(r);
  for (const h of snap.hypotheses) for (const p of h.predictions) for (const r of p.testResultRefs || []) known.add(r);
  for (const a of snap.actions || []) for (const r of a.evidenceRefs || []) known.add(r);
  for (const s of snap.derived?.strip || []) known.add(s.ref);
  for (const n of snap.derived?.graph.refs || []) known.add(n.id);
  const hits = [...known].filter((k) => k.startsWith(ref));
  return hits.length === 1 ? hits[0] : undefined;
}

/** Live approval-window countdown (03 §3.3): the analyst's only warning for lazy expiry. */
export function Countdown({ expiresAt, className }: { expiresAt: string; className?: string }) {
  const [now, setNow] = useState(() => Date.now());
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
