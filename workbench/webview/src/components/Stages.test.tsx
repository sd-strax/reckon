// Every recorded stage renders without error and shows its seed line — the
// structural check the harness shots extend by eye (impl §5.4).
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport } from "../../../harness/transport";
import { snapshots } from "../../../harness/snapshots";

describe("recorded stages", () => {
  for (const s of snapshots) {
    it(`renders ${s.name}`, () => {
      render(<AppProvider transport={new HarnessTransport(s)}><App /></AppProvider>);
      expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(s.snapshot.investigation.title);
      expect(document.querySelector(".state")?.textContent).toContain(s.snapshot.investigation.state.toUpperCase());
      const seed = s.snapshot.investigation.seed;
      if (seed?.type === "case") expect(screen.getAllByText(new RegExp(seed.caseId!)).length).toBeGreaterThan(0); // the seed line: "From case <source>: <id>"
    });
  }
  it("shows the one-shot Note the case link under the confirm posture", () => {
    const seeded = snapshots.find((s) => s.name === "seeded")!;
    render(<AppProvider transport={new HarnessTransport(seeded)}><App /></AppProvider>);
    expect(screen.getByText("Note the case")).toBeTruthy();
    expect((document.querySelector("#input") as HTMLTextAreaElement).disabled).toBe(false); // a draft converses freely
  });
});
