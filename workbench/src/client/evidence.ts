// Evidence over HTTP: one cited object with what the record holds around it
// (02 §2.8), and where an entity appears across investigations (binding §6.1).
import type { BackendHttp } from "./http";
import type { Appearance, EvidenceDoc } from "../viewTypes";

interface RawEvidence {
  ref?: string; kind?: string; type?: string; payload?: unknown;
  class_uid?: number; time?: string; recorded_at?: string; source_tool?: string;
  related?: { sightings?: RawEvidence[]; identity?: RawEvidence | null; evidence?: RawEvidence[] };
}

function mapEvidence(r: RawEvidence, ref: string): EvidenceDoc {
  const doc: EvidenceDoc = {
    ref: r.ref ?? ref,
    kind: r.kind ?? "",
    type: r.type ?? "",
    payload: r.payload,
    classUid: r.class_uid,
    time: r.time,
    recordedAt: r.recorded_at,
    sourceTool: r.source_tool,
  };
  if (r.related) {
    doc.related = {
      sightings: (r.related.sightings ?? []).map((s) => mapEvidence(s, s.ref ?? "")),
      identity: r.related.identity ? mapEvidence(r.related.identity, r.related.identity.ref ?? "") : null,
      evidence: (r.related.evidence ?? []).map((e) => mapEvidence(e, e.ref ?? "")),
    };
  }
  return doc;
}

export class EvidenceClient {
  constructor(private readonly http: BackendHttp) {}

  /** GET /api/evidence/{ref} — citation-open (02 §2.8). */
  async get(ref: string): Promise<EvidenceDoc> {
    const r = await this.http.get<RawEvidence>(`/api/evidence/${encodeURIComponent(ref)}`);
    return mapEvidence(r, ref);
  }

  /**
   * GET /api/entities/{ref}/appearances — cross-investigation memory
   * (binding §6.1): every investigation whose thread cites the ref, powered by
   * deterministic identity. The entity inspector's "appears in N other
   * investigations" list.
   */
  async appearances(ref: string): Promise<Appearance[]> {
    interface Raw {
      investigation_id?: string; title?: string; status?: string;
      seed_summary?: string; first_seen?: string; last_seen?: string; mentions?: number;
    }
    const body = await this.http.get<{ appearances?: Raw[] }>(
      `/api/entities/${encodeURIComponent(ref)}/appearances`,
    );
    return (body.appearances ?? []).map((a) => ({
      investigationId: a.investigation_id ?? "",
      title: a.title ?? "",
      status: a.status ?? "",
      seedSummary: a.seed_summary,
      firstSeen: a.first_seen,
      lastSeen: a.last_seen,
      mentions: a.mentions ?? 0,
    }));
  }
}
