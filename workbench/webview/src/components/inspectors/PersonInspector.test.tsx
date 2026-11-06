// The Person inspector (adr/0105): each act says what reckon required of it,
// the summary says what the issuer asserted across the person's acts, and an
// act recorded before either existed says so instead of reading as "no roles".
import { useEffect } from "react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { InspectorTarget } from "../../../../src/protocol";

const data = synthetic as unknown as HarnessSnapshot;

function Open({ target }: { target: InspectorTarget }) {
  const { openInspector } = useApp();
  useEffect(() => { openInspector(target); }, [openInspector, target]);
  return null;
}

function mountWith(target: InspectorTarget) {
  const transport = new HarnessTransport(JSON.parse(JSON.stringify(data)) as HarnessSnapshot);
  render(<AppProvider transport={transport}><App /><Open target={target} /></AppProvider>);
  return document.querySelector(".inspector") as HTMLElement;
}

describe("person inspector", () => {
  it("says what reckon required of each act and what the issuer asserted, and marks an older act", () => {
    const inspector = mountWith({ kind: "person", principalId: "analyst-1" });
    expect(within(inspector).getByText("approved host.isolate").parentElement!.textContent).toContain("as analyst");
    expect(within(inspector).getByText("pinned: two interactive logons").parentElement!.textContent).toContain("authorization not on this act");
    expect(within(inspector).getByText("Reckon required").parentElement!.textContent).toContain("analyst");
    expect(within(inspector).getByText("Issuer asserted").parentElement!.textContent).toContain("analyst, approver");
    expect(within(inspector).getByText(/carry neither/).textContent).toContain("1 of their acts carry neither");
  });

  it("attributes the engine's acts to the engine, claiming no approval, and carries no authorization for them", () => {
    const inspector = mountWith({ kind: "person", principalId: "system" });
    expect(within(inspector).getAllByText(/the engine, in their name/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/on their approval/)).toBeNull();
    expect(within(inspector).getByText("no acts of their own here")).toBeTruthy();
    expect(screen.queryByText("Issuer asserted")).toBeNull();
  });
});
