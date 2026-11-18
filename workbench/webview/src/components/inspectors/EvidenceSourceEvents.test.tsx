// The observation inspector leads back to the raw events the observation was
// made from, and each one opens the event inspector (adr/0364).
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";

const od = "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04";
const event = "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8";

function Open({ target }: { target: InspectorTarget }) {
  const { openInspector } = useApp();
  return <button onClick={() => openInspector(target)}>open-target</button>;
}

describe("an observation's source events", () => {
  it("lists the raw event it was made from, which opens the event inspector", async () => {
    const data = JSON.parse(JSON.stringify(synthetic)) as HarnessSnapshot & { docs: Record<string, { sourceEventIds?: string[] }> };
    data.docs[od]!.sourceEventIds = [event];
    render(<AppProvider transport={new HarnessTransport(data)}><App /><Open target={{ kind: "ref", ref: od }} /></AppProvider>);
    fireEvent.click(screen.getByText("open-target"));
    const made = (await screen.findByText("Made from")).closest("section")!;
    const chip = made.querySelector(`[data-ref="${event}"]`) as HTMLElement | null;
    expect(chip).toBeTruthy();
    fireEvent.click(chip!);
    expect(await screen.findByText(/Telemetry · OCSF event/)).toBeTruthy();
  });

  it("says so when the record holds no raw event for it", async () => {
    const data = JSON.parse(JSON.stringify(synthetic)) as HarnessSnapshot;
    render(<AppProvider transport={new HarnessTransport(data)}><App /><Open target={{ kind: "ref", ref: od }} /></AppProvider>);
    fireEvent.click(screen.getByText("open-target"));
    expect(await screen.findByText("The record holds no raw event for this observation.")).toBeTruthy();
  });
});
