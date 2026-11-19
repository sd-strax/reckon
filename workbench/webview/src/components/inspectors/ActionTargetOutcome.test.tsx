// A target's recorded outcome is OK, FAIL or UNKNOWN (08 §5), never a
// lifecycle status, so its badge tone keys on those values.
import { describe, expect, it } from "vitest";
import { targetOutcome, targetOutcomeTone } from "../../derive/actions";

describe("per-target outcome badge", () => {
  it("colours OK as ok, FAIL as bad and UNKNOWN as warn", () => {
    expect(targetOutcomeTone("OK")).toBe("ok");
    expect(targetOutcomeTone("FAIL")).toBe("bad");
    expect(targetOutcomeTone("UNKNOWN")).toBe("warn");
  });

  it("finds a target's outcome by its index or by the identifier sent", () => {
    expect(targetOutcome([{ target: "0", outcome: "FAIL" }], 0, "WIN-A")).toBe("FAIL");
    expect(targetOutcome([{ target: "WIN-A", outcome: "OK" }], 0, "WIN-A")).toBe("OK");
    expect(targetOutcome([{ target: "1", outcome: "OK" }], 0, "WIN-A")).toBeUndefined();
  });
});
