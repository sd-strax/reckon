import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { canReverse } from "./actions";

const row = (over: Partial<ActionRow>) => ({ actionId: "o", actionType: "ioc.block", status: "SUCCEEDED", isReversal: false, ...over }) as ActionRow;

describe("reversing a best-effort action (#151)", () => {
  it("is offered, once, like a reversible one; never on an irreversible one", () => {
    const block = row({ reversibility: "BEST_EFFORT" });
    expect(canReverse(block, [block])).toBe(true);
    const attempted = row({ actionId: "r", actionType: "ioc.unblock", reversalOfRef: "o", status: "SUCCEEDED", isReversal: true });
    expect(canReverse(block, [block, attempted])).toBe(false); // a reversal was already attempted and took
    expect(canReverse(row({ reversibility: "IRREVERSIBLE" }), [])).toBe(false);
  });
});
