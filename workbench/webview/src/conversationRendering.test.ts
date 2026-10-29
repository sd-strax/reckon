import { describe, expect, it } from "vitest";
import { md } from "./markdown";
import { fullRef } from "./components/shared";
import type { InvestigationSnapshot } from "../../src/protocol";

describe("the agent's prose reads as an answer (#136)", () => {
  it("renders an evidence id as a chip, not text", () => {
    const html = md("RDP logon by svc_backup (observed-data--c973ba98).");
    expect(html).toContain('data-ref="observed-data--c973ba98"');
    expect(html).not.toMatch(/\(observed-data--c973ba98\)/);
    expect(html).toContain(">observed data<");
  });
  it("renders the engine's footer as a badge", () => {
    const html = md("Done.\n\n[engine record — authoritative, verified against the action log: ca2926f3=REQUESTED]");
    expect(html).toContain('<div class="enginenote"><span class="badge info">engine-verified</span> authoritative, verified against the action log: ca2926f3=REQUESTED</div>');
  });
  it("resolves a shortened id to the one full id the record holds", () => {
    const full = "observed-data--c973ba98-642d-5ce8-8344-fc8b294973cf";
    const snap = { thread: [{ inputRefs: [full], outputRefs: [] }], pins: [], hypotheses: [], actions: [], derived: null } as unknown as InvestigationSnapshot;
    expect(fullRef("observed-data--c973ba98", snap)).toBe(full);
    expect(fullRef("observed-data--deadbeef", snap)).toBeUndefined();
    expect(fullRef(full, null)).toBe(full);
  });
});
