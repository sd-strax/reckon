import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { approvalFact } from "./ActionCard";

const row = (over: Partial<ActionRow>): ActionRow => ({
  actionId: "a", actionType: "host.isolate", tier: "T2", status: "REQUESTED", requiredMode: "MANUAL",
  targets: ["h"], pending: true, pendingLabel: "PENDING_MANUAL", expired: false, isReversal: false,
  tierEscalated: false, evidenceRefs: [], ...over,
});

// The Approval row states the frozen Gate 2 requirement (adr/0099): what the
// list row carries, in the sheet's words; nothing is inferred.
describe("the approval fact on a card", () => {
  it("says no policy pre-authorizes a manual request", () => {
    expect(approvalFact(row({}))).toBe("needs your approval · no policy pre-authorizes it");
  });
  it("names who completes a two-party request, from the frozen pool", () => {
    expect(approvalFact(row({ requiredMode: "TWO_PARTY", pendingLabel: "PENDING_TWO_PARTY", secondaryApproverPool: ["analyst-b", "analyst-c"] })))
      .toBe("two-party · a second approval from one of analyst-b, analyst-c");
  });
  it("falls back to the on-call rotation when the policy named no pool", () => {
    expect(approvalFact(row({ requiredMode: "TWO_PARTY", pendingLabel: "PENDING_TWO_PARTY" })))
      .toBe("two-party · a second approval from the on-call rotation");
  });
  it("says a half-approved request awaits its second approver", () => {
    expect(approvalFact(row({ status: "PENDING_SECONDARY", requiredMode: "TWO_PARTY", pendingLabel: "PENDING_TWO_PARTY", secondaryApproverPool: ["analyst-b"] })))
      .toBe("awaiting a second approval from one of analyst-b");
  });
  it("keeps the settled words once decided", () => {
    expect(approvalFact(row({ status: "APPROVED", pending: false }))).toBe("approved");
    expect(approvalFact(row({ status: "REJECTED", pending: false }))).toBe("rejected");
    expect(approvalFact(row({ status: "EXPIRED", pending: false, expired: true }))).toBe("—");
  });
});
