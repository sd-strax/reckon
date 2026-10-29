import { act, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import { turnRenderer } from "../derive/inspect";
import type { ThreadEntry } from "../../../src/viewTypes";

const human = { principal: "u", principalName: "Ana", kind: "HUMAN" };
const entry = (over: Partial<ThreadEntry>) => ({ sequenceNo: 9100, occurredAt: "2026-10-28T19:07:54Z", actor: human, interpretationId: "x", interpretationType: "verdict", summary: "s", superseded: false, inputRefs: [], outputRefs: [], toolCalls: 0, hasTranscript: false, consultedSops: [], consultedSimilar: [], ...over }) as ThreadEntry;

let open: ReturnType<typeof useApp>["openInspector"] = () => undefined;
function Grab() { open = useApp().openInspector; return null; }

describe("acts that are not agent turns (#144)", () => {
  it("chooses the pane from the entry: a turn only when there is a transcript", () => {
    expect(turnRenderer(entry({ hasTranscript: true }))).toBe("turn");
    expect(turnRenderer(entry({ interpretationType: "evidence-pin" }))).toBe("pin");
    expect(turnRenderer(entry({ interpretationType: "action-rejection" }))).toBe("act");
  });
  it("a pin and a verdict open their own panes and never ask for a transcript", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    d.snapshot.thread = [...(d.snapshot.thread || []),
      entry({ interpretationId: "pin-1", interpretationType: "evidence-pin", summary: "the logon came from 10.0.4.20", inputRefs: ["observed-data--a"] }),
      entry({ interpretationId: "verdict-1", summary: "confirmed hands-on-keyboard" }),
    ];
    d.snapshot.pins = [...d.snapshot.pins, { interpretationId: "pin-1", finding: "the logon came from 10.0.4.20", inputRefs: ["observed-data--a"], actor: "HUMAN", pinnedAt: "2026-10-28T19:03:10Z", superseded: false }];
    const transport = new HarnessTransport(d);
    render(<AppProvider transport={transport}><App /><Grab /></AppProvider>);
    act(() => open({ kind: "turn", interpretationId: "pin-1" }));
    expect(screen.getByText("Pinned finding")).toBeTruthy();
    expect(screen.getAllByText("the logon came from 10.0.4.20").length).toBeGreaterThan(0);
    act(() => open({ kind: "turn", interpretationId: "verdict-1" }));
    expect(screen.getByText("Act on the record")).toBeTruthy();
    expect(screen.queryByText(/COULD NOT OPEN/i)).toBeNull();
    expect(transport.intents.filter((i) => i.type === "inspector.open" && i.target.kind === "turn")).toEqual([]);
  });
});
