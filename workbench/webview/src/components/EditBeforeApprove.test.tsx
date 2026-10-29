import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("edit before approve (#127)", () => {
  it("edits the pending message in place and sends the whole new parameters", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    const a2 = d.snapshot.actions!.find((a) => a.actionId === "act-2")!;
    a2.inputs = [{ name: "subject", required: true }, { name: "body", required: true }, { name: "follow_up_hours", required: false }, { name: "thread_ref", required: false }];
    a2.parameters = { subject: "Update", body: "draft text" };
    const transport = new HarnessTransport(d);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: /^Response/ }));
    fireEvent.click(within(document.getElementById("obj-act-2")!).getByText("Edit…"));
    const dlg = screen.getByRole("dialog");
    expect((within(dlg).getAllByRole("textbox")[0] as HTMLInputElement).value).toBe("Update"); // the subject, prefilled
    const main = dlg.querySelector("textarea") as HTMLTextAreaElement;
    expect(main.value).toBe("draft text");
    fireEvent.change(main, { target: { value: "final text" } });
    fireEvent.change(dlg.querySelector("#promptExtra_follow_up_hours")!, { target: { value: "48" } });
    fireEvent.click(within(dlg).getByText("Save changes"));
    expect(transport.intents.find((i) => i.type === "action.edit")).toEqual({
      type: "action.edit", actionId: "act-2", parameters: { subject: "Update", body: "final text", follow_up_hours: 48 }, reason: undefined,
    });
  });
});
