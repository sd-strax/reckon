// Turn (Answer · Tool calls · Consulted · Transcript), Similar past case
// (Summary · Why it matched), Procedure (What the AI read · Whole document ·
// Versions) and Person — the provenance inspectors (08 §8.6, §8.6.1).
import { useMemo, useState } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { parseTranscript } from "../../transcript";
import { Badge, Markdown, RefChip, fmtDateTime, fmtTime } from "../shared";
import { ToolRow } from "../ToolRow";
import { Json, KV, Loading, Section, Tabs } from "./primitives";
import { received, type Received } from "../../derive/knowledgeUse";
import { diffLines } from "../../derive/diff";
import { delivered, deliveryKey, type Delivery } from "../../derive/delivered";
import { relevanceOfBand } from "../../../../src/relevance";
import { recordedSections } from "../../../../src/procedureReads";
import type { KnowledgeCitations, ProcedureReads, ProcedureSectionRead, ProcedureTurnRead, ThreadActor } from "../../../../src/viewTypes";

/**
 * Where a procedure or a past case was consulted in other investigations
 * (adr/0109): one row each, in adr/0236's two words — "cited" when an act
 * there named it by ref, "recalled, not cited" otherwise. The current
 * investigation is left out; "In this case" above already says what happened
 * here. A row opens that investigation.
 */
function CitedElsewhere({ citations, here, arrived }: { citations: KnowledgeCitations | undefined; here: string | undefined; arrived: boolean }) {
  const { send } = useApp();
  // Three states: the inspector's answer is still in flight; it came without
  // citations (that read failed); it came with them.
  if (!arrived) return <Loading what="where it was consulted" />;
  if (!citations) return <div className="empty">{here === undefined ? "Where this was consulted could not be read." : "Where this was consulted elsewhere could not be read."}</div>;
  const rows = citations.investigations.filter((c) => !sameInvestigation(c.investigationId, here));
  if (!rows.length) return <div className="empty">{here === undefined ? "Not consulted in any investigation." : "Not consulted in any other investigation."}</div>;
  return (
    <>
      {rows.map((c) => (
        <div key={c.investigationId} className="irow">
          {c.cited ? "cited" : "recalled, not cited"}: <a onClick={() => send({ type: "investigation.open", id: c.investigationId, title: c.title })}>{c.title || c.investigationId}</a>
          <span className="imeta">{" · "}{c.status.toLowerCase()} · {fmtDateTime(c.lastAt)}{c.acts > 1 ? ` · ${c.acts} turns` : ""}{c.revisions.length ? ` · revision ${c.revisions.join(", ")}` : ""}</span>
        </div>
      ))}
    </>
  );
}

/** A citation row names the aggregate id; a snapshot or a case ref may carry the `grouping--` prefix. */
function sameInvestigation(a: string, b: string | undefined): boolean {
  return b !== undefined && a.replace(/^grouping--/, "") === b.replace(/^grouping--/, "");
}

/** The tab label: the count of other investigations the ref was consulted in. */
function citedLabel(citations: KnowledgeCitations | undefined, here: string | undefined): string {
  // Outside an investigation (the Knowledge view) there is no "elsewhere".
  if (here === undefined) return citations ? `Where cited · ${citations.citedIn} of ${citations.citedIn + citations.recalledIn}` : "Where cited";
  if (!citations) return "Cited elsewhere";
  const others = citations.investigations.filter((c) => !sameInvestigation(c.investigationId, here));
  const cited = others.filter((c) => c.cited).length;
  return `Cited elsewhere · ${cited} of ${others.length}`;
}

export function TurnInspector({ interpretationId }: { interpretationId: string }) {
  const { state, openInspector } = useApp();
  const e = state.snap?.thread?.find((x) => x.interpretationId === interpretationId);
  const body = state.transcripts[interpretationId];
  const parsed = useMemo(() => (body ? parseTranscript(body) : null), [body]);
  const hs = state.snap?.hypotheses || [];
  if (!e) return <div className="empty">This act is not in the current snapshot.</div>;
  const ask = parsed?.items.find((i) => i.kind === "user");
  const answers = parsed?.items.filter((i) => i.kind === "assistant") || [];
  const tools = parsed?.items.filter((i) => i.kind === "tool") || [];
  const produced = (e.outputRefs || []).map((r) => {
    const h = hs.find((x) => x.id === r);
    if (h) return { ref: r, text: `hypothesis ${hypName(h, hs)}` };
    for (const hh of hs) { const p = hh.predictions.find((x) => x.id === r); if (p) return { ref: r, text: `prediction P${p.ordinal ?? "?"} under ${hypName(hh, hs)}` }; }
    return { ref: r, text: "" };
  });
  const who = e.actor.principalName || e.actor.principal;
  return (
    <>
      <div className="isub">{ask && ask.kind === "user" ? <>“{ask.text.length > 160 ? ask.text.slice(0, 159) + "…" : ask.text}”<br /></> : null}
        {e.actor.kind === "AI_DELEGATED" ? <>asked by <a onClick={() => openInspector({ kind: "person", principalId: e.actor.principal })}>{who}</a> · answered by reckon{e.actor.model ? " · " + e.actor.model : ""}</> : <>by <a onClick={() => openInspector({ kind: "person", principalId: e.actor.principal })}>{who}</a></>}
        {" · "}{fmtDateTime(e.occurredAt)} · <span className="mono">{e.interpretationType}</span>{e.superseded ? <> · <Badge tone="muted">superseded</Badge></> : null}
      </div>
      <Tabs tabs={[
        { id: "answer", label: "Answer", body: () => (
          <>
            {parsed ? (answers.length ? answers.map((a, i) => a.kind === "assistant" ? <Markdown key={i} text={a.text} /> : null) : <div className="empty">no prose in this turn</div>) : e.hasTranscript ? <Loading what="transcript" /> : <p>{e.summary}</p>}
            <Section title="Records written">
              {produced.length ? produced.map((p) => <div key={p.ref} className="irow">{p.text ? <>{p.text} · </> : null}<RefChip refId={p.ref} /></div>) : <div className="empty">none</div>}
              {(e.inputRefs || []).length ? <div className="imeta">cites {(e.inputRefs || []).slice(0, 8).map((r) => <RefChip key={r} refId={r} />)}</div> : null}
              <div className="imeta">actor {who}{e.actor.kind === "AI_DELEGATED" ? ", delegate reckon" : ""}{e.confidence ? " · confidence " + e.confidence : ""}</div>
            </Section>
          </>
        ) },
        { id: "tools", label: `Tool calls · ${e.toolCalls || tools.length}`, body: () => (
          parsed ? (tools.length ? tools.map((t, i) => t.kind === "tool" ? <ToolRow key={i} name={t.name} argsHint={t.argsRaw} argsPretty={t.argsPretty} result={t.result} /> : null) : <div className="empty">no tool calls</div>) : e.hasTranscript ? <Loading what="tool calls" /> : <div className="empty">{e.toolCalls} tool call(s); no transcript committed</div>
        ) },
        { id: "consulted", label: "Consulted", body: () => (
          <>
            {(e.consultedSops || []).map((s) => <div key={s.sopId} className="irow">{s.cited ? "cited" : "recalled, not cited"}: <a onClick={() => openInspector({ kind: "sop", sopId: s.sopId })}>{s.title || s.sopId}</a></div>)}
            {(e.consultedSimilar || []).map((s) => <div key={s.investigationRef} className="irow">{s.cited ? "cited case" : "recalled case, not cited"}: <a onClick={() => openInspector({ kind: "case", investigationRef: s.investigationRef })}>{s.title || s.investigationRef}</a>{s.band ? <> <Badge>{s.band}</Badge></> : null}</div>)}
            {!(e.consultedSops || []).length && !(e.consultedSimilar || []).length ? <div className="empty">no knowledge consulted in this turn</div> : null}
          </>
        ) },
        { id: "transcript", label: "Transcript", body: () => (
          body !== undefined ? <pre className="itranscript">{body}</pre> : e.hasTranscript ? <Loading what="transcript" /> : <div className="empty">no transcript committed for this act</div>
        ) },
      ]} />
    </>
  );
}

/** A pinned finding (#144): what was pinned, by whom, when, over which
 * evidence, and whether it still stands. Never an agent turn. */
export function PinInspector({ interpretationId }: { interpretationId: string }) {
  const { state, openInspector } = useApp();
  const e = state.snap?.thread?.find((x) => x.interpretationId === interpretationId);
  const pin = (state.snap?.pins || []).find((p) => p.interpretationId === interpretationId);
  if (!e && !pin) return <div className="empty">This pin is not in the current snapshot.</div>;
  const refs = pin?.inputRefs || e?.inputRefs || [];
  const who = e ? e.actor.principalName || e.actor.principal : pin?.actor === "AI_DELEGATED" ? "reckon" : "an analyst";
  return (
    <>
      <div className="isub">
        pinned by {e ? <a onClick={() => openInspector({ kind: "person", principalId: e.actor.principal })}>{who}</a> : who}
        {e?.actor.kind === "AI_DELEGATED" ? " (reckon, on their behalf)" : ""} · {fmtDateTime(pin?.pinnedAt || e?.occurredAt)}
        {(pin?.superseded || e?.superseded) ? <> · <Badge tone="muted">removed from evidence</Badge></> : null}
      </div>
      <Section title="Finding"><p>{pin?.finding || e?.summary}</p></Section>
      <Section title="Over this evidence">
        {refs.length ? refs.map((r) => <div key={r} className="irow"><RefChip refId={r} /></div>) : <div className="empty">no evidence cited</div>}
      </Section>
    </>
  );
}

/** A human act on the record (#144) — a verdict, a rejection, a lifecycle
 * step: who did it, when, what it says and cites. No transcript to fetch. */
export function ActInspector({ interpretationId }: { interpretationId: string }) {
  const { state, openInspector } = useApp();
  const e = state.snap?.thread?.find((x) => x.interpretationId === interpretationId);
  if (!e) return <div className="empty">This act is not in the current snapshot.</div>;
  const who = e.actor.principalName || e.actor.principal;
  return (
    <>
      <div className="isub">
        by <a onClick={() => openInspector({ kind: "person", principalId: e.actor.principal })}>{who}</a>
        {e.actor.kind === "AI_DELEGATED" ? " (reckon, on their behalf)" : e.actor.kind === "SYSTEM" ? " (the engine)" : ""} · {fmtDateTime(e.occurredAt)}
        {" · "}<span className="mono">{e.interpretationType}</span>
        {e.superseded ? <> · <Badge tone="muted">superseded</Badge></> : null}
      </div>
      <Section title="On the record"><p>{e.summary || <span className="empty">no text recorded</span>}</p></Section>
      {(e.inputRefs || []).length || (e.outputRefs || []).length ? (
        <Section title="Cites">
          {[...(e.inputRefs || []), ...(e.outputRefs || [])].map((r) => <div key={r} className="irow"><RefChip refId={r} /></div>)}
        </Section>
      ) : null}
    </>
  );
}

export function CaseInspector({ investigationRef }: { investigationRef: string }) {
  const { state, send, openInspector } = useApp();
  const summary = state.caseSummaries[investigationRef];
  const citations = state.citations[investigationRef];
  const here = state.snap?.investigation.id;
  // How this case was used here (#132): only what the agent's recalls carried.
  const got = received(state.snap, "case", investigationRef);
  const match = got.find((g) => g.band || g.rationale);
  const consulted = (state.snap?.thread || []).flatMap((e) => (e.consultedSimilar || []).filter((s) => s.investigationRef === investigationRef).map((s) => ({ e, s })));
  // Outside an investigation (the Knowledge view) there is no "In this case"
  // and nothing the agent read: knowledge only (ui/09 §9.1).
  const inInvestigation = !!state.snap;
  return (
    <>
      <div className="isub">
        {match?.band ? <Badge tone={match.band === "NEAR_DUPLICATE" ? "ok" : match.band === "DISTINCT" ? "muted" : "info"}>{relevanceOfBand(match.band)}</Badge> : null}
        {summary ? <> Concluded {summary.conclusionAt ? fmtDateTime(summary.conclusionAt) : "—"}{summary.verdictDisposition ? " · " + summary.verdictDisposition.toLowerCase() : ""}{summary.durationHours ? ` · ${summary.durationHours} hours` : ""}</> : null}
      </div>
      {inInvestigation ? (
        <div className="ithiscase"><span className="kicker">In this case:</span>{" "}
          {consulted.map(({ e, s }) => <span key={e.sequenceNo} className="icite">{s.cited ? "the agent cited it" : "the agent recalled it, not cited"} in <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || "a turn"}</a></span>)}
          {!consulted.length ? <span className="empty">the agent has not recalled it in this investigation</span> : null}
        </div>
      ) : null}
      <Tabs tabs={[
        { id: "summary", label: "Summary", body: () => (
          summary === undefined ? <Loading what="case summary" />
            : summary === null ? <div className="empty">No summary is recorded for this case in the knowledge corpus.</div>
            : (
              <>
                <Section title="What happened there"><Markdown text={summary.body} />{summary.generatorModel ? <div className="imeta">narrative by {summary.generatorModel}</div> : <div className="imeta">deterministic baseline (no narrative written yet)</div>}</Section>
                <Section title="Structured">
                  <KV rows={[
                    { k: "Techniques", v: summary.techniques.join(", ") || undefined, mono: true },
                    { k: "Entities", v: summary.primaryEntities.join(", ") || undefined, mono: true },
                    { k: "Seed", v: summary.seedKind ? `${summary.seedKind}${summary.seedValue ? " · " + summary.seedValue : ""}` : undefined, mono: true },
                    { k: "Actions", v: summary.actionsTaken.length ? summary.actionsTaken.map((a) => `${a.actionType} ${a.outcome.toLowerCase()}`).join(" · ") : undefined, mono: true },
                    { k: "Outcome", v: summary.conclusionOutcome },
                    { k: "Verdict", v: summary.verdictDisposition ? `${summary.verdictDisposition}${summary.verdictRationale ? " — " + summary.verdictRationale : ""}` : undefined },
                  ]} />
                </Section>
                <Section title="Procedures learned from this case">
                  {summary.learnedProcedures.length
                    ? summary.learnedProcedures.map((p) => <div key={p.id} className="irow"><a onClick={() => openInspector({ kind: "sop", sopId: p.id })}>{p.title || p.id}</a></div>)
                    : <div className="empty">none accepted into the library from this case</div>}
                </Section>
              </>
            )
        ) },
        { id: "cited", label: citedLabel(citations, here), body: () => <CitedElsewhere citations={citations} here={here} arrived={summary !== undefined} /> },
        ...(inInvestigation ? [{ id: "read", label: "What the agent read", body: () => (
          got.length ? got.map((g, i) => (
            <div key={i}>
              {g.excerpt ? <blockquote className="iquote">{g.excerpt}</blockquote> : null}
              <div className="imeta">{[g.band ? relevanceOfBand(g.band) : "", g.rationale || ""].filter(Boolean).join(" · ")} — in <a onClick={() => openInspector({ kind: "turn", interpretationId: g.interpretationId })}>{g.summary || "a turn"}</a></div>
            </div>
          )) : <div className="empty">The agent has not read this case in this investigation.</div>
        ) }] : []),
      ]} />
      <div className="decide">
        <button onClick={() => send({ type: "investigation.open", id: investigationRef.replace(/^grouping--/, ""), title: summary?.title })}>Open that investigation</button>
      </div>
      {summary ? <details className="iraw"><summary className="kicker">Raw</summary><Json value={summary} /></details> : null}
    </>
  );
}

/**
 * What the agent read of a procedure here, as it was (adr/0097). For each act
 * that recorded sections, the sections under the revision and cut it saw,
 * read back from the library, and beside each how much of it reached the
 * model, read from the committed tool results (derive/delivered.ts): a recall
 * excerpt is cut to a budget and a large section is read in parts, so the
 * stored section and the text delivered are two things. "Exactly as
 * retrieved" is said only when the revision is current and every section of
 * the turn was received whole; otherwise the pane says how much was
 * received. A revised procedure shows the current text under the same heading
 * and the changes between them. The recall results' excerpts follow as what
 * recall showed. `reads` is absent while nothing was recorded (acts before
 * sections existed) and when the read-back failed; the two read differently.
 */
function WhatTheAgentRead({ reads, recorded, arrived, got, deliveries, turnLabel, openTurn }: {
  reads: ProcedureReads | undefined;
  recorded: boolean;
  /** The host's answer for this procedure has arrived (the document is here); before that, the reads are in flight, not failed. */
  arrived: boolean;
  got: Received[];
  deliveries: Map<string, Delivery>;
  turnLabel: (interpretationId: string) => string;
  openTurn: (interpretationId: string) => void;
}) {
  if (!recorded && !got.length) return <div className="empty">The agent has not read this procedure in this investigation.</div>;
  if (recorded && !arrived) return <Loading what="what the agent read" />;
  const deliveryOf = (t: ProcedureTurnRead, s: ProcedureSectionRead) => deliveries.get(deliveryKey(t.interpretationId, t.revision, t.cut, s.idx));
  const header = (t: ProcedureTurnRead) => {
    if (t.revision !== reads?.currentRevision) return `revised since this case consulted it: revision ${t.revision} then, ${reads?.currentRevision} now`;
    return t.sections.every((s) => deliveryOf(t, s)?.whole)
      ? `revision ${t.revision}, exactly as retrieved`
      : `revision ${t.revision}, as it stood when the agent retrieved it`;
  };
  return (
    <>
      {recorded && !reads ? <div className="empty">What the agent read could not be read back from the library.</div> : null}
      {reads?.turns.map((t) => (
        <div key={t.interpretationId + "/" + t.revision + "/" + t.cut} className="iread">
          <div className="imeta">
            {header(t)}
            {" — in "}<a onClick={() => openTurn(t.interpretationId)}>{turnLabel(t.interpretationId)}</a>
          </div>
          {t.sections.map((s) => <SectionRead key={s.idx} s={s} turn={t} current={reads.currentRevision} delivery={deliveryOf(t, s)} />)}
        </div>
      ))}
      {got.length ? (
        <Section title="Recall showed">
          {got.map((g, i) => (
            <div key={i}>
              {g.excerpt ? <blockquote className="iquote">{g.excerpt}</blockquote> : <div className="empty">recalled without an excerpt</div>}
              <div className="imeta">{g.rationale ? g.rationale + " — " : ""}in <a onClick={() => openTurn(g.interpretationId)}>{g.summary || "a turn"}</a></div>
            </div>
          ))}
        </Section>
      ) : null}
    </>
  );
}

const runes = (text: string) => Array.from(text).length;

/** How much of the section reached the model, in a line. */
function deliveryLine(d: Delivery | undefined, section: string): string {
  if (!d) return "what the agent received of it is not in the loaded transcript";
  if (d.parts) return d.whole ? `received whole, read in ${d.parts.of} parts` : `read in parts: ${d.parts.got} of ${d.parts.of} received`;
  if (d.whole) return d.via === "recall" ? "received whole, in the recall result" : "received whole, read with read_sop";
  return `the recall result carried the first ${runes(d.text)} of ${runes(section)} runes`;
}

/**
 * One section: the revision's text, what of it was received when that is
 * less, and under a revised procedure the current text and the changes.
 */
function SectionRead({ s, turn, current, delivery }: { s: ProcedureSectionRead; turn: ProcedureTurnRead; current: number; delivery: Delivery | undefined }) {
  const partial = !!delivery && !delivery.whole;
  const [view, setView] = useState<"then" | "got" | "now" | "diff">(partial ? "got" : "then");
  const same = turn.revision === current;
  const views: [typeof view, string][] = [["then", `Revision ${turn.revision}`]];
  if (partial) views.push(["got", "Received"]);
  if (!same) views.push(["now", "Current"], ["diff", "Changes"]);
  const gone = <div className="empty">No section of revision {current} sits under this heading any more.</div>;
  return (
    <div className="isect">
      <div className="isecthead">
        <span className="kicker">{s.location}</span>
        {views.length > 1 ? (
          <span className="iviews">
            {views.map(([v, label]) => (
              <button key={v} className={"ghost" + (view === v ? " on" : "")} aria-pressed={view === v} onClick={() => setView(v)}>{label}</button>
            ))}
          </span>
        ) : null}
      </div>
      <div className="imeta">{deliveryLine(delivery, s.text)}</div>
      {view === "then" ? <pre className="isecttext">{s.text}</pre> : null}
      {view === "got" && delivery ? <pre className="isecttext">{delivery.text}</pre> : null}
      {view === "now" ? (s.current ? <pre className="isecttext">{s.current.text}</pre> : gone) : null}
      {view === "diff" ? (
        s.current
          ? (s.current.text === s.text
            ? <div className="empty">Unchanged in revision {current}.</div>
            : <div className="idiff">{diffLines(s.text, s.current.text).map((l, i) => <div key={i} className={"dl " + l.kind}>{l.text || " "}</div>)}</div>)
          : gone
      ) : null}
    </div>
  );
}

export function ProcedureInspector({ sopId }: { sopId: string }) {
  const { state, openInspector } = useApp();
  const sop = state.sops[sopId];
  const citations = state.citations[sopId];
  const reads = state.procedureReads[sopId];
  const here = state.snap?.investigation.id;
  const got = received(state.snap, "sop", sopId);
  const consulted = (state.snap?.thread || []).flatMap((e) => (e.consultedSops || []).filter((s) => s.sopId === sopId).map((s) => ({ e, s })));
  const recorded = consulted.some(({ s }) => recordedSections(s));
  const deliveries = useMemo(() => delivered(state.snap, sopId), [state.snap, sopId]);
  const turnLabel = (interpretationId: string) => (state.snap?.thread || []).find((e) => e.interpretationId === interpretationId)?.summary || "a turn";
  const ledTo = (state.snap?.actions || []).filter((a) => consulted.some(({ e }) => (e.outputRefs || []).includes(a.actionId)));
  const outline = state.outlines[sopId];
  // Outside an investigation (the Knowledge view, ui/09 §9.1) there is no
  // "In this case" and nothing the agent read: knowledge only.
  const inInvestigation = !!state.snap;
  return (
    <>
      <div className="isub">{sop ? <>{sop.source?.system ? sop.source.system + " · " : ""}{sop.source?.url ? <span className="mono">{sop.source.url}</span> : "markdown"}{sop.importedBy ? ` · imported by ${sop.importedBy}` : ""}{sop.importedAt ? ` · ${fmtDateTime(sop.importedAt)}` : ""}</> : null}</div>
      {inInvestigation ? (
        <div className="ithiscase"><span className="kicker">In this case:</span>{" "}
          {consulted.map(({ e, s }) => <span key={e.sequenceNo} className="icite">{s.cited ? "the agent cited it" : "the agent recalled it, not cited"} in <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || "a turn"}</a></span>)}
          {ledTo.map((a) => <span key={a.actionId} className="icite">led to <a onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{a.actionType}</a></span>)}
          {!consulted.length ? <span className="empty">the agent has not recalled it in this investigation</span> : null}
        </div>
      ) : null}
      <Tabs tabs={[
        ...(inInvestigation ? [{ id: "read", label: "What the agent read", body: () => (
          <WhatTheAgentRead reads={reads} recorded={recorded} arrived={sop !== undefined} got={got} deliveries={deliveries} turnLabel={turnLabel} openTurn={(interpretationId) => openInspector({ kind: "turn", interpretationId })} />
        ) }] : []),
        { id: "whole", label: "Whole document", body: () => (
          sop === undefined ? <Loading what="procedure" /> : (
            <>
              <KV rows={[
                { k: "Status", v: sop.status, mono: true },
                { k: "Recommends", v: sop.recommendation, mono: true },
                { k: "Tags", v: sop.tags.join(", ") || undefined, mono: true },
                { k: "Author", v: sop.authorId },
                { k: "Version", v: sop.source?.version, mono: true },
                { k: "Revision", v: outline ? String(outline.revision) : undefined, mono: true },
              ]} />
              {outline ? (
                <Section title={`Outline · ${outline.sections.length} section${outline.sections.length === 1 ? "" : "s"}`}>
                  <ol className="ioutline">{outline.sections.map((o) => <li key={o.idx}>{o.location}</li>)}</ol>
                </Section>
              ) : null}
              <Markdown text={sop.body} className="idoc" />
            </>
          )
        ) },
        { id: "cited", label: citedLabel(citations, here), body: () => <CitedElsewhere citations={citations} here={here} arrived={sop !== undefined} /> },
      ]} />
    </>
  );
}

export function PersonInspector({ principalId }: { principalId: string }) {
  const { state, openInspector } = useApp();
  const acts = (state.snap?.thread || []).filter((e) => e.actor.principal === principalId);
  // The engine's acts in the principal's name (a dispatch or a result on
  // their approval, an expiry on their request, adr/0217) are attributed to
  // them but are not theirs: they carry no authorization, and the label claims
  // no approval, since an expiry records the absence of one.
  const own = acts.filter((e) => e.actor.kind !== "SYSTEM");
  const name = acts.find((e) => e.actor.principalName)?.actor.principalName || principalId;
  const approvals = acts.filter((e) => e.interpretationType === "action-approval").length;
  const first = acts[0];
  const opened = state.snap?.investigation.seed && first && first.sequenceNo <= 2;
  // Two facts with two owners (adr/0105): the gate reckon applied to each act,
  // and what the identity provider asserted in the token at the time. Unions
  // across the principal's acts here; an act recorded before either existed
  // says so rather than reading as "no roles".
  const union = (pick: (a: ThreadActor) => string[] | undefined) => Array.from(new Set(own.flatMap((e) => pick(e.actor) || []))).sort();
  const required = union((a) => a.requiredRoles);
  const asserted = union((a) => a.roles);
  const recorded = own.filter((e) => e.actor.requiredRoles?.length || e.actor.roles?.length).length;
  // One label per act, consistent with what the summary counts as recorded:
  // the gate when there is one; "gate not recorded" when only the token's
  // roles are; "authorization not on this act" when neither is, which is an
  // act before this record, or an approval a policy made in the person's name.
  const gate = (e: { actor: ThreadActor }) =>
    e.actor.kind === "SYSTEM" ? " · the engine, in their name"
    : e.actor.requiredRoles?.length ? ` · as ${e.actor.requiredRoles.join(" or ")}`
    : e.actor.roles?.length ? " · gate not recorded" : " · authorization not on this act";
  return (
    <>
      <div className="isub">What authorized each act, as the record kept it at the time; reckon mirrors no roles and the identity provider may say otherwise today.</div>
      <div className="ithiscase"><span className="kicker">In this case:</span> <span className="icite">{opened ? "opened it · " : ""}{acts.length} act{acts.length === 1 ? "" : "s"}{approvals ? ` · ${approvals} approval${approvals === 1 ? "" : "s"}` : ""}</span></div>
      <Section title="What they did here">
        {acts.length ? acts.map((e) => (
          <div key={e.sequenceNo} className="irow"><span className="imeta">{fmtTime(e.occurredAt)}</span> {e.actor.kind === "AI_DELEGATED" ? "reckon on their behalf: " : ""}<a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || e.interpretationType}</a><span className="imeta">{gate(e)}</span></div>
        )) : <div className="empty">no acts in this investigation</div>}
      </Section>
      <Section title="What authorized their acts">
        {recorded ? (
          <KV rows={[
            { k: "Reckon required", v: required.length ? required.join(", ") : "—", mono: true },
            { k: "Issuer asserted", v: asserted.length ? asserted.join(", ") : "—", mono: true },
          ]} />
        ) : <div className="empty">{own.length ? "on none of their acts here" : "no acts of their own here"}</div>}
        <p className="imeta">"Reckon required" is the role the route demanded of the act, reckon's own check. "Issuer asserted" is what the identity provider put in the token at the act, as the deployment maps it for reckon: not the person's entitlements at large.{own.length > recorded ? ` ${own.length - recorded} of their acts carry neither: recorded before this version, or an approval a policy made in their name, which the action record attributes to the policy.` : ""}</p>
      </Section>
      <Section title="Identity"><KV rows={[{ k: "Principal id", v: principalId, mono: true }, { k: "Display name", v: name }]} /></Section>
    </>
  );
}
