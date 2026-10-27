import { describe, expect, it } from "vitest";
import { rendererFor, targetKey, targetName } from "./inspect";
import { inThisCase, isEmpty } from "./citations";
import type { InvestigationSnapshot } from "../../../src/protocol";
import synthetic from "../../../harness/snapshots/synthetic.json";

const snap = (synthetic as unknown as { snapshot: InvestigationSnapshot }).snapshot;

describe("rendererFor", () => {
  it("resolves a target from the id's shape", () => {
    expect(rendererFor({ kind: "ref", ref: "x-null-observation--1" })).toBe("absence");
    expect(rendererFor({ kind: "ref", ref: "indicator--1" })).toBe("detection");
    expect(rendererFor({ kind: "ref", ref: "sighting--1" })).toBe("detection");
    expect(rendererFor({ kind: "ref", ref: "note--1" })).toBe("attestation");
    expect(rendererFor({ kind: "ref", ref: "x-prediction--1" })).toBe("prediction");
    expect(rendererFor({ kind: "ref", ref: "x-hypothesis--1" })).toBe("hypothesis");
    expect(rendererFor({ kind: "ref", ref: "x-host--1" })).toBe("entity");
    expect(rendererFor({ kind: "ref", ref: "user-account--1" })).toBe("entity");
    expect(rendererFor({ kind: "ref", ref: "observed-data--1" })).toBe("evidence");
    expect(rendererFor({ kind: "ref", ref: "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8" })).toBe("evidence");
    expect(rendererFor({ kind: "sop", sopId: "s" })).toBe("procedure");
    expect(rendererFor({ kind: "case", investigationRef: "g" })).toBe("case");
    expect(rendererFor({ kind: "person", principalId: "p" })).toBe("person");
  });
  it("treats a relationship as an alias only when the edge says so", () => {
    const t = { kind: "ref" as const, ref: "relationship--1" };
    expect(rendererFor(t)).toBe("evidence");
    expect(rendererFor(t, { ref: t.ref, kind: "stix", type: "relationship", payload: { relationship_type: "aliases" } })).toBe("alias");
    expect(rendererFor(t, { ref: t.ref, kind: "stix", type: "relationship", payload: { relationship_type: "related-to" } })).toBe("evidence");
  });
  it("keys targets stably", () => {
    expect(targetKey({ kind: "ref", ref: "a" })).toBe("ref:a");
    expect(targetKey({ kind: "action", actionId: "x" })).toBe("action:x");
  });
});

describe("targetName", () => {
  it("names snapshot objects without a fetch, falls back to labels and type words", () => {
    const h = snap.hypotheses[0];
    expect(targetName({ kind: "ref", ref: h.id }, snap, {})).toBe(h.handle + " · " + h.statement);
    expect(targetName({ kind: "ref", ref: h.predictions[0].id }, snap, {})).toBe("P1 · " + h.predictions[0].statement);
    expect(targetName({ kind: "ref", ref: "x-host--z" }, snap, { "x-host--z": "host-z" })).toBe("host-z");
    expect(targetName({ kind: "ref", ref: "x-host--z" }, snap, {})).toBe("host");
    expect(targetName({ kind: "action", actionId: "act-1" }, snap, {})).toBe("host.isolate → host-a");
    expect(targetName({ kind: "sop", sopId: "sop-1" }, snap, {})).toBe("Procedure one");
    expect(targetName({ kind: "case", investigationRef: "grouping--prior-1" }, snap, {})).toBe("Prior case A");
    expect(targetName({ kind: "person", principalId: "analyst-1" }, snap, {})).toBe("analyst-1"); // no display name recorded in this stage
  });
});

describe("inThisCase", () => {
  it("finds predictions, actions, pins, acts and the seed that cite a ref", () => {
    const od1 = snap.pins[0].inputRefs[0];
    const c = inThisCase(od1, snap);
    expect(c.pins.length).toBe(1);
    expect(c.actions.map((a) => a.a.actionId + ":" + a.role)).toEqual(["act-1:evidence", "act-2:evidence"]);
    expect(c.acts.some((a) => a.role === "read")).toBe(true);
    expect(c.seed).toBe(false);
    const note = snap.hypotheses[0].predictions[1].testResultRefs[0];
    expect(inThisCase(note, snap).predictions.map((x) => x.p.ordinal)).toEqual([2]);
    expect(inThisCase(snap.investigation.seed!.entityRef!, snap).seed).toBe(true);
    expect(isEmpty(inThisCase("nothing--here", snap))).toBe(true);
  });
});
