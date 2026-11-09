// The knowledge library (adr/0129): the organization's procedures and past
// cases, to browse, search, read and import. Nothing here is in the agent's
// context — the agent reads knowledge only through its recall tools, when the
// analyst asks or on its own judgement. "Ask reckon to use this" stages that
// request in the composer.
import { useEffect, useState } from "react";
import { useApp } from "../app";
import { QueryBar } from "./QueryBar";
import type { LibraryItem } from "../../../src/protocol";

/**
 * One group of library rows. `ask` offers "Ask reckon to use this", which
 * stages a request in the investigation's composer: inside an investigation
 * only, since the Knowledge view acts on none (ui/09 §9.1).
 */
export function Items({ title, items, empty, ask: offerAsk = true }: { title: string; items: LibraryItem[]; empty: string; ask?: boolean }) {
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
          {k.detail || k.citedIn !== undefined ? (
            <div className="imeta">
              {[k.detail, k.citedIn !== undefined ? `cited in ${k.citedIn} investigation${k.citedIn === 1 ? "" : "s"}` : ""].filter(Boolean).join(" · ")}
            </div>
          ) : null}
          {offerAsk ? <a className="libask" onClick={() => ask(k)}>Ask reckon to use this</a> : null}
        </div>
      )) : <div className="empty">{empty}</div>}
    </div>
  );
}

export function Library() {
  const { state, send, dispatch } = useApp();
  const lib = state.library;
  const [open, setOpen] = useState(true);
  useEffect(() => { if (open && !lib.loaded) send({ type: "library.get" }); }, [open, lib.loaded, send]);
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
          <QueryBar showing={!!r} onSearch={(text) => { dispatch({ type: "library.searching", query: text }); send({ type: "library.search", text }); }} onClear={() => dispatch({ type: "library.clear" })} />
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
            <button className="ghost" onClick={() => send({ type: "knowledge.open" })}>Open the Knowledge view</button>
            <span className="imeta">The agent reads these only when it recalls them — ask it in the conversation.</span>
          </div>
        </>
      ) : null}
    </section>
  );
}
