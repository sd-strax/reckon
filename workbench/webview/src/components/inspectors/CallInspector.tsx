// Capability call (08 §8.6.1, adr/0092): the object the Story timeline, a
// tool row and the bundle point at. Tabs: Call · Result · Raw. The record says
// which binding answered, with what parameters, how it went, and what it
// produced; the other bindings for the verb show the ways it could have been
// answered, including an adapter that is installed but not enabled.
import { useApp } from "../../app";
import type { CapabilityCall } from "../../../../src/viewTypes";
import { Badge, RefChip, fmtDateTime } from "../shared";
import { Json, KV, Loading, Section, Tabs } from "./primitives";

function coverageTone(coverage: string): string {
  if (coverage === "COMPLETE") return "ok";
  if (coverage === "PARTIAL") return "warn";
  return "bad";
}

export function CallInspector({ callId }: { callId: string }) {
  const { state } = useApp();
  const call: CapabilityCall | undefined = state.calls[callId];
  if (!call) return <Loading what="capability call" />;
  const answered = call.adapter ? `${call.adapter} / ${call.operation}` : "none";
  const produced = call.produced;
  const eventCount = produced?.eventIds.length ?? 0;
  return (
    <>
      <div className="isub">
        <Badge tone={coverageTone(call.coverage)}>{call.coverage}</Badge>
        <span className="mono">{call.verb}</span> · {call.bindingsAnswered} of {call.bindingsTotal} binding{call.bindingsTotal === 1 ? "" : "s"} answered · {call.durationMs} ms
      </div>
      <Tabs tabs={[
        { id: "call", label: "Call", body: () => (
          <>
            <KV rows={[
              { k: "Verb", v: call.verb, mono: true },
              { k: "Answered by", v: answered, mono: true },
              { k: "Priority", v: call.adapter ? String(call.priority) : undefined, mono: true },
              { k: "Coverage", v: call.coverage, mono: true },
              { k: "Started", v: fmtDateTime(call.startedAt) },
              { k: "Took", v: `${call.durationMs} ms`, mono: true },
            ]} />
            <Section title="Parameters sent">
              {call.paramsSent === undefined || call.paramsSent === null
                ? <div className="empty">No binding was asked, so nothing was sent.</div>
                : <Json value={call.paramsSent} />}
            </Section>
            {call.degradationNotes.length ? (
              <Section title="Degradation notes">
                {call.degradationNotes.map((n, i) => <div key={i} className="irow mono">{n}</div>)}
              </Section>
            ) : null}
            <Section title="Bindings for this verb">
              {!call.bindings ? <div className="empty">not read</div>
                : !call.bindings.length ? <div className="empty">No binding is configured for this verb in the tenant.</div>
                : call.bindings.map((b) => {
                  const isAnswered = b.adapter === call.adapter && b.operation === call.operation;
                  return (
                    <div key={b.adapter + "/" + b.operation} className="irow">
                      <span className="mono">{b.adapter} / {b.operation}</span> <span className="imeta">p{b.priority}</span>{" "}
                      {isAnswered ? <Badge tone="ok">answered</Badge> : b.enabled ? <Badge tone="muted">enabled</Badge> : <Badge tone="warn">installed, not enabled</Badge>}
                    </div>
                  );
                })}
            </Section>
          </>
        ) },
        { id: "result", label: "Result", body: () => (
          <>
            {call.nullObservationRef ? (
              <Section title="Nothing observed">
                <div>A complete answer with no events: the absence is a citeable record.</div>
                <RefChip refId={call.nullObservationRef} />
              </Section>
            ) : null}
            <Section title={`Events · ${eventCount}`}>
              {!produced ? <div className="empty">not read</div>
                : !eventCount ? <div className="empty">{call.nullObservationRef ? "none, by a complete answer" : "none"}</div>
                : produced.eventIds.slice(0, 40).map((id) => <RefChip key={id} refId={id} />)}
              {eventCount > 40 ? <div className="imeta">+{eventCount - 40} more</div> : null}
            </Section>
            <Section title={`Observed data · ${produced?.observedDataRefs.length ?? 0}`}>
              {produced?.observedDataRefs.length ? produced.observedDataRefs.slice(0, 40).map((r) => <RefChip key={r} refId={r} />) : <div className="empty">none</div>}
            </Section>
            <Section title={`Entities · ${produced?.entityRefs.length ?? 0}`}>
              {produced?.entityRefs.length ? produced.entityRefs.slice(0, 40).map((r) => <RefChip key={r} refId={r} />) : <div className="empty">none</div>}
            </Section>
          </>
        ) },
        { id: "raw", label: "Raw", body: () => <Json value={call} /> },
      ]} />
    </>
  );
}
