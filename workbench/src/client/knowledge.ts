// The knowledge library over HTTP (design/06, adr/0129): the procedures and
// past cases, one search over both corpora, one procedure whole, one case's
// summary, and the markdown import that grows the corpus.
import type { BackendHttp } from "./http";
import type { LibraryItem } from "../protocol";
import { relevanceOfBand, relevanceOfScore } from "../relevance";
import type { CaseSummaryView, SOPDoc } from "../viewTypes";

export class KnowledgeClient {
  constructor(private readonly http: BackendHttp) {}

  /** GET /api/sops — the procedures in the library (adr/0129). */
  async procedures(): Promise<LibraryItem[]> {
    const r = await this.http.get<{ sops?: { id?: string; title?: string; tags?: string[] }[] }>("/api/sops");
    return (r.sops ?? []).filter((x) => x.id).map((x) => ({ kind: "sop" as const, ref: x.id!, title: x.title || x.id!, detail: (x.tags ?? []).join(", ") || undefined }));
  }

  /** GET /api/knowledge/case_summaries — the past cases in the library (adr/0129). */
  async cases(): Promise<LibraryItem[]> {
    const r = await this.http.get<{ case_summaries?: { investigation_ref?: string; title?: string; verdict_disposition?: string; conclusion_at?: string }[] }>("/api/knowledge/case_summaries");
    return (r.case_summaries ?? []).filter((x) => x.investigation_ref).map((x) => ({
      kind: "case" as const, ref: x.investigation_ref!, title: x.title || x.investigation_ref!,
      detail: [x.verdict_disposition?.toLowerCase(), x.conclusion_at ? "concluded " + x.conclusion_at.slice(0, 10) : ""].filter(Boolean).join(" · ") || undefined,
    }));
  }

  /** Search both corpora (adr/0129): the substrate's recall, relevance in words. */
  async search(query: string): Promise<{ sops: LibraryItem[]; cases: LibraryItem[] }> {
    const [s, c] = await Promise.all([
      this.http.post<{ results?: { sop_id?: string; title?: string; score?: number }[] }>("/api/knowledge/recall_sops", { query, limit: 10 }),
      this.http.post<{ results?: { investigation_ref?: string; title?: string; band?: string }[] }>("/api/knowledge/recall_similar_investigations", { query, limit: 10 }),
    ]);
    return {
      sops: (s.results ?? []).filter((x) => x.sop_id).map((x) => ({ kind: "sop" as const, ref: x.sop_id!, title: x.title || x.sop_id!, relevance: relevanceOfScore(x.score) })),
      cases: (c.results ?? []).filter((x) => x.investigation_ref).map((x) => ({ kind: "case" as const, ref: x.investigation_ref!, title: x.title || x.investigation_ref!, relevance: relevanceOfBand(x.band) })),
    };
  }

  /** GET /api/sops/{id} — one procedure, whole (08 §8.6 "Whole document"). */
  async sop(id: string): Promise<SOPDoc> {
    interface Raw {
      id?: string; title?: string; body?: string; tags?: string[]; status?: string; recommendation?: string;
      author_id?: string; source?: { system?: string; url?: string; version?: string }; imported_by?: string;
      imported_at?: string; created_at?: string; updated_at?: string;
    }
    const r = await this.http.get<Raw>(`/api/sops/${encodeURIComponent(id)}`);
    return {
      id: r.id ?? id, title: r.title ?? "", body: r.body ?? "", tags: r.tags ?? [], status: r.status ?? "",
      recommendation: r.recommendation, authorId: r.author_id,
      source: r.source ? { system: r.source.system, url: r.source.url ?? "", version: r.source.version } : undefined,
      importedBy: r.imported_by, importedAt: r.imported_at, createdAt: r.created_at, updatedAt: r.updated_at,
    };
  }

  /** GET /api/knowledge/case_summaries/{ref} — one concluded case's summary; null when none exists. */
  async caseSummary(investigationRef: string): Promise<CaseSummaryView | null> {
    interface Raw {
      investigation_ref?: string; title?: string; body?: string; generator_model?: string; conclusion_at?: string;
      seed_kind?: string; seed_value?: string; techniques?: string[]; primary_entities?: string[];
      actions_taken?: { action_type?: string; target_count?: number; outcome?: string }[];
      conclusion_outcome?: string; duration_hours?: number; verdict_disposition?: string; verdict_rationale?: string;
      tags?: string[]; revision?: number; content_hash?: string;
    }
    let r: Raw;
    try {
      r = await this.http.get<Raw>(`/api/knowledge/case_summaries/${encodeURIComponent(investigationRef)}`);
    } catch (err) {
      if (err instanceof Error && /\b404\b/.test(err.message)) return null;
      throw err;
    }
    return {
      investigationRef: r.investigation_ref ?? investigationRef, title: r.title ?? "", body: r.body ?? "",
      generatorModel: r.generator_model, conclusionAt: r.conclusion_at, seedKind: r.seed_kind, seedValue: r.seed_value,
      techniques: r.techniques ?? [], primaryEntities: r.primary_entities ?? [],
      actionsTaken: (r.actions_taken ?? []).map((a) => ({ actionType: a.action_type ?? "", targetCount: a.target_count ?? 0, outcome: a.outcome ?? "" })),
      conclusionOutcome: r.conclusion_outcome, durationHours: r.duration_hours, verdictDisposition: r.verdict_disposition,
      verdictRationale: r.verdict_rationale, tags: r.tags ?? [], revision: r.revision ?? 0, contentHash: r.content_hash,
    };
  }

  /**
   * Import one institutional-knowledge document into the SOP corpus (design/06
   * §2.4): the raw markdown + its filename go to the server, which parses
   * frontmatter and upserts (re-import of the same source revises in place).
   * Parsing stays server-side — one robust parser, never a TS reimplementation.
   */
  async importMarkdown(filename: string, content: string): Promise<{ title: string; outcome: string }> {
    const r = await this.http.post<{ title?: string; outcome?: string }>(
      "/api/sops/import_markdown",
      { filename, content },
    );
    return { title: r.title ?? filename, outcome: r.outcome ?? "ok" };
  }
}
