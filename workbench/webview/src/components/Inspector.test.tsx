// The inspector (08 §8.6, §8.6.1): every citation opens it, the frame is the
// same for every type, the back stack and the pin work, Esc closes it, and
// each renderer shows its record from the harness stage.
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;

function mount() {
  const transport = new HarnessTransport(data);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}
const chip = (text: string) => Array.from(document.querySelectorAll(".refchip")).find((c) => c.textContent === text) as HTMLElement;
const pane = () => document.querySelector(".inspector.active") as HTMLElement;
const lens = () => document.querySelector(".lensscroll") as HTMLElement;
/** Chips on the Reasoning lens live in the pinned-evidence fold and in expanded rows; expand every row first. */
const expandRows = () => { for (const l of Array.from(document.querySelectorAll(".prow .pline"))) fireEvent.click(l); };

describe("the inspector", () => {
  it("opens an entity from a chip with the frame, In this case, pivot and raw properties", () => {
    const transport = mount();
    fireEvent.click(chip("host · host-a"));
    expect(transport.intents.find((i) => i.type === "inspector.open")).toEqual({ type: "inspector.open", target: { kind: "ref", ref: data.snapshot.investigation.seed!.entityRef } });
    const p = pane();
    expect(within(p).getByText("Entity")).toBeTruthy();
    expect(within(p).getByRole("heading", { level: 2 }).textContent).toBe("host-a");
    expect(within(p).getByText(/In this case/)).toBeTruthy();
    expect(within(p).getByText("the seed")).toBeTruthy();
    expect(within(p).getByText(/Appears in 1 other investigation|Prior case A/)).toBeTruthy();
    expect(within(p).getByText("hostname")).toBeTruthy(); // properties from the recorded doc
    fireEvent.click(within(p).getByText("Pivot on entity"));
    expect((document.querySelector("#input") as HTMLTextAreaElement).value).toContain("Pivot on host-a");
    expect(transport.intents.some((i) => i.type === "send")).toBe(false);
  });

  it("keeps a back stack, pins a pane and closes on Escape", () => {
    mount();
    fireEvent.click(chip("host · host-a"));
    fireEvent.click(within(pane()).getByText("host.isolate")); // the action on this entity
    expect(within(pane()).getByText("Action")).toBeTruthy();
    expect(within(pane()).getByText("‹ back")).toBeTruthy();
    fireEvent.click(within(pane()).getByText("‹ back"));
    expect(within(pane()).getByText("Entity")).toBeTruthy();
    fireEvent.click(within(pane()).getByTitle(/Pin this pane/));
    expect(document.querySelector(".inspector.pinned")).toBeTruthy();
    expect(document.querySelector(".inspector.active")).toBeNull();
    fireEvent.click(chip("observed data · 3 objects"));
    expect(document.querySelectorAll(".inspector").length).toBe(2);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(document.querySelector(".inspector.active")).toBeNull();
    expect(document.querySelector(".inspector.pinned")).toBeTruthy();
  });

  it("renders evidence with Details · Provenance · Raw, and the objects an observed-data contains", () => {
    mount();
    fireEvent.click(chip("observed data · 3 objects"));
    const p = pane();
    expect(within(p).getByText("Evidence")).toBeTruthy();
    expect(within(p).getByRole("tab", { name: "Provenance" })).toBeTruthy();
    expect(within(p).getByText("Objects it contains")).toBeTruthy();
    fireEvent.click(within(p).getByRole("tab", { name: "Raw" }));
    expect(p.querySelector(".ijson pre")!.textContent).toContain('"number_observed": 1');
    fireEvent.click(within(p).getByRole("tab", { name: "Provenance" }));
    expect(within(p).getByText("authentication v1")).toBeTruthy();
  });

  it("renders the absence with its search facts and stages a wider re-run", () => {
    mount();
    fireEvent.click(chip("nothing observed · get_network_connections over 7 days")); // cited by a pin in the fold
    const p = pane();
    expect(within(p).getByText("Evidence · absence")).toBeTruthy();
    expect(within(p).getByText("COMPLETE · 0 EVENTS")).toBeTruthy();
    expect(within(p).getByText("get_network_connections")).toBeTruthy();
    fireEvent.click(within(p).getByText("Re-run with a wider window"));
    expect((document.querySelector("#input") as HTMLTextAreaElement).value).toContain("Re-run get_network_connections for host-a with a wider window");
  });

  it("renders a detection finding as inferred by the vendor with its sighting and evidence", () => {
    mount();
    fireEvent.click(chip("indicator"));
    const p = pane();
    expect(within(p).getByText("Detection finding")).toBeTruthy();
    expect(within(p).getByText("inferred by the vendor")).toBeTruthy();
    expect(within(p).getByText("vendor-x")).toBeTruthy();
    expect(within(p).getByText("T1021.001")).toBeTruthy();
    fireEvent.click(within(p).getByRole("tab", { name: "Evidence" }));
    expect(within(p).getByText(/normalized like any observed event/)).toBeTruthy();
    expect(within(p).getByText(/No prediction in this investigation has cited/)).toBeTruthy();
  });

  it("renders an attestation as a human statement with who said it and the question it answers", () => {
    mount();
    expandRows(); // the attestation chip is P2's test result
    fireEvent.click(chip("attestation · attested by analyst-1 by phone: account owner"));
    const p = pane();
    expect(within(p).getByText("Attestation")).toBeTruthy();
    expect(within(p).getByText(/Owner confirmed by phone/, { selector: "blockquote" })).toBeTruthy();
    expect(within(p).getByText("account owner (as recorded)")).toBeTruthy();
    expect(within(p).getByText("phone")).toBeTruthy();
    expect(within(p).getAllByText("P2").length).toBeGreaterThan(0); // In this case + the question it answers
  });

  it("renders a prediction and its hypothesis from the snapshot alone, linked both ways", () => {
    mount();
    fireEvent.click(Array.from(lens().querySelectorAll(".prow .pord")).find((el) => el.textContent === "P3")!);
    let p = pane();
    expect(p.getAttribute("aria-label")).toBe("Prediction");
    expect(within(p).getByText("Prediction")).toBeTruthy();
    expect(within(p).getByText("UNTESTED")).toBeTruthy();
    expect(within(p).getByText(/H2 \(expects the opposite\)/)).toBeTruthy();
    expect(within(p).getByText(/tool · list_scheduled_tasks/)).toBeTruthy();
    fireEvent.click(within(p).getAllByText("H1 not-owner")[0]); // the owner link in the sub-line
    p = pane();
    expect(within(p).getByText("Hypothesis")).toBeTruthy();
    expect(within(p).getByText("Predictions · 2 of 3 tested")).toBeTruthy();
    expect(within(p).getByText("Competing set")).toBeTruthy();
    fireEvent.click(within(p).getByRole("tab", { name: "Wording history" }));
    expect(within(p).getAllByText(/enumerated logons/).length).toBeGreaterThan(0); // the producing turn, in the history and the raw fold
  });

  it("renders an action's record: history with actors, authorization, targets with results", () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: /^Response/ }));
    fireEvent.click(within(lens()).getAllByTitle(/^Open the action record/).find((a) => a.textContent === "host.isolate")!);
    const p = pane();
    expect(within(p).getByText("Action")).toBeTruthy();
    expect(within(p).getByText(/approved by Analyst One/)).toBeTruthy();
    expect(within(p).getByText(/via fixture-edr/)).toBeTruthy();
    fireEvent.click(within(p).getByRole("tab", { name: "Authorization" }));
    expect(within(p).getByText(/manual · no tenant policy auto-approves this/)).toBeTruthy();
    expect(within(p).getByText(/shadow: would have fired \(not in force\)/)).toBeTruthy();
    expect(within(p).getByText("Request reversal")).toBeTruthy(); // SUCCEEDED + REVERSIBLE
    fireEvent.click(within(p).getByRole("tab", { name: "Targets" }));
    expect(within(p).getByText("ISO-1")).toBeTruthy();
  });

  it("renders a notification thread and a turn, a procedure and a past case from the library", () => {
    const transport = mount();
    fireEvent.click(screen.getByRole("tab", { name: /^Response/ }));
    fireEvent.click(within(lens()).getAllByTitle("Open the thread")[0]);
    let p = pane();
    expect(within(p).getByText("Notification thread")).toBeTruthy();
    expect(within(p).getByText(/No maintenance was scheduled/)).toBeTruthy();
    fireEvent.click(within(p).getByRole("tab", { name: "Sent message" }));
    expect(within(p).getByText("Session on host-a", { selector: ".subj" })).toBeTruthy();

    fireEvent.click(within(document.querySelector("#companion") as HTMLElement).getByRole("tab", { name: "Thread" }));
    fireEvent.click(screen.getByText(/earlier reasoning — open transcript/)); // the stub
    p = pane();
    expect(within(p).getByText("Turn")).toBeTruthy();
    expect(transport.intents.some((i) => i.type === "inspector.open" && i.target.kind === "turn")).toBe(true);
    expect(within(p).getByText(/transcript not recorded in this harness stage/)).toBeTruthy(); // the honest error for a stub

    fireEvent.click(within(document.querySelector("#knowledgeSection") as HTMLElement).getByText("Procedure one"));
    p = pane();
    expect(within(p).getByText("Procedure")).toBeTruthy();
    expect(within(p).getByText(/Confirm the session with the owner first/)).toBeTruthy(); // what the agent's recall carried (#132)
    fireEvent.click(within(p).getByRole("tab", { name: "Whole document" }));
    expect(within(p).getByText("confirm-with-owner")).toBeTruthy();

    fireEvent.click(within(document.querySelector("#knowledgeSection") as HTMLElement).getByText("Prior case A"));
    p = pane();
    expect(within(p).getByText("Similar past case")).toBeTruthy();
    expect(within(p).getByText(/contained by isolating the host/, { selector: "p" })).toBeTruthy();
    expect(within(p).queryByText("Include in context")).toBeNull(); // the library is not context (adr/0129)
    fireEvent.click(within(p).getByRole("tab", { name: "What the agent read" }));
    expect(within(p).getByText("The agent has not read this case in this investigation.")).toBeTruthy();
  });

  it("shows an honest error for a ref the record cannot open", () => {
    const transport = mount();
    fireEvent.click(screen.getByRole("tab", { name: /^Summary/ })); // key entities carry the account chip
    fireEvent.click(chip("account · user-a"));
    act(() => transport.emit({ type: "inspector.error", target: "user-account--5a9b3c2d-8c4d-4f60-ab2c-3d4e5f6a7b02", message: "no persisted record for this ref" }));
    expect(within(pane()).getByText(/no persisted record for this ref/)).toBeTruthy();
  });
});
