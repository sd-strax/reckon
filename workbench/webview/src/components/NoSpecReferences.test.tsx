// No analyst-facing text cites a design spec (#166): every lens and the
// inspector panes render from the harness snapshots without a "§".
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport } from "../../../harness/transport";
import { snapshots } from "../../../harness/snapshots";

describe("no spec references in what the analyst reads (#166)", () => {
  for (const s of snapshots) {
    it(`every lens of ${s.name}`, () => {
      render(<AppProvider transport={new HarnessTransport(s)}><App /></AppProvider>);
      for (const lens of ["Summary", "Story", "Reasoning", "Response"]) {
        fireEvent.click(screen.getByRole("tab", { name: new RegExp("^" + lens) }));
        expect(document.body.textContent).not.toMatch(/§\s?\d|gap 00/);
      }
    });
  }
});
