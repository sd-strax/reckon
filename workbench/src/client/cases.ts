// The case system of record over HTTP (adr/0030, 14 §4): the queue, one case,
// the tenant's configured queue, and the seed picker's search. Read-only and
// non-persisting: browsing is not evidence; a case is promoted once, when it
// seeds an investigation. Answers are decoded through the wire shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { RawCase } from "../wire/backendShapes";
import type { CaseQueue, CaseRow, CaseSummary } from "../viewTypes";

function mapCase(r: RawCase): CaseRow {
  return {
    number: r.number,
    title: r.title,
    status: r.status,
    priority: r.priority,
    assignmentGroup: r.assignment_group,
    summary: r.summary,
    link: r.link,
    source: r.source,
    openedAt: r.opened_at,
    investigation: r.investigation,
  };
}

export class CasesClient {
  constructor(private readonly http: BackendHttp) {}

  /**
   * GET /api/cases — the case queue, read-only and non-persisting
   * (adr/0030-cases-pane.md). queue defaults server-side to the tenant's
   * configured one (14 §4.2); filter narrows within it. Each row carries the
   * open investigation already rooted on it, if any.
   */
  async queue(opts: { filter?: string; queue?: string } = {}): Promise<CaseQueue> {
    const q = new URLSearchParams();
    if (opts.filter) q.set("filter", opts.filter);
    if (opts.queue) q.set("queue", opts.queue);
    const qs = q.toString();
    const body = await this.http.get("/api/cases" + (qs ? "?" + qs : ""), shapes.cases);
    return { queue: body.queue ?? "", cases: body.cases.map(mapCase) };
  }

  /** GET /api/cases/{number} — one case, read-only and non-persisting. */
  async get(number: string): Promise<CaseRow> {
    return mapCase(await this.http.get("/api/cases/" + encodeURIComponent(number), shapes.case));
  }

  /**
   * GET /api/capabilities — the tenant's configured case queue
   * (`policies.case_queue`, 14 §4.2), or undefined when none is set or the
   * capability layer is off. The "From a case…" picker opens on it.
   */
  async configuredQueue(): Promise<string | undefined> {
    const body = await this.http.get("/api/capabilities", shapes.capabilities);
    return body.case_queue || undefined;
  }

  /**
   * Query the case system of record for the seed picker (14-case-seed.md §4.1):
   * runs query_external_cases and pulls each case's metadata out of the
   * class-2005 ObservedData extensions the case normalizer wrote (03 §2.9).
   */
  async search(filter?: string, queue?: string): Promise<CaseSummary[]> {
    // The picker reads the same non-persisting queue surface the Cases view
    // does (adr/0030): browsing is not evidence; the case is promoted once,
    // when it seeds an investigation.
    const res = await this.queue({ filter, queue });
    return res.cases.map((c) => ({ number: c.number, title: c.title ?? "", status: c.status ?? "" }));
  }
}
