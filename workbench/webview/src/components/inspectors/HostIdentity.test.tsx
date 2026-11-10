// A host's identity in the Entity inspector (adr/0211): every alias with its
// origin, a withdrawn one labelled, the split listed, the seen-under line only
// when the id was seen under more than one domain, and the join, split and
// withdraw acts on a host and on nothing else.
import { cleanup, fireEvent, render, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AppProvider } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { HostIdentity } from "../../../../src/viewTypes";

const base = synthetic as unknown as HarnessSnapshot;
const host = base.snapshot.investigation.seed!.entityRef!;
const ip = "ipv4-addr--6b0c4d3e-9d5e-4071-bc3d-4e5f6a7b8c03";
const at = "2026-01-02T03:04:05Z";

function identity(domains: string[]): HostIdentity {
  return {
    aliases: [
      { edgeId: "relationship--1", otherRef: "x-host--00000000-0000-4000-8000-000000000002", kind: "observed", source: "edr", at, withdrawn: false },
      { edgeId: "relationship--3", otherRef: "x-host--00000000-0000-4000-8000-000000000004", kind: "asserted", by: "u-1", byName: "Ana Analyst", at, note: "same serial in the asset register", withdrawn: false },
      { edgeId: "relationship--5", otherRef: "x-host--00000000-0000-4000-8000-000000000006", kind: "asserted", by: "u-1", byName: "Ana Analyst", at, withdrawn: true },
    ],
    distinct: [{ edgeId: "relationship--7", otherRef: "x-host--00000000-0000-4000-8000-000000000008", kind: "asserted", by: "u-2", byName: "Ben Analyst", at, note: "two machines", withdrawn: false }],
    seenAs: { domains, agents: [{ tool: "edr", uid: "a-7" }, { tool: "edr", uid: "a-8" }, { tool: "ids", uid: "x-9" }], ips: ["10.0.0.7"] },
  };
}

function mount(domains: string[]) {
  const data: HarnessSnapshot = {
    ...base,
    docs: {
      ...base.docs,
      [host]: { ...base.docs![host]!, identity: identity(domains) },
      [ip]: { ref: ip, kind: "stix", type: "ipv4-addr", payload: { type: "ipv4-addr", value: "10.0.0.9" } },
    },
    labels: { ...base.labels, "x-host--00000000-0000-4000-8000-000000000004": "ws1-old" },
  };
  const transport = new HarnessTransport(data);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}
const pane = () => document.querySelector(".inspector.active") as HTMLElement;
const open = (ref: string) => fireEvent.click(document.querySelector(`.refchip[data-ref="${ref}"]`) as HTMLElement);
const edge = (id: string) => pane().querySelector(`[data-edge="${id}"]`) as HTMLElement;

afterEach(cleanup);

describe("a host's identity in the inspector (adr/0211)", () => {
  it("lists each alias with its origin, the withdrawn one labelled, the split, and the facts", () => {
    mount(["corp", "corp.example"]);
    open(host);
    const p = pane();
    expect(within(p).getByText("Also seen as")).toBeTruthy();
    expect(within(edge("relationship--1")).getByText("from edr")).toBeTruthy();
    expect(within(edge("relationship--3")).getByText(/^asserted by Ana Analyst · /)).toBeTruthy();
    expect(within(edge("relationship--3")).getByText("ws1-old")).toBeTruthy(); // the other host by its label
    expect(within(edge("relationship--3")).getByText("“same serial in the asset register”")).toBeTruthy();
    expect(edge("relationship--5").classList.contains("withdrawn")).toBe(true);
    expect(within(edge("relationship--5")).getByText("withdrawn")).toBeTruthy();
    expect(within(edge("relationship--5")).queryByText("Withdraw…")).toBeNull(); // nothing to withdraw again
    expect(within(edge("relationship--1")).queryByText("Withdraw…")).toBeNull(); // a source's record
    expect(within(p).getByText("Not the same host as")).toBeTruthy();
    expect(within(edge("relationship--7")).getByText(/asserted by Ben Analyst/)).toBeTruthy();
    expect(within(p).getByText("seen under 2 domains: corp, corp.example")).toBeTruthy();
    expect(within(p).getByText("edr a-7, a-8 · ids x-9")).toBeTruthy();
    expect(within(p).getByText("10.0.0.7")).toBeTruthy();
  });

  it("says no warning when the id was seen under one domain, only the fact", () => {
    mount(["corp.example"]);
    open(host);
    expect(within(pane()).queryByText(/seen under \d+ domains/)).toBeNull();
    expect(within(pane()).getByText("corp.example")).toBeTruthy();
  });

  it("offers the join, the split and the withdraw on a host, each an inline form that sends the act", () => {
    const transport = mount(["corp"]);
    open(host);
    const p = pane();
    fireEvent.click(within(p).getByText("Same as another host…"));
    const join = within(p).getByRole("form", { name: "Same as another host" });
    const [other, note] = Array.from(join.querySelectorAll("input"));
    fireEvent.change(other!, { target: { value: "WIN-FILE01.corp.example" } });
    fireEvent.change(note!, { target: { value: "asset register" } });
    fireEvent.click(within(join).getByText("Join"));
    expect(transport.intents.at(-1)).toEqual({ type: "identity.assert", ref: host, relation: "aliases", other: "WIN-FILE01.corp.example", note: "asset register" });

    fireEvent.click(within(p).getByText("Not the same host as…"));
    const split = within(p).getByRole("form", { name: "Not the same host as" });
    fireEvent.change(split.querySelector("input")!, { target: { value: "x-host--00000000-0000-4000-8000-000000000002" } });
    fireEvent.click(within(split).getByText("Split"));
    expect(transport.intents.at(-1)).toEqual({ type: "identity.assert", ref: host, relation: "distinct", other: "x-host--00000000-0000-4000-8000-000000000002" });

    fireEvent.click(within(edge("relationship--3")).getByText("Withdraw…"));
    const withdraw = within(p).getByRole("form", { name: "Withdraw this assertion" });
    fireEvent.change(withdraw.querySelector("input")!, { target: { value: "wrong link" } });
    fireEvent.click(within(withdraw).getByText("Withdraw"));
    expect(transport.intents.at(-1)).toEqual({ type: "identity.withdraw", ref: host, edgeId: "relationship--3", note: "wrong link" });
  });

  it("offers no identity acts on an address", () => {
    mount(["corp"]);
    open(ip);
    const p = pane();
    expect(within(p).getByText("Entity")).toBeTruthy();
    expect(within(p).queryByText("Same as another host…")).toBeNull();
    expect(within(p).queryByText("Not the same host as…")).toBeNull();
    expect(within(p).queryByText("Also seen as")).toBeNull();
  });
});
