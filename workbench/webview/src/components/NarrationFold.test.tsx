import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("narration folds into the activity (#136)", () => {
  it("shows the last thing the agent said as the answer and the rest under Activity", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    const actor = { principal: "u", kind: "AI_DELEGATED", model: "m" };
    d.snapshot.thread = [{ sequenceNo: 9300, occurredAt: "2026-10-28T23:57:13Z", actor, interpretationId: "t-n", interpretationType: "other", summary: "s", superseded: false, inputRefs: [], outputRefs: [], toolCalls: 1, hasTranscript: true, consultedSops: [], consultedSimilar: [] }] as never;
    d.snapshot.turns = [{ sequenceNo: 9300, occurredAt: "2026-10-28T23:57:13Z", body: [
      "[user] What happened on this host?",
      "[assistant] I'll pull telemetry across the main vectors for this host.",
      '[tool_use enumerate_logons id=t1] {"entity":{"host":{"hostname":"WIN-FILE01"}}}',
      '[tool_result enumerate_logons id=t1 error=false] {"verb":"enumerate_logons","coverage":"COMPLETE","observed_data_refs":["observed-data--x"]}',
      "[assistant] Two RDP logons hit WIN-FILE01 at 14:32.",
    ].join("\n") }];
    render(<AppProvider transport={new HarnessTransport(d)}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: "Conversation" }));
    const answer = screen.getByText("Two RDP logons hit WIN-FILE01 at 14:32.");
    expect(answer.closest("details")).toBeNull();
    const narration = screen.getByText("I'll pull telemetry across the main vectors for this host.");
    expect(narration.closest("details.activity")).toBeTruthy();
  });
});
