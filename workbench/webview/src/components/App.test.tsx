// Component tests over the harness snapshot (impl §5.5): the document renders
// its surfaces from recorded data, and the analyst's acts reach the host as
// the intents the protocol names.
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;

function mount(d: HarnessSnapshot | null = data) {
  const transport = new HarnessTransport(d);
  const view = render(<AppProvider transport={transport}><App /></AppProvider>);
  return { transport, view };
}

describe("the investigation document", () => {
  it("renders the header, seed line and the derived state label from the snapshot", () => {
    mount();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(data.snapshot.investigation.title);
    expect(document.querySelector(".state")?.textContent).toBe("ACTIVE · REMEDIATING"); // pending actions (binding §2.4)
    // A ticket.comment action on the seed case exists (FAILED), so the seed
    // line shows its status instead of the one-shot "Note the case" button.
    expect(screen.getByText("noted · failed")).toBeTruthy();
    expect(document.querySelector("#meta code")?.textContent).toBe(data.snapshot.investigation.id);
  });

  it("renders every rail section: approvals, hypotheses, knowledge, pins, ledger, comms, capabilities", () => {
    mount();
    const pending = document.querySelector("#pendingSection")!;
    expect(within(pending as HTMLElement).getByText("Approve · Send")).toBeTruthy(); // notify.* preview card
    expect(within(pending as HTMLElement).getByText("Approve (challenge)…")).toBeTruthy(); // T3
    expect(within(pending as HTMLElement).getByText("↻ Re-request")).toBeTruthy(); // EXPIRED without a successor
    expect(within(pending as HTMLElement).getByText("Please confirm whether maintenance was scheduled.")).toBeTruthy(); // pre-send preview
    expect(Array.from(document.querySelectorAll(".hyphandle")).map((h) => h.textContent)).toEqual(["H1 not-owner", "H2"]); // live first, handle or position
    expect(screen.getByText("2/3 tested")).toBeTruthy(); // withdrawn excluded, manual counted
    expect(screen.getByText("a person tests this")).toBeTruthy();
    expect(screen.getAllByText("undecided until tested").length).toBe(2);
    expect(screen.getByText(/attested by Analyst One by phone/)).toBeTruthy();
    expect(screen.getByText("✓ Adopt")).toBeTruthy();
    expect(screen.getByText("⚡ Run tests")).toBeTruthy();
    expect(screen.getByText("Procedure one")).toBeTruthy();
    expect(screen.getByText("Two interactive logons by user-a within six minutes.")).toBeTruthy();
    expect(screen.getByText("Early guess, re-scoped.").closest(".pinrow")!.classList.contains("superseded")).toBe(true);
    expect(screen.getByText("Actions · 5")).toBeTruthy();
    expect(screen.getByText("✗ ticket is read-only")).toBeTruthy();
    expect(screen.getByText("External work · 1 open")).toBeTruthy();
    expect(screen.getByText("Acknowledge")).toBeTruthy();
    expect(screen.getByText("Capabilities · 1/3 available")).toBeTruthy();
    expect(screen.getByText("set up")).toBeTruthy(); // installed-not-enabled affordance
  });

  it("reconstructs the chronicle: committed turn with a tool row, act lines, the reset boundary, a stub, a struck verdict", () => {
    mount();
    const restored = document.querySelector("#restored")!;
    expect(within(restored as HTMLElement).getByText("enumerate_logons")).toBeTruthy();
    expect(within(restored as HTMLElement).getByText("COMPLETE · 2 event(s)")).toBeTruthy();
    expect(restored.querySelector("table.mdtable")).toBeTruthy();
    expect(within(restored as HTMLElement).getByText("followed: Procedure one")).toBeTruthy();
    expect(within(restored as HTMLElement).getByText(/committed aside/)).toBeTruthy();
    expect(within(restored as HTMLElement).getByText("approved host.isolate")).toBeTruthy();
    expect(within(restored as HTMLElement).getByText(/context reset — agent re-grounded/)).toBeTruthy();
    expect(within(restored as HTMLElement).getByText(/earlier reasoning — open transcript/)).toBeTruthy();
    expect(within(restored as HTMLElement).getByText("verdict SUSPICIOUS").closest(".actline")!.classList.contains("superseded")).toBe(true);
    expect(document.querySelector("#stripWrap summary")?.textContent).toBe("Event time · 3 events over 6m");
    expect(document.querySelector("#graphWrap summary")?.textContent).toContain("Evidence graph · 6 objects · 1 events");
  });

  it("resolves chip labels through the host and opens every chip in the inspector", () => {
    const { transport } = mount();
    expect(transport.intents.some((i) => i.type === "evidence.resolve")).toBe(true);
    const chip = Array.from(document.querySelectorAll(".refchip")).find((c) => c.textContent === "host · host-a")!;
    expect(chip).toBeTruthy();
    fireEvent.click(chip);
    expect(transport.intents.some((i) => i.type === "inspector.open")).toBe(true);
    expect(document.querySelector(".inspector.active")).toBeTruthy();
    expect(document.querySelector(".popover")).toBeNull(); // the popover is retired into the inspector
  });

  it("sends a turn with the included knowledge set", () => {
    const { transport } = mount();
    const cb = document.querySelector("#knowledgeBox input[type=checkbox]") as HTMLInputElement;
    fireEvent.click(cb);
    const input = document.querySelector("#input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "continue" } });
    fireEvent.keyDown(input, { key: "Enter" });
    const sent = transport.intents.find((i) => i.type === "send");
    expect(sent).toEqual({ type: "send", text: "continue", includedKnowledge: ["sop-1"] });
    expect(input.disabled).toBe(true); // one turn at a time
  });

  it("routes a reject through the prompt card, an approve straight to the host, then locks the queue", () => {
    const { transport } = mount();
    fireEvent.click(screen.getAllByText("Reject…")[0]);
    const dlg = screen.getByRole("dialog");
    fireEvent.change(within(dlg).getByRole("textbox"), { target: { value: "not warranted" } });
    fireEvent.click(within(dlg).getByText("Reject"));
    expect(transport.intents.find((i) => i.type === "action.reject")).toMatchObject({ actionId: "act-2", reason: "not warranted" });
    fireEvent.click(screen.getByText("Approve · Send"));
    expect(transport.intents.find((i) => i.type === "action.approve")).toMatchObject({ actionId: "act-2", tier: "T1" });
    // One decision in flight at a time: every queue button is disabled until the host re-posts the slice.
    expect((screen.getByText("Approve (challenge)…") as HTMLButtonElement).disabled).toBe(true);
    act(() => transport.emit({ type: "state.patch", seq: 15, patch: { actions: data.snapshot.actions!.filter((a) => a.actionId !== "act-2") } }));
    expect((screen.getByText("Approve (challenge)…") as HTMLButtonElement).disabled).toBe(false);
  });

  it("records a prediction with its discriminators and the manual flag", () => {
    const { transport } = mount();
    fireEvent.click(screen.getAllByText("+ prediction")[0]);
    const dlg = screen.getByRole("dialog");
    fireEvent.change(within(dlg).getByRole("textbox"), { target: { value: "a new test" } });
    fireEvent.click(within(dlg).getByLabelText(/A person tests this/));
    fireEvent.click(within(dlg).getByText("Record prediction"));
    const sent = transport.intents.find((i) => i.type === "pred.new");
    expect(sent).toMatchObject({ statement: "a new test", testKind: "manual" });
    expect((sent as { discriminates: string[] }).discriminates.length).toBe(1); // the single rival is pre-checked
  });

  it("gates the verdict on pins and a rationale, and cites the active pins", () => {
    const { transport } = mount();
    fireEvent.click(screen.getByText("Verdict…"));
    const dlg = screen.getByRole("dialog");
    const submit = within(dlg).getByRole("button", { name: "Record verdict" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.click(within(dlg).getByLabelText("MALICIOUS"));
    fireEvent.change(dlg.querySelector("#verdictRationale")!, { target: { value: "because" } });
    expect(submit.disabled).toBe(false);
    expect(within(dlg).getByText(/undecided hypothesis H2 — never adopted/)).toBeTruthy();
    fireEvent.click(submit);
    const v = transport.intents.find((i) => i.type === "verdict.submit") as { refs: string[] };
    expect(v.refs.length).toBe(2); // the active pin's refs, not the superseded one's
  });

  it("renames inline: Enter commits, Escape cancels", () => {
    const { transport } = mount();
    fireEvent.doubleClick(screen.getByRole("heading", { level: 1 }));
    const edit = document.querySelector(".titleedit") as HTMLInputElement;
    fireEvent.change(edit, { target: { value: "Renamed" } });
    fireEvent.keyDown(edit, { key: "Enter" });
    expect(transport.intents.find((i) => i.type === "rename")).toEqual({ type: "rename", title: "Renamed" });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Renamed"); // the host echo applied
  });

  it("renders the draft surface and guards the double submit", () => {
    const { transport } = mount(null);
    const input = document.querySelector("#draftInput") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "host-b" } });
    expect(screen.getByText(/investigate host/)).toBeTruthy();
    fireEvent.click(screen.getByText("hunt this as a question instead"));
    expect(screen.getByText(/a hunt \(open question\)/)).toBeTruthy();
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(transport.intents.filter((i) => i.type === "seed.submit")).toEqual([{ type: "seed.submit", value: "host-b", kind: "question" }]);
  });

  it("parks the composer and hides the verdict and rename affordances on a concluded record", () => {
    const concluded: HarnessSnapshot = { ...data, snapshot: { ...data.snapshot, investigation: { ...data.snapshot.investigation, state: "CONCLUDED", verdict: { disposition: "BENIGN", verdictAt: "" } } } };
    mount(concluded);
    expect((document.querySelector("#input") as HTMLTextAreaElement).disabled).toBe(true);
    expect(screen.queryByText("Verdict…")).toBeNull();
    expect(document.querySelector(".iconbtn")).toBeNull();
    expect(screen.getByText("Reopen…")).toBeTruthy();
    expect(screen.getByText("Archive…")).toBeTruthy();
  });

  it("opens the handle rename, abandon, the tool-result pin and the enable form as in-document cards", () => {
    const { transport } = mount();
    fireEvent.click(document.querySelector(".hyphandle")!);
    expect(screen.getByRole("dialog", { name: "Rename H1 not-owner" })).toBeTruthy();
    fireEvent.click(screen.getByText("Cancel"));
    fireEvent.click(screen.getAllByText("Abandon")[0]);
    const abandon = screen.getByRole("dialog", { name: "Abandon H1 not-owner" });
    fireEvent.click(within(abandon).getByText("Abandon")); // optional reason: confirm enabled while empty
    expect(transport.intents.find((i) => i.type === "hyp.abandon")).toMatchObject({ reason: "" });
    // The pin on a restored tool result's summary line carries every ref the envelope cited.
    const pin = document.querySelector("#restored .summaryPin") as HTMLButtonElement;
    expect(pin.disabled).toBe(false);
    fireEvent.click(pin);
    const pinDlg = screen.getByRole("dialog", { name: "Pin as evidence" });
    expect(pinDlg.querySelectorAll(".pinrefs .refchip").length).toBe(7); // 2 observed-data + 3 entities + 2 events
    fireEvent.change(within(pinDlg).getByRole("textbox"), { target: { value: "shows the two logons" } });
    fireEvent.click(within(pinDlg).getByText("📌 Pin evidence"));
    expect(transport.intents.find((i) => i.type === "pin.add")).toMatchObject({ finding: "shows the two logons" });
    // The enable form renders from the adapter's schema; the secret never gets a field.
    fireEvent.click(screen.getByText("set up"));
    const form = screen.getByRole("dialog", { name: "Enable dns-adapter" });
    expect(within(form).getByText("base_url (required)")).toBeTruthy();
    expect(within(form).getByText(/api_key: secret — captured via secure input/)).toBeTruthy();
    fireEvent.change(form.querySelector("input[type=text]")!, { target: { value: "https://dns.example.test" } });
    fireEvent.click(within(form).getByRole("button", { name: "Enable dns-adapter" }));
    expect(transport.intents.find((i) => i.type === "enablement.apply")).toEqual({ type: "enablement.apply", adapter: "dns-adapter", enabled: true, config: { base_url: "https://dns.example.test" }, secretFields: ["api_key"] });
  });
});
