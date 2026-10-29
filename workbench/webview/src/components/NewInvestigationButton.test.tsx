import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("starting a new investigation from the list (#162)", () => {
  it("the Investigations header has a + that opens the seed surface", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("button", { name: "New investigation" }));
    expect(transport.intents).toContainEqual({ type: "investigation.new" });
  });
});
