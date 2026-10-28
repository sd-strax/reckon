// Search results and the Ask answer (09 §9.2). Search lists both corpora
// with their relevance signals, nothing summarized: "No procedure matches" is
// information, a keyword-fallback answer is named as degraded (the backend
// word verbatim). Ask is the knowledge-only agent (adr/0111): its frame
// renders the streamed answer with [n] citations
// opening the inspector and the footer from the sidecar's result.
import { useApp } from "../../app";
import { Badge, Empty } from "../shared";
import { BackToSection, useKnowledgeView } from "./KnowledgeApp";

function degraded(backend?: string): boolean {
  return !!backend && !backend.startsWith("vector");
}

export function SearchResults() {
  const { state, openInspector } = useApp();
  const view = useKnowledgeView();
  const r = state.kdoc?.results;
  const stale = !r || r.query !== view.lastQuery;
  const backend = r?.procedures[0]?.backend || r?.cases[0]?.backend;
  return (
    <div className="kresults">
      <div className="kdhead">
        <h3 className="kdtitle">Results for “{view.lastQuery}”{r && !stale ? <span className="imeta"> · {r.procedures.length} procedure{r.procedures.length === 1 ? "" : "s"} · {r.cases.length} past case{r.cases.length === 1 ? "" : "s"}{backend ? " · " + backend : ""}</span> : null}</h3>
        <BackToSection />
      </div>
      {stale ? <Empty>searching…</Empty> : (
        <>
          {degraded(backend) ? <div className="banner warn">Keyword search answered this query ({backend}); semantic recall needs an embeddings backend.</div> : null}
          <section className="lsection">
            <h3>Procedures <span className="imeta">{r.coverage.procedures.toLowerCase()}</span></h3>
            {!r.procedures.length ? <Empty>No procedure matches.</Empty> : r.procedures.map((p) => (
              <div key={p.sopId} className="kres">
                <div className="kresline"><a onClick={() => openInspector({ kind: "sop", sopId: p.sopId })}>{p.title}</a>{p.status === "retired" ? <Badge tone="muted">retired</Badge> : null}{p.recommendation ? <Badge tone="info">{p.recommendation}</Badge> : null}<span className="imeta mono">{p.score.toFixed(2)}</span></div>
                {p.excerpt ? <blockquote className="iquote">{p.excerpt}</blockquote> : null}
                <div className="imeta">{p.rationale}{p.tags.length ? " · " + p.tags.join(", ") : ""}</div>
              </div>
            ))}
          </section>
          <section className="lsection">
            <h3>Past cases <span className="imeta">{r.coverage.cases.toLowerCase()}</span></h3>
            {!r.cases.length ? <Empty>No past case matches.</Empty> : r.cases.map((c) => (
              <div key={c.investigationRef} className="kres">
                <div className="kresline"><a onClick={() => openInspector({ kind: "case", investigationRef: c.investigationRef })}>{c.title}</a><Badge tone={c.band === "NEAR_DUPLICATE" ? "ok" : c.band === "DISTINCT" ? "muted" : "info"}>{c.band}</Badge><span className="imeta mono">{c.score.toFixed(2)}</span></div>
                {c.excerpt ? <blockquote className="iquote">{c.excerpt}</blockquote> : null}
                <div className="imeta">{c.rationale}{c.tags.length ? " · " + c.tags.join(", ") : ""}</div>
              </div>
            ))}
          </section>
        </>
      )}
    </div>
  );
}

export function AnswerView() {
  const { state, openInspector } = useApp();
  const view = useKnowledgeView();
  const a = state.kdoc?.answer;
  return (
    <div className="kanswer">
      <div className="kdhead">
        <h3 className="kdtitle">“{view.lastQuery}”</h3>
        <BackToSection />
      </div>
      {a && a.question === view.lastQuery ? (
        <>
          <div className="md">{a.text || <Empty>answering…</Empty>}</div>
          {a.citations.length ? <div className="kcites">{a.citations.map((c) => <a key={c.n} onClick={() => openInspector(c.target)}>[{c.n}] {c.title}</a>)}</div> : null}
          {a.model || a.toolsUsed.length ? <div className="imeta kfoot">reckon{a.model ? " · " + a.model : ""} · {a.toolsUsed.length ? "answered from the knowledge base only (" + a.toolsUsed.join(", ") + ")" : "answered without consulting the knowledge base"} · nothing recorded in any investigation</div> : null}
        </>
      ) : (
        <Empty>asking the knowledge base…</Empty>
      )}
    </div>
  );
}
