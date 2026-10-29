import { describe, expect, it } from "vitest";
import { candidateSOPMarkdown, errText, slugifyTitle, yamlScalar } from "./documentText";

describe("document text", () => {
  it("renders a candidate SOP as the markdown the import path parses, keyed to its investigation", () => {
    const md = candidateSOPMarkdown({ warranted: true, title: "RDP: contain: a service account", body: "1. Isolate.", tags: ["rdp", " "] } as never, "inv-1");
    expect(md).toBe(['---', 'title: "RDP: contain: a service account"', "tags: [rdp]", "source:", "  system: reckon", "  url: reckon://investigation/inv-1", "---", "", "1. Isolate.", ""].join("\n"));
  });
  it("quotes only what YAML could misread, and slugs a title", () => {
    expect(yamlScalar("plain title")).toBe("plain title");
    expect(yamlScalar("a: b")).toBe('"a: b"');
    expect(slugifyTitle("RDP — Containment!")).toBe("rdp-containment");
    expect(slugifyTitle("!!!")).toBe("candidate-sop");
    expect(errText(new Error("boom"))).toBe("boom");
  });
});
