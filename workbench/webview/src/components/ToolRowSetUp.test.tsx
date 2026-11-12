// The tool row's gap hint offers Set up by the one rule the sidebar uses
// (src/enablement.ts, adr/0342): never for an adapter the form cannot enable.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { ToolRow } from "./ToolRow";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const unavailable = { isError: false, detail: "", coverage: "UNAVAILABLE", refs: [] };

function mount(supportable: boolean) {
  const d = structuredClone(synthetic) as unknown as HarnessSnapshot;
  d.snapshot.enablement!.adapters[0]!.supportable = supportable;
  render(<AppProvider transport={new HarnessTransport(d)}><ToolRow name="query_dns" argsHint="" argsPretty="" result={unavailable} /></AppProvider>);
}

describe("the tool row's Set up (adr/0342)", () => {
  it("offers Set up for a supportable, disabled adapter that would serve the verb", () => {
    mount(true);
    expect(screen.getByText("Set up…")).toBeTruthy();
  });

  it("offers no Set up for an adapter the form cannot enable", () => {
    mount(false);
    expect(screen.queryByText("Set up…")).toBeNull();
  });
});
