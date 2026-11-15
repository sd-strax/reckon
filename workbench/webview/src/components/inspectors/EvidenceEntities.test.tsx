// The evidence inspector lists an observation's entities, never its
// relationships or the artifact that stands for the record itself; a
// directory record's person shows as the person it describes
// (adr/0324, adr/0357).
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";
import { stixExtensionIds } from "../../../../src/stixExtensions";

const od = "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04";
const host = "reckon-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";
const person = "identity--1f2e3d4c-5b6a-4978-8a9b-0c1d2e3f4a5b";

function Open({ target }: { target: InspectorTarget }) {
  const { openInspector } = useApp();
  return <button onClick={() => openInspector(target)}>open-target</button>;
}

describe("an observation's entities", () => {
  it("lists entities only, and the person a directory record describes apart", async () => {
    const data = JSON.parse(JSON.stringify(synthetic)) as HarnessSnapshot & { docs: Record<string, { payload: Record<string, unknown> }> };
    const payload = data.docs[od]!.payload;
    payload.object_refs = [host, "relationship--9a8b7c6d-5e4f-4321-8a9b-0c1d2e3f4a5b", "artifact--0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8"];
    payload.extensions = { ...(payload.extensions as object), [stixExtensionIds["reckon-observation"]]: { extension_type: "property-extension", person_ref: person } };
    render(<AppProvider transport={new HarnessTransport(data)}><App /><Open target={{ kind: "ref", ref: od }} /></AppProvider>);
    fireEvent.click(screen.getByText("open-target"));
    const objects = (await screen.findByText("Objects it contains")).closest("section")!;
    const refs = [...objects.querySelectorAll("[data-ref]")].map((e) => e.getAttribute("data-ref"));
    expect(refs).toEqual([host]);
    const described = screen.getByText("Person it describes").closest("section")!;
    expect(within(described).getByTitle("Open in the inspector: " + person)).toBeTruthy();
  });
});
