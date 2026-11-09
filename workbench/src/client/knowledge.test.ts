import { describe, expect, it } from "vitest";
import type { BackendHttp } from "./http";
import { KnowledgeClient, nextDay } from "./knowledge";

/** An HTTP double: records each path and answers with the canned body. */
function client(body: unknown): { k: KnowledgeClient; paths: string[] } {
  const paths: string[] = [];
  const http = { get: async (path: string) => { paths.push(path); return body; } } as unknown as BackendHttp;
  return { k: new KnowledgeClient(http), paths };
}

describe("the past-case listing over HTTP (adr/0107)", () => {
  it("sends the filter as the route's parameters, the inclusive last day as the next day", async () => {
    const { k, paths } = client({ case_summaries: [], total: 0 });
    await k.pastCases({ techniques: ["T1078", "T1021.001"], disposition: "MALICIOUS", from: "2026-05-01", to: "2026-05-31" });
    expect(paths[0]).toBe("/api/knowledge/case_summaries?technique=T1078&technique=T1021.001&disposition=MALICIOUS&concluded_after=2026-05-01&concluded_before=2026-06-01");
    await k.pastCases({ techniques: [] });
    expect(paths[1]).toBe("/api/knowledge/case_summaries");
  });

  it("maps a row to its typed fields", async () => {
    const { k } = client({ total: 3, case_summaries: [{
      investigation_ref: "grouping--a", title: "RDP", generator_model: "m", verdict_disposition: "MALICIOUS", conclusion_at: "2026-05-14T18:20:00Z",
      techniques: ["T1078"], primary_entities: ["DC-02"], actions_taken: [{ action_type: "host.isolate", target_count: 1, outcome: "SUCCEEDED" }], duration_hours: 6.5, cited_in: 2,
    }] });
    expect(await k.pastCases({ techniques: [] })).toEqual({ total: 3, rows: [{
      ref: "grouping--a", title: "RDP", concludedAt: "2026-05-14T18:20:00Z", disposition: "MALICIOUS", techniques: ["T1078"], entities: ["DC-02"],
      actions: [{ actionType: "host.isolate", targetCount: 1, outcome: "SUCCEEDED" }], durationHours: 6.5, citedIn: 2, narrative: true,
    }] });
  });

  it("steps a day across a month and a year end", () => {
    expect(nextDay("2026-05-31")).toBe("2026-06-01");
    expect(nextDay("2026-12-31")).toBe("2027-01-01");
    expect(nextDay("2028-02-28")).toBe("2028-02-29");
  });
});
