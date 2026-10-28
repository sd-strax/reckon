// The query bar (09 §9.2): one natural-language box with two modes. Search
// runs semantic recall over both corpora and lists results with their
// relevance signals; Ask answers from the knowledge base only (adr/0111).
// Both record nothing in any investigation. Filters bind to tags (impl §8.0):
// a technique is a tag in both corpora.
import { useState } from "react";
import { useApp } from "../../app";
import { useKnowledgeView } from "./KnowledgeApp";

export function QueryBar() {
  const { send } = useApp();
  const view = useKnowledgeView();
  const [text, setText] = useState("");
  const [tags, setTags] = useState("");
  const [includeRetired, setIncludeRetired] = useState(false);
  const submit = () => {
    const q = text.trim();
    if (!q) return;
    const tagList = tags.split(/[,\s]+/).map((t) => t.trim()).filter(Boolean);
    send({ type: "knowledge.query", mode: view.mode, text: q, filters: { tags: tagList.length ? tagList : undefined, includeRetired: includeRetired || undefined } });
    view.setLastQuery(q);
    view.setShowingResults(view.mode);
  };
  return (
    <div id="querybar" role="search">
      <div className="modetoggle" role="tablist" aria-label="Query mode">
        <button role="tab" aria-selected={view.mode === "search"} className={view.mode === "search" ? "on" : ""} onClick={() => view.setMode("search")}>Search</button>
        <button role="tab" aria-selected={view.mode === "ask"} className={view.mode === "ask" ? "on" : ""} title="Answer from the knowledge base only, citing what it used; nothing is recorded" onClick={() => view.setMode("ask")}>Ask</button>
      </div>
      <input id="queryText" type="text" value={text} placeholder={view.mode === "search" ? "Search procedures and past cases…" : "Ask the knowledge base…"} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); submit(); } }} />
      <input id="queryTags" type="text" value={tags} placeholder="tags · techniques" title="Filter by tag or ATT&CK technique (both corpora carry techniques as tags)" onChange={(e) => setTags(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); submit(); } }} />
      {view.mode === "search" ? <label className="imeta qretired"><input type="checkbox" checked={includeRetired} onChange={(e) => setIncludeRetired(e.target.checked)} /> retired too</label> : null}
      <button className="primary" disabled={!text.trim()} onClick={submit}>{view.mode === "search" ? "Search" : "Ask"}</button>
    </div>
  );
}
