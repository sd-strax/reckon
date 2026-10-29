import { describe, expect, it } from "vitest";
import { decideTurn, hypCite, proposeTestsTurn, rivalsClause, runTestsTurn, testPredictionTurn } from "./scopedTurns";

const rival = { id: "x-hypothesis--b", handle: "H2 legit", statement: "an admin did it", untestedDiscriminators: 1 };

describe("scoped turns", () => {
  it("cite the hypothesis by handle, claim and id", () => {
    expect(hypCite("H1 rdp", "RDP was used", "x-hypothesis--a")).toBe(`"H1 rdp: RDP was used" (x-hypothesis--a)`);
    expect(hypCite(undefined, "RDP was used", "x-hypothesis--a")).toBe(`"RDP was used" (x-hypothesis--a)`);
  });
  it("name the live rivals only when there are some", () => {
    expect(rivalsClause([], "x")).toBe("");
    expect(rivalsClause([rival], "Decide against them.")).toBe(`Live rivals: H2 legit: "an admin did it" (x-hypothesis--b) [1 untested discriminating prediction]. Decide against them.`);
  });
  it("each turn starts with its ⚡ marker and renders under its label", () => {
    const turns = [
      [proposeTestsTurn("x-hypothesis--a", "RDP was used", "H1 rdp", [rival]), "⚡ Propose tests:", "⚡ propose tests · H1 rdp"],
      [runTestsTurn("x-hypothesis--a", "RDP was used", "H1 rdp"), "⚡ Run tests:", "⚡ run tests · H1 rdp"],
      [decideTurn("x-hypothesis--a", "RDP was used", "H1 rdp", [rival]), "⚡ Decide:", "⚡ decide · H1 rdp"],
      [testPredictionTurn({ predictionRef: "x-prediction--p", predictionName: "P2", statement: "baseline differs", hypothesisRef: "x-hypothesis--a", hypStatement: "RDP was used", handle: "H1 rdp" }), "⚡ Test P2:", "⚡ test P2 · H1 rdp"],
    ] as const;
    for (const [t, prefix, label] of turns) {
      expect(t.instruction.startsWith(prefix)).toBe(true);
      expect(t.label).toBe(label);
    }
    expect(proposeTestsTurn("x-hypothesis--a", "RDP was used", "H1 rdp", [rival]).instruction).toContain("x-hypothesis--b");
  });
});
