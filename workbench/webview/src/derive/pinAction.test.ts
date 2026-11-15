import { describe, expect, it } from "vitest";
import type { PinRow } from "../../../src/viewTypes";
import { pinTargets } from "./pinAction";

describe("a pin's targets (#146)", () => {
  it("are the entities its evidence observed, by their resolved names", () => {
    const pin = { interpretationId: "p", finding: "f", inputRefs: ["observed-data--a"], actor: "HUMAN", pinnedAt: "", superseded: false } as PinRow;
    const graph = { refs: [], edges: [
      { from: "observed-data--a", to: "reckon-host--1", ai: false, join: true },
      { from: "observed-data--a", to: "user-account--2", ai: false, join: true },
      { from: "observed-data--b", to: "reckon-host--3", ai: false, join: true },
      { from: "reckon-prediction--p", to: "observed-data--a", ai: false },
    ] };
    expect(pinTargets(pin, graph, { "reckon-host--1": "WIN-FILE01", "user-account--2": "svc_backup", "reckon-host--3": "OTHER" })).toEqual([
      { value: "WIN-FILE01", label: "WIN-FILE01 · host" },
      { value: "svc_backup", label: "svc_backup · user-account" },
    ]);
  });
});
