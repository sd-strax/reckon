// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTransport, TurnOutcome, TurnProgress } from "./agentTransport";
import type { RenderMessage } from "./protocol";
import { createdPanels, resetVscodeFake } from "./test/vscode";
import { TurnDriver } from "./turnDriver";

const outcome = (over: Partial<TurnOutcome> = {}): TurnOutcome => ({
  text: "", toolRounds: 2, pendingActions: [],
  usage: { input: 10, output: 5, cacheRead: 3, cacheWrite: 1 }, ...over,
});

/** A transport whose turn plays `script` against the progress sink, then settles. */
function transport(script: (p: TurnProgress) => void, settle: () => Promise<TurnOutcome>): AgentTransport {
  return {
    turn: async (_id, _text, p) => { script(p); return settle(); },
    cancel: async () => undefined,
    summarizeConcluded: async () => undefined,
    proposeSOP: async () => { throw new Error("unused"); },
    dispose: () => undefined,
  };
}

function setup(t: AgentTransport) {
  const panel = vscode.window.createWebviewPanel("reckon.investigation", "T", vscode.ViewColumn.Active, {});
  const refresh = vi.fn(async () => undefined);
  const driver = new TurnDriver(t, (p, msg) => p.webview.postMessage(msg), refresh);
  const posted = () => createdPanels()[0].webview.posted as RenderMessage[];
  return { panel, refresh, driver, posted };
}

beforeEach(() => resetVscodeFake());

describe("TurnDriver", () => {
  it("streams a turn to the panel in order and ends it with the usage", async () => {
    const { panel, refresh, driver, posted } = setup(transport((p) => {
      p.onStep(1);
      p.onToolCall("query", { q: 1 });
      p.onToolResult("query", "{}", false, "COMPLETE", 4, ["r-1"]);
      p.onTextDelta("hel");
      p.onText("lo");
    }, async () => outcome({ interpretationId: "i-9" })));

    await driver.run("inv-1", panel, "look at host-a");

    expect(posted().map((m) => m.type)).toEqual([
      "turn.user", "turn.start", "turn.step", "turn.tool", "turn.toolResult", "turn.text", "turn.text", "turn.committed", "turn.end",
    ]);
    expect(posted()[4]).toEqual({ type: "turn.toolResult", name: "query", isError: false, coverage: "COMPLETE", events: 4, refs: ["r-1"] });
    // Billed input folds the cache reads and writes in; cacheRead rides separately.
    expect(posted().at(-1)).toEqual({ type: "turn.end", rounds: 2, usage: { input: 14, output: 5, cacheRead: 3 } });
    expect(refresh).toHaveBeenCalledWith("inv-1", panel);
  });

  it("opens a scoped turn as an aside, without echoing the instruction", async () => {
    const { panel, driver, posted } = setup(transport(() => undefined, async () => outcome()));
    await driver.run("inv-1", panel, "the instruction", { label: "⚡ Propose tests" });
    expect(posted()[0]).toEqual({ type: "aside.start", label: "⚡ Propose tests" });
    expect(posted().some((m) => m.type === "turn.user")).toBe(false);
  });

  it("surfaces a part-failed turn's error and still ends it", async () => {
    const { panel, driver, posted } = setup(transport(() => undefined, async () => outcome({ error: "model overloaded" })));
    await driver.run("inv-1", panel, "x");
    const types = posted().map((m) => m.type);
    expect(types.slice(-2)).toEqual(["turn.error", "turn.end"]);
  });

  it("ends the turn and refreshes when the transport throws", async () => {
    const { panel, refresh, driver, posted } = setup(transport(() => undefined, async () => { throw new Error("sidecar gone"); }));
    await driver.run("inv-1", panel, "x");
    expect(posted().slice(-2)).toEqual([{ type: "turn.error", message: "sidecar gone" }, { type: "turn.end" }]);
    expect(refresh).toHaveBeenCalledOnce();
  });
});
