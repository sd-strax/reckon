import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { approvalFact } from "./ActionCard";

const row = (over: Partial<ActionRow>): ActionRow => ({
  actionId: "a", actionType: "host.isolate", tier: "T2", status: "REQUESTED", requiredMode: "MANUAL",
  targets: ["h"], pending: true, pendingLabel: "PENDING_MANUAL", expired: false, isReversal: false,
  tierEscalated: false, evidenceRefs: [], ...over,
});
const twoParty = (over: Partial<ActionRow>): ActionRow => row({ requiredMode: "TWO_PARTY", pendingLabel: "PENDING_TWO_PARTY", ...over });

// The Approval row states the human approval still needed, from what the list
// row carries (adr/0099); whether a policy matched is the record's to say.
describe("the approval fact on a card", () => {
  it("states the requirement on a manual request without a claim about policies", () => {
    expect(approvalFact(row({}))).toBe("needs your approval");
  });
  it("names who completes a two-party request, from the frozen pool", () => {
    expect(approvalFact(twoParty({ secondaryApproverPool: ["analyst-b", "analyst-c"] })))
      .toBe("two-party · a second approval from one of analyst-b, analyst-c");
  });
  it("asks for a second analyst when the policy named no pool, never a rotation the engine does not hold", () => {
    expect(approvalFact(twoParty({}))).toBe("two-party · a second analyst must also approve");
    expect(approvalFact(twoParty({ status: "PENDING_SECONDARY", primaryApproverRef: "analyst-a" }))).toBe("awaiting a second analyst's approval");
  });
  it("leaves the first approver out of who can complete it", () => {
    expect(approvalFact(twoParty({ status: "PENDING_SECONDARY", secondaryApproverPool: ["analyst-b", "analyst-c"], primaryApproverRef: "analyst-b" })))
      .toBe("awaiting a second approval from one of analyst-c");
  });
  it("says when no one in the pool is left to complete it", () => {
    expect(approvalFact(twoParty({ status: "PENDING_SECONDARY", secondaryApproverPool: ["analyst-b"], primaryApproverRef: "analyst-b" })))
      .toBe("no one left in the pool can complete it · its only member gave the first approval");
  });
  it("asks for the reader's approval on a row frozen AUTO_POLICY that is still pending, claiming nothing about why", () => {
    expect(approvalFact(row({ requiredMode: "AUTO_POLICY" }))).toBe("needs your approval");
  });
  it("keeps the settled words once decided", () => {
    expect(approvalFact(row({ status: "APPROVED", pending: false }))).toBe("approved");
    expect(approvalFact(row({ status: "REJECTED", pending: false }))).toBe("rejected");
    expect(approvalFact(row({ status: "EXPIRED", pending: false, expired: true }))).toBe("—");
  });
});
