// The knowledge library over HTTP (design/06, adr/0129): the procedures and
// past cases, one search over both corpora, one procedure whole, one case's
// summary, and the markdown import that grows the corpus. Answers are decoded
// through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import type { LibraryItem } from "../protocol";
import { relevanceOfBand, relevanceOfScore } from "../relevance";
import * as shapes from "../wire/backendShapes";
import type { Decoded } from "../wire/decode";
import type { CaseSummaryView, KnowledgeCitations, SOPDoc } from "../viewTypes";

function citationsView(r: Decoded<typeof shapes.citations>): KnowledgeCitations {
  return {
    ref: r.ref, citedIn: r.cited_in, recalledIn: r.recalled_in,
    investigations: r.investigations.map((i) => ({
      investigationId: i.investigation_id, title: i.title, status: i.status, cited: i.cited,
      lastAt: i.last_at, acts: i.acts, revisions: i.revisions,
    })),
  };
}

export class KnowledgeClient {
  constructor(private readonly http: BackendHttp) {}

  /** GET /api/sops — the procedures in the library (adr/0129). */
  async procedures(): Promise<LibraryItem[]> {
    const r = await this.http.get("/api/sops", shapes.sops);
    return r.sops.filter((x) => x.id).map((x) => ({ kind: "sop" as const, ref: x.id, title: x.title || x.id, detail: x.tags.join(", ") || undefined, citedIn: x.cited_in }));
  }

  /** GET /api/knowledge/case_summaries — the past cases in the library (adr/0129). */
  async cases(): Promise<LibraryItem[]> {
    const r = await this.http.get("/api/knowledge/case_summaries", shapes.caseSummaries);
    return r.case_summaries.filter((x) => x.investigation_ref).map((x) => ({
      kind: "case" as const, ref: x.investigation_ref, title: x.title || x.investigation_ref,
      detail: [x.verdict_disposition?.toLowerCase(), x.conclusion_at ? "concluded " + x.conclusion_at.slice(0, 10) : ""].filter(Boolean).join(" · ") || undefined,
      citedIn: x.cited_in,
    }));
  }

  /**
   * GET /api/sops/{id}/citations — the investigations that consulted a
   * procedure (adr/0109), one row each, "cited" or "recalled, not cited".
   */
  async sopCitations(id: string): Promise<KnowledgeCitations> {
    return citationsView(await this.http.get(`/api/sops/${encodeURIComponent(id)}/citations`, shapes.citations));
  }

  /** GET /api/knowledge/case_summaries/{ref}/citations — the same for a past case. */
  async caseCitations(investigationRef: string): Promise<KnowledgeCitations> {
    return citationsView(await this.http.get(`/api/knowledge/case_summaries/${encodeURIComponent(investigationRef)}/citations`, shapes.citations));
  }

  /** Search both corpora (adr/0129): the substrate's recall, relevance in words. */
  async search(query: string): Promise<{ sops: LibraryItem[]; cases: LibraryItem[] }> {
    const [s, c] = await Promise.all([
      this.http.post("/api/knowledge/recall_sops", { query, limit: 10 }, shapes.recallSOPs),
      this.http.post("/api/knowledge/recall_similar_investigations", { query, limit: 10 }, shapes.recallSimilar),
    ]);
    return {
      sops: s.results.filter((x) => x.sop_id).map((x) => ({ kind: "sop" as const, ref: x.sop_id, title: x.title || x.sop_id, relevance: relevanceOfScore(x.score) })),
      cases: c.results.filter((x) => x.investigation_ref).map((x) => ({ kind: "case" as const, ref: x.investigation_ref, title: x.title || x.investigation_ref, relevance: relevanceOfBand(x.band) })),
    };
  }

  /** GET /api/sops/{id} — one procedure, whole (08 §8.6 "Whole document"). */
  async sop(id: string): Promise<SOPDoc> {
    const r = await this.http.get(`/api/sops/${encodeURIComponent(id)}`, shapes.sop);
    return {
      id: r.id, title: r.title, body: r.body, tags: r.tags, status: r.status,
      recommendation: r.recommendation, authorId: r.author_id,
      source: r.source ? { system: r.source.system, url: r.source.url, version: r.source.version } : undefined,
      importedBy: r.imported_by, importedAt: r.imported_at, createdAt: r.created_at, updatedAt: r.updated_at,
    };
  }

  /** GET /api/knowledge/case_summaries/{ref} — one concluded case's summary; null when none exists. */
  async caseSummary(investigationRef: string): Promise<CaseSummaryView | null> {
    let r: Decoded<typeof shapes.caseSummary>;
    try {
      r = await this.http.get(`/api/knowledge/case_summaries/${encodeURIComponent(investigationRef)}`, shapes.caseSummary);
    } catch (err) {
      if (err instanceof Error && /\b404\b/.test(err.message)) return null;
      throw err;
    }
    return {
      investigationRef: r.investigation_ref, title: r.title, body: r.body,
      generatorModel: r.generator_model, conclusionAt: r.conclusion_at, seedKind: r.seed_kind, seedValue: r.seed_value,
      techniques: r.techniques, primaryEntities: r.primary_entities,
      actionsTaken: r.actions_taken.map((a) => ({ actionType: a.action_type, targetCount: a.target_count, outcome: a.outcome })),
      conclusionOutcome: r.conclusion_outcome, durationHours: r.duration_hours, verdictDisposition: r.verdict_disposition,
      verdictRationale: r.verdict_rationale, tags: r.tags, revision: r.revision, contentHash: r.content_hash,
      learnedProcedures: r.learned_procedures.map((p) => ({ id: p.id, title: p.title })),
    };
  }

  /**
   * Import one institutional-knowledge document into the SOP corpus (design/06
   * §2.4): the raw markdown + its filename go to the server, which parses
   * frontmatter and upserts (re-import of the same source revises in place).
   * Parsing stays server-side — one robust parser, never a TS reimplementation.
   */
  async importMarkdown(filename: string, content: string): Promise<{ title: string; outcome: string }> {
    const r = await this.http.post("/api/sops/import_markdown", { filename, content }, shapes.sopImported);
    return { title: r.title, outcome: r.outcome };
  }
}
