// The action record's attempts (adr/0384): 0 means the dispatch failed
// before the call and nothing was sent, so it is shown, never hidden by a
// truthiness test.
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { attemptsLabel } from "../../derive/actions";
import { KV } from "./primitives";

afterEach(cleanup);

describe("action record attempts", () => {
  it("labels 0 as not sent and a count as itself", () => {
    expect(attemptsLabel(0)).toBe("0 (not sent)");
    expect(attemptsLabel(1)).toBe("1");
    expect(attemptsLabel(3)).toBe("3");
  });

  it("renders attempts 0 in the result rows", () => {
    const { container } = render(<KV rows={[{ k: "Attempts", v: attemptsLabel(0), mono: true }]} />);
    const row = container.querySelector(".ikvrow");
    expect(row?.querySelector("dt")?.textContent).toBe("Attempts");
    expect(row?.querySelector("dd")?.textContent).toBe("0 (not sent)");
  });
});
