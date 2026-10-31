import { describe, expect, it } from "vitest";
import type * as vscode from "vscode";
import { SeenTracker } from "./seenTracker";

function memento(seed: Record<string, unknown> = {}) {
  const data = new Map(Object.entries(seed));
  return {
    data,
    get: <T>(key: string, fallback: T) => (data.has(key) ? data.get(key) as T : fallback),
    update: async (key: string, value: unknown) => {
      if (value === undefined) data.delete(key); else data.set(key, value);
    },
  } as unknown as vscode.Memento & { data: Map<string, unknown> };
}

describe("SeenTracker (adr/0241)", () => {
  it("keeps what each person has seen apart, across sign-outs", async () => {
    let who: string | undefined = "alice";
    const seen = new SeenTracker(memento(), () => who);
    await seen.set("inv-1", 5);

    who = "bob";
    expect(seen.get("inv-1")).toBe(0);
    await seen.set("inv-1", 2);

    who = undefined;
    expect(seen.get("inv-1")).toBe(0);

    who = "alice";
    expect(seen.get("inv-1")).toBe(5);
  });

  it("only moves forward", async () => {
    const seen = new SeenTracker(memento(), () => "alice");
    await seen.set("inv-1", 5);
    await seen.set("inv-1", 3);
    expect(seen.get("inv-1")).toBe(5);
  });

  it("records nothing with nobody signed in", async () => {
    const m = memento();
    await new SeenTracker(m, () => undefined).set("inv-1", 5);
    expect(m.data.size).toBe(0);
  });

  it("drops the marks that belonged to no one in particular", () => {
    const m = memento({ "reckon.seenSeq": { "inv-1": 9 } });
    const seen = new SeenTracker(m, () => "alice");
    expect(m.data.has("reckon.seenSeq")).toBe(false);
    expect(seen.get("inv-1")).toBe(0);
  });
});
