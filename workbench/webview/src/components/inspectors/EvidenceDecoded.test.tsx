// The evidence inspector shows an encoded command line read back (adr/0300):
// the decoded text, the encoding and the field, with the stored command line
// still in the summary; an event without one has no such section.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";

const data = synthetic as unknown as HarnessSnapshot;

// A button, not a mount effect: the snapshot reaches the provider in its own
// effect, after a child's, so an inspector opened on mount would be dropped.
function Open({ target }: { target: InspectorTarget }) {
  const { openInspector } = useApp();
  return <button onClick={() => openInspector(target)}>open-target</button>;
}

function mountWith(target: InspectorTarget) {
  const transport = new HarnessTransport(JSON.parse(JSON.stringify(data)) as HarnessSnapshot);
  render(<AppProvider transport={transport}><App /><Open target={target} /></AppProvider>);
  fireEvent.click(screen.getByText("open-target"));
}

describe("decoded command line", () => {
  it("shows the decoded text beside the stored command line, naming the encoding and the field", async () => {
    mountWith({ kind: "ref", ref: "7c6b5a49-3827-4160-9f8e-7d6c5b4a3920" });
    const section = (await screen.findByText("Decoded command line")).closest("section")!;
    expect(within(section).getByText("IEX (New-Object Net.WebClient)")).toBeTruthy();
    expect(section.textContent).toContain("base64 of UTF-16LE");
    expect(section.textContent).toContain("process.cmd_line");
    const inspector = document.querySelector(".inspector") as HTMLElement;
    // The stored command line stays, in the summary and in the raw fields.
    expect(within(inspector).getAllByText(/-EncodedCommand SQBFAFgAIAAo/).length).toBeGreaterThan(0);
  });

  it("has no decoded section for an event that carries none", async () => {
    mountWith({ kind: "ref", ref: "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8" });
    await screen.findByText(/Telemetry · OCSF event/);
    expect(screen.queryByText("Decoded command line")).toBeNull();
  });
});
