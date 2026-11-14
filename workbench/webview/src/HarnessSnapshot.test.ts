// The synthetic harness stage carries every record its views open: the
// capability call behind a tool row and an entity's folded repeats (adr/0092,
// adr/0095), so a fixture edit that drops one shows here.
import { describe, expect, it } from "vitest";
import { HarnessTransport, type HarnessSnapshot } from "../../harness/transport";
import synthetic from "../../harness/snapshots/synthetic.json";
import type { RenderMessage } from "../../src/protocol";

describe("the synthetic harness stage", () => {
  it("answers the capability call and the entity's repeats from its records", () => {
    const data = synthetic as unknown as HarnessSnapshot;
    const t = new HarnessTransport(data);
    const seen: RenderMessage[] = [];
    t.onMessage((m) => seen.push(m));
    const callId = Object.keys(data.calls ?? {})[0]!;
    const host = Object.keys(data.repeats ?? {})[0]!;
    expect(callId).toBeTruthy();
    expect(host).toBeTruthy();
    t.send({ type: "inspector.open", target: { kind: "call", callId } });
    t.send({ type: "inspector.open", target: { kind: "ref", ref: host } });
    expect(seen.find((m) => m.type === "inspector.call")).toMatchObject({ callId });
    const ev = seen.find((m) => m.type === "inspector.evidence") as Extract<RenderMessage, { type: "inspector.evidence" }>;
    expect(ev.repeats).toEqual(data.repeats![host]);
    expect(seen.some((m) => m.type === "inspector.error")).toBe(false);
  });
});
