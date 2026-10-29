import { describe, expect, it } from "vitest";
import type { InvestigationSnapshot } from "../../../src/protocol";
import { received } from "./knowledgeUse";

const snap = {
  thread: [{ sequenceNo: 2, interpretationId: "t2", summary: "checked the procedure" }],
  turns: [{ sequenceNo: 2, occurredAt: "", body: [
    "[user] what does our SOP say?",
    '[tool_result recall_sops id=k1 error=false] {"results":[{"sop_id":"sop-1","title":"P","excerpt":"Confirm with the owner.","match_rationale":"seed host"}],"coverage":"COMPLETE"}',
    '[tool_result recall_similar_investigations id=k2 error=false] {"results":[{"investigation_ref":"grouping--a","band":"RELATED","excerpt":"Contained by isolation."}],"coverage":"COMPLETE"}',
  ].join("\n") }],
} as unknown as InvestigationSnapshot;

describe("what the agent was given (#132)", () => {
  it("reads it from the recall results in the committed turns, nothing else", () => {
    expect(received(snap, "sop", "sop-1")).toEqual([{ interpretationId: "t2", summary: "checked the procedure", excerpt: "Confirm with the owner.", band: undefined, score: undefined, rationale: "seed host" }]);
    expect(received(snap, "case", "grouping--a")[0]).toMatchObject({ band: "RELATED", excerpt: "Contained by isolation." });
    expect(received(snap, "sop", "sop-9")).toEqual([]);
  });
});
