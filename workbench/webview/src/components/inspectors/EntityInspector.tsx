// Entity (account, host, IP, domain, process …): type, value, deterministic
// id on hover; role and counts in this case; actions on it; other
// investigations; Pivot (stages, never fires), Pin, Copy. The popover of
// ui/02 §2.4 folded in (impl §2.1). The baseline (adr/0095) is the record's
// counting fact for this entity within this investigation, shown only when
// the record holds one — never inferred.
import { useApp } from "../../app";
import { inThisCase } from "../../derive/citations";
import { refTypeLabel } from "../../refs";
import { Badge, fmtDateTime } from "../shared";
import { pinSpec } from "../PromptCard";
import { patternSentence } from "../../derive/baseline";
import { Fields, Json, Loading, Section } from "./primitives";

export function EntityInspector({ refId }: { refId: string }) {
  const { state, send, openPrompt, openInspector, stage } = useApp();
  const doc = state.docs[refId];
  const label = state.labels[refId];
  const apps = state.appearances[refId];
  const baseline = state.baselines[refId];
  const c = inThisCase(refId, state.snap);
  const typeLabel = refTypeLabel(refId);
  const seed = state.snap?.investigation.seed;
  const role = seed?.entityRef === refId ? "seed subject" : c.actions.some((x) => x.role === "target") ? "action target" : c.predictions.length || c.pins.length ? "cited" : "appears";
  const actionsOn = (state.snap?.actions || []).filter((a) => (a.targets || []).includes(label || "") || (a.targets || []).includes(refId));
  const props = doc && doc.payload && typeof doc.payload === "object"
    ? (((doc.payload as Record<string, unknown>).properties as Record<string, unknown>) || (doc.payload as Record<string, unknown>))
    : null;
  return (
    <>
      <div className="isub">
        <span className="kicker">{typeLabel}</span>{" "}
        <span className="mono">{label || "…"}</span>
        <div className="pop-id" title="Deterministic id (engine-minted UUIDv5) — same entity, same id, everywhere">{refId}</div>
      </div>
      <div className="decide">
        <button className="primary" title="Stage a pivot question in the composer — you review and send it" onClick={() => stage("Pivot on " + (label || typeLabel + " " + refId) + ": what other activity involves this " + typeLabel + " across the available telemetry?")}>Pivot on entity</button>
        <button onClick={() => openPrompt(pinSpec([refId], undefined, (finding) => send({ type: "pin.add", refs: [refId], finding })))}>Pin as evidence</button>
        <button onClick={() => send({ type: "copy", text: label || refId })}>Copy value</button>
      </div>
      <Section title="Role in this case">
        <div>{role} · {c.acts.length} act{c.acts.length === 1 ? "" : "s"} cite it · {actionsOn.length} action{actionsOn.length === 1 ? "" : "s"} on it</div>
      </Section>
      <Section title="Normal behaviour">
        {baseline === undefined ? <div className="empty">reading the retrieved record…</div>
          : baseline === null ? <div className="empty">No baseline exists for this entity in the record: nothing retrieved for it repeats on three or more days. A baseline is never inferred.</div>
          : (
            <>
              <div className="imeta">From the record retrieved for this investigation: {baseline.eventsConsidered} events, {fmtDateTime(baseline.from)} → {fmtDateTime(baseline.to)}.</div>
              {baseline.patterns.map((p, i) => (
                <div key={i} className="irow">
                  <b>{patternSentence(p)}</b> · {p.className}{" "}
                  <span className="mono">{Object.entries(p.key).map(([k, v]) => `${k}=${v}`).join(" · ")}</span>
                  {" "}<a onClick={() => openInspector({ kind: "ref", ref: p.eventIds[0] })}>first</a> · <a onClick={() => openInspector({ kind: "ref", ref: p.eventIds[p.eventIds.length - 1] })}>last</a>
                </div>
              ))}
              {baseline.deviations.length ? (
                <>
                  <div className="kicker">How this case deviates</div>
                  {baseline.deviations.map((d) => (
                    <div key={d.eventId} className="irow deviation">
                      <a onClick={() => openInspector({ kind: "ref", ref: d.eventId })}>{d.className} at {fmtDateTime(d.time)}</a>: differs in <span className="mono">{d.differs.map((f) => `${f}=${d.key[f]}`).join(" · ")}</span>
                    </div>
                  ))}
                </>
              ) : <div className="imeta">Every retrieved event of a patterned class matches a pattern.</div>}
            </>
          )}
      </Section>
      {actionsOn.length ? (
        <Section title="Actions on this entity">
          {actionsOn.map((a) => (
            <div key={a.actionId} className="irow">
              <a onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{a.actionType}</a> <Badge tone={a.tier}>{a.tier}</Badge> <Badge tone={a.pending ? "warn" : ""}>{a.pending ? a.pendingLabel : a.status}</Badge>
            </div>
          ))}
        </Section>
      ) : null}
      <Section title="Other investigations">
        {apps === undefined ? <div className="empty">checking other investigations…</div>
          : !apps.length ? <div className="pop-first">First seen in this investigation. Cross-investigation identity is preserved by deterministic id — not surveillance, just join keys.</div>
          : apps.slice(0, 8).map((a) => (
            <div key={a.investigationId} className="app-row" title={(a.seedSummary || a.title) + " — open"} onClick={() => send({ type: "investigation.open", id: a.investigationId, title: a.title })}>
              <span className={"dot" + (a.status === "concluded" || a.status === "archived" ? " unavailable" : "")} />
              <span className="app-title">{a.title}</span>
              <span className="app-status">{a.status} · {a.mentions}×</span>
            </div>
          ))}
      </Section>
      {doc === undefined ? <Loading what="object" /> : doc ? (
        <>
          <Section title="Properties"><Fields obj={props} /></Section>
          <details className="iraw"><summary className="kicker">Raw</summary><Json value={{ ref: doc.ref, kind: doc.kind, type: doc.type, object: doc.payload }} /></details>
        </>
      ) : null}
    </>
  );
}
