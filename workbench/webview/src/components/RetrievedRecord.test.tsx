// The retrieved record (adr/0092): the Story timeline lists the events a
// capability call brought in, each opening the call that retrieved it; an
// opened event shows the retrieved events around it; the Call inspector shows
// which binding answered, with what, and what it produced.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { timelineItems } from "./lenses/StoryLens";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import type { CapabilityCall, EventRow } from "../../../src/viewTypes";

const base = synthetic as unknown as HarnessSnapshot;
const callId = "0192f3a0-0000-7000-8000-0000000000c1";
const cited = "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8"; // an event the synthetic stage cites and holds a doc for
const row = (over: Partial<EventRow>): EventRow => ({
  id: "0192f3a0-0000-7000-8000-00000000e001", classUid: 3002, className: "Authentication", time: "2026-10-20T08:50:00Z", recordedAt: "2026-10-20T09:05:00Z",
  sourceTool: "fixture:scenario", callId, verb: "enumerate_logons", coverage: "COMPLETE", observedDataRefs: [], entityRefs: ["x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01"], ...over,
});
const events: EventRow[] = [
  row({ id: "0192f3a0-0000-7000-8000-00000000e001", time: "2026-10-20T08:50:00Z" }),
  row({ id: cited, time: "2026-10-20T09:00:00Z" }),
  row({ id: "0192f3a0-0000-7000-8000-00000000e003", time: "2026-10-20T09:10:00Z", classUid: 1007, className: "Process Activity" }),
];
const call: CapabilityCall = {
  callId, verb: "enumerate_logons", adapter: "fixture", operation: "replay", priority: 100, paramsSent: { host: "host-a" }, coverage: "COMPLETE",
  bindingsAnswered: 1, bindingsTotal: 2, degradationNotes: ["binding enumerate_logons/edr: adapter not configured or disabled"], startedAt: "2026-10-20T09:05:00Z", durationMs: 42,
  produced: { eventIds: events.map((e) => e.id), observedDataRefs: ["observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04"], entityRefs: ["x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01"] },
  bindings: [{ adapter: "edr", operation: "logons", priority: 200, enabled: false }, { adapter: "fixture", operation: "replay", priority: 100, enabled: true }],
};
const data: HarnessSnapshot = { ...base, snapshot: { ...base.snapshot, events }, calls: { [callId]: call }, around: { [cited]: [events[0], events[1]] } };

function mount(d: HarnessSnapshot = data) {
  const transport = new HarnessTransport(d);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}
const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name: new RegExp("^" + name) }));
const lens = () => document.querySelector(".lensscroll") as HTMLElement;
const pane = () => document.querySelector(".inspector.active") as HTMLElement;

describe("timelineItems", () => {
  it("lists the retrieved events in event time and joins the cited refs the record holds no membership for, each once", () => {
    const items = timelineItems(events, [
      { ref: cited, time: "2026-10-20T09:00:00Z", label: "Authentication" },
      { ref: "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04", time: "2026-10-20T09:00:00Z", label: "observed-data" },
    ]);
    expect(items.map((i) => i.ref)).toEqual([events[0].id, cited, "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04", events[2].id]);
    expect(items[1].callId).toBe(callId);
    expect(items[2].callId).toBeUndefined();
    expect(items.map((i) => i.category)).toEqual(["auth", "auth", "observed", "process"]);
  });
  it("falls back to the cited events alone when the record holds no membership", () => {
    const items = timelineItems(null, [{ ref: "e1", time: "2026-10-20T09:00:00Z", label: "Authentication" }]);
    expect(items).toEqual([{ ref: "e1", time: "2026-10-20T09:00:00Z", label: "Authentication", category: "auth" }]);
  });
});

describe("the Story timeline over the retrieved record", () => {
  it("shows every retrieved event, filters by the class's category, and opens the call that retrieved a row", () => {
    const transport = mount();
    tab("Story");
    expect(within(lens()).getByText("3 retrieved events in the record")).toBeTruthy();
    const rows = lens().querySelectorAll(".trow .tvia");
    expect(rows.length).toBe(3);
    fireEvent.click(within(lens()).getByRole("tab", { name: "Process" }));
    expect(lens().querySelectorAll(".trow .tvia").length).toBe(1);
    fireEvent.click(within(lens()).getByRole("tab", { name: "Network" }));
    expect(within(lens()).getByText("No retrieved or cited event in this category.")).toBeTruthy();
    fireEvent.click(within(lens()).getByRole("tab", { name: "All" }));
    fireEvent.click(lens().querySelector(".tvia") as HTMLElement);
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "call", callId } });
    const p = pane();
    expect(within(p).getByText("Capability call")).toBeTruthy();
    expect(within(p).getByRole("heading", { level: 2 }).textContent).toBe("enumerate_logons");
    expect(within(p).getAllByText("fixture / replay").length).toBe(2); // Answered by, and the row in the bindings list
    expect(within(p).getByText(/1 of 2 bindings answered · 42 ms/)).toBeTruthy();
    expect(within(p).getByText("installed, not enabled")).toBeTruthy();
    expect(within(p).getByText("answered")).toBeTruthy();
    expect(p.querySelector(".ijson pre")!.textContent).toContain('"host": "host-a"');
    fireEvent.click(within(p).getByRole("tab", { name: "Result" }));
    expect(within(p).getByText("Events · 3")).toBeTruthy();
    expect(within(p).getByText("Entities · 1")).toBeTruthy();
  });

  it("opens an event with the retrieved events around it, the anchor left out, and names the call under Provenance", () => {
    mount();
    tab("Story");
    const chip = Array.from(lens().querySelectorAll(".refchip")).find((c) => c.getAttribute("data-ref") === cited) as HTMLElement;
    fireEvent.click(chip);
    const p = pane();
    expect(within(p).getByText("Around it")).toBeTruthy();
    const around = p.querySelectorAll(".irow.around");
    expect(around.length).toBe(1);
    expect(around[0].querySelector(".refchip")!.getAttribute("data-ref")).toBe(events[0].id);
    fireEvent.click(within(p).getByRole("tab", { name: "Provenance" }));
    expect(within(p).getByText("Retrieved by")).toBeTruthy();
    fireEvent.click(within(within(p).getByText("Retrieved by").parentElement as HTMLElement).getByText("enumerate_logons"));
    expect(within(pane()).getByText("Capability call")).toBeTruthy();
  });

  it("says the record holds nothing around an event when the window is empty", () => {
    mount({ ...data, around: { [cited]: [events[1]] } });
    tab("Story");
    const chip = Array.from(lens().querySelectorAll(".refchip")).find((c) => c.getAttribute("data-ref") === cited) as HTMLElement;
    fireEvent.click(chip);
    expect(within(pane()).getByText(/No other retrieved event on the same entity/)).toBeTruthy();
  });

  it("shows the cited events alone on a record without membership", () => {
    mount({ ...base });
    tab("Story");
    expect(screen.queryByText(/retrieved event/)).toBeNull();
    expect(lens().querySelectorAll(".trow").length).toBeGreaterThan(2);
    expect(lens().querySelectorAll(".tvia").length).toBe(0);
  });
});
