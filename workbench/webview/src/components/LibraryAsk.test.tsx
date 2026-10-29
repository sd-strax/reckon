import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("asking the agent to use a library item (adr/0129)", () => {
  it("stages a request in the composer; nothing is put in context", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    const lib = document.querySelector("#knowledgeSection") as HTMLElement;
    fireEvent.click(within(lib).getAllByText("Ask reckon to use this")[0]);
    expect((document.querySelector("#input") as HTMLTextAreaElement).value).toMatch(/^Use the procedure “Procedure one” \(SOP sop-1\)/);
    expect(transport.intents.some((i) => i.type === "send")).toBe(false);
    fireEvent.click(within(lib).getByText("Import procedures…"));
    expect(transport.intents).toContainEqual({ type: "library.import" });
    void screen;
  });
});
