// Evidence (an OCSF event or observed-data), Absence (x-null-observation,
// adr/0061), Detection finding (indicator + sighting + vendor identity,
// 03 §4.12) and Alias (an alias edge) — the record-shaped inspectors
// (08 §8.6, §8.6.1). The per-class sentence and curated field table are gap
// §6.12; until it lands the generic field renderer shows every field.
import { useApp } from "../../app";
import type { EvidenceDoc } from "../../../../src/viewTypes";
import { refType, refTypeLabel } from "../../refs";
import { RefChip, fmtDateTime } from "../shared";
import { Fields, Json, KV, Loading, Section, Tabs } from "./primitives";

function payloadOf(doc: EvidenceDoc): Record<string, unknown> {
  return (doc.payload && typeof doc.payload === "object" ? doc.payload : {}) as Record<string, unknown>;
}
function propsOf(doc: EvidenceDoc): Record<string, unknown> {
  const p = payloadOf(doc);
  return doc.kind === "ocsf" ? p : ((p.properties && typeof p.properties === "object" ? p.properties : p) as Record<string, unknown>);
}

/** The exact JSON the raw tab shows, so the card and the tab never disagree. */
function rawOf(doc: EvidenceDoc): unknown {
  return doc.kind === "ocsf"
    ? { ref: doc.ref, kind: doc.kind, class_uid: doc.classUid, class_name: doc.type, time: doc.time, recorded_at: doc.recordedAt, source_tool: doc.sourceTool, payload: doc.payload }
    : { ref: doc.ref, kind: doc.kind, type: doc.type, object: doc.payload };
}

function ProvenanceTab({ doc }: { doc: EvidenceDoc }) {
  const p = payloadOf(doc);
  const prov = (p.provenance && typeof p.provenance === "object" ? p.provenance : null) as Record<string, unknown> | null;
  if (doc.kind === "ocsf") {
    return <KV rows={[
      { k: "Source tool", v: doc.sourceTool, mono: true },
      { k: "Event time", v: doc.time ? fmtDateTime(doc.time) : undefined },
      { k: "Recorded", v: doc.recordedAt ? fmtDateTime(doc.recordedAt) : undefined },
      { k: "Class", v: doc.classUid ? `${doc.type} (${doc.classUid})` : doc.type, mono: true },
    ]} />;
  }
  if (!prov) return <div className="empty">No capability provenance on this object (03 §4.13): it was not produced by a normalizer.</div>;
  return <KV rows={[
    { k: "Observed via", v: prov.tool as string, mono: true },
    { k: "Normalizer", v: prov.normalizer ? `${prov.normalizer}${prov.normalizer_version ? " v" + prov.normalizer_version : ""}` : undefined, mono: true },
    { k: "Observed at", v: typeof prov.observed_at === "string" ? fmtDateTime(prov.observed_at) : undefined },
    { k: "Derivation", v: prov.derivation_mode as string, mono: true },
    { k: "Content hash", v: prov.content_hash as string, mono: true },
    { k: "Query sent", v: prov.query ? JSON.stringify(prov.query) : undefined, mono: true },
    { k: "Coverage", v: prov.coverage as string, mono: true },
  ]} />;
}

function useDoc(refId: string): EvidenceDoc | null | undefined {
  const { state } = useApp();
  return state.docs[refId];
}

export function EvidenceInspector({ target }: { target: { kind: "ref"; ref: string } }) {
  const doc = useDoc(target.ref);
  if (doc === undefined) return <Loading what={refTypeLabel(target.ref)} />;
  if (doc === null) return null; // the frame shows the error
  const p = payloadOf(doc);
  const isOD = doc.type === "observed-data";
  const objectRefs = Array.isArray(p.object_refs) ? (p.object_refs as unknown[]).filter((x): x is string => typeof x === "string") : [];
  const sub = doc.kind === "ocsf"
    ? [doc.type, doc.classUid ? "class " + doc.classUid : "", doc.time ? fmtDateTime(doc.time) : ""].filter(Boolean).join(" · ")
    : [doc.type, isOD && typeof p.first_observed === "string" ? "first " + fmtDateTime(p.first_observed) : "", isOD && typeof p.number_observed === "number" ? `×${p.number_observed}` : ""].filter(Boolean).join(" · ");
  return (
    <>
      <div className="isub">{doc.kind === "ocsf" ? "Telemetry · OCSF event" : "Interpretation · STIX object"} · {sub}</div>
      <Tabs tabs={[
        { id: "details", label: "Details", body: () => (
          <>
            {isOD && objectRefs.length ? <Section title="Objects it contains">{objectRefs.map((r) => <RefChip key={r} refId={r} />)}</Section> : null}
            <Section title={doc.kind === "ocsf" ? "Event fields" : "Properties"}><Fields obj={propsOf(doc)} /></Section>
          </>
        ) },
        { id: "provenance", label: "Provenance", body: () => <ProvenanceTab doc={doc} /> },
        { id: "raw", label: "Raw", body: () => <Json value={rawOf(doc)} /> },
      ]} />
    </>
  );
}

/** Absence (adr/0061): a search that answered in full and found nothing. */
export function AbsenceInspector({ refId }: { refId: string }) {
  const doc = useDoc(refId);
  const { stage } = useApp();
  if (doc === undefined) return <Loading what="absence record" />;
  if (doc === null) return null;
  const p = propsOf(doc);
  const win = (k: string) => (typeof p[k] === "string" ? fmtDateTime(p[k] as string) : "");
  const params = (p.parameters && typeof p.parameters === "object" ? p.parameters : {}) as Record<string, unknown>;
  const sources = Array.isArray(p.sources) ? (p.sources as unknown[]).map(String) : [];
  const verb = typeof p.verb === "string" ? p.verb : "query";
  return (
    <>
      <div className="isub"><span className="badge ok">COMPLETE · 0 EVENTS</span> A complete search that returned nothing. Absence is citeable only when the answer was complete.</div>
      <Tabs tabs={[
        { id: "search", label: "Search", body: () => (
          <>
            <KV rows={[
              { k: "Verb", v: verb, mono: true },
              { k: "Subject", v: typeof p.subject === "string" ? p.subject : undefined, mono: true },
              { k: "Looked for", v: Object.keys(params).length ? Object.entries(params).map(([k, v]) => `${k}: ${String(v)}`).join(" · ") : undefined, mono: true },
              { k: "Window", v: win("window_from") || win("window_to") ? `${win("window_from") || "…"} → ${win("window_to") || "…"}` : undefined, mono: true },
              { k: "Sources", v: sources.length ? `${sources.length} answered · ${sources.join(", ")}` : undefined, mono: true },
            ]} />
            <Section title="What this does and does not say">
              <p><b>Says:</b> every configured source answered in full, and none recorded this in the window.</p>
              <p><b>Does not say:</b> anything about hosts or sources outside the search, or before the window.</p>
              <p className="imeta">A PARTIAL empty answer produces no absence object, so it cannot be cited as “nothing found”.</p>
            </Section>
            <div className="decide">
              <button onClick={() => stage(`Re-run ${verb}${typeof p.subject === "string" ? " for " + p.subject : ""} with a wider window than ${win("window_from") || "…"} → ${win("window_to") || "…"} and record what it finds.`)}>Re-run with a wider window</button>
            </div>
          </>
        ) },
        { id: "provenance", label: "Provenance", body: () => <ProvenanceTab doc={doc} /> },
        { id: "raw", label: "Raw", body: () => <Json value={rawOf(doc)} /> },
      ]} />
    </>
  );
}

/** Detection finding (03 §4.12): inferred by the vendor, the one inferred
 * object the capability layer produces. A sighting ref opens its indicator. */
export function DetectionInspector({ refId }: { refId: string }) {
  const { state, openInspector } = useApp();
  const doc = useDoc(refId);
  if (doc === undefined) return <Loading what="detection finding" />;
  if (doc === null) return null;
  const p = propsOf(doc);
  if (refType(refId) === "sighting") {
    const ind = typeof p.sighting_of_ref === "string" ? p.sighting_of_ref : "";
    return (
      <>
        <div className="isub">A sighting of a detection finding.</div>
        {ind ? <div className="decide"><button className="primary" onClick={() => openInspector({ kind: "ref", ref: ind })}>Open the finding</button></div> : null}
        <Section title="Sighting"><Fields obj={p} /></Section>
      </>
    );
  }
  const related = doc.related;
  const identity = related?.identity ? propsOf(related.identity) : null;
  const techniques = Array.isArray(p.indicator_types) ? (p.indicator_types as unknown[]).map(String) : [];
  const confirmed: { ref: string; names: string[] }[] = [];
  for (const ev of related?.evidence || []) {
    const names: string[] = [];
    for (const h of state.snap?.hypotheses || []) for (const pr of h.predictions) if ((pr.testResultRefs || []).includes(ev.ref)) names.push(pr.ordinal ? "P" + pr.ordinal : pr.id);
    if (names.length) confirmed.push({ ref: ev.ref, names });
  }
  return (
    <>
      <div className="isub"><span className="badge warn">inferred by the vendor</span> Not observed by reckon: the only inferred object the capability layer produces.</div>
      <Tabs tabs={[
        { id: "finding", label: "Finding", body: () => (
          <>
            <KV rows={[
              { k: "Vendor", v: identity ? String(identity.name || "") : undefined },
              { k: "Indicator", v: String(p.name || refId), mono: true },
              { k: "Techniques", v: techniques.length ? techniques.join(", ") : undefined, mono: true },
              { k: "Severity", v: typeof p.severity === "string" ? p.severity : undefined },
              { k: "Valid from", v: typeof p.valid_from === "string" ? fmtDateTime(p.valid_from) : undefined },
            ]} />
            {(related?.sightings || []).map((s) => {
              const sp = propsOf(s);
              return <Section key={s.ref} title="Sighting"><KV rows={[
                { k: "Where", v: Array.isArray(sp.where_sighted_refs) ? <>{(sp.where_sighted_refs as string[]).map((r) => <RefChip key={r} refId={r} />)}</> : undefined },
                { k: "When", v: typeof sp.first_seen === "string" ? fmtDateTime(sp.first_seen) : undefined },
                { k: "Count", v: typeof sp.count === "number" ? String(sp.count) : undefined, mono: true },
              ]} /></Section>;
            })}
            {!related ? <div className="empty">The finding's sightings and vendor identity were not returned with it.</div> : null}
          </>
        ) },
        { id: "evidence", label: "Evidence", body: () => (
          <>
            <Section title="The evidence the vendor attached">
              {related?.evidence.length ? related.evidence.map((e) => <div key={e.ref}><RefChip refId={e.ref} /> <span className="imeta">normalized like any observed event</span></div>) : <div className="empty">none persisted</div>}
            </Section>
            <Section title="How far to trust it">
              {confirmed.length
                ? <p>The verdict is the vendor's. reckon confirmed the underlying events independently: {confirmed.map((c) => c.names.join(", ")).join("; ")}.</p>
                : <p>The verdict is the vendor's. No prediction in this investigation has cited the underlying events yet.</p>}
            </Section>
          </>
        ) },
        { id: "raw", label: "Raw", body: () => <Json value={rawOf(doc)} /> },
      ]} />
    </>
  );
}

/** Alias (an alias edge): linked, never merged. Renders only when the record holds one (impl §9). */
export function AliasInspector({ refId }: { refId: string }) {
  const doc = useDoc(refId);
  if (doc === undefined) return <Loading what="alias edge" />;
  if (doc === null) return null;
  const p = propsOf(doc);
  const left = typeof p.source_ref === "string" ? p.source_ref : "";
  const right = typeof p.target_ref === "string" ? p.target_ref : "";
  return (
    <>
      <div className="isub">Two identities joined by a link. Linked, never merged: each keeps its own id and history.</div>
      <Section title="Identities">
        {left ? <RefChip refId={left} /> : null} <span className="imeta">⇄</span> {right ? <RefChip refId={right} /> : null}
      </Section>
      <Section title="Why they are linked">
        <KV rows={[
          { k: "Basis", v: typeof p.description === "string" ? p.description : undefined },
          { k: "Linked by", v: typeof p.created_by_ref === "string" ? p.created_by_ref : undefined, mono: true },
          { k: "When", v: typeof p.created === "string" ? fmtDateTime(p.created) : undefined },
        ]} />
        <p className="imeta">Removing a link is an engine act the binding does not list yet; nothing was merged, so nothing would be lost.</p>
      </Section>
      <Json value={rawOf(doc)} />
    </>
  );
}
