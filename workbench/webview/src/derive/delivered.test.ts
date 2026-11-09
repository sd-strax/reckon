import { describe, expect, it } from "vitest";
import type { InvestigationSnapshot } from "../../../src/protocol";
import { delivered, deliveryKey } from "./delivered";

const snap = (lines: string[]) => ({
  thread: [{ sequenceNo: 2, interpretationId: "t2", summary: "read the procedure" }],
  turns: [{ sequenceNo: 2, occurredAt: "", body: ["[user] read it", ...lines].join("\n") }],
} as unknown as InvestigationSnapshot);

describe("what of a section reached the model (adr/0097)", () => {
  it("reads a recall hit as the excerpt, whole unless truncated", () => {
    const got = delivered(snap([
      '[tool_result recall_sops id=k1 error=false] {"results":[{"sop_id":"sop-1","revision":1,"cut":"1/16000","excerpt":"## Triage\\\\nConfirm.","section":{"idx":2}},{"sop_id":"sop-1","revision":1,"cut":"1/16000","excerpt":"## Long\\\\nFirst part","truncated":true,"section":{"idx":3}},{"sop_id":"sop-9","revision":1,"cut":"1/16000","excerpt":"x","section":{"idx":1}}]}',
    ]), "sop-1");
    expect(got.get(deliveryKey("t2", 1, "1/16000", 2))).toEqual({ text: "## Triage\nConfirm.", whole: true, via: "recall" });
    expect(got.get(deliveryKey("t2", 1, "1/16000", 3))).toEqual({ text: "## Long\nFirst part", whole: false, via: "recall" });
    expect(got.size).toBe(2);
  });

  it("reads a read_sop result as the section whole, and a paged one as the parts received", () => {
    const got = delivered(snap([
      '[tool_result read_sop id=r1 error=false] {"sop_id":"sop-1","revision":1,"cut":"1/16000","section":{"idx":2,"text":"## Triage\\\\nConfirm."},"outline":["A","B"]}',
      '[tool_result read_sop id=r2 error=false] {"sop_id":"sop-1","revision":1,"cut":"1/16000","section":{"idx":3},"text_parts":["aaa","bbb"],"result_bounded":{"result_id":"res-1","offset":0,"next_offset":2,"lists":{"text_parts":{"returned":2,"total":5}}}}',
      '[tool_result read_result id=r3 error=false] {"sop_id":"sop-1","revision":1,"cut":"1/16000","section":{"idx":3},"text_parts":["ccc"],"result_bounded":{"result_id":"res-1","offset":2,"next_offset":3,"lists":{"text_parts":{"returned":1,"total":5}}}}',
    ]), "sop-1");
    expect(got.get(deliveryKey("t2", 1, "1/16000", 2))).toEqual({ text: "## Triage\nConfirm.", whole: true, via: "read_sop" });
    expect(got.get(deliveryKey("t2", 1, "1/16000", 3))).toEqual({ text: "aaabbbccc", whole: false, via: "read_sop", parts: { got: 3, of: 5 } });
  });

  it("keeps the fuller delivery when a turn received a section twice", () => {
    const got = delivered(snap([
      '[tool_result recall_sops id=k1 error=false] {"results":[{"sop_id":"sop-1","revision":1,"cut":"1/16000","excerpt":"## Triage\\\\nConf","truncated":true,"section":{"idx":2}}]}',
      '[tool_result read_sop id=r1 error=false] {"sop_id":"sop-1","revision":1,"cut":"1/16000","section":{"idx":2,"text":"## Triage\\\\nConfirm."}}',
    ]), "sop-1");
    expect(got.get(deliveryKey("t2", 1, "1/16000", 2))).toMatchObject({ whole: true, via: "read_sop" });
  });

  it("ignores results that do not name the tuple, and an outline read", () => {
    const got = delivered(snap([
      '[tool_result recall_sops id=k1 error=false] {"results":[{"sop_id":"sop-1","excerpt":"old shape"}]}',
      '[tool_result read_sop id=r1 error=false] {"sop_id":"sop-1","revision":1,"cut":"1/16000","sections":[{"idx":1}]}',
      "[tool_result read_sop id=r2 error=true] boom",
    ]), "sop-1");
    expect(got.size).toBe(0);
    expect(delivered(null, "sop-1").size).toBe(0);
  });
});
