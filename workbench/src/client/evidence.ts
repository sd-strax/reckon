// Evidence over HTTP: one cited object with what the record holds around it
// (02 §2.8), and where an entity appears across investigations (binding §6.1).
// Answers are decoded through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawEvidence, RawEventView } from "../wire/backendShapes";
import type { Decoded } from "../wire/decode";
import type { Appearance, CapabilityCall, EventClassKeys, EventGroup, EventRow, EventView, EvidenceDoc } from "../viewTypes";

/** The rendering as decoded, narrowed to the view's vocabulary (adr/0090). */
function mapView(v: RawEventView): EventView {
  const view: EventView = {
    fields: v.fields.map((f) => ({
      label: f.label, value: f.value, ref: f.ref, mono: f.mono,
      tone: f.tone === "ok" || f.tone === "warn" || f.tone === "bad" ? f.tone : undefined,
    })),
    category: v.category,
  };
  if (v.sentence) view.sentence = v.sentence.map((t) => ({ t: t.t === "entity" && t.ref ? "entity" : "text", ref: t.ref, text: t.text }));
  if (v.decoded) view.decoded = { field: v.decoded.field, encoding: v.decoded.encoding, text: v.decoded.text };
  return view;
}

function mapEvidence(r: RawEvidence): EvidenceDoc {
  const doc: EvidenceDoc = {
    ref: r.ref,
    kind: r.kind,
    type: r.type,
    payload: r.payload,
    classUid: r.class_uid,
    time: r.time,
    recordedAt: r.recorded_at,
    sourceTool: r.source_tool,
  };
  if (r.view) doc.view = mapView(r.view);
  if (r.related) {
    doc.related = {
      sightings: (r.related.sightings ?? []).map((s) => mapEvidence(s)),
      identity: r.related.identity ? mapEvidence(r.related.identity) : null,
      evidence: (r.related.evidence ?? []).map((e) => mapEvidence(e)),
    };
  }
  return doc;
}

type RawEventRow = Decoded<typeof shapes.investigationEvents>["events"][number];
type RawCall = Decoded<typeof shapes.investigationCalls>["calls"][number] & Partial<Decoded<typeof shapes.capabilityCall>>;

function mapEventRow(r: RawEventRow): EventRow {
  return {
    id: r.id, classUid: r.class_uid, className: r.class_name, time: r.time, recordedAt: r.recorded_at, sourceTool: r.source_tool,
    callId: r.call_id, verb: r.verb, coverage: r.coverage, observedDataRefs: r.observed_data_refs, entityRefs: r.entity_refs,
    view: mapView(r.view),
  };
}

function mapCall(r: RawCall): CapabilityCall {
  const call: CapabilityCall = {
    callId: r.call_id, investigationId: r.investigation_id, verb: r.verb, adapter: r.adapter, operation: r.operation, priority: r.priority,
    paramsSent: r.params_sent, coverage: r.coverage, bindingsAnswered: r.bindings_answered, bindingsTotal: r.bindings_total,
    nullObservationRef: r.null_observation_ref, degradationNotes: r.degradation_notes, startedAt: r.started_at, durationMs: r.duration_ms,
    outcomes: r.outcomes.map((o) => ({ adapter: o.adapter, operation: o.operation, priority: o.priority, params: o.params, answered: o.answered, skipped: o.skipped })),
  };
  if (r.produced) {
    call.produced = { eventIds: r.produced.event_ids, observedDataRefs: r.produced.observed_data_refs, entityRefs: r.produced.entity_refs };
  }
  if (r.bindings) {
    call.bindings = r.bindings;
  }
  return call;
}

/**
 * How an events read is narrowed (adr/0092). `around` is an event id; `cursor`
 * is the `nextCursor` of the previous page. `groupBy` (adr/0095) folds the
 * newest events by class and key instead of paging: "default", or
 * "class:slot,slot;…"; `entity` is then the pivot.
 */
export interface EventQuery {
  entity?: string;
  category?: "auth" | "process" | "network" | "detection";
  around?: string;
  limit?: number;
  order?: "oldest" | "newest";
  cursor?: string;
  groupBy?: string;
}

/**
 * A page of retrieved events with the unpaged total and, when more remain, the
 * cursor of the next page; a grouped read carries the groups, the keys per
 * class and how many of the total the fold covered instead of a cursor.
 */
export interface EventPage {
  events: EventRow[];
  total: number;
  nextCursor?: string;
  groups?: EventGroup[];
  keys?: EventClassKeys[];
  foldedOver?: number;
}

export class EvidenceClient {
  constructor(private readonly http: BackendHttp) {}

  /**
   * GET /api/investigations/{id}/events — the events retrieved for the
   * investigation, in event time, by entity, category or around one event
   * (adr/0092). A read over the record, never a capability call.
   */
  async events(investigationId: string, q: EventQuery = {}): Promise<EventPage> {
    const params = new URLSearchParams();
    if (q.entity) params.set("entity", q.entity);
    if (q.category) params.set("category", q.category);
    if (q.around) params.set("around", q.around);
    if (q.limit) params.set("limit", String(q.limit));
    if (q.order) params.set("order", q.order);
    if (q.cursor) params.set("cursor", q.cursor);
    if (q.groupBy !== undefined) params.set("group_by", q.groupBy);
    const qs = params.toString();
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(investigationId)}/events${qs ? "?" + qs : ""}`, shapes.investigationEvents);
    const page: EventPage = { events: body.events.map(mapEventRow), total: body.total };
    if (body.next_cursor) page.nextCursor = body.next_cursor;
    if (q.groupBy !== undefined) {
      page.groups = (body.groups ?? []).map((g) => ({ classUid: g.class_uid, key: g.key.map((p) => ({ slot: p.slot, label: p.label, text: p.text, ref: p.ref })), count: g.count, first: g.first, last: g.last, days: g.days, eventIds: g.event_ids }));
      page.keys = (body.keys ?? []).map((k) => ({ classUid: k.class_uid, className: k.class_name, slots: k.slots, default: k.default, key: k.key }));
      page.foldedOver = body.folded_over ?? body.events.length;
    }
    return page;
  }

  /** GET /api/investigations/{id}/calls — the investigation's capability calls, oldest first. */
  async calls(investigationId: string): Promise<CapabilityCall[]> {
    const body = await this.http.get(`/api/investigations/${encodeURIComponent(investigationId)}/calls`, shapes.investigationCalls);
    return body.calls.map((c) => mapCall(c));
  }

  /** GET /api/investigations/{id}/calls/{call_id} — one call with what it produced and the verb's bindings. */
  async call(investigationId: string, callId: string): Promise<CapabilityCall> {
    return mapCall(await this.http.get(`/api/investigations/${encodeURIComponent(investigationId)}/calls/${encodeURIComponent(callId)}`, shapes.capabilityCall));
  }

  /** GET /api/evidence/{ref} — citation-open (02 §2.8). */
  async get(ref: string): Promise<EvidenceDoc> {
    const r = await this.http.get(`/api/evidence/${encodeURIComponent(ref)}`, shapes.evidence);
    return mapEvidence(r);
  }

  /**
   * GET /api/entities/{ref}/appearances — cross-investigation memory
   * (binding §6.1): every investigation whose thread cites the ref, powered by
   * deterministic identity. The entity popover's "appears in N other
   * investigations" list.
   */
  async appearances(ref: string): Promise<Appearance[]> {
    const body = await this.http.get(`/api/entities/${encodeURIComponent(ref)}/appearances`, shapes.appearances);
    return body.appearances.map((a) => ({
      investigationId: a.investigation_id,
      title: a.title,
      status: a.status,
      seedSummary: a.seed_summary,
      firstSeen: a.first_seen,
      lastSeen: a.last_seen,
      mentions: a.mentions,
    }));
  }
}
