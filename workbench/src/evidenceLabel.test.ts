import { describe, expect, it } from "vitest";
import type { EvidenceDoc } from "./backend";
import { deriveEvidenceLabel } from "./evidenceLabel";
import { stixExtensionIds } from "./stixExtensions";

describe("deriveEvidenceLabel", () => {
  it("labels a windows-registry-key by its key and value name (adr/0341)", () => {
    const doc = (values?: unknown[]): EvidenceDoc => ({
      ref: "windows-registry-key--1",
      kind: "stix",
      type: "windows-registry-key",
      payload: { key: "HKEY_LOCAL_MACHINE\\Run", ...(values ? { values } : {}) },
    });
    expect(deriveEvidenceLabel(doc([{ name: "Updater", data: "u.exe" }]))).toBe("HKEY_LOCAL_MACHINE\\Run (Updater)");
    expect(deriveEvidenceLabel(doc([{ name: "" }]))).toBe("HKEY_LOCAL_MACHINE\\Run (default)");
    expect(deriveEvidenceLabel(doc())).toBe("HKEY_LOCAL_MACHINE\\Run");
  });

  it("labels the artifact for a stored record by its type alone, and an observation of nothing else as the stored record (adr/0357)", () => {
    const artifact: EvidenceDoc = {
      ref: "artifact--1",
      kind: "stix",
      type: "artifact",
      payload: { type: "artifact", spec_version: "2.1", id: "artifact--1", url: "reckon://ocsf-record/sha256/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", mime_type: "application/json", hashes: { "SHA-256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" } },
    };
    expect(deriveEvidenceLabel(artifact)).toBe("");
    const od = (refs: string[]): EvidenceDoc => ({ ref: "observed-data--1", kind: "stix", type: "observed-data", payload: { object_refs: refs } });
    expect(deriveEvidenceLabel(od(["artifact--1"]))).toBe("the stored record");
    expect(deriveEvidenceLabel(od(["reckon-host--a", "relationship--r"]))).toBe("1 object");
  });

  it("reads a process's name and a file's path from their property-extensions (adr/0357)", () => {
    const proc: EvidenceDoc = {
      ref: "process--1",
      kind: "stix",
      type: "process",
      payload: {
        type: "process", spec_version: "2.1", id: "process--1", pid: 6120,
        extensions: { [stixExtensionIds["reckon-process"]]: { extension_type: "property-extension", name: "powershell.exe" } },
      },
    };
    expect(deriveEvidenceLabel(proc)).toBe("powershell.exe (pid 6120)");
    const file: EvidenceDoc = {
      ref: "file--1",
      kind: "stix",
      type: "file",
      payload: {
        type: "file", spec_version: "2.1", id: "file--1",
        extensions: { [stixExtensionIds["reckon-file"]]: { extension_type: "property-extension", path: "C:\\Temp\\u.exe" } },
      },
    };
    expect(deriveEvidenceLabel(file)).toBe("C:\\Temp\\u.exe");
  });
});
