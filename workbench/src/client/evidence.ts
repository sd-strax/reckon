// Evidence over HTTP: one cited object with what the record holds around it
// (02 §2.8), and where an entity appears across investigations (binding §6.1).
// Answers are decoded through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawEvidence } from "../wire/backendShapes";
import type { Appearance, EvidenceDoc } from "../viewTypes";

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
  if (r.related) {
    doc.related = {
      sightings: (r.related.sightings ?? []).map((s) => mapEvidence(s)),
      identity: r.related.identity ? mapEvidence(r.related.identity) : null,
      evidence: (r.related.evidence ?? []).map((e) => mapEvidence(e)),
    };
  }
  return doc;
}

export class EvidenceClient {
  constructor(private readonly http: BackendHttp) {}

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
