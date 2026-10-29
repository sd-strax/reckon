import { describe, expect, it } from "vitest";
import type { InvestigationSnapshot } from "../../../src/protocol";
import { firstStep, isFresh } from "./firstStep";

const snap = (over: Partial<InvestigationSnapshot> = {}, inv: Record<string, unknown> = {}) => ({
  seq: 1, investigation: { id: "i", title: "host WIN-FILE01", state: "DRAFT", lastEventSequence: 1, seed: { type: "entity", entityIdentifier: "WIN-FILE01" }, ...inv },
  hypotheses: [], actions: [], pins: [], comms: [], thread: [{ interpretationType: "lifecycle", hasTranscript: false }], turns: [],
  capabilities: null, enablement: null, knowledge: [], derived: null, ...over,
}) as unknown as InvestigationSnapshot;

describe("a new investigation's first step (#160)", () => {
  it("is shown until anything has happened", () => {
    expect(isFresh(snap())).toBe(true);
    expect(isFresh(snap({ thread: [{ interpretationType: "other", hasTranscript: true }] as never }))).toBe(false);
    expect(isFresh(snap({ hypotheses: [{}] as never }))).toBe(false);
    expect(isFresh(snap({}, { state: "CONCLUDED" }))).toBe(false);
  });
  it("names what it is rooted on and stages a request to start", () => {
    expect(firstStep(snap())).toEqual({ rootedOn: "WIN-FILE01", ask: "Investigate WIN-FILE01: pull the logon, process and network telemetry and tell me what happened." });
    expect(firstStep(snap({}, { seed: { type: "case", caseId: "INC0010042" } })).rootedOn).toBe("case INC0010042");
  });
});
