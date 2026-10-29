import { describe, expect, it } from "vitest";
import { primaryAction, stageOf } from "./stage";

describe("the step's action beside the strip (#142)", () => {
  it("offers Record verdict once every hypothesis is decided, not before", () => {
    const base = { status: "ACTIVE", verdict: null, actions: [], comms: [], openOwnerItems: 0 };
    expect(primaryAction({ ...base, allDecided: true }, stageOf(base), false)).toEqual({ kind: "verdict" });
    expect(primaryAction({ ...base, allDecided: false }, stageOf(base), false)).toBeNull();
  });
});
