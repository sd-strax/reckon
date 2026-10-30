import { describe, expect, it } from "vitest";
import type { ThreadEntry } from "../../../src/viewTypes";
import { groupTurns, isContextReset } from "./turns";

const entry = (sequenceNo: number, interpretationType: string, summary: string, hasTranscript: boolean): ThreadEntry => ({
  sequenceNo, occurredAt: "2026-01-01T00:00:00Z", actor: { principal: "a", kind: "AI_DELEGATED" },
  interpretationId: "i" + sequenceNo, interpretationType, summary, inputRefs: [], outputRefs: [],
  toolCalls: 0, hasTranscript, superseded: false, consultedSops: [], consultedSimilar: [],
});

// adr/0235: the reset marker is its type; a turn's summary is the model's own
// text and may start with anything.
describe("context reset marker", () => {
  it("is recognised by type, not by summary text", () => {
    expect(isContextReset(entry(1, "context-reset", "context reset: …", false))).toBe(true);
    expect(isContextReset(entry(2, "other", "context reset: here is what a reset does", true))).toBe(false);
  });

  it("marks the turn after a reset, and only that turn", () => {
    const thread = [
      entry(1, "other", "first", true),
      entry(2, "context-reset", "context reset: the analyst rebuilt…", false),
      entry(3, "other", "second", true),
      entry(4, "other", "context reset: quoted by the model", true),
    ];
    const bodies = [1, 3, 4].map((n) => ({ sequenceNo: n, body: `[user] q${n}\n[assistant] a${n}\n` }));
    const turns = groupTurns(thread, bodies as never, []);
    expect(turns.map((t) => [t.entry.sequenceNo, !!t.resetBefore])).toEqual([[1, false], [3, true], [4, false]]);
  });
});
