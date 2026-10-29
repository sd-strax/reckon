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
});
