import { describe, expect, it } from "vitest";
import { SingleFlight } from "./singleFlight";

describe("one session per investigation (#157)", () => {
  it("concurrent callers for one key share one run; another key runs on its own", async () => {
    const sf = new SingleFlight<string>();
    let runs = 0;
    let release!: (v: string) => void;
    const start = () => { runs++; return new Promise<string>((r) => { release = r; }); };
    const a = sf.run("inv-1", start);
    const b = sf.run("inv-1", start);
    expect(runs).toBe(1);
    const other = sf.run("inv-2", async () => "s-2");
    release("s-1");
    expect(await a).toBe("s-1");
    expect(await b).toBe("s-1");
    expect(await other).toBe("s-2");
    await sf.run("inv-1", async () => "again");
    expect(runs).toBe(1); // settled: the next call starts afresh (and the transport's map already holds the session)
  });
  it("a failed run is not cached", async () => {
    const sf = new SingleFlight<string>();
    await expect(sf.run("k", async () => { throw new Error("boom"); })).rejects.toThrow("boom");
    expect(await sf.run("k", async () => "ok")).toBe("ok");
  });
  it("settled waits out a run, success or failure, and is immediate with none", async () => {
    const sf = new SingleFlight<string>();
    let fail!: (e: Error) => void;
    void sf.run("k", () => new Promise((_, reject) => { fail = reject; })).catch(() => undefined);
    let settled = false;
    const waiting = sf.settled("k").then(() => { settled = true; });
    await Promise.resolve();
    expect(settled).toBe(false);
    fail(new Error("boom"));
    await waiting;
    expect(settled).toBe(true);
    await sf.settled("none");
  });
  it("a run from before a clear does not end a newer run's share", async () => {
    const sf = new SingleFlight<string>();
    let finishOld!: (v: string) => void;
    const old = sf.run("k", () => new Promise((r) => { finishOld = r; }));
    sf.clear();
    let finishNew!: (v: string) => void;
    let runs = 0;
    const fresh = sf.run("k", () => { runs++; return new Promise((r) => { finishNew = r; }); });
    finishOld("old");
    await old;
    const joined = sf.run("k", () => { runs++; return Promise.resolve("again"); });
    finishNew("new");
    expect(await fresh).toBe("new");
    expect(await joined).toBe("new");
    expect(runs).toBe(1);
  });
});
