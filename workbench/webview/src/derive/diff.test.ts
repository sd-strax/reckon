import { describe, expect, it } from "vitest";
import { diffLines } from "./diff";

describe("the section diff (adr/0097)", () => {
  it("marks identical texts as all same", () => {
    expect(diffLines("a\nb\nc", "a\nb\nc")).toEqual([
      { kind: "same", text: "a" }, { kind: "same", text: "b" }, { kind: "same", text: "c" },
    ]);
  });

  it("shows one changed line as a removal then an addition in place", () => {
    expect(diffLines("## Triage\nConfirm the session.\nThen isolate.", "## Triage\nConfirm the session with the owner.\nThen isolate.")).toEqual([
      { kind: "same", text: "## Triage" },
      { kind: "del", text: "Confirm the session." },
      { kind: "add", text: "Confirm the session with the owner." },
      { kind: "same", text: "Then isolate." },
    ]);
  });

  it("shows an added line and a removed line without disturbing the rest", () => {
    expect(diffLines("a\nb", "a\nx\nb")).toEqual([
      { kind: "same", text: "a" }, { kind: "add", text: "x" }, { kind: "same", text: "b" },
    ]);
    expect(diffLines("a\nx\nb", "a\nb")).toEqual([
      { kind: "same", text: "a" }, { kind: "del", text: "x" }, { kind: "same", text: "b" },
    ]);
  });

  it("treats an empty side as all added or all removed", () => {
    expect(diffLines("", "a\nb")).toEqual([{ kind: "add", text: "a" }, { kind: "add", text: "b" }]);
    expect(diffLines("a\nb", "")).toEqual([{ kind: "del", text: "a" }, { kind: "del", text: "b" }]);
    expect(diffLines("", "")).toEqual([]);
  });
});
