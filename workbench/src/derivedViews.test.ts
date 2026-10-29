import { describe, expect, it } from "vitest";
import { buildDerivedViews } from "./derivedViews";
import type { EvidenceDoc, Hypothesis, ThreadEntry } from "./viewTypes";

// The road-test shape (#141): the thread lists only the records each act made;
// the evidence sits on a prediction's test results and on the seed.
const thread = [{ sequenceNo: 1, interpretationId: "i1", inputRefs: [], outputRefs: ["x-prediction--p"], actor: { kind: "AI_DELEGATED" } }] as unknown as ThreadEntry[];
const hypotheses = [{ id: "x-hypothesis--h", predictions: [{ id: "x-prediction--p", testResultRefs: ["observed-data--logon"] }] }] as unknown as Hypothesis[];
const docs: Record<string, EvidenceDoc> = {
  "observed-data--logon": { ref: "observed-data--logon", kind: "stix", type: "observed-data", payload: { first_observed: "2026-04-20T14:32:47Z", object_refs: ["x-host--win", "user-account--svc"] } } as unknown as EvidenceDoc,
  "observed-data--case": { ref: "observed-data--case", kind: "stix", type: "observed-data", payload: { first_observed: "2026-04-20T14:40:00Z", object_refs: ["x-host--win"] } } as unknown as EvidenceDoc,
};
const getDoc = async (ref: string) => docs[ref] ?? null;

describe("derived views over every citing record (#141)", () => {
  it("the timeline, entities and graph read evidence cited outside the thread", async () => {
    const v = await buildDerivedViews(thread, getDoc, { hypotheses, seed: { type: "case", caseRef: "observed-data--case" } });
    expect(v.strip.map((s) => s.ref)).toEqual(["observed-data--logon", "observed-data--case"]);
    expect(v.entities).toEqual([{ ref: "x-host--win", citations: 2 }, { ref: "user-account--svc", citations: 1 }]);
    expect(v.graph.edges).toContainEqual({ from: "x-prediction--p", to: "observed-data--logon", ai: false, join: undefined });
  });
  it("with the thread alone it stays as before", async () => {
    const v = await buildDerivedViews(thread, getDoc);
    expect(v.strip).toEqual([]);
  });
});
