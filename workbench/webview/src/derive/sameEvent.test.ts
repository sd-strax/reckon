// One row per event (adr/0348).
import { describe, expect, it } from "vitest";
import type { EventGroup, EventRow } from "../../../src/viewTypes";
import { eventOfRecord, groupOverEvents, mergeSameEvents, viaVerbs } from "./sameEvent";

const od = "observed-data--19fc20e7-0000-4000-8000-000000000001";
const ev = (id: string, callId: string, verb: string, recordedAt: string, over: Partial<EventRow> = {}): EventRow => ({
  id, classUid: 1007, className: "Process Activity", time: "2026-10-20T09:00:00Z", recordedAt, sourceTool: "fixture", callId, verb,
  coverage: "COMPLETE", observedDataRefs: [od], entityRefs: [], view: { fields: [], category: "process" }, ...over,
});

describe("mergeSameEvents", () => {
  it("merges one PowerShell launch four queries returned into one event naming every call in retrieval order", () => {
    const rows = [
      ev("r4", "c3", "get_process_executions", "2026-10-20T09:03:00Z"),
      ev("r1", "c1", "get_process_executions", "2026-10-20T09:01:00Z"),
      ev("r2", "c2", "get_process_ancestry", "2026-10-20T09:02:00Z"),
      ev("r3", "c4", "get_process_ancestry", "2026-10-20T09:04:00Z", { observedDataRefs: ["observed-data--other"] }),
    ];
    const m = mergeSameEvents(rows.slice(0, 3));
    expect(m.length).toBe(1);
    expect(m[0]!.row.id).toBe("r1");
    expect(m[0]!.ids).toEqual(["r1", "r2", "r4"]);
    expect(m[0]!.calls.map((c) => c.callId)).toEqual(["c1", "c2", "c3"]);
    expect(viaVerbs(m[0]!.calls).map((v) => `${v.verb}×${v.calls.length}`)).toEqual(["get_process_executions×2", "get_process_ancestry×1"]);
    // A different observed-data ref is a different event.
    expect(mergeSameEvents(rows).length).toBe(2);
  });

  it("keeps two identical records one call returned as two events, each taking a copy from the other calls", () => {
    const m = mergeSameEvents([
      ev("a1", "c1", "enumerate_logons", "2026-10-20T09:01:00Z"),
      ev("a2", "c1", "enumerate_logons", "2026-10-20T09:01:00Z"),
      ev("b1", "c2", "enumerate_logons", "2026-10-20T09:02:00Z"),
    ]);
    expect(m.map((s) => s.ids)).toEqual([["a1", "b1"], ["a2"]]);
  });

  it("never merges records with no observed-data refs, nor across class or event time", () => {
    expect(mergeSameEvents([
      ev("n1", "c1", "v", "2026-10-20T09:01:00Z", { observedDataRefs: [] }),
      ev("n2", "c2", "v", "2026-10-20T09:02:00Z", { observedDataRefs: [] }),
    ]).length).toBe(2);
    expect(mergeSameEvents([
      ev("t1", "c1", "v", "2026-10-20T09:01:00Z"),
      ev("t2", "c2", "v", "2026-10-20T09:02:00Z", { time: "2026-10-20T09:00:01Z" }),
      ev("t3", "c3", "v", "2026-10-20T09:03:00Z", { classUid: 3002 }),
    ]).length).toBe(3);
  });

  it("compares the observed-data refs as a set", () => {
    const refs = [od, "observed-data--b"];
    expect(mergeSameEvents([
      ev("s1", "c1", "v", "2026-10-20T09:01:00Z", { observedDataRefs: refs }),
      ev("s2", "c2", "v", "2026-10-20T09:02:00Z", { observedDataRefs: [...refs].reverse() }),
    ]).length).toBe(1);
  });
});

describe("groupOverEvents", () => {
  it("counts the events a group's records stand for, not the retrievals", () => {
    const rows = [
      ev("d1", "c1", "v", "2026-10-20T09:01:00Z", { time: "2026-10-17T02:00:00Z" }),
      ev("d1b", "c2", "v", "2026-10-20T09:02:00Z", { time: "2026-10-17T02:00:00Z" }),
      ev("d2", "c1", "v", "2026-10-20T09:01:00Z", { time: "2026-10-18T02:00:00Z" }),
    ];
    const g: EventGroup = { classUid: 1007, key: [], count: 3, first: "2026-10-17T02:00:00Z", last: "2026-10-18T02:00:00Z", days: 2, eventIds: ["d2", "d1b", "d1"] };
    const over = groupOverEvents(g, eventOfRecord(mergeSameEvents(rows)));
    expect(over.count).toBe(2);
    expect(over.eventIds).toEqual(["d2", "d1"]);
  });
});
