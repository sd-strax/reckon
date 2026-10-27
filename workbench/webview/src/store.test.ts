import { describe, expect, it } from "vitest";
import { initialState, reducer, type State } from "./store";
import type { InvestigationSnapshot, RenderMessage } from "../../src/protocol";

function snap(seq: number, title = "T"): InvestigationSnapshot {
  return {
    seq,
    investigation: { id: "inv-1", title, state: "ACTIVE", lastEventSequence: seq },
    hypotheses: [], actions: [], pins: [], comms: [], thread: [], turns: [],
    capabilities: null, enablement: null, knowledge: [], derived: null,
  };
}

function run(msgs: RenderMessage[], from: State = initialState): State {
  return msgs.reduce((s, msg) => reducer(s, { type: "msg", msg }), from);
}

describe("reducer: sequence discipline", () => {
  it("applies a snapshot and patches at or after its seq, drops older ones", () => {
    let s = run([{ type: "state.snapshot", seq: 5, snapshot: snap(5) }]);
    expect(s.seq).toBe(5);
    expect(s.loading).toBe(false);
    s = run([{ type: "state.patch", seq: 4, patch: { pins: [{ interpretationId: "p", finding: "stale", inputRefs: [], actor: "HUMAN", pinnedAt: "", superseded: false }] } }], s);
    expect(s.snap?.pins).toEqual([]); // stale patch dropped
    s = run([{ type: "state.patch", seq: 6, patch: { pins: [{ interpretationId: "p", finding: "fresh", inputRefs: [], actor: "HUMAN", pinnedAt: "", superseded: false }] } }], s);
    expect(s.seq).toBe(6);
    expect(s.snap?.pins[0].finding).toBe("fresh");
  });
  it("ignores a patch before any snapshot", () => {
    const s = run([{ type: "state.patch", seq: 1, patch: { pins: [] } }]);
    expect(s.snap).toBeNull();
  });
});

describe("reducer: the live conversation", () => {
  it("streams text into one segment, settles tool rows FIFO, keeps a failed turn", () => {
    let s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "turn.user", text: "hi" },
      { type: "turn.start" },
      { type: "turn.text", delta: "Hel" },
      { type: "turn.text", delta: "lo" },
      { type: "turn.tool", name: "a", input: { x: 1 } },
      { type: "turn.tool", name: "b", input: {} },
      { type: "turn.toolResult", name: "a", isError: false, coverage: "COMPLETE", events: 2, refs: ["r1"] },
      { type: "turn.text", delta: "after" },
    ]);
    expect(s.streaming).toBe(true);
    const turn = s.live[1];
    if (turn.kind !== "assistant") throw new Error("expected a turn");
    expect(turn.segments.map((x) => x.kind)).toEqual(["text", "tool", "tool", "text"]);
    const first = turn.segments[1];
    if (first.kind !== "tool") throw new Error();
    expect(first.row.result?.detail).toBe("COMPLETE · 2 event(s)");
    const second = turn.segments[2];
    if (second.kind !== "tool") throw new Error();
    expect(second.row.result).toBeUndefined();
    // A thread re-post while streaming must not clear the live view.
    s = run([{ type: "state.patch", seq: 2, patch: { thread: [] } }], s);
    expect(s.live.length).toBe(2);
    // A failed turn is preserved across the next thread render.
    s = run([{ type: "turn.error", message: "boom" }, { type: "turn.end" }, { type: "state.patch", seq: 3, patch: { thread: [] } }], s);
    expect(s.preserveLive).toBe(true);
    expect(s.live.some((i) => i.kind === "error")).toBe(true);
    // A new turn supersedes the preserved view.
    s = run([{ type: "turn.user", text: "again" }], s);
    expect(s.preserveLive).toBe(false);
  });
  it("clears the live view once the committed thread re-posts after an idle turn", () => {
    let s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "turn.user", text: "hi" }, { type: "turn.start" }, { type: "turn.text", delta: "x" },
      { type: "turn.end", usage: { input: 10, output: 5, cacheRead: 0 }, rounds: 1 },
    ]);
    expect(s.streaming).toBe(false);
    const turn = s.live[1];
    if (turn.kind !== "assistant") throw new Error();
    expect(turn.segments[turn.segments.length - 1]).toEqual({ kind: "usage", text: "1 tool round · 10 in / 5 out" });
    s = run([{ type: "state.patch", seq: 2, patch: { thread: [] } }], s);
    expect(s.live).toEqual([]);
  });
  it("renders an aside collapsed and marks it done", () => {
    const s = run([{ type: "state.snapshot", seq: 1, snapshot: snap(1) }, { type: "aside.start", label: "⚡ decide · H1" }, { type: "turn.end" }]);
    const t = s.live[0];
    if (t.kind !== "assistant") throw new Error();
    expect(t.aside).toEqual({ label: "⚡ decide · H1", done: true });
  });
});

describe("reducer: knowledge toggles survive slice re-posts", () => {
  it("keeps the analyst's choice when the rail is re-posted", () => {
    const item = { kind: "sop" as const, ref: "sop-1", title: "S", excerpt: "", score: 0.9, included: false };
    let s = run([{ type: "state.snapshot", seq: 1, snapshot: { ...snap(1), knowledge: [item] } }]);
    s = reducer(s, { type: "knowledge.toggle", ref: "sop-1", included: true });
    s = run([{ type: "state.patch", seq: 1, patch: { knowledge: [item] } }], s);
    expect(s.knowledge[0].included).toBe(true);
  });
});

describe("reducer: rename", () => {
  it("updates the title in place and bumps a tree-initiated request", () => {
    let s = run([{ type: "state.snapshot", seq: 1, snapshot: snap(1, "Old") }, { type: "renamed", title: "New" }]);
    expect(s.title).toBe("New");
    expect(s.snap?.investigation.title).toBe("New");
    s = run([{ type: "rename.begin", title: "New" }, { type: "rename.begin", title: "New" }], s);
    expect(s.renameRequest?.nonce).toBe(2);
  });
});
