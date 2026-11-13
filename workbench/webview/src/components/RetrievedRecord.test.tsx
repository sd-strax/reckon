// The retrieved record (adr/0092): the Story timeline lists the events a
// capability call brought in, each opening the call that retrieved it; an
// opened event shows the retrieved events around it; the Call inspector shows
// which binding answered, with what, and what it produced.
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { retrievedCount, timelineItems } from "./lenses/StoryLens";
import { keySpec, spanText } from "../derive/events";
import { bindingRows } from "./inspectors/CallInspector";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import type { CapabilityCall, EventRow } from "../../../src/viewTypes";

// The stage without its retrieved record: these tests build their own.
const stage = synthetic as unknown as HarnessSnapshot;
const base: HarnessSnapshot = { ...stage, snapshot: { ...stage.snapshot, events: undefined, eventsTotal: undefined, eventsCursor: undefined, eventGroups: undefined, eventKeys: undefined, eventsFoldedOver: undefined }, calls: undefined, repeats: undefined };
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
    expect(items).toEqual([{ ref: "e1", time: "2026-10-20T09:00:00Z", label: "Authentication", category: "auth", ids: ["e1"] }]);
  });
});

// One row per event (adr/0348): an event overlapping queries returned is one
// row naming every call; a citation of any of its records marks the row.
describe("the Story timeline, one row per event", () => {
  const od = "observed-data--19fc20e7-0000-4000-8000-000000000001";
  const ps = (id: string, cid: string, verb: string, recordedAt: string): EventRow => row({ id, callId: cid, verb, recordedAt, time: "2026-10-20T09:00:00Z", observedDataRefs: [od] });
  const four = [
    ps("0192f3a0-0000-7000-8000-00000000f001", "c-exec-1", "get_process_executions", "2026-10-20T09:01:00Z"),
    ps("0192f3a0-0000-7000-8000-00000000f002", "c-anc-1", "get_process_ancestry", "2026-10-20T09:02:00Z"),
    ps("0192f3a0-0000-7000-8000-00000000f003", "c-exec-2", "get_process_executions", "2026-10-20T09:03:00Z"),
    ps(cited, "c-exec-3", "get_process_executions", "2026-10-20T09:04:00Z"), // the cited record, not the first retrieved
  ];

  it("shows one PowerShell launch four queries returned as one row naming each query, every call reachable", () => {
    const items = timelineItems(four, [{ ref: cited, time: "2026-10-20T09:00:00Z", label: "Authentication" }]);
    expect(items.length).toBe(1);
    expect(items[0]!.ref).toBe(four[0]!.id);
    // A prediction cites the last-retrieved record only.
    const [h0, ...hRest] = data.snapshot.hypotheses;
    const hypotheses = [{ ...h0!, predictions: h0!.predictions.map((p, i) => i ? p : { ...p, testResultRefs: [cited] }) }, ...hRest];
    const transport = mount({ ...data, snapshot: { ...data.snapshot, hypotheses, events: four } });
    tab("Story");
    const l = lens();
    expect(l.querySelectorAll(".trow .tvias").length).toBe(1);
    expect((l.querySelector(".tvias") as HTMLElement).textContent).toBe("via get_process_executions ×3, get_process_ancestry");
    fireEvent.click(within(l).getByText("get_process_ancestry"));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "call", callId: "c-anc-1" } });
    fireEvent.click(within(l).getByRole("button", { name: "×3" }));
    fireEvent.click(within(l).getByTitle("Open get_process_executions call 3 of 3"));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "call", callId: "c-exec-3" } });
    // The cited record is the last retrieved, and the row still carries its citations.
    expect(l.querySelector(".tvias")!.closest(".trow")!.querySelectorAll(".pcite").length).toBeGreaterThan(0);
  });

  it("counts a repeat group's events, not its retrievals", () => {
    const night = (id: string, day: string, cid: string) => row({ id, time: `2026-10-${day}T02:00:00Z`, callId: cid, observedDataRefs: [`observed-data--${day}`] });
    const recs = [night("n3", "19", "c1"), night("n2", "18", "c1"), night("n2b", "18", "c2"), night("n1", "17", "c1")];
    const g = { ...nightlyGroup, count: 4, eventIds: recs.map((e) => e.id) };
    const items = timelineItems(recs, [], [g]);
    expect(items.length).toBe(1);
    expect(spanText(items[0]!.group!)).toBe("3 events over 3 days");
    expect(items[0]!.members!.map((m) => m.ids)).toEqual([["n3"], ["n2", "n2b"], ["n1"]]);
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

// The folded record (adr/0095): a group that repeats is one row that opens to
// its members; the analyst changes the key per class or turns the fold off;
// the Entity inspector names what repeats on the entity.
const nightly = [
  row({ id: "0192f3a0-0000-7000-8000-00000000a003", time: "2026-10-19T02:00:00Z" }),
  row({ id: "0192f3a0-0000-7000-8000-00000000a002", time: "2026-10-18T02:00:00Z" }),
  row({ id: "0192f3a0-0000-7000-8000-00000000a001", time: "2026-10-17T02:00:00Z" }),
];
const nightlyGroup = {
  classUid: 3002, count: 3, days: 3, first: nightly[2]!.time, last: nightly[0]!.time, eventIds: nightly.map((e) => e.id),
  key: [{ slot: "user", label: "Account", text: "user-a" }, { slot: "logon_type", label: "Logon type", text: "interactive" }, { slot: "src", label: "Source", text: "10.0.0.9" }, { slot: "dst", label: "Destination", text: "host-a", ref: host }],
};
const burst = events.slice(0, 2); // two events on one day: a group that does not repeat
const burstGroup = { ...nightlyGroup, count: 2, days: 1, first: burst[0]!.time, last: burst[1]!.time, eventIds: burst.map((e) => e.id).reverse() };
const authKeys = { classUid: 3002, className: "Authentication", slots: [{ name: "user", label: "Account" }, { name: "logon_type", label: "Logon type" }, { name: "src", label: "Source" }, { name: "dst", label: "Destination" }, { name: "status", label: "Result" }], default: ["user", "logon_type", "src", "dst"], key: ["user", "logon_type", "src", "dst"] };
const foldedData: HarnessSnapshot = { ...data, snapshot: { ...data.snapshot, events: [...nightly, ...events], eventsTotal: 6, eventGroups: [burstGroup, nightlyGroup], eventKeys: [authKeys], eventsFoldedOver: 6 } };

describe("timelineItems over groups", () => {
  it("folds a group that repeats into one item at its first event and leaves a group that does not as rows", () => {
    const items = timelineItems([...nightly, ...events], [], [burstGroup, nightlyGroup]);
    expect(items.map((i) => i.group ? `group:${i.group.count}` : i.ref)).toEqual([`group:3`, events[0]!.id, events[1]!.id, events[2]!.id]);
    expect(items[0]!.time).toBe(nightly[2]!.time);
    expect(items[0]!.ref).toBe(nightly[0]!.id); // the newest member stands for the group
  });

  it("says how many of the record the fold covered", () => {
    expect(retrievedCount(6, 6, 6)).toBe("6 retrieved events in the record");
    expect(retrievedCount(1000, 1400, 1000)).toBe("1400 retrieved events in the record · folded over the newest 1000");
  });
});

describe("the Story timeline folded", () => {
  it("shows a repeating group as one row that opens to its members, and the fold control on", () => {
    mount(foldedData);
    tab("Story");
    const l = lens();
    expect(within(l).getByText("6 retrieved events in the record")).toBeTruthy();
    expect((within(l).getByLabelText("Fold repeated events") as HTMLInputElement).checked).toBe(true);
    expect(l.querySelectorAll(".trow.group").length).toBe(1); // the nightly group as one row
    expect(l.querySelectorAll(".trow .tvia").length).toBe(3); // the three unfolded retrieved rows; the group row names no single call
    const fold = within(l).getByText(/^3 events over 3 days/);
    expect(l.querySelectorAll(".trow.member").length).toBe(0);
    fireEvent.click(fold);
    expect(l.querySelectorAll(".trow.member").length).toBe(3);
    fireEvent.click(fold);
    expect(l.querySelectorAll(".trow.member").length).toBe(0);
  });

  it("asks the host for a different key, or for the flat page when the fold is turned off", () => {
    const transport = mount(foldedData);
    tab("Story");
    const l = lens();
    fireEvent.click(within(l).getByText(/^Authentication: Account · Logon type · Source · Destination/));
    fireEvent.click(within(l).getByLabelText("Logon type"));
    expect(transport.intents).toContainEqual({ type: "events.fold", keys: "3002:user,src,dst" });
    fireEvent.click(within(l).getByLabelText("Fold repeated events"));
    expect(transport.intents).toContainEqual({ type: "events.fold", keys: null });
    cleanup();
    const flat = mount(); // a flat record: the control is off and turns the fold on with the default key
    tab("Story");
    expect((within(lens()).getByLabelText("Fold repeated events") as HTMLInputElement).checked).toBe(false);
    fireEvent.click(within(lens()).getByLabelText("Fold repeated events"));
    expect(flat.intents).toContainEqual({ type: "events.fold", keys: "default" });
  });

  it("names what repeats on an opened entity, and nothing when nothing does", () => {
    const repeats = { events: [...nightly, events[0]!], groups: [nightlyGroup, { ...burstGroup, count: 1, eventIds: [events[0]!.id] }], total: 4, foldedOver: 4 };
    mount({ ...foldedData, repeats: { [host]: repeats } });
    tab("Story");
    fireEvent.click(within(lens()).getAllByText("host-a")[0]!);
    expect(within(pane()).getByText("Repeated observations")).toBeTruthy();
    expect(within(pane()).getByText("3 events over 3 days")).toBeTruthy();
    expect(within(pane()).getByText("1 retrieved event on this entity does not repeat.")).toBeTruthy();
    cleanup();
    // A record larger than the fold covers: the claim is scoped to what was covered.
    mount({ ...foldedData, repeats: { [host]: { ...repeats, total: 1200, foldedOver: 4 } } });
    tab("Story");
    fireEvent.click(within(lens()).getAllByText("host-a")[0]!);
    expect(within(pane()).getByText("1200 retrieved events on this entity · folded over the newest 4")).toBeTruthy();
    expect(within(pane()).getByText("1 event of the newest 4 on this entity does not repeat.")).toBeTruthy();
    cleanup();
    mount(foldedData);
    tab("Story");
    fireEvent.click(within(lens()).getAllByText("host-a")[0]!);
    expect(within(pane()).queryByText("Repeated observations")).toBeNull();
    expect(within(pane()).queryByText(/Normal behaviour/)).toBeNull();
  });
});

describe("keySpec", () => {
  it("round-trips the read's own key list, leaving out a class with nothing to key on, and keeps an empty key as a choice", () => {
    const opaque = { classUid: 6003, className: "API Activity", slots: [], default: [], key: [] };
    expect(keySpec([authKeys, opaque], 3002, ["user"])).toBe("3002:user");
    expect(keySpec([authKeys, opaque], 3002, [])).toBe("3002:");
  });
});
