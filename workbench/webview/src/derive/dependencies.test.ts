import { describe, expect, it } from "vitest";
import type { Dependency } from "../../../src/viewTypes";
import { openDependencies } from "./dependencies";
import { concludePreflight } from "./preflight";
import { stageOf } from "./stage";
import { stillToDo } from "./summary";

const dep = (over: Partial<Dependency>): Dependency => ({ dependencyId: "d", title: "rotate", owner: "platform", status: "OPEN", createdBy: "a", createdAt: "", history: [], ...over });
const verdict = { disposition: "MALICIOUS", verdictAt: "" };

// What the conclude preflight and the Notify step count (adr/0101): OPEN and
// SENT dependencies; a confirmed or waived one no longer does.
describe("dependencies in the derivations", () => {
  it("counts the ones still owed", () => {
    expect(openDependencies([dep({ status: "OPEN" }), dep({ status: "SENT" }), dep({ status: "CONFIRMED" }), dep({ status: "WAIVED" })])).toBe(2);
  });
  it("tells a slice that could not be read from an empty one", () => {
    expect(openDependencies([])).toBe(0);
    expect(openDependencies(null)).toBeNull();
    expect(openDependencies(undefined)).toBeNull();
  });
  it("names them on the preflight, singular and plural, and says when the count is unchecked", () => {
    const line = (n: number | null) => concludePreflight({ verdict, pins: [], actions: [], comms: [], openDependencies: n })[4]!;
    expect(line(0)).toMatchObject({ ok: true, text: "no dependency on others outstanding" });
    expect(line(1)).toMatchObject({ ok: false, text: "1 dependency on others not confirmed or waived", lens: "response" });
    expect(line(2).text).toBe("2 dependencies on others not confirmed or waived");
    expect(line(null)).toMatchObject({ ok: false, text: "dependencies on others could not be read — the count is unchecked", lens: "response" });
  });
  it("holds the Notify step while one is owed, or while the count is unchecked", () => {
    const row = { pending: false, targets: [], evidenceRefs: [], tier: "T2", requiredMode: "MANUAL", pendingLabel: "", expired: false, isReversal: false, tierEscalated: false };
    const contained = { ...row, actionId: "c", actionType: "host.isolate", status: "SUCCEEDED" };
    const sent = { ...row, actionId: "n", actionType: "notify.email", status: "SUCCEEDED" };
    expect(stageOf({ status: "ACTIVE", verdict, actions: [contained, sent], comms: [], openDependencies: 1 }).current).toBe("notify");
    expect(stageOf({ status: "ACTIVE", verdict, actions: [contained, sent], comms: [], openDependencies: null }).current).toBe("notify");
    expect(stageOf({ status: "ACTIVE", verdict, actions: [contained, sent], comms: [], openDependencies: 0 }).current).toBe("close");
  });
  it("keeps Notify in play for a benign verdict while a dependency is owed", () => {
    const benign = { disposition: "BENIGN", verdictAt: "" };
    const skipped = stageOf({ status: "ACTIVE", verdict: benign, actions: [], comms: [], openDependencies: 0 });
    expect(skipped.notNeeded.notify).toBe(true);
    expect(skipped.current).toBe("close");
    const owed = stageOf({ status: "ACTIVE", verdict: benign, actions: [], comms: [], openDependencies: 1 });
    expect(owed.notNeeded.notify).toBe(false);
    expect(owed.current).toBe("notify");
    expect(stageOf({ status: "ACTIVE", verdict: benign, actions: [], comms: [], openDependencies: null }).notNeeded.notify).toBe(false);
  });
  it("adds a still-to-do row per owed dependency, worded by whether it was asked yet", () => {
    const rows = stillToDo([], [], [], {}, [dep({ dependencyId: "1", status: "SENT" }), dep({ dependencyId: "2", status: "OPEN", title: "re-image", owner: "desktop" }), dep({ dependencyId: "3", status: "CONFIRMED" })]);
    expect(rows.map((r) => r.text)).toEqual(["Confirm: rotate (platform)", "Ask and confirm: re-image (desktop)"]);
    expect(rows.map((r) => r.dependencyId)).toEqual(["1", "2"]);
  });
});
