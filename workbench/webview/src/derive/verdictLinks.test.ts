import { describe, expect, it } from "vitest";
import type { ThreadEntry } from "../../../src/viewTypes";
import { verdictLinks } from "./verdict";

const v = (seq: number, id: string, disposition: string, superseded: boolean) =>
  ({ sequenceNo: seq, interpretationId: id, interpretationType: "verdict", disposition, superseded, occurredAt: "" }) as unknown as ThreadEntry;

describe("verdict links (#155)", () => {
  it("pairs each replaced verdict with the next one, both ways", () => {
    const links = verdictLinks([v(38, "a", "SUSPICIOUS", true), { sequenceNo: 40, interpretationType: "pivot" } as ThreadEntry, v(144, "b", "MALICIOUS", false)]);
    expect(links.get("a")?.replacedBy?.interpretationId).toBe("b");
    expect(links.get("b")?.replaces?.interpretationId).toBe("a");
    expect(verdictLinks([v(1, "only", "BENIGN", false)]).size).toBe(0);
  });
});
