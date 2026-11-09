import { act, fireEvent, render, screen, within } from "@testing-library/react";
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
    fireEvent.click(within(lib).getAllByText("Ask reckon to use this")[0]!);
    expect((document.querySelector("#input") as HTMLTextAreaElement).value).toMatch(/^Use the procedure “Procedure one” \(SOP sop-1\)/);
    expect(transport.intents.some((i) => i.type === "send")).toBe(false);
    fireEvent.click(within(lib).getByText("Import procedures…"));
    expect(transport.intents).toContainEqual({ type: "library.import" });
    void screen;
  });

  it("opens the Knowledge view from the Library and from the left region's header (adr/0107)", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    const lib = document.querySelector("#knowledgeSection") as HTMLElement;
    fireEvent.click(within(lib).getByText("Open the Knowledge view"));
    expect(transport.intents.filter((i) => i.type === "knowledge.open").length).toBe(1);
    fireEvent.click(within(document.querySelector("#leftRegion") as HTMLElement).getByRole("button", { name: "Knowledge" }));
    expect(transport.intents.filter((i) => i.type === "knowledge.open").length).toBe(2);
  });

  it("stages the text once: it does not return after the analyst sends and the turn ends", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    const lib = document.querySelector("#knowledgeSection") as HTMLElement;
    const input = () => document.querySelector("#input") as HTMLTextAreaElement;
    fireEvent.click(within(lib).getAllByText("Ask reckon to use this")[0]!);
    fireEvent.keyDown(input(), { key: "Enter" });
    expect(input().value).toBe("");
    act(() => transport.emit({ type: "turn.end" }));
    expect(input().disabled).toBe(false);
    expect(input().value).toBe("");
  });
});
