// A detection finding's sighting shows the entities the alert names
// (03 §4.12). A Sighting carries only the observations it was seen through,
// so the entities, the time and the count are read from those observations,
// which come back with the finding as its evidence. STIX keeps
// where_sighted_refs for who saw the indicator, and the engine does not fill
// it.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";

const INDICATOR = "indicator--bf519283-e2a3-45c6-8182-9d0e1f2a3b08";
const HOST = "x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";

function Open({ target }: { target: InspectorTarget }) {
  const { openInspector } = useApp();
  return <button onClick={() => openInspector(target)}>open-target</button>;
}

function mountWith(data: HarnessSnapshot) {
  render(<AppProvider transport={new HarnessTransport(data)}><App /><Open target={{ kind: "ref", ref: INDICATOR }} /></AppProvider>);
  fireEvent.click(screen.getByText("open-target"));
}

function snapshot(): HarnessSnapshot {
  return JSON.parse(JSON.stringify(synthetic)) as HarnessSnapshot;
}

async function sightingSection(): Promise<HTMLElement> {
  const inspector = await screen.findByText(/inferred by the vendor/);
  const root = inspector.closest(".inspector") as HTMLElement;
  return within(root).getByText("Sighting").closest("section") as HTMLElement;
}

describe("a detection finding's sighting", () => {
  it("shows the entities of the observations it cites, with their time and count", async () => {
    mountWith(snapshot());
    const section = await sightingSection();
    expect(section.textContent).toContain("Entities");
    expect(section.querySelector(`[data-ref="${HOST}"]`)).toBeTruthy();
    expect(section.textContent).toContain("When");
    expect(section.textContent).toContain("Count");
    expect(section.textContent).not.toContain("Where");
  });

  it("reads nothing from a where-sighted list on the sighting itself", async () => {
    const data = snapshot();
    const docs = (data as unknown as { docs: Record<string, { related: { sightings: { payload: Record<string, unknown> }[]; evidence: unknown[] } }> }).docs;
    const related = docs[INDICATOR]!.related;
    related.sightings[0]!.payload.where_sighted_refs = [HOST];
    related.evidence = [];
    mountWith(data);
    const section = await sightingSection();
    expect(section.querySelector(`[data-ref="${HOST}"]`)).toBeNull();
    expect(section.textContent).not.toContain("Entities");
  });
});
