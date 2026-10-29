// The knowledge library (adr/0129): the organization's procedures and past
// cases, to browse, search, read and import. Nothing here is in the agent's
// context — the agent reads knowledge only through its recall tools, when the
// analyst asks or on its own judgement. "Ask reckon to use this" stages that
// request in the composer.
import { useEffect, useState } from "react";
import { useApp } from "../app";
import type { LibraryItem } from "../../../src/protocol";

function Items({ title, items, empty }: { title: string; items: LibraryItem[]; empty: string }) {
  const { openInspector, stage, setCompanionMode } = useApp();
  const ask = (k: LibraryItem) => {
    setCompanionMode("conversation");
    stage(k.kind === "sop"
      ? `Use the procedure “${k.title}” (SOP ${k.ref}) for this investigation: recall it and apply what fits.`
      : `Compare this investigation with the past case “${k.title}” (${k.ref}): recall it and tell me what carries over.`);
  };
  return (
    <div className="libgroup">
      <div className="libhead">{title}<span className="count">{items.length}</span></div>
      {items.length ? items.map((k) => (
        <div key={k.kind + k.ref} className={"libitem " + k.kind}>
          <a className="libtitle" onClick={() => openInspector(k.kind === "sop" ? { kind: "sop", sopId: k.ref } : { kind: "case", investigationRef: k.ref })}>{k.title}</a>
          {k.relevance ? <span className={"librel " + k.relevance.replace(/ /g, "-")}>{k.relevance}</span> : null}
          {k.detail ? <div className="imeta">{k.detail}</div> : null}
          <a className="libask" onClick={() => ask(k)}>Ask reckon to use this</a>
        </div>
      )) : <div className="empty">{empty}</div>}
    </div>
  );
}

export function Library() {
  const { state, send, dispatch } = useApp();
  const lib = state.library;
  const [open, setOpen] = useState(true);
  const [q, setQ] = useState("");
  useEffect(() => { if (open && !lib.loaded) send({ type: "library.get" }); }, [open, lib.loaded, send]);
  const search = () => { const text = q.trim(); if (text) send({ type: "library.search", text }); };
  const clear = () => { setQ(""); dispatch({ type: "library.clear" }); };
  const r = lib.results;
  return (
    <section className="ksection" id="knowledgeSection">
      <div className="khead" onClick={() => setOpen((o) => !o)} role="button" aria-expanded={open}>
        <span className="kicker">Library</span>
        <span className="imeta">{lib.sops.length} procedure{lib.sops.length === 1 ? "" : "s"} · {lib.cases.length} past case{lib.cases.length === 1 ? "" : "s"}</span>
        <span className="pchev" aria-hidden="true">{open ? "⌄" : "›"}</span>
      </div>
      {open ? (
        <>
          <div className="ksearch">
            <input type="text" value={q} placeholder="Search procedures and past cases…" onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); search(); } if (e.key === "Escape") clear(); }} />
            <button disabled={!q.trim()} onClick={search}>Search</button>
            {r ? <button className="ghost" onClick={clear}>Clear</button> : null}
          </div>
          {r ? (
            <div className="libresults">
              <div className="imeta">Results for “{r.query}”</div>
              <Items title="Procedures" items={r.sops} empty="no procedure matches" />
              <Items title="Past cases" items={r.cases} empty="no past case matches" />
            </div>
          ) : (
            <>
              <Items title="Procedures" items={lib.sops} empty={lib.loaded ? "no procedures yet — import some" : "loading…"} />
              <Items title="Past cases" items={lib.cases} empty={lib.loaded ? "no concluded investigations yet" : "loading…"} />
            </>
          )}
          <div className="libfoot">
            <button onClick={() => send({ type: "library.import" })}>Import procedures…</button>
            <span className="imeta">The agent reads these only when it recalls them — ask it in the conversation.</span>
          </div>
        </>
      ) : null}
    </section>
  );
}
