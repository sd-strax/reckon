// Turn (Answer · Tool calls · Consulted · Transcript), Similar past case
// (Summary · Why it matched), Procedure (What the AI read · Whole document ·
// Versions) and Person — the provenance inspectors (08 §8.6, §8.6.1).
import { useMemo } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { parseTranscript } from "../../transcript";
import { Badge, Markdown, RefChip, fmtDateTime, fmtTime } from "../shared";
import { ToolRow } from "../ToolRow";
import { Json, KV, Loading, Section, Tabs } from "./primitives";
import { received } from "../../derive/knowledgeUse";
import { relevanceOfBand } from "../../../../src/backend";

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
            {(e.consultedSops || []).map((s) => <div key={s.sopId} className="irow">{s.used ? "applied" : "consulted, not applied"}: <a onClick={() => openInspector({ kind: "sop", sopId: s.sopId })}>{s.title || s.sopId}</a></div>)}
            {(e.consultedSimilar || []).map((s) => <div key={s.investigationRef} className="irow">{s.used ? "drew on case" : "similar case, not applied"}: <a onClick={() => openInspector({ kind: "case", investigationRef: s.investigationRef })}>{s.title || s.investigationRef}</a>{s.band ? <> <Badge>{s.band}</Badge></> : null}</div>)}
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
  // How this case was used here (#132): only what the agent's recalls carried.
  const got = received(state.snap, "case", investigationRef);
  const match = got.find((g) => g.band || g.rationale);
  const consulted = (state.snap?.thread || []).flatMap((e) => (e.consultedSimilar || []).filter((s) => s.investigationRef === investigationRef).map((s) => ({ e, s })));
  return (
    <>
      <div className="isub">
        {match?.band ? <Badge tone={match.band === "NEAR_DUPLICATE" ? "ok" : match.band === "DISTINCT" ? "muted" : "info"}>{relevanceOfBand(match.band)}</Badge> : null}
        {summary ? <> Concluded {summary.conclusionAt ? fmtDateTime(summary.conclusionAt) : "—"}{summary.verdictDisposition ? " · " + summary.verdictDisposition.toLowerCase() : ""}{summary.durationHours ? ` · ${summary.durationHours} hours` : ""}</> : null}
      </div>
      <div className="ithiscase"><span className="kicker">In this case:</span>{" "}
        {consulted.map(({ e, s }) => <span key={e.sequenceNo} className="icite">{s.used ? "the agent drew on it" : "the agent recalled it, not used"} in <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || "a turn"}</a></span>)}
        {!consulted.length ? <span className="empty">the agent has not recalled it in this investigation</span> : null}
      </div>
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
              </>
            )
        ) },
        { id: "read", label: "What the agent read", body: () => (
          got.length ? got.map((g, i) => (
            <div key={i}>
              {g.excerpt ? <blockquote className="iquote">{g.excerpt}</blockquote> : null}
              <div className="imeta">{[g.band ? relevanceOfBand(g.band) : "", g.rationale || ""].filter(Boolean).join(" · ")} — in <a onClick={() => openInspector({ kind: "turn", interpretationId: g.interpretationId })}>{g.summary || "a turn"}</a></div>
            </div>
          )) : <div className="empty">The agent has not read this case in this investigation.</div>
        ) },
      ]} />
      <div className="decide">
        <button onClick={() => send({ type: "investigation.open", id: investigationRef.replace(/^grouping--/, ""), title: summary?.title })}>Open that investigation</button>
      </div>
      {summary ? <details className="iraw"><summary className="kicker">Raw</summary><Json value={summary} /></details> : null}
    </>
  );
}

export function ProcedureInspector({ sopId }: { sopId: string }) {
  const { state, openInspector } = useApp();
  const sop = state.sops[sopId];
  const got = received(state.snap, "sop", sopId);
  const consulted = (state.snap?.thread || []).flatMap((e) => (e.consultedSops || []).filter((s) => s.sopId === sopId).map((s) => ({ e, s })));
  const ledTo = (state.snap?.actions || []).filter((a) => consulted.some(({ e }) => (e.outputRefs || []).includes(a.actionId)));
  return (
    <>
      <div className="isub">{sop ? <>{sop.source?.system ? sop.source.system + " · " : ""}{sop.source?.url ? <span className="mono">{sop.source.url}</span> : "markdown"}{sop.importedBy ? ` · imported by ${sop.importedBy}` : ""}{sop.importedAt ? ` · ${fmtDateTime(sop.importedAt)}` : ""}</> : null}</div>
      <div className="ithiscase"><span className="kicker">In this case:</span>{" "}
        {consulted.map(({ e, s }) => <span key={e.sequenceNo} className="icite">{s.used ? "the agent applied it" : "the agent recalled it, not applied"} in <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || "a turn"}</a></span>)}
        {ledTo.map((a) => <span key={a.actionId} className="icite">led to <a onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{a.actionType}</a></span>)}
        {!consulted.length ? <span className="empty">the agent has not recalled it in this investigation</span> : null}
      </div>
      <Tabs tabs={[
        { id: "read", label: "What the agent read", body: () => (
          got.length ? got.map((g, i) => (
            <div key={i}>
              {g.excerpt ? <blockquote className="iquote">{g.excerpt}</blockquote> : <div className="empty">recalled without an excerpt</div>}
              <div className="imeta">{g.rationale ? g.rationale + " — " : ""}in <a onClick={() => openInspector({ kind: "turn", interpretationId: g.interpretationId })}>{g.summary || "a turn"}</a></div>
            </div>
          )) : <div className="empty">The agent has not read this procedure in this investigation.</div>
        ) },
        { id: "whole", label: "Whole document", body: () => (
          sop === undefined ? <Loading what="procedure" /> : (
            <>
              <KV rows={[
                { k: "Status", v: sop.status, mono: true },
                { k: "Recommends", v: sop.recommendation, mono: true },
                { k: "Tags", v: sop.tags.join(", ") || undefined, mono: true },
                { k: "Author", v: sop.authorId },
                { k: "Version", v: sop.source?.version, mono: true },
              ]} />
              <Markdown text={sop.body} className="idoc" />
            </>
          )
        ) },
      ]} />
    </>
  );
}

export function PersonInspector({ principalId }: { principalId: string }) {
  const { state, openInspector } = useApp();
  const acts = (state.snap?.thread || []).filter((e) => e.actor.principal === principalId);
  const name = acts.find((e) => e.actor.principalName)?.actor.principalName || principalId;
  const approvals = acts.filter((e) => e.interpretationType === "action-approval").length;
  const opened = state.snap?.investigation.seed && acts.length && acts[0].sequenceNo <= 2;
  return (
    <>
      <div className="isub">From the identity provider at the time of each act; reckon stores no roles of its own.</div>
      <div className="ithiscase"><span className="kicker">In this case:</span> <span className="icite">{opened ? "opened it · " : ""}{acts.length} act{acts.length === 1 ? "" : "s"}{approvals ? ` · ${approvals} approval${approvals === 1 ? "" : "s"}` : ""}</span></div>
      <Section title="What they did here">
        {acts.length ? acts.map((e) => (
          <div key={e.sequenceNo} className="irow"><span className="imeta">{fmtTime(e.occurredAt)}</span> {e.actor.kind === "AI_DELEGATED" ? "reckon on their behalf: " : ""}<a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || e.interpretationType}</a></div>
        )) : <div className="empty">no acts in this investigation</div>}
      </Section>
      <Section title="Identity"><KV rows={[{ k: "Principal id", v: principalId, mono: true }, { k: "Display name", v: name }]} /></Section>
    </>
  );
}
