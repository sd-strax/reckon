// A host's identity in the Entity inspector (adr/0211): every alias with its
// origin, a withdrawn one labelled, the split listed, the seen-under line only
// when the id was seen under more than one domain, and the join, split and
// withdraw acts on a host and on nothing else.
import { act, cleanup, fireEvent, render, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AppProvider } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { HostIdentity } from "../../../../src/viewTypes";
import type { InboundMessage, RenderMessage } from "../../../../src/protocol";

const base = synthetic as unknown as HarnessSnapshot;
const host = base.snapshot.investigation.seed!.entityRef!;
const ip = "ipv4-addr--6b0c4d3e-9d5e-4071-bc3d-4e5f6a7b8c03";
const hostB = "x-host--00000000-0000-4000-8000-000000000004";
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
      [hostB]: { ref: hostB, kind: "stix", type: "x-host", payload: { type: "x-host", properties: { hostname: "ws1-old" } }, identity: { aliases: [{ edgeId: "relationship--b1", otherRef: host, kind: "observed", source: "edr", at, withdrawn: false }], distinct: [], seenAs: { domains: [], agents: [], ips: [] } } },
    },
    labels: { ...base.labels, [hostB]: "ws1-old" },
  };
  const transport = new HarnessTransport(data);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}
const pane = () => document.querySelector(".inspector.active") as HTMLElement;

type IdentityIntent = Extract<InboundMessage, { type: "identity.assert" | "identity.withdraw" }>;
const lastIdentityIntent = (t: HarnessTransport): IdentityIntent | undefined =>
  t.intents.filter((i): i is IdentityIntent => i.type === "identity.assert" || i.type === "identity.withdraw").at(-1);
/** Post a message as the host would (the harness only records identity acts). */
const emit = (t: HarnessTransport, m: RenderMessage) => act(() => { (t as unknown as { emit(m: RenderMessage): void }).emit(m); });
/** Answer the last identity act as recorded, with the view after it (null: unreadable). */
const settle = (t: HarnessTransport, view: HostIdentity | null) => {
  const i = lastIdentityIntent(t)!;
  emit(t, { type: "inspector.identity", ref: i.ref, requestId: i.requestId, views: view ? { identity: view } : null });
};
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
    expect(lastIdentityIntent(transport)).toEqual({ type: "identity.assert", ref: host, requestId: expect.any(String), relation: "aliases", other: "WIN-FILE01.corp.example", note: "asset register" });
    settle(transport, identity(["corp"]));
    expect(within(p).queryByRole("form", { name: "Same as another host" })).toBeNull(); // its own act landed

    fireEvent.click(within(p).getByText("Not the same host as…"));
    const split = within(p).getByRole("form", { name: "Not the same host as" });
    fireEvent.change(split.querySelector("input")!, { target: { value: "x-host--00000000-0000-4000-8000-000000000002" } });
    fireEvent.click(within(split).getByText("Split"));
    expect(lastIdentityIntent(transport)).toEqual({ type: "identity.assert", ref: host, requestId: expect.any(String), relation: "distinct", other: "x-host--00000000-0000-4000-8000-000000000002" });
    settle(transport, identity(["corp"]));

    fireEvent.click(within(edge("relationship--3")).getByText("Withdraw…"));
    const withdraw = within(p).getByRole("form", { name: "Withdraw this assertion" });
    fireEvent.change(withdraw.querySelector("input")!, { target: { value: "wrong link" } });
    fireEvent.click(within(withdraw).getByText("Withdraw"));
    expect(lastIdentityIntent(transport)).toEqual({ type: "identity.withdraw", ref: host, requestId: expect.any(String), edgeId: "relationship--3", note: "wrong link" });
  });

  it("does not carry a form opened on one host to the next host the pane shows", () => {
    const transport = mount(["corp"]);
    open(host);
    fireEvent.click(within(pane()).getByText("Same as another host…"));
    const join = within(pane()).getByRole("form", { name: "Same as another host" });
    fireEvent.change(join.querySelector("input")!, { target: { value: "WIN-FILE01" } });
    // Open the other host from its alias row, in the same pane.
    fireEvent.click(edge("relationship--3").querySelector(`[data-ref="${hostB}"]`) as HTMLElement);
    expect(within(pane()).getByRole("heading", { level: 2 }).textContent).toBe("ws1-old");
    expect(within(pane()).queryByRole("form", { name: "Same as another host" })).toBeNull();
    fireEvent.click(within(pane()).getByText("Same as another host…"));
    const fresh = within(pane()).getByRole("form", { name: "Same as another host" });
    expect((fresh.querySelector("input") as HTMLInputElement).value).toBe("");
    expect(transport.intents.some((i) => i.type === "identity.assert")).toBe(false);
  });

  it("holds one act per host: a second form cannot submit while one is in flight, and an answer settles only its own act", () => {
    const transport = mount(["corp"]);
    open(host);
    const p = pane();
    fireEvent.click(within(p).getByText("Same as another host…"));
    const join = within(p).getByRole("form", { name: "Same as another host" });
    fireEvent.change(join.querySelector("input")!, { target: { value: "WIN-FILE01" } });
    fireEvent.click(within(join).getByText("Join"));
    const joinIntent = lastIdentityIntent(transport)!;
    fireEvent.click(within(join).getByText("Cancel"));

    fireEvent.click(within(p).getByText("Not the same host as…"));
    const split = within(p).getByRole("form", { name: "Not the same host as" });
    fireEvent.change(split.querySelector("input")!, { target: { value: "ws9" } });
    const submit = within(split).getByText("Split") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    expect(within(p).getByText(/An act on this host is in flight/)).toBeTruthy();
    fireEvent.click(submit);
    expect(transport.intents.filter((i) => i.type === "identity.assert").length).toBe(1);

    // The join's answer settles the join alone: the split form stays open, now free to submit.
    emit(transport, { type: "inspector.identity", ref: host, requestId: joinIntent.requestId, views: { identity: identity(["corp"]) } });
    const still = within(p).getByRole("form", { name: "Not the same host as" });
    expect((within(still).getByText("Split") as HTMLButtonElement).disabled).toBe(false);
  });

  it("asks again for a host revealed by Back after an act elsewhere: its held view predates the act", () => {
    const transport = mount(["corp"]);
    open(host);
    // A → B through A's alias chip, then B → A through B's alias chip: the pane's stack is B, A.
    fireEvent.click(edge("relationship--3").querySelector(`[data-ref="${hostB}"]`) as HTMLElement);
    fireEvent.click(edge("relationship--b1").querySelector(`[data-ref="${host}"]`) as HTMLElement);
    expect(within(pane()).getByRole("heading", { level: 2 }).textContent).toBe("host-a");
    fireEvent.click(within(pane()).getByText("Same as another host…"));
    const join = within(pane()).getByRole("form", { name: "Same as another host" });
    fireEvent.change(join.querySelector("input")!, { target: { value: "ws9" } });
    fireEvent.click(within(join).getByText("Join"));
    settle(transport, identity(["corp"]));
    const before = transport.intents.length;
    fireEvent.click(within(pane()).getByText("‹ back"));
    expect(within(pane()).getByRole("heading", { level: 2 }).textContent).toBe("ws1-old");
    expect(transport.intents.slice(before)).toContainEqual({ type: "inspector.open", target: { kind: "ref", ref: hostB } });
  });

  it("reads a held host afresh when Back reveals it, with no act anywhere", () => {
    const transport = mount(["corp"]);
    open(host);
    fireEvent.click(edge("relationship--3").querySelector(`[data-ref="${hostB}"]`) as HTMLElement);
    const before = transport.intents.length;
    fireEvent.click(within(pane()).getByText("‹ back"));
    expect(within(pane()).getByRole("heading", { level: 2 }).textContent).toBe("host-a");
    expect(transport.intents.slice(before).filter((i) => i.type === "inspector.open")).toEqual([{ type: "inspector.open", target: { kind: "ref", ref: host } }]);
  });

  it("reads the host a pane shows afresh on a refresh, and once: an answer without its view does not ask again", () => {
    const transport = mount(["corp"]);
    open(host);
    // From here the host's answers are the test's, not the harness's.
    transport.send = (i) => { transport.intents.push(i); };
    const before = transport.intents.length;
    emit(transport, { type: "state.snapshot", seq: base.snapshot.seq, snapshot: base.snapshot }); // the refresh's reload
    const opens = () => transport.intents.slice(before).filter((i) => i.type === "inspector.open");
    expect(opens()).toEqual([{ type: "inspector.open", target: { kind: "ref", ref: host } }]);
    // The answer comes back degraded (no identity): the held view stays, marked, and nothing is asked again.
    const { identity: _dropped, ...degraded } = { ...base.docs![host]!, identity: identity(["corp"]) };
    emit(transport, { type: "inspector.evidence", ref: host, doc: degraded, appearances: [] });
    expect(opens().length).toBe(1);
    expect(within(pane()).getByText("This view may be out of date.")).toBeTruthy();
    expect(within(edge("relationship--1")).getByText("from edr")).toBeTruthy();
    // A second refresh retries though the host is still stale: exactly one more read.
    emit(transport, { type: "state.snapshot", seq: base.snapshot.seq, snapshot: base.snapshot });
    expect(opens().length).toBe(2);
    // Degraded again: nothing more on its own.
    emit(transport, { type: "inspector.evidence", ref: host, doc: degraded, appearances: [] });
    expect(opens().length).toBe(2);
  });

  it("retries a host whose first read failed on a reload", () => {
    const transport = mount(["corp"]);
    transport.send = (i) => { transport.intents.push(i); };
    open(host);
    emit(transport, { type: "inspector.error", target: host, message: "backend unreachable" });
    const before = transport.intents.length;
    emit(transport, { type: "state.snapshot", seq: base.snapshot.seq, snapshot: base.snapshot });
    expect(transport.intents.slice(before).filter((i) => i.type === "inspector.open")).toEqual([{ type: "inspector.open", target: { kind: "ref", ref: host } }]);
  });

  it("says a host's view may be out of date while it is stale, and asks for it again", () => {
    const transport = mount(["corp"]);
    open(host);
    expect(within(pane()).queryByText(/may be out of date/)).toBeNull();
    const before = transport.intents.length;
    // Another panel's act landed; the harness's answer to the re-ask is lost here.
    transport.send = (i) => { transport.intents.push(i); };
    emit(transport, { type: "identity.changed", ref: host });
    expect(within(pane()).getByText("This view may be out of date.")).toBeTruthy();
    expect(transport.intents.slice(before)).toContainEqual({ type: "inspector.open", target: { kind: "ref", ref: host } });
  });

  it("says an act was recorded when its view could not be read, keeping the last view", () => {
    const transport = mount(["corp"]);
    open(host);
    const p = pane();
    fireEvent.click(within(p).getByText("Not the same host as…"));
    const split = within(p).getByRole("form", { name: "Not the same host as" });
    fireEvent.change(split.querySelector("input")!, { target: { value: "ws9" } });
    fireEvent.click(within(split).getByText("Split"));
    settle(transport, null);
    expect(within(split).getByRole("status").textContent).toBe("recorded; the view could not be refreshed");
    expect(within(split).getByText("Close")).toBeTruthy();
    expect(within(edge("relationship--1")).getByText("from edr")).toBeTruthy(); // the last view stands
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
