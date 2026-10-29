import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("a revised verdict (#155)", () => {
  it("stands out at both ends of the Thread and on the Summary's verdict tile", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    const actor = { principal: "u", kind: "HUMAN" };
    d.snapshot.thread = [
      ...(d.snapshot.thread || []),
      { sequenceNo: 9001, occurredAt: "2026-10-28T19:07:54Z", actor, interpretationId: "v-old", interpretationType: "verdict", summary: "first look", disposition: "SUSPICIOUS", superseded: true, inputRefs: [], outputRefs: [], toolCalls: 0, hasTranscript: false, consultedSops: [], consultedSimilar: [] },
      { sequenceNo: 9002, occurredAt: "2026-10-28T23:10:20Z", actor, interpretationId: "v-new", interpretationType: "verdict", summary: "confirmed", disposition: "MALICIOUS", superseded: false, inputRefs: [], outputRefs: [], toolCalls: 0, hasTranscript: false, consultedSops: [], consultedSimilar: [] },
    ] as never;
    render(<AppProvider transport={new HarnessTransport(d)}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: /^Summary/ }));
    expect(screen.getByText(/replaces a SUSPICIOUS verdict from/)).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Thread" }));
    const restored = document.querySelector("#restored") as HTMLElement;
    const old = restored.querySelector("#act-v-old") as HTMLElement;
    expect(old.className).toContain("replaced");
    expect(within(old).getByText("Verdict SUSPICIOUS — first look")).toBeTruthy();
    expect(within(old).getByText(/^replaced at/)).toBeTruthy();
    expect(within(restored.querySelector("#act-v-new") as HTMLElement).getByText(/replaces the .* verdict \(SUSPICIOUS\)/)).toBeTruthy();
  });
});
