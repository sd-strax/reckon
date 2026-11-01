import { act, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider, useApp } from "../app";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("a failed evidence read (adr/0241)", () => {
  it("is asked for again after the next refresh, not on every render", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    let resolve!: (ref: string) => void;
    function Probe() {
      resolve = useApp().resolve;
      return null;
    }
    render(<AppProvider transport={transport}><Probe /></AppProvider>);
    const asks = () => transport.intents.filter((i) => i.type === "evidence.resolve" && i.ref === "ref-x").length;

    act(() => resolve("ref-x"));
    act(() => resolve("ref-x"));
    expect(asks()).toBe(1);

    act(() => transport.emit({ type: "evidence.unresolved", ref: "ref-x" }));
    act(() => resolve("ref-x"));
    expect(asks()).toBe(1);

    act(() => transport.emit({ type: "state.patch", seq: 2, patch: {} }));
    act(() => resolve("ref-x"));
    expect(asks()).toBe(2);
  });
});
