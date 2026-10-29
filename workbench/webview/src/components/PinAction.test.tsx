import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("requesting an action from a pin (#146)", () => {
  it("picks a type, a target and a reason, then the type's fields, citing the pin's evidence", async () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    const transport = new HarnessTransport(d);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    act(() => transport.emit({ type: "actionTypes", types: [
      { actionType: "ticket.comment", name: "Comment on a ticket", category: "case_record", inputs: [{ name: "body", required: true }] },
      { actionType: "account.disable", name: "Disable account", category: "containment", inputs: [] },
    ] }));
    fireEvent.click(screen.getByRole("tab", { name: /^Reasoning/ }));
    const pin = d.snapshot.pins.find((p) => !p.superseded)!;
    fireEvent.click(screen.getAllByText("Request action on this…")[0]);
    let dlg = screen.getByRole("dialog");
    const typeSel = dlg.querySelector("#promptExtra_type") as HTMLSelectElement;
    expect(typeSel.options[0].value).toBe("account.disable"); // containment first, case-record writes last
    fireEvent.change(dlg.querySelector("#promptExtra_other")!, { target: { value: "CONTOSO\\\\jdoe" } });
    fireEvent.change(document.querySelector("#promptInput")!, { target: { value: "contain the account" } });
    fireEvent.click(within(dlg).getByText("Next"));
    expect(transport.intents.find((i) => i.type === "action.request")).toEqual({
      type: "action.request", actionType: "account.disable", target: "CONTOSO\\\\jdoe", parameters: {}, rationale: "contain the account", evidenceRefs: pin.inputRefs,
    });

    fireEvent.click(screen.getAllByText("Request action on this…")[0]);
    dlg = screen.getByRole("dialog");
    fireEvent.change(dlg.querySelector("#promptExtra_type")!, { target: { value: "ticket.comment" } });
    fireEvent.change(dlg.querySelector("#promptExtra_other")!, { target: { value: "INC0010042" } });
    fireEvent.click(within(dlg).getByText("Next"));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    dlg = screen.getByRole("dialog");
    fireEvent.change(dlg.querySelector("#promptExtra_body")!, { target: { value: "Containment done." } });
    fireEvent.click(within(dlg).getByText("Request"));
    expect(transport.intents.filter((i) => i.type === "action.request").pop()).toMatchObject({ actionType: "ticket.comment", target: "INC0010042", parameters: { body: "Containment done." } });
  });
});
