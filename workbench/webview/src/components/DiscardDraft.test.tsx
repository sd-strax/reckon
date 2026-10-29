import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport } from "../../../harness/transport";
import { snapshots } from "../../../harness/snapshots";

describe("discarding a draft (#161)", () => {
  it("a draft offers Discard draft…, which asks why and sends the transition", () => {
    const seeded = structuredClone(snapshots.find((s) => s.name === "seeded")!);
    seeded.snapshot.investigation.state = "DRAFT";
    const transport = new HarnessTransport(seeded);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByText("Discard draft…"));
    const dlg = screen.getByRole("dialog");
    fireEvent.change(within(dlg).getByRole("textbox"), { target: { value: "mistyped host" } });
    fireEvent.click(within(dlg).getByText("Discard draft"));
    expect(transport.intents.find((i) => i.type === "lifecycle")).toEqual({ type: "lifecycle", transition: "discard", reason: "mistyped host" });
  });
});
