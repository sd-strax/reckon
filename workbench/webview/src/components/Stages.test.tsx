// Every recorded stage renders without error with its regions — the
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
      expect(document.querySelectorAll(".stage .step").length).toBe(5);
      expect(document.querySelectorAll(".lenstab").length).toBe(4);
      expect(document.querySelector("#leftRegion")).toBeTruthy();
      expect(document.querySelector("#companion #composer")).toBeTruthy();
      const seed = s.snapshot.investigation.seed;
      if (seed?.type === "case") expect(screen.getAllByText(new RegExp(seed.caseId!)).length).toBeGreaterThan(0);
    });
  }
  it("shows the one-shot Note the case link under the confirm posture on a fresh seed", () => {
    const seeded = snapshots.find((s) => s.name === "seeded")!;
    render(<AppProvider transport={new HarnessTransport(seeded)}><App /></AppProvider>);
    expect(screen.getByText("Note the case")).toBeTruthy();
    expect((document.querySelector("#input") as HTMLTextAreaElement).disabled).toBe(false); // a draft converses freely
    expect(screen.getByRole("tab", { name: /^Reasoning/ }).getAttribute("aria-selected")).toBe("true");
  });
});
