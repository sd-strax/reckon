import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;
const clone = (): HarnessSnapshot => JSON.parse(JSON.stringify(data));

function mount(d: HarnessSnapshot) {
  const transport = new HarnessTransport(d);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  fireEvent.click(screen.getByRole("tab", { name: /^Summary/ }));
  return transport;
}
const lens = () => document.querySelector(".lensscroll") as HTMLElement;

describe("the narrative on the Summary lens (adr/0103)", () => {
  it("offers Draft and Write when none exists, and shows the draft's progress and failure", () => {
    const d = clone();
    delete d.snapshot.investigation.narrative;
    const transport = mount(d);
    expect(within(lens()).getByText(/No narrative yet\. Draft one from the record, or write your own/)).toBeTruthy();
    expect(within(lens()).queryByText(/written when the investigation concludes/)).toBeNull(); // the gap line is gone
    fireEvent.click(within(lens()).getByRole("button", { name: "Draft narrative" }));
    expect(transport.intents).toContainEqual({ type: "narrative.draft" });
    act(() => transport.emit({ type: "narrative.status", state: "drafting" }));
    expect(within(lens()).getByText("Drafting from the record…")).toBeTruthy();
    expect((within(lens()).getByRole("button", { name: "Draft narrative" }) as HTMLButtonElement).disabled).toBe(true);
    act(() => transport.emit({ type: "narrative.status", state: "failed", message: "no key" }));
    expect(within(lens()).getByText(/Drafting failed: no key/)).toBeTruthy();
  });

  it("shows the record's narrative with who drafted and who edited, and saves an edit as the analyst's revision", () => {
    const transport = mount(data);
    expect(within(lens()).getByText(/Two interactive logons by user-a/)).toBeTruthy();
    expect(within(lens()).getByText(/drafted by reckon \(demo-model\) · edited by Analyst One · revision 2/)).toBeTruthy();
    fireEvent.click(within(lens()).getByRole("button", { name: "Edit" }));
    const box = within(lens()).getByLabelText("Narrative") as HTMLTextAreaElement;
    expect(box.value).toContain("Two interactive logons");
    fireEvent.change(box, { target: { value: "Rewritten by the analyst." } });
    fireEvent.click(within(lens()).getByText("Save as my revision"));
    expect(transport.intents).toContainEqual({ type: "narrative.edit", body: "Rewritten by the analyst." });
    // The text stays in the editor, read-only, until the record carries it.
    expect((within(lens()).getByLabelText("Narrative") as HTMLTextAreaElement).value).toBe("Rewritten by the analyst.");
    expect((within(lens()).getByText("Saving…") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(lens()).getByText("Saving…"));
    expect(transport.intents.filter((i) => i.type === "narrative.edit").length).toBe(1);
    act(() => transport.emit({ type: "narrative.status", state: "saving" }));
    act(() => transport.emit({ type: "narrative.status", state: "saved" }));
    expect(within(lens()).queryByLabelText("Narrative")).toBeNull();
    // Cancel records nothing; an unchanged save records nothing.
    fireEvent.click(within(lens()).getByRole("button", { name: "Edit" }));
    fireEvent.click(within(lens()).getByText("Cancel"));
    fireEvent.click(within(lens()).getByRole("button", { name: "Edit" }));
    fireEvent.click(within(lens()).getByText("Save as my revision"));
    expect(transport.intents.filter((i) => i.type === "narrative.edit").length).toBe(1);
    expect(within(lens()).getByRole("button", { name: "Draft again" })).toBeTruthy();
  });

  it("keeps a refused edit in the editor with the reason, and lets the analyst fix and resave it", () => {
    const transport = mount(data);
    fireEvent.click(within(lens()).getByRole("button", { name: "Edit" }));
    fireEvent.change(within(lens()).getByLabelText("Narrative"), { target: { value: "Too long." } });
    fireEvent.click(within(lens()).getByText("Save as my revision"));
    act(() => transport.emit({ type: "narrative.status", state: "saving" }));
    act(() => transport.emit({ type: "narrative.status", state: "save-failed", message: "Body is 20001 runes; at most 20000" }));
    const box = within(lens()).getByLabelText("Narrative") as HTMLTextAreaElement;
    expect(box.value).toBe("Too long.");
    expect(box.disabled).toBe(false);
    expect(within(lens()).getByText(/Not saved: Body is 20001 runes; at most 20000/)).toBeTruthy();
    fireEvent.change(box, { target: { value: "Short." } });
    expect(within(lens()).queryByText(/Not saved/)).toBeNull();
    fireEvent.click(within(lens()).getByText("Save as my revision"));
    expect(transport.intents.filter((i) => i.type === "narrative.edit").map((i) => (i as { body: string }).body)).toEqual(["Too long.", "Short."]);
    act(() => transport.emit({ type: "narrative.status", state: "saved" }));
    expect(within(lens()).queryByLabelText("Narrative")).toBeNull();
    expect(within(lens()).queryByText(/Not saved/)).toBeNull();
  });

  it("says a model-only narrative was drafted by reckon, and a human-first one by its author", () => {
    const d = clone();
    const n = d.snapshot.investigation.narrative!;
    n.revision = 1; n.author = n.draftedBy;
    mount(d);
    expect(within(lens()).getByText(/^drafted by reckon \(demo-model\) · revision 1/)).toBeTruthy();
    document.body.innerHTML = "";
    const h = clone();
    const m = h.snapshot.investigation.narrative!;
    m.revision = 1; m.draftedBy = { kind: "HUMAN", name: "Analyst One" }; m.author = m.draftedBy; m.sequenceNo = 14;
    mount(h);
    expect(within(lens()).getByText(/^drafted by Analyst One · revision 1/)).toBeTruthy();
    expect(within(lens()).queryByText(/moved/)).toBeNull(); // written at the record's head
  });

  it("names the current revision's model on a redraft, and offers no draft of an empty record", () => {
    const d = clone();
    const n = d.snapshot.investigation.narrative!;
    n.author = { kind: "AI_DELEGATED", name: "Analyst One", model: "other-model" };
    mount(d);
    expect(within(lens()).getByText(/drafted by reckon \(demo-model\) · redrafted by reckon \(other-model\) · revision 2/)).toBeTruthy();
    document.body.innerHTML = "";
    const e = clone();
    delete e.snapshot.investigation.narrative;
    e.snapshot.thread = [];
    e.snapshot.turns = [];
    e.snapshot.hypotheses = [];
    e.snapshot.actions = [];
    mount(e);
    expect((within(lens()).getByRole("button", { name: "Draft narrative" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(lens()).getByRole("button", { name: "Write" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("drops a failed draft's note once the investigation is re-read", () => {
    const transport = mount(data);
    act(() => transport.emit({ type: "narrative.status", state: "failed", message: "no key" }));
    expect(within(lens()).getByText(/Drafting failed: no key/)).toBeTruthy();
    act(() => transport.emit({ type: "state.patch", seq: 99, patch: { investigation: { ...data.snapshot.investigation } } }));
    expect(within(lens()).queryByText(/Drafting failed/)).toBeNull();
  });

  it("prefills the conclude summary with the current narrative", () => {
    const d = clone();
    // Conclude needs a verdict of record; the prefill is the narrative.
    d.snapshot.investigation.verdict = { disposition: "MALICIOUS", rationale: "confirmed", verdictAt: "2026-10-20T09:12:00Z" };
    mount(d);
    fireEvent.click(screen.getByText("Conclude…"));
    expect(screen.getByText("Conclude investigation")).toBeTruthy();
    expect((document.querySelector("#promptInput") as HTMLTextAreaElement).value).toContain("Two interactive logons by user-a");
  });
});
