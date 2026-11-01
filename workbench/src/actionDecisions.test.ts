// @vitest-environment node
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ActionDecisions } from "./actionDecisions";
import type { ActionRow } from "./backend";
import type { DocumentHost } from "./documentHost";
import type { RenderMessage } from "./protocol";
import { fakeLog, resetVscodeFake } from "./test/vscode";

/** A host whose record the test sets; refreshes and posts are counted, nothing else is real. */
function host(over: { action?: ActionRow; approve?: () => Promise<unknown>; open?: boolean } = {}) {
  const posted: RenderMessage[] = [];
  const refresh = vi.fn(async () => undefined);
  const h = {
    client: { actions: { approve: over.approve ?? (async () => ({ status: "APPROVED" })) } },
    log: fakeLog(),
    cache: { action: () => over.action },
    post: (_p: vscode.WebviewPanel, m: RenderMessage) => { posted.push(m); return Promise.resolve(true); },
    refresh,
    sameIdentity: () => () => true,
    isOpen: () => over.open ?? true,
  } as unknown as DocumentHost;
  return { h, posted, refresh };
}

const panel = () => vscode.window.createWebviewPanel("t", "T", vscode.ViewColumn.Active, {});

beforeEach(() => resetVscodeFake());
afterEach(() => vi.useRealTimers());

describe("ActionDecisions: deciding", () => {
  it("re-reads once to find an action it does not hold, and not again when it is not there", async () => {
    const { h, posted, refresh } = host();
    await new ActionDecisions(h).approve("inv-1", panel(), { actionId: "a-1" });

    expect(refresh).toHaveBeenCalledTimes(1);
    expect(vscode.window.showErrorMessage).toHaveBeenCalledWith(expect.stringContaining("not in this investigation's record"));
    expect(posted).toEqual([{ type: "action.settled", actionId: "a-1" }]);
  });

  it("refreshes after a decision over a held action, whether it succeeded or was refused", async () => {
    const row = { actionId: "a-1", actionType: "host.isolate", tier: "T1", status: "PROPOSED" } as ActionRow;
    const ok = host({ action: row });
    await new ActionDecisions(ok.h).approve("inv-1", panel(), { actionId: "a-1" });
    expect(ok.refresh).toHaveBeenCalledTimes(1);
    expect(ok.posted).toEqual([{ type: "action.settled", actionId: "a-1" }]);

    const refused = host({ action: row, approve: async () => { throw new Error("Gate 2: denied"); } });
    await new ActionDecisions(refused.h).approve("inv-1", panel(), { actionId: "a-1" });
    expect(refused.refresh).toHaveBeenCalledTimes(1);
    expect(vscode.window.showErrorMessage).toHaveBeenCalledWith("reckon: approve failed — Gate 2: denied");
    expect(refused.posted).toEqual([{ type: "action.settled", actionId: "a-1" }]);
  });
});

describe("ActionDecisions: the dispatch watch", () => {
  const inFlight = [{ actionId: "a-1", status: "EXECUTING" }] as ActionRow[];

  it("re-reads while an action is mid-dispatch, stops once nothing is, and never outlives its panel", async () => {
    vi.useFakeTimers();
    const { h, refresh } = host();
    const watch = new ActionDecisions(h);
    const p = panel();

    watch.scheduleDispatchWatch("inv-1", p, inFlight);
    await vi.advanceTimersByTimeAsync(4000);
    expect(refresh).toHaveBeenCalledTimes(1);

    watch.scheduleDispatchWatch("inv-1", p, [{ actionId: "a-1", status: "SUCCEEDED" }] as ActionRow[]);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(refresh).toHaveBeenCalledTimes(1);

    watch.scheduleDispatchWatch("inv-1", p, inFlight);
    watch.forget("inv-1");
    await vi.advanceTimersByTimeAsync(10_000);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("gives up after its bound so a wedged workflow cannot be polled forever", async () => {
    vi.useFakeTimers();
    const { h, refresh } = host();
    const watch = new ActionDecisions(h);
    const p = panel();
    // Each re-arm counts a tick; the 46th finds the bound and stops.
    for (let i = 0; i < 46; i++) {
      watch.scheduleDispatchWatch("inv-1", p, inFlight);
    }
    await vi.advanceTimersByTimeAsync(10_000);
    expect(refresh).not.toHaveBeenCalled();
  });
});
