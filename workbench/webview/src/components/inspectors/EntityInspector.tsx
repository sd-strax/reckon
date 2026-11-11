// Entity (account, host, IP, domain, process …): type, value, deterministic
// id on hover; role and counts in this case; repeated observations (the
// entity's retrieved events folded by the default key, adr/0095, shown only
// when a group repeats); a host's identity and the acts on it (adr/0211);
// actions on it; other investigations; Pivot (stages,
// never fires), Pin, Copy. The popover of ui/02 §2.4 folded in (impl §2.1).
import { useEffect, useRef } from "react";
import { useApp } from "../../app";
import { inThisCase } from "../../derive/citations";
import { collapses, fmtDay, spanText } from "../../derive/events";
import { refTypeLabel } from "../../refs";
import { Badge, Sentence } from "../shared";
import { pinSpec } from "../PromptCard";
import { Fields, Json, Loading, Section } from "./primitives";
import { HostIdentitySection } from "./HostIdentity";

export function EntityInspector({ refId }: { refId: string }) {
  const { state, send, openPrompt, openInspector, stage } = useApp();
  const doc = state.docs[refId];
  const label = state.labels[refId];
  const apps = state.appearances[refId];
  const c = inThisCase(refId, state.snap);
  const typeLabel = refTypeLabel(refId);
  // A host's identity and the acts on it (adr/0211); hosts only for now.
  const isHost = refId.startsWith("x-host--");
  // A host's identity is mutable (adr/0211): it is read fresh whenever a pane
  // reveals a host it already holds (Back, a pinned pane, a change of host),
  // and again when the held view turns stale while shown (an act, a
  // refresh), and on every reload, stale or not, so a second Refresh retries
  // a read the first could not complete. The held doc stays on screen until
  // the answer lands. Once per reveal, reload or turn to stale: an answer
  // that leaves it stale changes none of them, so nothing asks again on its
  // own. A host not held yet is being read by the open that showed it.
  const stale = isHost && !!state.staleHosts[refId];
  const held = isHost && !!state.docs[refId];
  // Its first read failed (the pane shows the error): a reload retries it.
  const failed = isHost && state.docs[refId] === null;
  const reloadSeq = state.reloadSeq;
  const seen = useRef<{ ref: string; stale: boolean; reloadSeq: number } | null>(null);
  useEffect(() => {
    const before = seen.current;
    seen.current = { ref: refId, stale, reloadSeq };
    if (!isHost || (!held && !failed)) return;
    const revealed = before === null || before.ref !== refId;
    const reloaded = !revealed && before.reloadSeq !== reloadSeq;
    const turnedStale = !revealed && stale && !before.stale;
    if (reloaded || (held && (revealed || turnedStale))) {
      send({ type: "inspector.open", target: { kind: "ref", ref: refId } });
    }
  }, [isHost, held, failed, stale, reloadSeq, refId, send]);
  const seed = state.snap?.investigation.seed;
  const role = seed?.entityRef === refId ? "seed subject" : c.actions.some((x) => x.role === "target") ? "action target" : c.predictions.length || c.pins.length ? "cited" : "appears";
  const actionsOn = (state.snap?.actions || []).filter((a) => (a.targets || []).includes(label || "") || (a.targets || []).includes(refId));
  // Repeated observations (adr/0095): the entity's retrieved events folded by
  // the default key; shown only when a group repeats, never inferred.
  const rep = state.repeats[refId];
  const repeated = rep ? rep.groups.filter(collapses) : [];
  const unrepeated = rep ? rep.events.length - repeated.reduce((n, g) => n + g.count, 0) : 0;
  // The fold covers the newest events up to a bound; a claim about repetition
  // is scoped to what it covered when the entity's record is larger.
  const truncated = !!rep && rep.total > rep.foldedOver;
  const covered = rep ? `the newest ${rep.foldedOver}` : "";
  const plural = (n: number, s: string) => `${n} ${s}${n === 1 ? "" : "s"}`;
  const repeatsLine = !rep ? "" : unrepeated === 0
    ? (truncated ? `Every one of ${covered} retrieved events on this entity repeats.` : "Every retrieved event on this entity repeats.")
    : (truncated ? `${plural(unrepeated, "event")} of ${covered} on this entity ${unrepeated === 1 ? "does" : "do"} not repeat.` : `${plural(unrepeated, "retrieved event")} on this entity ${unrepeated === 1 ? "does" : "do"} not repeat.`);
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
      {isHost && doc ? <HostIdentitySection key={refId} refId={refId} identity={doc.identity} /> : null}
      {repeated.length && rep ? (
        <Section title="Repeated observations">
          {truncated ? <div className="imeta">{plural(rep.total, "retrieved event")} on this entity · folded over {covered}</div> : null}
          {repeated.map((g) => {
            const newest = rep.events.find((e) => e.id === g.eventIds[0]);
            return (
              <div key={g.eventIds[0]} className="irow repeat">
                <div><b>{spanText(g)}</b> <span className="imeta">· {fmtDay(g.first)} – {fmtDay(g.last)}</span></div>
                {newest ? (newest.view.sentence?.length ? <Sentence tokens={newest.view.sentence} className="isentence" /> : <span className="imeta">{newest.className}</span>) : null}
              </div>
            );
          })}
          <div className="imeta">{repeatsLine}</div>
        </Section>
      ) : null}
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
