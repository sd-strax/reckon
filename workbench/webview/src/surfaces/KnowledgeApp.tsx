// The Knowledge view (adr/0107, ui/09 §9.1): the organization's procedures
// and past cases outside any investigation. Sections, each with its count:
// Procedures, Past cases (filtered by technique, disposition and conclusion
// day, ui/09 §9.4) and Import. A query bar above searches both corpora; a
// search replaces the content with its results under a header naming the
// query, with a way back. An item opens in the inspector beside the list,
// the same procedure and past-case panes an investigation shows, without
// their "In this case" parts.
//
// It mounts the investigation document's store with no investigation
// loaded, so those panes render unchanged; it shows no investigation state
// and nothing in it acts on one. A past case names the investigation it came
// from as a link.
import { useEffect, useMemo, useState } from "react";
import { AppProvider, useApp } from "../app";
import { Items } from "../components/Library";
import { QueryBar } from "../components/QueryBar";
import { InspectorDock } from "../components/inspectors/Inspector";
import type { InboundMessage, KnowledgeInboundMessage, KnowledgeRenderMessage, RenderMessage } from "../../../src/protocol";
import type { PastCaseFilter, PastCaseRow } from "../../../src/viewTypes";
import type { WebviewTransport } from "../transport";

export type KnowledgeTransport = WebviewTransport<KnowledgeInboundMessage, KnowledgeRenderMessage>;

type Section = "procedures" | "cases" | "import";

/**
 * The store's transport is the investigation document's pair; this surface
 * speaks a subset of it: the knowledge reads, opening an investigation a case
 * names, and copying a record's JSON. The store itself sends investigation
 * intents the panes here never need (the evidence label reads of chips in a
 * document's text); they are dropped rather than sent to a host that has no
 * investigation to answer them from.
 */
const KNOWLEDGE_INTENTS = new Set<InboundMessage["type"]>(["ready", "refresh", "library.get", "library.search", "library.import", "pastCases.get", "inspector.open", "investigation.open", "copy"]);

function storeTransport(t: KnowledgeTransport): WebviewTransport {
  return {
    send: (m: InboundMessage) => { if (KNOWLEDGE_INTENTS.has(m.type)) t.send(m as KnowledgeInboundMessage); },
    onMessage: (h: (m: RenderMessage) => void) => t.onMessage(h),
    getState: <T,>() => t.getState<T>(),
    setState: (s: unknown) => t.setState(s),
  };
}

export function KnowledgeApp({ transport }: { transport: KnowledgeTransport }) {
  const store = useMemo(() => storeTransport(transport), [transport]);
  return (
    <AppProvider transport={store}>
      <KnowledgeView />
    </AppProvider>
  );
}

const SECTION_LABELS: Record<Section, string> = { procedures: "Procedures", cases: "Past cases", import: "Import" };

function KnowledgeView() {
  const { state, send, dispatch, openInspector } = useApp();
  const [section, setSection] = useState<Section>("procedures");
  const lib = state.library;
  const r = lib.results;
  const counts: Record<Section, number | undefined> = { procedures: lib.loaded ? lib.sops.length : undefined, cases: lib.loaded ? lib.casesTotal : undefined, import: undefined };
  return (
    <div className="kview">
      <nav className="knav" aria-label="Knowledge sections">
        <div className="kicker">Knowledge</div>
        {(Object.keys(SECTION_LABELS) as Section[]).map((s) => (
          <button key={s} className={"knavrow" + (section === s && !r ? " on" : "")} aria-current={section === s && !r ? "page" : undefined}
            onClick={() => { setSection(s); dispatch({ type: "library.clear" }); }}>
            {SECTION_LABELS[s]}{counts[s] !== undefined ? <span className="count">{counts[s]}</span> : null}
          </button>
        ))}
      </nav>
      <main className="kmain">
        {state.error ? (
          <div className="kerror" role="alert">
            {state.error} <button onClick={() => { dispatch({ type: "error.clear" }); send({ type: "refresh" }); }}>Retry</button>
          </div>
        ) : null}
        <QueryBar showing={!!r} onSearch={(text) => send({ type: "library.search", text })} onClear={() => dispatch({ type: "library.clear" })} />
        {r ? (
          <div className="kresults">
            <div className="kresultshead">
              <span>Results for “{r.query}”</span>
              <button className="linklike" onClick={() => dispatch({ type: "library.clear" })}>‹ back to {SECTION_LABELS[section]}</button>
            </div>
            <Items title="Procedures" items={r.sops} empty="no procedure matches" ask={false} />
            <Items title="Past cases" items={r.cases} empty="no past case matches" ask={false} />
          </div>
        ) : section === "procedures" ? (
          <Items title="Procedures" items={lib.sops} empty={lib.loaded ? "no procedures yet — import some" : "loading…"} ask={false} />
        ) : section === "cases" ? (
          <PastCases onOpen={(ref) => openInspector({ kind: "case", investigationRef: ref })} />
        ) : (
          <div className="kimport">
            <p>Procedures are imported as markdown with YAML frontmatter. A document whose source URL is already in the library is a new revision of it, not a second copy.</p>
            <button className="primary" onClick={() => send({ type: "library.import" })}>Import procedures…</button>
          </div>
        )}
      </main>
      <aside className="kdetail" aria-label="Detail">
        <InspectorDock />
      </aside>
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
function PastCases({ onOpen }: { onOpen: (ref: string) => void }) {
  const { state, send } = useApp();
  const [filter, setFilter] = useState<PastCaseFilter>(EMPTY_FILTER);
  // Every filter change is the API's to answer: the listing is filtered
  // server-side, never sieved here.
  useEffect(() => { send({ type: "pastCases.get", filter }); }, [filter, send]);
  const facets = state.caseFacets;
  // An answer for another filter (the section was left and come back to, or
  // a newer ask is in flight) is not this list.
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
