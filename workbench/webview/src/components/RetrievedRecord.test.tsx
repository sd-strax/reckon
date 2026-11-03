// The retrieved record (adr/0092): the Story timeline lists the events a
// capability call brought in, each opening the call that retrieved it; an
// opened event shows the retrieved events around it; the Call inspector shows
// which binding answered, with what, and what it produced.
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { retrievedCount, timelineItems } from "./lenses/StoryLens";
import { bindingRows } from "./inspectors/CallInspector";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import type { CapabilityCall, EventRow } from "../../../src/viewTypes";

const base = synthetic as unknown as HarnessSnapshot;
const callId = "0192f3a0-0000-7000-8000-0000000000c1";
const cited = "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8"; // an event the synthetic stage cites and holds a doc for
const host = "x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";
// The server's rendering of the authentication rows (adr/0090): the host as a chip, the rest as text.
const authView = {
  sentence: [{ t: "text" as const, text: "user-a interactive logon to " }, { t: "entity" as const, ref: host, text: "host-a" }, { t: "text" as const, text: " from 10.0.0.9" }],
  fields: [{ label: "User", value: "user-a", mono: true }, { label: "Destination", value: "host-a", ref: host, mono: true }, { label: "Result", value: "Success", tone: "ok" as const }],
  category: "auth",
};
const row = (over: Partial<EventRow>): EventRow => ({
  id: "0192f3a0-0000-7000-8000-00000000e001", classUid: 3002, className: "Authentication", time: "2026-10-20T08:50:00Z", recordedAt: "2026-10-20T09:05:00Z",
  sourceTool: "fixture:scenario", callId, verb: "enumerate_logons", coverage: "COMPLETE", observedDataRefs: [], entityRefs: [host], view: authView, ...over,
});
const events: EventRow[] = [
  row({ id: "0192f3a0-0000-7000-8000-00000000e001", time: "2026-10-20T08:50:00Z" }),
  row({ id: cited, time: "2026-10-20T09:00:00Z" }),
  // A class without a template: no sentence, the class name reads instead.
  row({ id: "0192f3a0-0000-7000-8000-00000000e003", time: "2026-10-20T09:10:00Z", classUid: 1007, className: "Process Activity", view: { fields: [{ label: "pid", value: "7" }], category: "process" } }),
];
const call: CapabilityCall = {
  callId, verb: "enumerate_logons", adapter: "fixture", operation: "replay", priority: 100, paramsSent: { host: "host-a" }, coverage: "COMPLETE",
  bindingsAnswered: 1, bindingsTotal: 2, degradationNotes: ["binding enumerate_logons/edr: adapter not configured or disabled"], startedAt: "2026-10-20T09:05:00Z", durationMs: 42,
  outcomes: [
    { adapter: "edr", operation: "logons", priority: 200, params: { host: "host-a" }, answered: false, skipped: "adapter not configured or disabled" },
    { adapter: "fixture", operation: "replay", priority: 100, params: { host: "host-a" }, answered: true },
  ],
  produced: { eventIds: events.map((e) => e.id), observedDataRefs: ["observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04"], entityRefs: ["x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01"] },
  bindings: [{ adapter: "edr", operation: "logons", priority: 200, enabled: false }, { adapter: "fixture", operation: "replay", priority: 100, enabled: true }],
};
const data: HarnessSnapshot = { ...base, snapshot: { ...base.snapshot, events }, calls: { [callId]: call }, around: { [cited]: [events[0]!, events[1]!] } };

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
    expect(items.map((i) => i.ref)).toEqual([events[0]!.id, cited, "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04", events[2]!.id]);
    expect(items[1]!.callId).toBe(callId);
    expect(items[2]!.callId).toBeUndefined();
    expect(items.map((i) => i.category)).toEqual(["auth", "auth", "observed", "process"]);
  });
  it("falls back to the cited events alone when the record holds no membership", () => {
    const items = timelineItems(null, [{ ref: "e1", time: "2026-10-20T09:00:00Z", label: "Authentication" }]);
    expect(items).toEqual([{ ref: "e1", time: "2026-10-20T09:00:00Z", label: "Authentication", category: "auth" }]);
  });
});

describe("bindingRows", () => {
  it("drives each badge from the stored outcome and lists the configured bindings the resolver never reached", () => {
    const rows = bindingRows(call.outcomes, [
      { adapter: "siem", operation: "search", priority: 300, enabled: false },
      { adapter: "fixture", operation: "replay", priority: 100, enabled: true },
    ]);
    expect(rows.map((r) => [r.adapter, r.outcome?.answered ?? null, r.enabled ?? null])).toEqual([
      ["siem", null, false],
      ["edr", false, null],
      ["fixture", true, true],
    ]);
  });
});

describe("retrievedCount", () => {
  it("says how many of the record's events are shown when the record holds more than the page", () => {
    expect(retrievedCount(3, undefined)).toBe("3 retrieved events in the record");
    expect(retrievedCount(3, 3)).toBe("3 retrieved events in the record");
    expect(retrievedCount(1, 1)).toBe("1 retrieved event in the record");
    expect(retrievedCount(500, 1200)).toBe("1200 retrieved events in the record · showing the newest 500");
  });
});

describe("the Story timeline over the retrieved record", () => {
  it("names the unpaged total and says the record could not be read when it could not", () => {
    mount({ ...data, snapshot: { ...data.snapshot, events, eventsTotal: 12 } });
    tab("Story");
    expect(within(lens()).getByText("12 retrieved events in the record · showing the newest 3")).toBeTruthy();
    cleanup();
    mount({ ...base, snapshot: { ...base.snapshot, eventsError: "503 from the backend" } });
    tab("Story");
    expect(within(lens()).getByText(/The retrieved record could not be read: 503 from the backend\. Only the cited events are shown\./)).toBeTruthy();
    expect(lens().querySelectorAll(".trow").length).toBeGreaterThan(2); // the cited events still show
    cleanup();
    // A failed "Show older" leaves the pages already read on screen, so the line is about the next page.
    mount({ ...data, snapshot: { ...data.snapshot, events, eventsTotal: 12, eventsCursor: "c", eventsError: "store down" } });
    tab("Story");
    expect(within(lens()).getByText(/^The next page of the retrieved record could not be read: store down\.$/)).toBeTruthy();
    expect(lens().querySelectorAll(".trow .tvia").length).toBe(3);
  });

  it("asks the host for the next older page while the record holds more", () => {
    const transport = mount({ ...data, snapshot: { ...data.snapshot, events, eventsTotal: 12, eventsCursor: "2026-10-20T08:50:00Z,e001" } });
    tab("Story");
    fireEvent.click(within(lens()).getByText("Show older retrieved events"));
    expect(transport.intents).toContainEqual({ type: "events.older", cursor: "2026-10-20T08:50:00Z,e001" });
    cleanup();
    mount({ ...data, snapshot: { ...data.snapshot, events, eventsTotal: 3, eventsCursor: null } });
    tab("Story");
    expect(within(lens()).queryByText("Show older retrieved events")).toBeNull();
  });

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
    expect(within(p).getByText("skipped · adapter not configured or disabled")).toBeTruthy();
    expect(within(p).getByText("answered")).toBeTruthy();
    expect(within(p).queryByText("installed, not enabled")).toBeNull(); // the stored outcome says why, the config flag does not override it
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
    expect(around[0]!.querySelector(".refchip")!.getAttribute("data-ref")).toBe(events[0]!.id);
    fireEvent.click(within(p).getByRole("tab", { name: "Provenance" }));
    expect(within(p).getByText("Retrieved by")).toBeTruthy();
    fireEvent.click(within(within(p).getByText("Retrieved by").parentElement as HTMLElement).getByText("enumerate_logons"));
    expect(within(pane()).getByText("Capability call")).toBeTruthy();
  });

  it("reads each retrieved row as the server's sentence with its entities as chips, the class name when there is none, and the curated fields in the opened event", () => {
    const transport = mount({ ...data, docs: { ...base.docs, [cited]: { ...base.docs![cited]!, view: authView } } });
    tab("Story");
    const rows = Array.from(lens().querySelectorAll(".trow"));
    const first = rows.find((r) => r.querySelector(`[data-ref="${events[0]!.id}"]`))!;
    expect(first.querySelector(".sentence")!.textContent).toBe("user-a interactive logon to host-a from 10.0.0.9");
    expect(first.querySelector(`.sentence .refchip[data-ref="${host}"]`)!.textContent).toBe("host-a");
    const third = rows.find((r) => r.querySelector(`[data-ref="${events[2]!.id}"]`))!;
    expect(third.querySelector(".sentence")).toBeNull();
    expect(within(third as HTMLElement).getByText("Process Activity")).toBeTruthy();
    // The opened event: the sentence under the header, the curated fields before the payload's.
    fireEvent.click(Array.from(lens().querySelectorAll(".refchip")).find((c) => c.getAttribute("data-ref") === cited) as HTMLElement);
    const p = pane();
    expect(p.querySelector(".isentence .sentence")!.textContent).toBe("user-a interactive logon to host-a from 10.0.0.9");
    expect(within(p).getByText("Summary")).toBeTruthy();
    expect(within(p).getByText("Destination").nextElementSibling!.querySelector(".refchip")!.getAttribute("data-ref")).toBe(host);
    expect(p.querySelector(".badge.ok")!.textContent).toBe("Success"); // the Result field, toned; the raw payload shows the value as text too
    expect(within(p).getByText("Event fields")).toBeTruthy();
    // An around row reads as its sentence too.
    expect(p.querySelector(".irow.around .sentence")!.textContent).toBe("user-a interactive logon to host-a from 10.0.0.9");
    // The entity chip inside the sentence opens the entity.
    fireEvent.click(p.querySelector(`.isentence .refchip[data-ref="${host}"]`) as HTMLElement);
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "ref", ref: host } });
  });

  it("says the record holds nothing around an event when the window is empty", () => {
    mount({ ...data, around: { [cited]: [events[1]!] } });
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
