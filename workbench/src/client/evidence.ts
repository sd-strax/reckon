// Evidence over HTTP: one cited object with what the record holds around it
// (02 §2.8), and where an entity appears across investigations (binding §6.1).
// Answers are decoded through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawAccountIdentity, RawEvidence, RawEventView, RawHostIdentity, RawPersonIdentity } from "../wire/backendShapes";
import type { Decoded } from "../wire/decode";
import type { AccountIdentity, Appearance, CutEdge, CapabilityCall, EventClassKeys, EventGroup, EventRow, EventView, EvidenceDoc, HostIdentity, IdentityEdge, IdentityViews, PersonIdentity } from "../viewTypes";
import { identityKindOf } from "../identityKinds";
import type { IdentityOther } from "../protocol";

type RawEdge = RawHostIdentity["aliases"][number];

/** An edge's origin as decoded; an unknown kind reads as a source's record. */
const originKind = (k: string) => (k === "asserted" ? "asserted" as const : "observed" as const);

function mapEdge(e: RawEdge): IdentityEdge {
  return {
    edgeId: e.edge_id, otherRef: e.other_ref, kind: originKind(e.kind),
    source: e.source, by: e.by, byName: e.by_name, at: e.at, note: e.note, withdrawn: e.withdrawn,
  };
}

/** A host's identity view as decoded (adr/0211). */
export function mapIdentity(r: RawHostIdentity): HostIdentity {
  return {
    aliases: r.aliases.map(mapEdge),
    distinct: r.distinct.map(mapEdge),
    seenAs: { domains: r.seen_as.domains, agents: r.seen_as.agents.map((a) => ({ tool: a.tool, uid: a.uid })), ips: r.seen_as.ips },
  };
}

/** The cutting edge a cut row names, when the wire carries all of it. */
function cutBy(r: {
  cut: boolean; cut_edge_id?: string; cut_account_ref?: string; cut_person_ref?: string;
  cut_by?: string; cut_by_name?: string; cut_at?: string; cut_note?: string;
}): { cutBy?: CutEdge } {
  return r.cut && r.cut_edge_id && r.cut_account_ref && r.cut_person_ref
    ? { cutBy: { edgeId: r.cut_edge_id, accountRef: r.cut_account_ref, personRef: r.cut_person_ref, by: r.cut_by, byName: r.cut_by_name, at: r.cut_at, note: r.cut_note } }
    : {};
}

/** An account's identity view as decoded (adr/0324). */
export function mapAccountIdentity(r: RawAccountIdentity): AccountIdentity {
  return {
    aliases: r.aliases.map(mapEdge),
    distinct: r.distinct.map(mapEdge),
    seenAs: { logins: r.seen_as.logins, ids: r.seen_as.ids.map((i) => ({ tool: i.tool, uid: i.uid })), principalNames: r.seen_as.principal_names },
    owners: r.owners.map((o) => ({
      personRef: o.person_ref, accountRef: o.account_ref, edgeId: o.edge_id, kind: originKind(o.kind),
      source: o.source, by: o.by, byName: o.by_name, at: o.at, note: o.note, withdrawn: o.withdrawn, cut: o.cut,
      ...cutBy(o),
    })),
  };
}

/** A person's identity view as decoded (adr/0324). */
export function mapPersonIdentity(r: RawPersonIdentity): PersonIdentity {
  return {
    aliases: r.aliases.map(mapEdge),
    distinct: r.distinct.map(mapEdge),
    accounts: r.accounts.map((a) => ({
      accountRef: a.account_ref, personRef: a.person_ref, accountAliases: a.account_aliases.length ? a.account_aliases : [a.account_ref], edgeId: a.edge_id, kind: originKind(a.kind),
      source: a.source, by: a.by, byName: a.by_name, at: a.at, note: a.note, withdrawn: a.withdrawn, cut: a.cut,
      ...cutBy(a),
    })),
  };
}

/**
 * An identity act's answer as the view of `ref`, the entity whose view was
 * asked for, in the field of its kind; null when the server could not read
 * it (the act is recorded all the same).
 */
function actViews(r: Decoded<typeof shapes.identityAct>, ref: string): IdentityViews | null {
  switch (identityKindOf(ref)) {
    case "account": return r.account_identity ? { accountIdentity: mapAccountIdentity(r.account_identity) } : null;
    case "person": return r.person_identity ? { personIdentity: mapPersonIdentity(r.person_identity) } : null;
    default: return r.identity ? { identity: mapIdentity(r.identity) } : null;
  }
}

/** The two acts that join or split two entities (adr/0211, adr/0324): "same as" and "not the same". */
export type IdentityRelation = "aliases" | "distinct";

/**
 * The other end of a join or a split (protocol's IdentityOther): by ref, by a
 * hostname (a host), or by a hint the server looks the account or the person
 * up by (a login, a principal name, a mail address, a directory id).
 */
export type { IdentityOther };

/** The person an account is asserted to belong to: by ref, or by a hint. */
export type OwnerPerson = { personRef: string } | { personHint: string };

/** The assert body for the other end; a bare string is a host's, as otherHostBody reads it. */
export function otherBody(other: IdentityOther | string, note?: string): Record<string, string> {
  if (typeof other === "string") return otherHostBody(other, note);
  const body: Record<string, string> = "ref" in other ? { other_ref: other.ref.trim() }
    : "hostname" in other ? { other_hostname: other.hostname.trim() } : { other_hint: other.hint.trim() };
  if (note && note.trim()) body.note = note.trim();
  return body;
}

/**
 * The other host of a join or a split, as the analyst typed it: a host ref
 * (`x-host--…`) goes as `other_ref`, anything else as a hostname the server
 * mints as the seed does (a hostname may itself hold "--", as punycode does).
 */
export function otherHostBody(other: string, note?: string): Record<string, string> {
  const v = other.trim();
  const body: Record<string, string> = v.toLowerCase().startsWith("x-host--") ? { other_ref: v } : { other_hostname: v };
  if (note && note.trim()) body.note = note.trim();
  return body;
}

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
  if (r.identity) doc.identity = mapIdentity(r.identity);
  if (r.account_identity) doc.accountIdentity = mapAccountIdentity(r.account_identity);
  if (r.person_identity) doc.personIdentity = mapPersonIdentity(r.person_identity);
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

  /**
   * POST /api/entities/{ref}/aliases | /distinct — the analyst's "same as"
   * or "not the same" on a host, an account or a person (adr/0211,
   * adr/0324), and "this account is not this person's", the one split of an
   * account and a person. Resolving means the act is recorded; the answer is
   * the view of `ref` after it, in the field of its kind, or null when the
   * view could not be read. Human only: the server refuses a delegate. A
   * string `other` is a host's name or ref (otherHostBody).
   */
  async assertIdentity(ref: string, relation: IdentityRelation, other: IdentityOther | string, note?: string): Promise<IdentityViews | null> {
    const r = await this.http.post(`/api/entities/${encodeURIComponent(ref)}/${relation}`, otherBody(other, note), shapes.identityAct);
    return actViews(r, ref);
  }

  /**
   * POST /api/entities/{ref}/owner — the account at ref belongs to the
   * person named (adr/0324), by ref or by a hint. Answers the account's view
   * after it, or null when it could not be read.
   */
  async assertOwner(accountRef: string, person: OwnerPerson, note?: string): Promise<IdentityViews | null> {
    const body: Record<string, string> = "personRef" in person ? { person_ref: person.personRef.trim() } : { person_hint: person.personHint.trim() };
    if (note && note.trim()) body.note = note.trim();
    const r = await this.http.post(`/api/entities/${encodeURIComponent(accountRef)}/owner`, body, shapes.identityAct);
    return actViews(r, accountRef);
  }

  /**
   * POST /api/identity/{edge_id}/withdraw — withdraw an asserted edge
   * (adr/0211), answering the identity view of `ref`, one of the edge's
   * ends, or null when it could not be read.
   */
  async withdrawIdentity(edgeId: string, ref: string, note?: string): Promise<IdentityViews | null> {
    const body: Record<string, string> = { ref };
    if (note && note.trim()) body.note = note.trim();
    const r = await this.http.post(`/api/identity/${encodeURIComponent(edgeId)}/withdraw`, body, shapes.identityAct);
    return actViews(r, ref);
  }
}
