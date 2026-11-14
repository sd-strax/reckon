import { describe, expect, it } from "vitest";
import type { EvidenceDoc } from "./backend";
import { deriveEvidenceLabel } from "./evidenceLabel";

describe("deriveEvidenceLabel", () => {
  it("labels a windows-registry-key by its key and value name (adr/0341)", () => {
    const doc = (values?: unknown[]): EvidenceDoc => ({
      ref: "windows-registry-key--1",
      kind: "stix",
      type: "windows-registry-key",
      payload: { properties: { key: "HKEY_LOCAL_MACHINE\\Run", ...(values ? { values } : {}) } },
    });
    expect(deriveEvidenceLabel(doc([{ name: "Updater", data: "u.exe" }]))).toBe("HKEY_LOCAL_MACHINE\\Run (Updater)");
    expect(deriveEvidenceLabel(doc([{ name: "" }]))).toBe("HKEY_LOCAL_MACHINE\\Run (default)");
    expect(deriveEvidenceLabel(doc())).toBe("HKEY_LOCAL_MACHINE\\Run");
  });
});
