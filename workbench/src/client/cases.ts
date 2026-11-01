// The case system of record over HTTP (adr/0030, 14 §4): the queue, one case,
// the tenant's configured queue, and the seed picker's search. Read-only and
// non-persisting: browsing is not evidence; a case is promoted once, when it
// seeds an investigation.
import type { BackendHttp } from "./http";
import type { CaseQueue, CaseRow, CaseSummary } from "../viewTypes";

interface RawCase {
  number: string;
  title?: string;
  status?: string;
  priority?: string;
  assignment_group?: string;
  summary?: string;
  link?: string;
  source?: string;
  opened_at?: string;
  investigation?: { id: string; title: string; status: string };
}

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
    const body = await this.http.get<{ queue?: string; cases?: RawCase[] }>("/api/cases" + (qs ? "?" + qs : ""));
    return { queue: body.queue ?? "", cases: (body.cases ?? []).map(mapCase) };
  }

  /** GET /api/cases/{number} — one case, read-only and non-persisting. */
  async get(number: string): Promise<CaseRow> {
    return mapCase(await this.http.get<RawCase>("/api/cases/" + encodeURIComponent(number)));
  }

  /**
   * GET /api/capabilities — the tenant's configured case queue
   * (`policies.case_queue`, 14 §4.2), or undefined when none is set or the
   * capability layer is off. The "From a case…" picker opens on it.
   */
  async configuredQueue(): Promise<string | undefined> {
    const body = await this.http.get<{ case_queue?: string }>("/api/capabilities");
    return typeof body.case_queue === "string" && body.case_queue ? body.case_queue : undefined;
  }

  /**
   * The seed picker's search (14-case-seed.md §4.1): the same non-persisting
   * queue surface the Cases view reads, narrowed by the filter, as summaries.
   */
  async search(filter?: string, queue?: string): Promise<CaseSummary[]> {
    const res = await this.queue({ filter, queue });
    return res.cases.map((c) => ({ number: c.number, title: c.title ?? "", status: c.status ?? "" }));
  }
}
