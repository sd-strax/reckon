// A detection finding's sighting shows the entities the alert names
// (03 §4.12). A Sighting carries the observations it was seen through, so the
// entities, the time and the count are read from those observations, which
// come back with the finding as its evidence. STIX keeps where_sighted_refs
// for who saw the indicator, and the engine fills it with the vendor identity
// (adr/0357). "Where" shows that, never the entities.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";
import { stixExtensionIds } from "../../../../src/stixExtensions";

const INDICATOR = "indicator--bf519283-e2a3-45c6-8182-9d0e1f2a3b08";
const HOST = "reckon-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";
const VENDOR = "identity--d173b4a5-04c5-47e8-a3a4-bf2a3b4c5d0a";
const ARTIFACT = "artifact--0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9";
const PERSON = "identity--1f2e3d4c-5b6a-4978-8a9b-0c1d2e3f4a5b";

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
  it("shows the entities of the observations it cites, with their time and count, and the vendor under Where", async () => {
    mountWith(snapshot());
    const section = await sightingSection();
    expect(section.textContent).toContain("Entities");
    expect(section.querySelector(`[data-ref="${HOST}"]`)).toBeTruthy();
    expect(section.textContent).toContain("When");
    expect(section.textContent).toContain("Count");
    expect(section.textContent).toContain("Where");
    expect(section.querySelector(`[data-ref="${VENDOR}"]`)).toBeTruthy();
  });

  it("takes no entity from the where-sighted list, and none from its observations' record artifacts", async () => {
    const data = snapshot();
    type Doc = { payload: Record<string, unknown> };
    const docs = (data as unknown as { docs: Record<string, { related: { sightings: Doc[]; evidence: Doc[] } }> }).docs;
    const related = docs[INDICATOR]!.related;
    related.sightings[0]!.payload.where_sighted_refs = [HOST];
    related.evidence[0]!.payload.object_refs = [ARTIFACT];
    mountWith(data);
    const section = await sightingSection();
    expect(section.textContent).not.toContain("Entities");
    expect(section.querySelector(`[data-ref="${ARTIFACT}"]`)).toBeNull();
    // The host shows only as what the sighting's own list says: where it was seen.
    expect(section.textContent).toContain("Where");
  });

  it("reads an observation's person_ref from its stored extensions (adr/0357)", async () => {
    const data = snapshot();
    type Doc = { payload: Record<string, unknown> };
    const docs = (data as unknown as { docs: Record<string, { related: { sightings: Doc[]; evidence: Doc[] } }> }).docs;
    const ev = docs[INDICATOR]!.related.evidence[0]!.payload;
    ev.extensions = { ...(ev.extensions as object), [stixExtensionIds["reckon-observation"]]: { extension_type: "property-extension", person_ref: PERSON } };
    mountWith(data);
    const section = await sightingSection();
    expect(section.querySelector(`[data-ref="${PERSON}"]`)).toBeTruthy();
    expect(section.querySelector(`[data-ref="${HOST}"]`)).toBeTruthy();
  });
});
