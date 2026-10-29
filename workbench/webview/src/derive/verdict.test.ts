import { describe, expect, it } from "vitest";
import type { Capability, Hypothesis, PinRow } from "../../../src/viewTypes";
import { uncheckable, verdictChecklist, verdictResidual } from "./verdict";

const pin = { interpretationId: "p", finding: "f", inputRefs: ["r1"], actor: "HUMAN", pinnedAt: "", superseded: false } as PinRow;
const cap = (verb: string, intent: string): Capability => ({ verb, intent, inputs: [], output: "", status: "unavailable" });
const turn = (lines: string[]) => ({ sequenceNo: 1, occurredAt: "", body: lines.join("\n") });

describe("verdict dialog (#143)", () => {
  it("states only what is true: no citation line without pins", () => {
    expect(verdictChecklist([], null)).toEqual([{ ok: false, text: "no pinned evidence — pin the key findings first (the engine will refuse)" }]);
    expect(verdictChecklist([pin], null)).toEqual([{ ok: true, text: "1 pinned evidence item — the verdict cites it" }]);
  });
  it("lists only the checks this investigation asked for that the tenant could not answer", () => {
    const caps = [cap("get_indicator_context", "Look up reputation for an indicator. Returns verdicts."), cap("resolve_entity", "Resolve an entity."), cap("list_capabilities", "List verbs.")];
    const turns = [turn([
      '[tool_use get_indicator_context id=t1] {"indicator":"185.220.101.5"}',
      '[tool_result get_indicator_context id=t1 error=false] {"verb":"get_indicator_context","coverage":"UNAVAILABLE_TENANT"}',
      '[tool_use list_capabilities id=t2] {}',
      '[tool_result list_capabilities id=t2 error=false] {"verb":"list_capabilities","coverage":"UNAVAILABLE_TENANT"}',
    ])];
    expect(uncheckable(caps, turns)).toEqual([{ verb: "get_indicator_context", what: "Look up reputation for an indicator" }]);
    const lines = verdictResidual(caps, [], turns);
    expect(lines).toEqual(["Could not check: Look up reputation for an indicator — no integration in this tenant answers it."]);
    expect(lines.join(" ")).not.toContain("resolve_entity"); // never tried: does not bear on this case
  });
  it("names undecided hypotheses and what is untested under them, in words", () => {
    const h = { id: "h", handle: "H2 legit", statement: "an admin did it", status: "OPEN", labels: [], predictions: [
      { id: "p", statement: "a change ticket covers it", status: "UNTESTED", discriminates: ["h1"], testResultRefs: [] },
    ] } as unknown as Hypothesis;
    const lines = verdictResidual([], [h], []);
    expect(lines[0]).toBe('H2 legit — "an admin did it" is still open; this verdict leaves it undecided (1 untested test would tell it apart from a rival). Abandon it on its card if you are not pursuing it.');
    expect(lines[1]).toBe("Not tested: a change ticket covers it");
    expect(verdictResidual(null, [], [])).toEqual(["Nothing known left open: every hypothesis is decided and every check this investigation asked for was answered."]);
  });
});
