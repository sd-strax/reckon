// The Capability call inspector (08 §8.6.1; adr/0092): the object the Story
// timeline, the residual panel and the bundle point at. Call · Result · Raw.
// The Result tab is the bundle view: the call's events grouped by source with
// repetitions collapsed, each opening its Evidence inspector; the absence
// object when the call produced one; "nothing" when it produced nothing.
import { useApp } from "../../app";
import { bundleGroups } from "../../derive/attackPath";
import { EventSentence } from "../EventSentence";
import { Badge, RefChip, fmtDateTime } from "../shared";
import { Json, KV, Loading, Section, Tabs } from "./primitives";

export function CallInspector({ callId }: { callId: string }) {
  const { state, openInspector, openEnableForm } = useApp();
  const call = state.calls[callId];
  // The events listing already names the call on each row: the inspector can
  // open on them before the detail arrives.
  const rowsInSnapshot = (state.snap?.events || []).filter((e) => e.callId === callId);
  const verb = call?.verb || rowsInSnapshot[0]?.verb || "";
  const coverage = call?.coverage || rowsInSnapshot[0]?.coverage || "";
  const events = call?.produced.events || rowsInSnapshot;
  if (!call && !rowsInSnapshot.length) return <Loading what="capability call" />;
  return (
    <>
      <div className="isub">
        <span className="mono">{verb}</span>{coverage ? <> · <Badge tone={coverage === "COMPLETE" ? "ok" : coverage === "PARTIAL" ? "warn" : "bad"}>{coverage}</Badge></> : null}
        {call ? <> · {call.bindingsAnswered} of {call.bindingsTotal} binding{call.bindingsTotal === 1 ? "" : "s"} answered · took {call.durationMs} ms · {fmtDateTime(call.startedAt)}</> : null}
      </div>
      <Tabs tabs={[
        { id: "call", label: "Call", body: () => call ? (
          <>
            <KV rows={[
              { k: "Verb", v: call.verb, mono: true },
              { k: "Binding", v: call.adapter ? `${call.adapter} · ${call.operation}${call.priority !== undefined ? " · priority " + call.priority : ""}` : "none answered", mono: true },
              { k: "Coverage", v: call.coverage, mono: true },
              { k: "Answered", v: `${call.bindingsAnswered} of ${call.bindingsTotal}` },
              { k: "Took", v: `${call.durationMs} ms` },
              { k: "Started", v: fmtDateTime(call.startedAt) },
            ]} />
            <Section title="Parameters sent, exactly">{call.paramsSent !== undefined ? <Json value={call.paramsSent} /> : <div className="empty">nothing was sent — no binding was applicable</div>}</Section>
            {call.degradationNotes.length ? <Section title="Degradation notes">{call.degradationNotes.map((n, i) => <div key={i} className="irow mono">{n}</div>)}</Section> : null}
            <Section title="Bindings considered">
              {call.attempts.length ? call.attempts.map((a, i) => <div key={i} className="irow"><Badge tone={a.answered ? "ok" : "muted"}>{a.answered ? "answered" : "skipped"}</Badge> <span className="mono">{a.adapter} · {a.operation}</span> · priority {a.priority}{a.note ? <span className="imeta"> · {a.note}</span> : null}</div>) : <div className="empty">no binding is configured for this verb</div>}
            </Section>
            <Section title="Other bindings for the verb">
              {call.otherBindings.length ? call.otherBindings.map((b, i) => <div key={i} className="irow"><span className={"dot " + (b.configured && b.healthy ? "available" : b.configured ? "degraded" : "unavailable")} /> <span className="mono">{b.adapter} · {b.operation}</span> · priority {b.priority}{b.scope ? " · scope " + b.scope : ""}{!b.configured ? <span className="imeta"> · not enabled</span> : !b.healthy ? <span className="imeta"> · unhealthy</span> : null}</div>) : <div className="empty">none configured</div>}
              {(call.closableBy || []).map((a) => <div key={a} className="gaphint">⚡ {a} is installed but not enabled — it could serve {call.verb}. <button onClick={() => openEnableForm(a)}>Set up…</button></div>)}
            </Section>
          </>
        ) : <Loading what="the call's record" /> },
        { id: "result", label: `Result · ${events.length}`, body: () => (
          <>
            {call?.produced.nullObservationRef || call?.nullObservationRef ? <Section title="Absence recorded"><RefChip refId={call.produced.nullObservationRef || call.nullObservationRef!} /> <span className="imeta">a complete answer with nothing in it (adr/0061)</span></Section> : null}
            {!events.length && !(call?.nullObservationRef) ? <div className="empty">This call produced nothing: no events, and no absence object (the answer was not complete).</div> : null}
            {bundleGroups(events).map((g) => (
              <Section key={g.source} title={`From ${g.source} · ${g.rows.reduce((n, r) => n + r.ids.length, 0)}`}>
                {g.rows.map((r) => (
                  <div key={r.key} className="irow bundlerow">
                    <a onClick={() => openInspector({ kind: "ref", ref: r.first.id })}>{r.first.view?.sentence?.length ? <EventSentence spans={r.first.view.sentence} className="inline" /> : r.text}</a>
                    {r.ids.length > 1 ? <span className="imeta"> ×{r.ids.length} · {r.ids.slice(1, 4).map((id) => <a key={id} onClick={() => openInspector({ kind: "ref", ref: id })}>{id.slice(0, 8)}</a>).reduce<React.ReactNode[]>((acc, x, i) => i ? [...acc, ", ", x] : [x], [])}{r.ids.length > 4 ? " …" : ""}</span> : null}
                    <span className="imeta"> · {fmtDateTime(r.first.time)}</span>
                  </div>
                ))}
              </Section>
            ))}
            {call?.produced.entityRefs.length ? <Section title="Entities it named">{call.produced.entityRefs.slice(0, 16).map((r) => <RefChip key={r} refId={r} />)}</Section> : null}
          </>
        ) },
        { id: "raw", label: "Raw", body: () => <Json value={call ?? { callId, events: rowsInSnapshot }} /> },
      ]} />
    </>
  );
}
