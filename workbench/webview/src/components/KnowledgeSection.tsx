// The Knowledge section (09 §9.6): heads the companion column and replaces
// the rail. What the agent may read in this investigation, with include
// toggles whose initial state follows the posture; a collapsed summary once
// reviewed; a scoped search whose hits join the list with their toggle off;
// each item opens in the inspector. Inclusion rides the next turn.
import { useState } from "react";
import { useApp } from "../app";
import { Badge } from "./shared";

export function KnowledgeSection() {
  const { state, dispatch, send, openInspector } = useApp();
  const items = state.knowledge;
  const [open, setOpen] = useState(true);
  const [q, setQ] = useState("");
  const [searching, setSearching] = useState(false);
  const included = items.filter((k) => k.included).length;
  const workable = state.snap && ["DRAFT", "ACTIVE", "PAUSED"].includes(String(state.snap.investigation.state).toUpperCase());
  if (!items.length && !workable) return null;
  const search = () => {
    const text = q.trim();
    if (!text) return;
    setSearching(true);
    send({ type: "knowledge.search", text });
    setTimeout(() => setSearching(false), 1500);
  };
  return (
    <section className="ksection" id="knowledgeSection">
      <div className="khead" onClick={() => setOpen((o) => !o)} role="button" aria-expanded={open}>
        <span className="kicker">Knowledge</span>
        <span className="imeta">{items.length} retrieved · {included} included</span>
        <span className="pchev" aria-hidden="true">{open ? "⌄" : "›"}</span>
      </div>
      {open ? (
        <>
          <div className="imeta">The agent reads only what is included.</div>
          <div id="knowledgeBox">
            {items.map((k) => (
              <div key={k.ref} className={"kitem" + (k.included ? "" : " excluded")}>
                <input type="checkbox" checked={!!k.included} title={k.included ? "Included in the agent's context" : "Not in context — check to include"} onChange={(e) => dispatch({ type: "knowledge.toggle", ref: k.ref, included: e.target.checked })} />
                <div className="kbody" title={k.excerpt || ""}>
                  <div className="ktitle">
                    <span className="kkind">{k.kind === "sop" ? "Procedure" : "Past case"}</span>
                    {k.band ? <Badge tone={k.band === "NEAR_DUPLICATE" ? "ok" : k.band === "DISTINCT" ? "muted" : "info"}>{k.band}</Badge> : null}
                    <a onClick={() => openInspector(k.kind === "sop" ? { kind: "sop", sopId: k.ref } : { kind: "case", investigationRef: k.ref })}>{k.title || k.ref}</a>
                  </div>
                  {k.rationale ? <div className="krationale">{k.rationale}</div> : null}
                </div>
              </div>
            ))}
            {!items.length ? <div className="empty">nothing retrieved for this investigation yet</div> : null}
          </div>
          <div className="ksearch">
            <input type="text" value={q} placeholder="Search procedures and past cases…" onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); search(); } }} />
            <button disabled={!q.trim() || searching} onClick={search}>Search</button>
          </div>
          <div className="imeta"><a className="kopen" onClick={() => send({ type: "knowledge.open", section: "procedures" })}>Open the Knowledge view</a> · add, browse, edit and query knowledge outside this investigation</div>
        </>
      ) : null}
    </section>
  );
}
