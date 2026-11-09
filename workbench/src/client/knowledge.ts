// The knowledge library over HTTP (design/06, adr/0129): the procedures and
// past cases, one search over both corpora, one procedure whole, one case's
// summary, and the markdown import that grows the corpus. Answers are decoded
// through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import type { LibraryItem } from "../protocol";
import { relevanceOfBand, relevanceOfScore } from "../relevance";
import * as shapes from "../wire/backendShapes";
import type { Decoded } from "../wire/decode";
import type { CaseSummaryView, KnowledgeCitations, PastCaseFacets, PastCaseFilter, PastCaseRow, SOPDoc, SOPOutlineView, SOPRevisionRow, SOPSectionView } from "../viewTypes";

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

  /** GET /api/knowledge/case_summaries — the past cases in the library (adr/0129), unfiltered. */
  async cases(): Promise<LibraryItem[]> {
    return (await this.caseList()).items;
  }

  /** The unfiltered listing as library rows, and how many past cases the corpus holds beyond its page. */
  async caseList(): Promise<{ items: LibraryItem[]; total: number }> {
    const { rows, total } = await this.pastCases({ techniques: [] });
    return { total, items: rows.map((x) => ({
      kind: "case" as const, ref: x.ref, title: x.title || x.ref,
      detail: [x.disposition?.toLowerCase(), x.concludedAt ? "concluded " + x.concludedAt.slice(0, 10) : ""].filter(Boolean).join(" · ") || undefined,
      citedIn: x.citedIn,
    })) };
  }

  /**
   * GET /api/knowledge/case_summaries?technique=&disposition=&concluded_after=&concluded_before=
   * (adr/0107): the past cases matching the filter, newest first, and how many
   * matched. The analyst's "to" day is inclusive; the route's bound is
   * exclusive, so it is sent as the next day.
   */
  async pastCases(filter: PastCaseFilter): Promise<{ rows: PastCaseRow[]; total: number }> {
    const q = new URLSearchParams();
    for (const t of filter.techniques) q.append("technique", t);
    if (filter.disposition) q.set("disposition", filter.disposition);
    if (filter.from) q.set("concluded_after", filter.from);
    if (filter.to) q.set("concluded_before", nextDay(filter.to));
    const query = q.toString();
    const r = await this.http.get(`/api/knowledge/case_summaries${query ? "?" + query : ""}`, shapes.caseSummaries);
    return {
      total: r.total,
      rows: r.case_summaries.filter((x) => x.investigation_ref).map((x) => ({
        ref: x.investigation_ref, title: x.title, concludedAt: x.conclusion_at, disposition: x.verdict_disposition,
        techniques: x.techniques, entities: x.primary_entities,
        actions: x.actions_taken.map((a) => ({ actionType: a.action_type, targetCount: a.target_count, outcome: a.outcome })),
        durationHours: x.duration_hours, citedIn: x.cited_in, narrative: !!x.generator_model,
      })),
    };
  }

  /** GET /api/knowledge/case_summaries/facets — the techniques and dispositions present (adr/0107). */
  async caseFacets(): Promise<PastCaseFacets> {
    const r = await this.http.get("/api/knowledge/case_summaries/facets", shapes.caseFacets);
    return { techniques: r.techniques, dispositions: r.dispositions };
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
      revision: r.revision,
    };
  }

  /** GET /api/sops/{id}/revisions — every revision of a procedure, newest first, and which is current (adr/0097). */
  async revisions(id: string): Promise<{ current: number; revisions: SOPRevisionRow[] }> {
    const r = await this.http.get(`/api/sops/${encodeURIComponent(id)}/revisions`, shapes.sopRevisions);
    return {
      current: r.current,
      revisions: r.revisions.map((x) => ({
        revision: x.revision, contentHash: x.content_hash, createdAt: x.created_at, supersededAt: x.superseded_at,
        authoredBy: x.authored_by, importedBy: x.imported_by, importedAt: x.imported_at, current: x.current,
      })),
    };
  }

  /**
   * GET /api/sops/{id}/revisions/{r}/sections?cut= — one revision's outline
   * under a cut (the stored cut when none is given). Never 409: a cut the
   * store no longer holds is recomputed from the revision's body.
   */
  async revisionOutline(id: string, revision: number, cut?: string): Promise<SOPOutlineView> {
    const r = await this.http.get(`/api/sops/${encodeURIComponent(id)}/revisions/${revision}/sections${cutQuery(cut)}`, shapes.sopOutline);
    return { revision: r.revision, cut: r.cut, sections: r.sections.map((s) => ({ idx: s.idx, location: s.location, headingPath: s.heading_path, part: s.part, parts: s.parts })) };
  }

  /** GET /api/sops/{id}/revisions/{r}/sections/{n}?cut= — one section of one revision, as an act was shown it. */
  async revisionSection(id: string, revision: number, idx: number, cut?: string): Promise<SOPSectionView> {
    const r = await this.http.get(`/api/sops/${encodeURIComponent(id)}/revisions/${revision}/sections/${idx}${cutQuery(cut)}`, shapes.sopSection);
    return {
      revision: r.revision, cut: r.cut, idx: r.section.idx, of: r.section.of, location: r.section.location,
      headingPath: r.section.heading_path, part: r.section.part, parts: r.section.parts, text: r.section.text ?? "", outline: r.outline,
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

/** The ?cut= of a revision read; none when the record carries no cut. */
function cutQuery(cut: string | undefined): string {
  return cut ? `?cut=${encodeURIComponent(cut)}` : "";
}

/** The calendar day after a YYYY-MM-DD day, as YYYY-MM-DD (UTC). */
export function nextDay(day: string): string {
  const t = Date.parse(day + "T00:00:00Z");
  return Number.isNaN(t) ? day : new Date(t + 86_400_000).toISOString().slice(0, 10);
}
