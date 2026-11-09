// The inspector (08 §8.6, §8.6.1): every citation opens it, the frame is the
// same for every type, the back stack and the pin work, Esc closes it, and
// each renderer shows its record from the harness stage.
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;

function mount(d: HarnessSnapshot = data) {
  const transport = new HarnessTransport(d);
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
    fireEvent.click(within(p).getAllByText("H1 not-owner")[0]!); // the owner link in the sub-line
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
    fireEvent.click(within(lens()).getAllByTitle("Open the thread")[0]!);
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
    expect(within(p).getAllByText(/Confirm the session with the owner first/).length).toBe(2); // the section read back as it was (adr/0097), and what recall showed (#132)
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

  it("shows what the agent read of a procedure as it was, and whether it changed since (adr/0097)", () => {
    const lib = () => document.querySelector("#knowledgeSection") as HTMLElement;
    const clone = (): HarnessSnapshot => JSON.parse(JSON.stringify(data));

    // Revised since: the header names both revisions; the three views.
    mount();
    fireEvent.click(within(lib()).getByText("Procedure one"));
    let p = pane();
    expect(within(p).getByText(/revised since this case consulted it: revision 1 then, 2 now/)).toBeTruthy();
    expect(within(p).getByText("Procedure one", { selector: ".isecthead .kicker" })).toBeTruthy(); // the heading path
    expect(within(p).getByText("received whole, in the recall result")).toBeTruthy(); // from the committed tool result, not assumed
    expect(within(p).getByText(/with the owner first\.$/, { selector: ".isecttext" })).toBeTruthy(); // the revision's text, by default
    fireEvent.click(within(p).getByRole("button", { name: "Current" }));
    expect(within(p).getByText(/account owner first, by phone/, { selector: ".isecttext" })).toBeTruthy();
    fireEvent.click(within(p).getByRole("button", { name: "Changes" }));
    expect(within(p).getByText("Confirm the session with the owner first.", { selector: ".dl.del" })).toBeTruthy();
    expect(within(p).getByText("Confirm the session with the account owner first, by phone.", { selector: ".dl.add" })).toBeTruthy();
    expect(within(p).getByText("# Procedure one", { selector: ".dl.same" })).toBeTruthy();
    expect(within(p).getByText("Recall showed")).toBeTruthy(); // recall and read are two different things
    expect(within(p).queryByText(/applied|followed/)).toBeNull(); // never those words (adr/0236)
    cleanup();

    // Exactly as retrieved: the act saw the current revision; no views to switch.
    const same = clone();
    same.procedureReads = {}; same.procedureReads["sop-1"] = { currentRevision: 1, turns: [{ interpretationId: "interp-turn-1", revision: 1, cut: "1/16000", sections: [{ idx: 1, location: "Procedure one", text: "# Procedure one\n\nConfirm the session with the owner first." }] }] };
    mount(same);
    fireEvent.click(within(lib()).getByText("Procedure one"));
    p = pane();
    expect(within(p).getByText(/revision 1, exactly as retrieved/)).toBeTruthy();
    expect(within(p).queryByRole("button", { name: "Changes" })).toBeNull();
    cleanup();

    // The recall excerpt was cut to its budget: not "exactly as retrieved"; the Received view shows what arrived.
    const cut = clone();
    cut.procedureReads = same.procedureReads;
    for (const t of cut.snapshot.turns) t.body = t.body.replace('"excerpt": "# Procedure one', '"truncated": true, "excerpt": "# Procedure one');
    mount(cut);
    fireEvent.click(within(lib()).getByText("Procedure one"));
    p = pane();
    expect(within(p).getByText(/revision 1, as it stood when the agent retrieved it/)).toBeTruthy();
    expect(within(p).getByText(/the recall result carried the first \d+ of \d+ runes/)).toBeTruthy();
    expect(within(p).getByRole("button", { name: "Received", pressed: true })).toBeTruthy(); // what arrived is shown first
    fireEvent.click(within(p).getByRole("button", { name: "Revision 1" }));
    expect(within(p).getByText(/with the owner first\.$/, { selector: ".isecttext" })).toBeTruthy();
    cleanup();

    // The heading is gone from the current revision: Current and Changes say so.
    const gone = clone();
    const goneSection = gone.procedureReads?.["sop-1"]?.turns[0]?.sections[0];
    if (goneSection) goneSection.current = null;
    mount(gone);
    fireEvent.click(within(lib()).getByText("Procedure one"));
    p = pane();
    fireEvent.click(within(p).getByRole("button", { name: "Current" }));
    expect(within(p).getByText(/No section of revision 2 sits under this heading any more/)).toBeTruthy();
    cleanup();

    // The read-back failed (or the harness has none): the pane says so and still shows what recall showed.
    const failed = clone();
    delete failed.procedureReads;
    mount(failed);
    fireEvent.click(within(lib()).getByText("Procedure one"));
    p = pane();
    expect(within(p).getByText(/could not be read back from the library/)).toBeTruthy();
    expect(within(p).getByText("Recall showed")).toBeTruthy();
    cleanup();

    // Nothing recorded (an act before sections existed): only what recall showed, no failure.
    const bare = clone();
    delete bare.procedureReads;
    for (const e of bare.snapshot.thread || []) for (const s of e.consultedSops) { delete s.revision; delete s.cut; delete s.sections; }
    mount(bare);
    fireEvent.click(within(lib()).getByText("Procedure one"));
    p = pane();
    expect(within(p).queryByText(/could not be read back/)).toBeNull();
    expect(within(p).getByText("Recall showed")).toBeTruthy();
  });

  it("shows where a procedure and a past case were consulted elsewhere, in the two words, and the procedures learned from a case (adr/0109)", () => {
    const transport = mount();
    const lib = () => document.querySelector("#knowledgeSection") as HTMLElement;
    // The library rows carry the count.
    expect(within(lib()).getByText(/cited in 1 investigation$/)).toBeTruthy();
    expect(within(lib()).getByText(/cited in 0 investigations/)).toBeTruthy();

    fireEvent.click(within(lib()).getByText("Procedure one"));
    let p = pane();
    fireEvent.click(within(p).getByRole("tab", { name: "Cited elsewhere · 1 of 2" }));
    expect(within(p).getByText(/^cited:/)).toBeTruthy();
    expect(within(p).getByText(/^recalled, not cited:/)).toBeTruthy();
    expect(within(p).getAllByText(/revision 1/).length).toBe(2); // the revision each investigation had in front of it
    expect(within(p).queryByText(/applied|followed/)).toBeNull(); // never those words (adr/0236)
    fireEvent.click(within(p).getByText("Service account sweep"));
    expect(transport.intents.find((i) => i.type === "investigation.open")).toMatchObject({ id: "11111111-2222-4333-8444-555555555555", title: "Service account sweep" });

    fireEvent.click(within(lib()).getByText("Prior case A"));
    p = pane();
    expect(within(p).getByText("Procedures learned from this case")).toBeTruthy();
    fireEvent.click(within(p).getByText("Procedure one", { selector: ".irow a" }));
    expect(within(pane()).getByText("Procedure")).toBeTruthy(); // the learned procedure opens
    fireEvent.click(within(pane()).getByText("‹ back"));
    p = pane();
    fireEvent.click(within(p).getByRole("tab", { name: "Cited elsewhere · 0 of 1" }));
    expect(within(p).getByText(/^recalled, not cited:/)).toBeTruthy();
  });

  it("opens a procedure without the tab's rows when the citations read failed, and says loading while the answer is in flight", () => {
    const data = JSON.parse(JSON.stringify(synthetic)) as HarnessSnapshot;
    delete data.citations;
    const transport = new HarnessTransport(data);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(within(document.querySelector("#knowledgeSection") as HTMLElement).getByText("Procedure one"));
    const p = pane();
    fireEvent.click(within(p).getByRole("tab", { name: "Cited elsewhere" }));
    expect(within(p).getByText(/could not be read/)).toBeTruthy();
    expect(within(p).queryByText(/Not consulted in any other/)).toBeNull(); // a failed read is not "none"

    // A procedure the harness has not answered yet: the tab is loading, not failed.
    act(() => transport.emit({ type: "library", sops: [{ kind: "sop", ref: "sop-pending", title: "Pending procedure" }], cases: [] }));
    fireEvent.click(within(document.querySelector("#knowledgeSection") as HTMLElement).getByText("Pending procedure"));
    const q = pane();
    fireEvent.click(within(q).getByRole("tab", { name: "Cited elsewhere" }));
    expect(within(q).queryByText(/could not be read/)).toBeNull();
    expect(within(q).getByText(/where it was consulted/)).toBeTruthy();
  });

  it("shows an honest error for a ref the record cannot open", () => {
    const transport = mount();
    fireEvent.click(screen.getByRole("tab", { name: /^Summary/ })); // key entities carry the account chip
    fireEvent.click(chip("account · user-a"));
    act(() => transport.emit({ type: "inspector.error", target: "user-account--5a9b3c2d-8c4d-4f60-ab2c-3d4e5f6a7b02", message: "no persisted record for this ref" }));
    expect(within(pane()).getByText(/no persisted record for this ref/)).toBeTruthy();
  });
});
