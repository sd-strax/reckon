import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

describe("backing an attestation with where it happened (#139)", () => {
  it("Record result carries the reference and the fingerprint choice", () => {
    const d = structuredClone(synthetic as unknown as HarnessSnapshot);
    const h = d.snapshot.hypotheses[0];
    h.status = "OPEN";
    h.predictions[0] = { ...h.predictions[0], status: "UNTESTED", testKind: "manual", testResultRefs: [], attestation: undefined };
    const transport = new HarnessTransport(d);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: /^Reasoning/ }));
    fireEvent.click(document.querySelector(".prow .pline")!);
    fireEvent.click(screen.getAllByText("Record result")[0]);
    const dlg = screen.getByRole("dialog");
    fireEvent.change(document.querySelector("#promptInput")!, { target: { value: "Owner confirmed by phone." } });
    fireEvent.change(dlg.querySelector("#promptExtra_reference")!, { target: { value: "INC0010042" } });
    fireEvent.click(dlg.querySelector("#promptExtra_hashReference")!);
    fireEvent.click(within(dlg).getByText("Record result"));
    expect(transport.intents.find((i) => i.type === "pred.result")).toMatchObject({ reference: "INC0010042", hashReference: true });
  });
});
