// The entity popover (ui/02 §2.4): one at a time, fixed-positioned near the
// chip, viewport-clamped. Pivot STAGES a question in the composer — it never
// fires (the pivot pattern). Appearances are the cross-investigation memory.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { REF_CHIP_CLASS, refType, refTypeLabel } from "../refs";
import { pinSpec } from "./PromptCard";

export function EntityPopover({ refId: r, anchor }: { refId: string; anchor: DOMRect }) {
  const { state, send, resolve, closePopover, openPrompt, stage } = useApp();
  const box = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: anchor.left, top: anchor.bottom + 6 });
  const label = state.labels[r];
  const apps = state.appearances[r];

  useEffect(() => {
    if (label === undefined) resolve(r);
    send({ type: "entity.info", ref: r });
  }, [r, label, resolve, send]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const w = el.offsetWidth, h = el.offsetHeight;
    const x = Math.min(anchor.left, window.innerWidth - w - 8);
    let y = anchor.bottom + 6;
    if (y + h > window.innerHeight - 8) y = Math.max(8, anchor.top - h - 6);
    setPos({ left: Math.max(8, x), top: y });
  }, [anchor, apps]);

  useEffect(() => {
    const onClick = (e: MouseEvent) => { if (box.current && !box.current.contains(e.target as Node)) closePopover(); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") closePopover(); };
    document.addEventListener("click", onClick);
    document.addEventListener("keydown", onKey);
    return () => { document.removeEventListener("click", onClick); document.removeEventListener("keydown", onKey); };
  }, [closePopover]);

  const typeLabel = refTypeLabel(r);
  const act = (label2: string, title: string, primary: boolean, onClick: () => void) => (
    <button key={label2} className={primary ? "primary" : undefined} title={title} onClick={(e) => { e.stopPropagation(); onClick(); }}>{label2}</button>
  );

  return (
    <div className="popover" data-ref={r} ref={box} style={{ left: pos.left, top: pos.top }} onClick={(e) => e.stopPropagation()}>
      <div className="pop-head">
        <div className={"pop-type " + (REF_CHIP_CLASS[refType(r)] || "")}>{typeLabel}</div>
        <div className="pop-value">{label || "…"}</div>
      </div>
      {/* The deterministic id (03 §7): the engine's UUIDv5 — same entity, same id,
          across producers and investigations. Displayed, never computed here. */}
      <div className="pop-id" title="Deterministic id (engine-minted UUIDv5) — same entity, same id, everywhere">{r}</div>
      <div className="pop-apps">
        {apps === undefined ? <div className="empty">checking other investigations…</div>
          : !apps.length ? <div className="pop-first">First seen in this investigation. Cross-investigation identity is preserved by deterministic id — not surveillance, just join keys.</div>
          : (
            <>
              <div className="pop-appshead">Appears in {apps.length} other investigation{apps.length === 1 ? "" : "s"}</div>
              {apps.slice(0, 6).map((a) => (
                <div
                  key={a.investigationId}
                  className="app-row"
                  title={(a.seedSummary || a.title) + " — open"}
                  onClick={(e) => { e.stopPropagation(); send({ type: "investigation.open", id: a.investigationId, title: a.title }); closePopover(); }}
                >
                  <span className={"dot" + (a.status === "concluded" || a.status === "archived" ? " unavailable" : "")} />
                  <span className="app-title">{a.title}</span>
                  <span className="app-status">{a.status} · {a.mentions}×</span>
                </div>
              ))}
              {apps.length > 6 ? <div className="cardmeta">+{apps.length - 6} more</div> : null}
            </>
          )}
      </div>
      <div className="pop-grid">
        {act("Pivot on entity", "Stage a pivot question in the composer — you review and send it", true, () => {
          const who = label || typeLabel + " " + r;
          closePopover();
          stage("Pivot on " + who + ": what other activity involves this " + typeLabel + " across the available telemetry?");
        })}
        {act("Pin as evidence", "Pin this entity with a finding note", false, () => {
          closePopover();
          openPrompt(pinSpec([r], undefined, (finding) => send({ type: "pin.add", refs: [r], finding })));
        })}
        {act("Open raw JSON", "The underlying record, read-only (02 §2.8)", false, () => { closePopover(); send({ type: "evidence.open", ref: r }); })}
        {act("Copy value", "Copy the resolved value (falls back to the id)", false, () => { send({ type: "copy", text: label ? label : r }); closePopover(); })}
      </div>
    </div>
  );
}
