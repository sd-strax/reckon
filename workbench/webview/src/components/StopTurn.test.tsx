import { act, fireEvent, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("stopping a running turn (adr/0239)", () => {
  it("turns Send into Stop while a turn runs, and back when it ends", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    const button = () => document.querySelector("#send") as HTMLButtonElement;
    expect(button().textContent).toBe("Send");

    act(() => transport.emit({ type: "turn.start" }));
    expect(button().textContent).toBe("Stop");

    fireEvent.click(button());
    expect(transport.intents).toContainEqual({ type: "turn.stop" });
    expect(button().textContent).toBe("Stopping…");

    act(() => transport.emit({ type: "turn.end" }));
    expect(button().textContent).toBe("Send");
  });
});
