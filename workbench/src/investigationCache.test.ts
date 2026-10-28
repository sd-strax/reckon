import { describe, expect, it } from "vitest";
import type { ActionRow, Hypothesis, InvestigationDetail, ThreadEntry } from "./backend";
import { InvestigationCache, type CacheClient } from "./investigationCache";

const quiet = { debug: () => undefined };

function entry(sequenceNo: number, hasTranscript = false): ThreadEntry {
  return { sequenceNo, interpretationId: "i-" + sequenceNo, hasTranscript, occurredAt: "" } as ThreadEntry;
}

function detail(lastEventSequence: number, state = "ACTIVE"): InvestigationDetail {
  return { id: "inv-1", title: "T", state, lastEventSequence } as InvestigationDetail;
}

/** A backend whose answers the test sets, counting transcript reads. */
function fakeClient(over: Partial<Record<keyof CacheClient, () => Promise<unknown>>> = {}) {
  const calls = { transcript: 0 };
  const client = {
    getInvestigation: async () => detail(3),
    hypotheses: async () => [] as Hypothesis[],
    capabilities: async () => [],
    thread: async () => [entry(1), entry(2)],
    actions: async () => [] as ActionRow[],
    pins: async () => [],
    comms: async () => [],
    transcript: async (id: string) => { calls.transcript++; return { body: "body of " + id }; },
    ...over,
  } as unknown as CacheClient;
  return { client, calls };
}

describe("InvestigationCache: the watermark", () => {
  it("stamps a read with the investigation's last event sequence", async () => {
    const cache = new InvestigationCache(fakeClient().client, quiet);
    const snap = await cache.snapshot("inv-1");
    expect(snap.seq).toBe(3);
    expect(cache.watermark("inv-1")).toBe(3);
  });

  it("uses the thread's last entry when the thread read saw a newer event", async () => {
    const { client } = fakeClient({ thread: async () => [entry(4), entry(9)] });
    const cache = new InvestigationCache(client, quiet);
    expect((await cache.refresh("inv-1")).seq).toBe(9);
  });

  it("keeps the highest sequence read", async () => {
    let last = 8;
    const { client } = fakeClient({ getInvestigation: async () => detail(last), thread: async () => [] });
    const cache = new InvestigationCache(client, quiet);
    await cache.refresh("inv-1");
    last = 5; // a backend answering behind what was already read
    expect((await cache.refresh("inv-1")).seq).toBe(5);
    expect(cache.watermark("inv-1")).toBe(8);
  });
});

describe("InvestigationCache: what a read carries", () => {
  it("re-reads the investigation itself after a write, so its state is current", async () => {
    let state = "DRAFT";
    const { client } = fakeClient({ getInvestigation: async () => detail(4, state) });
    const cache = new InvestigationCache(client, quiet);
    await cache.snapshot("inv-1");
    state = "ACTIVE"; // approving the first action activated it
    const { patch } = await cache.refresh("inv-1");
    expect(patch.investigation?.state).toBe("ACTIVE");
  });

  it("leaves a failed optional slice out of a refresh, and degrades it in a snapshot", async () => {
    const { client } = fakeClient({ pins: async () => { throw new Error("503"); } });
    const cache = new InvestigationCache(client, quiet);
    const { patch } = await cache.refresh("inv-1");
    expect("pins" in patch).toBe(false);
    expect(patch.actions).toEqual([]);
    expect((await cache.snapshot("inv-1")).pins).toEqual([]);
  });

  it("fails the read when the investigation or its hypotheses cannot be read", async () => {
    const cache = new InvestigationCache(fakeClient({ hypotheses: async () => { throw new Error("500"); } }).client, quiet);
    await expect(cache.refresh("inv-1")).rejects.toThrow("500");
  });

  it("reads each committed transcript once", async () => {
    const { client, calls } = fakeClient({ thread: async () => [entry(1, true), entry(2, true)] });
    const cache = new InvestigationCache(client, quiet);
    const first = await cache.refresh("inv-1");
    await cache.refresh("inv-1");
    expect(first.patch.turns?.map((t) => t.body)).toEqual(["body of i-1", "body of i-2"]);
    expect(calls.transcript).toBe(2);
  });
});

describe("InvestigationCache: ordering", () => {
  it("runs operations on one investigation one after another, in the order asked", async () => {
    const cache = new InvestigationCache(fakeClient().client, quiet);
    const order: string[] = [];
    let release!: () => void;
    const slow = cache.exclusive("inv-1", async () => {
      order.push("slow start");
      await new Promise<void>((r) => { release = r; });
      order.push("slow end");
    });
    const fast = cache.exclusive("inv-1", async () => { order.push("fast"); });
    while (!release) await Promise.resolve();
    expect(order).toEqual(["slow start"]);
    release();
    await Promise.all([slow, fast]);
    expect(order).toEqual(["slow start", "slow end", "fast"]);
  });

  it("keeps going after a failed operation, and does not order different investigations", async () => {
    const cache = new InvestigationCache(fakeClient().client, quiet);
    const failed = cache.exclusive("inv-1", async () => { throw new Error("boom"); });
    await expect(failed).rejects.toThrow("boom");
    await expect(cache.exclusive("inv-1", async () => "after")).resolves.toBe("after");
    let release!: () => void;
    const held = cache.exclusive("inv-1", () => new Promise<void>((r) => { release = r; }));
    await expect(cache.exclusive("inv-2", async () => "other")).resolves.toBe("other");
    while (!release) await Promise.resolve();
    release();
    await held;
  });
});
