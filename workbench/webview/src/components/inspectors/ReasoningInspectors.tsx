// Attestation (note, adr/0054), Prediction (Record · History) and Hypothesis
// (Record · Wording history) — the reasoning inspectors (08 §8.6.1). Their
// data is in the snapshot; the history comes from the thread's events.
import { useApp } from "../../app";
import type { Hypothesis, Prediction } from "../../../../src/viewTypes";
import { badgeTone, blockersOf, counted, hypName, nameOf, predName, tested } from "../../derive/hyps";
import { Badge, RefChip, fmtDateTime, fmtTime } from "../shared";
import { Fields, Json, KV, Loading, Section, Tabs } from "./primitives";

function findPrediction(hs: Hypothesis[], id: string): { h: Hypothesis; p: Prediction } | null {
  for (const h of hs) {
    const p = h.predictions.find((x) => x.id === id);
    if (p) return { h, p };
  }
  return null;
}

/** Who said it (as recorded), who recorded it, when, how obtained; the prediction it decided. */
export function AttestationInspector({ refId }: { refId: string }) {
  const { state, openInspector } = useApp();
  const hs = state.snap?.hypotheses || [];
  const doc = state.docs[refId];
  // The prediction whose attestation this is carries the statement already;
  // the note's doc adds the recording actor and the raw object.
  let owner: { h: Hypothesis; p: Prediction } | null = null;
  for (const h of hs) for (const p of h.predictions) if (p.attestation?.noteRef === refId || (p.testResultRefs || []).includes(refId)) owner = { h, p };
  const a = owner?.p.attestation;
  const payload = doc && doc.payload && typeof doc.payload === "object" ? (doc.payload as Record<string, unknown>) : {};
  const recorded = state.snap?.thread?.find((e) => (e.outputRefs || []).includes(refId) || ((e.inputRefs || []).includes(refId) && e.interpretationType !== "support" && e.interpretationType !== "refutation"));
  return (
    <>
      <div className="isub">A statement by a person, recorded as a note. Not telemetry.</div>
      {a ? <blockquote className="iquote">“{a.content}”</blockquote> : typeof payload.content === "string" ? <blockquote className="iquote">“{payload.content}”</blockquote> : doc === undefined ? <Loading what="attestation" /> : null}
      <Section title="Who said it">
        <KV rows={[
          { k: "Statement by", v: a?.source ? a.source + " (as recorded)" : undefined },
          { k: "Recorded by", v: a?.authorName || a?.author || recorded?.actor.principalName || recorded?.actor.principal },
          { k: "When", v: a?.observedAt ? fmtDateTime(a.observedAt) : recorded ? fmtDateTime(recorded.occurredAt) : undefined },
          { k: "How obtained", v: a?.channel },
        ]} />
      </Section>
      {owner ? (
        <Section title="The question it answers">
          <p><a onClick={() => openInspector({ kind: "ref", ref: owner!.p.id })}>{predName(owner.p)}</a> under {hypName(owner.h, hs)}: {owner.p.statement}</p>
          <p>Outcome: <Badge tone={badgeTone(owner.p.status)}>{owner.p.status}</Badge></p>
        </Section>
      ) : null}
      <Section title="Rules"><p className="imeta">Only a person records an attestation; the AI can ask for one but never write it. The text is frozen once recorded.</p></Section>
      {doc ? <details className="iraw"><summary className="kicker">Raw</summary><Json value={{ ref: doc.ref, type: doc.type, object: doc.payload }} /></details> : null}
    </>
  );
}

export function PredictionInspector({ refId }: { refId: string }) {
  const { state, openInspector } = useApp();
  const hs = state.snap?.hypotheses || [];
  const found = findPrediction(hs, refId);
  if (!found) return <div className="empty">This prediction is not in the current snapshot.</div>;
  const { h, p } = found;
  const history = (state.snap?.thread || []).filter((e) => (e.outputRefs || []).includes(p.id) || (e.inputRefs || []).includes(p.id));
  return (
    <>
      <div className="isub"><Badge tone={badgeTone(p.status)}>{p.status}</Badge> <span className="pord">{predName(p)}</span> · under <a onClick={() => openInspector({ kind: "ref", ref: h.id })}>{hypName(h, hs)}</a></div>
      <Tabs tabs={[
        { id: "record", label: "Record", body: () => (
          <>
            <KV rows={[
              { k: "Statement", v: p.statement },
              { k: "Owner", v: hypName(h, hs), mono: true },
              { k: "Discriminates", v: (p.discriminates || []).length ? (p.discriminates || []).map((d) => nameOf(d, hs)).join(", ") + " (expects the opposite)" : "none — merely consistent with its owner" },
              { k: "Test kind", v: p.testKind === "manual" ? "manual · a person answers" : "tool" + (p.testQuery?.tool ? " · " + p.testQuery.tool : "") },
              { k: "Declared test", v: p.testQuery?.queryText, mono: true },
            ]} />
            <Section title="Result">
              {(p.testResultRefs || []).length ? (p.testResultRefs || []).map((r) => <RefChip key={r} refId={r} />) : <div className="empty">no result recorded</div>}
              {p.attestation ? <div className="attestline"><b>attested by {p.attestation.authorName || p.attestation.author}</b>: <q>{p.attestation.content}</q></div> : null}
            </Section>
            {p.status === "UNTESTED" ? <p className="imeta">If it goes the other way: DISCONFIRMED counts against {hypName(h, hs)}{(p.discriminates || []).length ? " and for " + (p.discriminates || []).map((d) => nameOf(d, hs)).join(", ") : ""}. Withdrawing is only possible while untested.</p> : null}
          </>
        ) },
        { id: "history", label: "History", body: () => (
          history.length ? (
            <ol className="ihistory">
              {history.map((e) => (
                <li key={e.sequenceNo}>
                  <span className="imeta">{fmtTime(e.occurredAt)}</span>{" "}
                  <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || e.interpretationType}</a>
                  <span className="imeta"> · {e.actor.kind === "AI_DELEGATED" ? "reckon on behalf of " : ""}{e.actor.principalName || e.actor.principal}</span>
                </li>
              ))}
            </ol>
          ) : <div className="empty">No thread entry cites this prediction yet.</div>
        ) },
      ]} />
    </>
  );
}

export function HypothesisInspector({ refId }: { refId: string }) {
  const { state, openInspector } = useApp();
  const hs = state.snap?.hypotheses || [];
  const h = hs.find((x) => x.id === refId);
  if (!h) return <div className="empty">This hypothesis is not in the current snapshot.</div>;
  const preds = h.predictions.filter(counted);
  const blockers = blockersOf(h, hs);
  const wording = (state.snap?.thread || []).filter((e) => (e.outputRefs || []).includes(h.id));
  const verdict = state.snap?.investigation.verdict;
  return (
    <>
      <div className="isub"><Badge tone={badgeTone(h.status)}>{h.status}</Badge> <span className="hyphandle">{hypName(h, hs)}</span> · one of {hs.length} in the competing set</div>
      <Tabs tabs={[
        { id: "record", label: "Record", body: () => (
          <>
            <Section title="Claim"><p>{h.statement}</p></Section>
            <Section title={`Predictions · ${preds.filter(tested).length} of ${preds.length} tested`}>
              {h.predictions.length ? h.predictions.map((p) => (
                <div key={p.id} className="irow">
                  <span className={"pglyph " + badgeTone(p.status)} title={p.status}>{p.status === "CONFIRMED" ? "✓" : p.status === "DISCONFIRMED" ? "✕" : p.status === "WITHDRAWN" ? "–" : "?"}</span>{" "}
                  <a onClick={() => openInspector({ kind: "ref", ref: p.id })}>{predName(p)}</a> {p.statement}
                </div>
              )) : <div className="empty">none recorded</div>}
            </Section>
            {blockers.length ? <p className="guardnote">blocked: {blockers.map((b) => predName(b.p) + " (" + hypName(b.owner, hs) + ")").join(", ")} untested and splitting it from a rival</p> : null}
            <Section title="Competing set">
              {hs.map((r) => <div key={r.id} className="irow"><a onClick={() => openInspector({ kind: "ref", ref: r.id })}>{hypName(r, hs)}</a> <Badge tone={badgeTone(r.status)}>{r.status}</Badge>{r.id === h.id ? <span className="imeta"> · this one</span> : null}</div>)}
              {verdict ? <p className="imeta">Verdict of record: {verdict.disposition}</p> : null}
            </Section>
            <KV rows={[{ k: "Labels", v: (h.labels || []).join(", ") || undefined, mono: true }, { k: "Parent", v: h.parentRef, mono: true }]} />
          </>
        ) },
        { id: "wording", label: "Wording history", body: () => (
          wording.length ? (
            <ol className="ihistory">
              {wording.map((e) => (
                <li key={e.sequenceNo}>
                  <span className="imeta">{fmtDateTime(e.occurredAt)}</span>{" "}
                  <a onClick={() => openInspector({ kind: "turn", interpretationId: e.interpretationId })}>{e.summary || e.interpretationType}</a>
                  <span className="imeta"> · {e.actor.kind === "AI_DELEGATED" ? "proposed by reckon on behalf of " : ""}{e.actor.principalName || e.actor.principal}</span>
                </li>
              ))}
              <li className="imeta">Tested wording is frozen; a new theory would be a refinement with this one as its parent.</li>
            </ol>
          ) : <div className="empty">No thread entry produced this hypothesis in the current snapshot.</div>
        ) },
      ]} />
      <details className="iraw"><summary className="kicker">Raw</summary><Fields obj={h} /></details>
    </>
  );
}
