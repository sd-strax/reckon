import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("drafting a procedure is the analyst's step (#156)", () => {
  it("the concluded Summary offers it, shows each outcome, and adds or discards on the panel", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    d.snapshot.investigation = { ...d.snapshot.investigation, state: "CONCLUDED", verdict: { disposition: "MALICIOUS", verdictAt: "" } };
    const transport = new HarnessTransport(d);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: /^Summary/ }));
    act(() => transport.emit({ type: "summary.status", state: "written", detail: "claude-opus-5-5" }));
    expect(screen.getByText("Knowledge summary: narrative written by claude-opus-5-5")).toBeTruthy();

    fireEvent.click(screen.getByText("Draft SOP from this case"));
    expect(transport.intents).toContainEqual({ type: "sop.draft" });
    act(() => transport.emit({ type: "sop.status", state: "drafting" }));
    expect(screen.getByText("Drafting a procedure from this case…")).toBeTruthy();

    act(() => transport.emit({ type: "sop.status", state: "declined", message: "Covered by Lateral Movement via RDP — Containment." }));
    expect(screen.getByText(/No new procedure: Covered by Lateral Movement via RDP/)).toBeTruthy();
    fireEvent.click(screen.getByText("Draft anyway"));
    expect(transport.intents).toContainEqual({ type: "sop.draft", force: true });

    act(() => transport.emit({ type: "sop.status", state: "ready", title: "RDP service-account misuse" }));
    fireEvent.click(screen.getByText("Add to SOP library"));
    expect(transport.intents).toContainEqual({ type: "sop.accept" });
    act(() => transport.emit({ type: "sop.status", state: "ready", title: "RDP service-account misuse" }));
    fireEvent.click(screen.getByText("Discard"));
    expect(transport.intents).toContainEqual({ type: "sop.discard" });
  });
});
