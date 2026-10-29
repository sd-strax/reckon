import { describe, expect, it } from "vitest";
import css from "../styles/app.css?raw";

const rule = (selector: string) => {
  const i = css.indexOf(selector + " {");
  return i < 0 ? "" : css.slice(i, css.indexOf("}", i));
};

describe("regions use the width they are given (#135)", () => {
  it("only running prose keeps a reading measure", () => {
    expect(rule(".lens")).toContain("max-width: none");
    for (const sel of ["details.tool", "#stripWrap", "#graphWrap", "details.aside"]) {
      expect(rule(sel)).not.toMatch(/max-width: *\d/);
    }
    expect(rule(".lens p, .lens .md, .lens .empty")).toContain("max-width: 80ch");
  });
});
