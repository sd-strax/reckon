// The Knowledge tab (adr/0107, adr/0346, ui/09 §9.1): the organization's
// procedures and past cases outside any investigation, one page at a time.
// The page is either the item, a procedure or a past case filling the tab
// with the same pane an investigation shows (outline and sections, where it
// was cited, revisions), without its "In this case" parts, links inside it
// moving within the tab with a way back; or the search page: the query bar,
// a search's results as two lists with relevance in words, and with no
// search showing, the past cases under their filters (technique,
// disposition, conclusion day, ui/09 §9.4). A result opens as the page.
//
// It mounts the investigation document's store with no investigation
// loaded, so those panes render unchanged; it shows no investigation state
// and nothing in it acts on one. A past case names the investigation it came
// from as a link.
//
// The sidebar's Knowledge view (adr/0344) is its only navigation: it opens
// the tab on one item, or on the search page with the query bar focused:
// cues the host posts, which this page acts on and the store never sees.
import { useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { AppProvider, useApp } from "../app";
import { QueryBar } from "../components/QueryBar";
import { InspectorPage } from "../components/inspectors/Inspector";
import type { InboundMessage, KnowledgeCue, KnowledgeInboundMessage, KnowledgeRenderMessage, KnowledgeTarget, LibraryItem, RenderMessage } from "../../../src/protocol";
import { citedInWords } from "../../../src/knowledgeWords";
import type { PastCaseFilter, PastCaseRow } from "../../../src/viewTypes";
import type { WebviewTransport } from "../transport";

export type KnowledgeTransport = WebviewTransport<KnowledgeInboundMessage, KnowledgeRenderMessage>;

/**
 * The store's transport is the investigation document's pair; this surface
 * speaks a subset of it: the knowledge reads, opening an investigation a case
 * names, and copying a record's JSON. The store itself sends investigation
 * intents the panes here never need (the evidence label reads of chips in a
 * document's text); they are dropped rather than sent to a host that has no
 * investigation to answer them from.
 */
const KNOWLEDGE_INTENTS = new Set<InboundMessage["type"]>(["ready", "refresh", "library.search", "pastCases.get", "knowledge.showing", "inspector.open", "investigation.open", "copy"]);

function isCue(m: KnowledgeRenderMessage): m is KnowledgeCue {
  return m.type === "knowledge.show" || m.type === "knowledge.search";
}

function storeTransport(t: KnowledgeTransport): WebviewTransport {
  return {
    send: (m: InboundMessage) => { if (KNOWLEDGE_INTENTS.has(m.type)) t.send(m as KnowledgeInboundMessage); },
    onMessage: (h: (m: RenderMessage) => void) => t.onMessage((m) => { if (!isCue(m)) h(m); }),
    getState: <T,>() => t.getState<T>(),
    setState: (s: unknown) => t.setState(s),
  };
}

export function KnowledgeApp({ transport }: { transport: KnowledgeTransport }) {
  const store = useMemo(() => storeTransport(transport), [transport]);
  return (
    <AppProvider transport={store}>
      <KnowledgeView transport={transport} />
    </AppProvider>
  );
}

/** One group of search results: a title opens the item as the page. */
function Items({ title, items, empty, onOpen }: { title: string; items: LibraryItem[]; empty: string; onOpen: (t: KnowledgeTarget) => void }) {
  return (
    <div className="libgroup">
      <div className="libhead">{title}<span className="count">{items.length}</span></div>
      {items.length ? items.map((k) => (
        <div key={k.kind + k.ref} className={"libitem " + k.kind}>
          <a className="libtitle" onClick={() => onOpen(k.kind === "sop" ? { kind: "sop", sopId: k.ref } : { kind: "case", investigationRef: k.ref })}>{k.title}</a>
          {k.relevance ? <span className={"librel " + k.relevance.replace(/ /g, "-")}>{k.relevance}</span> : null}
          {k.detail || k.citedIn !== undefined ? (
            <div className="imeta">
              {[k.detail, k.citedIn !== undefined ? citedInWords(k.citedIn) : ""].filter(Boolean).join(" · ")}
            </div>
          ) : null}
        </div>
      )) : <div className="empty">{empty}</div>}
    </div>
  );
}

function KnowledgeView({ transport }: { transport: KnowledgeTransport }) {
  const { state, send, dispatch, inspector, openInspector, closeInspector } = useApp();
  // Bumped by the sidebar's Search: the query bar takes the focus.
  const [focusSearch, setFocusSearch] = useState(0);
  // Held here, not in the listing, so an item opened and put away leaves the
  // filters as they were.
  const [filter, setFilter] = useState<PastCaseFilter>(EMPTY_FILTER);
  // The item was opened from the search page, so the page leads back to it;
  // an item from the sidebar starts afresh, with no way back.
  const [fromSearch, setFromSearch] = useState(false);
  const openFromSearch = (t: KnowledgeTarget) => { setFromSearch(true); openInspector(t); };
  // The host reads only what the shown page uses: it hears which one it is.
  const top = inspector.active?.stack[inspector.active.stack.length - 1];
  useEffect(() => {
    if (!top) send({ type: "knowledge.showing" });
    else if (top.kind === "sop" || top.kind === "case") send({ type: "knowledge.showing", target: top });
  }, [top, send]);
  // Subscribed before the store says ready, so a cue the host holds for the
  // ready is heard. An item from the sidebar starts a page of its own, not a
  // link from the one before it; Search puts the item away.
  useEffect(() => transport.onMessage((m) => {
    if (m.type === "knowledge.show") {
      setFromSearch(false);
      closeInspector("active");
      openInspector(m.target);
    } else if (m.type === "knowledge.search") {
      closeInspector("active");
      setFocusSearch((n) => n + 1);
    }
  }), [transport, openInspector, closeInspector]);
  const r = state.library.results;
  return (
    <div className="kview">
      {state.error ? (
        <div className="kerror" role="alert">
          {state.error} <button onClick={() => { dispatch({ type: "error.clear" }); send({ type: "refresh" }); }}>Retry</button>
        </div>
      ) : null}
      {inspector.active ? (
        <>
          {fromSearch ? <button className="linklike" onClick={() => closeInspector("active")}>‹ Back to {r ? "results" : "past cases"}</button> : null}
          <InspectorPage />
        </>
      ) : (
        <main aria-label="Search knowledge">
          <QueryBar focus={focusSearch} showing={!!r} query={r?.query} onSearch={(text) => { dispatch({ type: "library.searching", query: text }); send({ type: "library.search", text }); }} onClear={() => dispatch({ type: "library.clear" })} />
          {r ? (
            <div className="kresults">
              <div className="kicker">Results for “{r.query}”</div>
              <Items title="Procedures" items={r.sops} empty="no procedure matches" onOpen={openFromSearch} />
              <Items title="Past cases" items={r.cases} empty="no past case matches" onOpen={openFromSearch} />
            </div>
          ) : <PastCases filter={filter} setFilter={setFilter} onOpen={(ref) => openFromSearch({ kind: "case", investigationRef: ref })} />}
        </main>
      )}
    </div>
  );
}

const EMPTY_FILTER: PastCaseFilter = { techniques: [] };

function sameFilter(a: PastCaseFilter, b: PastCaseFilter): boolean {
  return a.techniques.length === b.techniques.length && a.techniques.every((t, i) => t === b.techniques[i])
    && (a.disposition ?? "") === (b.disposition ?? "") && (a.from ?? "") === (b.from ?? "") && (a.to ?? "") === (b.to ?? "");
}

function filtered(f: PastCaseFilter): boolean {
  return f.techniques.length > 0 || !!f.disposition || !!f.from || !!f.to;
}

/** Past cases (ui/09 §9.4): the filters, "n of m match", and one row per concluded case. */
function PastCases({ filter, setFilter, onOpen }: { filter: PastCaseFilter; setFilter: Dispatch<SetStateAction<PastCaseFilter>>; onOpen: (ref: string) => void }) {
  const { state, send } = useApp();
  // Every filter change is the API's to answer: the listing is filtered
  // server-side, never sieved here. Coming back to the page with the listing
  // for this filter already held asks nothing (the host re-reads it when the
  // library changed); a failed one is asked again.
  const held = useRef(state.pastCases);
  useEffect(() => { held.current = state.pastCases; }, [state.pastCases]);
  useEffect(() => {
    const have = held.current;
    if (have && !have.error && sameFilter(have.filter, filter)) return;
    send({ type: "pastCases.get", filter });
  }, [filter, send]);
  const facets = state.caseFacets;
  // An answer for another filter (a newer ask is in flight) is not this list.
  const list = state.pastCases && sameFilter(state.pastCases.filter, filter) ? state.pastCases : null;
  const corpus = state.library.casesTotal;
  const toggle = (t: string) => setFilter((f) => ({ ...f, techniques: f.techniques.includes(t) ? f.techniques.filter((x) => x !== t) : [...f.techniques, t] }));
  return (
    <section className="kcases" aria-label="Past cases">
      <div className="kfilters">
        <div className="kfacet" role="group" aria-label="Technique">
          <span className="kicker">Technique</span>
          {facets?.techniques.length ? facets.techniques.map((t) => (
            <button key={t.value} className={"chip" + (filter.techniques.includes(t.value) ? " on" : "")} aria-pressed={filter.techniques.includes(t.value)} onClick={() => toggle(t.value)}>
              <span className="mono">{t.value}</span> <span className="count">{t.count}</span>
            </button>
          )) : <span className="empty">{facets ? "none recorded" : "…"}</span>}
        </div>
        <label className="kfacet">
          <span className="kicker">Disposition</span>
          <select value={filter.disposition ?? ""} onChange={(e) => setFilter((f) => ({ ...f, disposition: e.target.value || undefined }))}>
            <option value="">any</option>
            {(facets?.dispositions || []).map((d) => <option key={d.value} value={d.value}>{d.value.toLowerCase()} ({d.count})</option>)}
          </select>
        </label>
        <label className="kfacet">
          <span className="kicker">Concluded from</span>
          <input type="date" value={filter.from ?? ""} onChange={(e) => setFilter((f) => ({ ...f, from: e.target.value || undefined }))} />
        </label>
        <label className="kfacet">
          <span className="kicker">to</span>
          <input type="date" value={filter.to ?? ""} onChange={(e) => setFilter((f) => ({ ...f, to: e.target.value || undefined }))} />
        </label>
        {filtered(filter) ? <button className="ghost" onClick={() => setFilter(EMPTY_FILTER)}>Clear filters</button> : null}
      </div>
      {!list ? <div className="empty">loading…</div>
        : list.error ? <div className="empty">The past cases could not be read: {list.error}</div>
        : (
          <>
            <div className="imeta kmatch">
              {filtered(list.filter) && corpus !== undefined ? `${list.total} of ${corpus} match` : `${list.total} past case${list.total === 1 ? "" : "s"}`}
              {list.rows.length < list.total ? ` · showing the ${list.rows.length} most recent` : ""}
            </div>
            {list.rows.length ? (
              <table className="ktable">
                <thead>
                  <tr><th>Case</th><th>Concluded</th><th>Disposition</th><th>Techniques</th><th>Entities</th><th>Actions</th><th>Duration</th><th>Cited in</th></tr>
                </thead>
                <tbody>{list.rows.map((c) => <PastCaseLine key={c.ref} c={c} onOpen={onOpen} />)}</tbody>
              </table>
            ) : <div className="empty">{filtered(list.filter) ? "No past case matches these filters." : "No concluded investigations yet."}</div>}
          </>
        )}
    </section>
  );
}

function PastCaseLine({ c, onOpen }: { c: PastCaseRow; onOpen: (ref: string) => void }) {
  return (
    <tr>
      <td><button className="linklike libtitle" onClick={() => onOpen(c.ref)}>{c.title || c.ref}</button></td>
      <td className="mono">{c.concludedAt ? c.concludedAt.slice(0, 10) : "—"}</td>
      <td>{c.disposition ? <span className={"verdictpill " + c.disposition.toLowerCase()}>{c.disposition.toLowerCase()}</span> : "—"}</td>
      <td className="mono">{c.techniques.join(", ") || "—"}</td>
      <td className="mono">{c.entities.join(", ") || "—"}</td>
      {/* The outcome is the action's recorded word, verbatim. */}
      <td>{c.actions.length ? c.actions.map((a, i) => <div key={i}><span className="mono">{a.actionType}</span> · {a.outcome}</div>) : "—"}</td>
      <td>{c.durationHours !== undefined ? `${c.durationHours} h` : "—"}</td>
      <td>{c.citedIn} investigation{c.citedIn === 1 ? "" : "s"}</td>
    </tr>
  );
}
