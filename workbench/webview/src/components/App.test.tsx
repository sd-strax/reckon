// Component tests over the harness snapshot (impl §5.5): the document renders
// its regions from recorded data (08 §8.1–§8.5), and the analyst's acts reach
// the host as the intents the protocol names.
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
const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name: new RegExp("^" + name) }));
const lens = () => document.querySelector(".lensscroll") as HTMLElement;
const header = () => document.querySelector("#header") as HTMLElement;

describe("the investigation document", () => {
  it("renders the header: title, case reference with Note the case, the stage, the primary action and the nearest expiry", () => {
    mount();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(data.snapshot.investigation.title);
    expect(within(header()).getByText("CASE-1001")).toBeTruthy();
    expect(within(header()).getByText(/opened by Analyst One/)).toBeTruthy();
    // A ticket.comment action on the seed case exists (FAILED), so the status word shows instead of the button.
    expect(within(header()).getByText(/noted · failed/)).toBeTruthy();
    const steps = Array.from(document.querySelectorAll(".stage .step")).map((s) => s.className);
    expect(steps[0]).toContain("current"); // no verdict yet → Investigate
    expect(within(header()).getByText("Review 2 proposed actions")).toBeTruthy();
    expect(within(header()).getByText(/first approval/)).toBeTruthy();
  });

  it("opens on the stage's default lens and remembers the analyst's choice", () => {
    const { transport } = mount();
    expect(screen.getByRole("tab", { name: /^Reasoning/ }).getAttribute("aria-selected")).toBe("true");
    tab("Story");
    expect(transport.intents.find((i) => i.type === "view.state")).toEqual({ type: "view.state", lens: "story" });
    expect(transport.getState<{ lens: Record<string, string> }>()?.lens[data.snapshot.investigation.id]).toBe("story");
  });

  it("renders the work queue needs-me-first with stage steps, and starts an investigation from a case", () => {
    const { transport } = mount();
    const nav = document.querySelector("#leftRegion") as HTMLElement;
    const rows = Array.from(nav.querySelectorAll(".qrow:not(.case)"));
    expect(rows.length).toBe(2); // the concluded one is not in the open queue
    expect(rows[0].classList.contains("current")).toBe(true);
    expect(rows[0].querySelector(".qstep")?.textContent).toBe("Investigate");
    expect(rows[1].querySelector(".qstep")?.textContent).toBe("Close");
    expect(within(nav).getByText("from CASE-1002")).toBeTruthy();
    fireEvent.click(rows[1]);
    expect(transport.intents.find((i) => i.type === "investigation.open")).toMatchObject({ id: "grouping--2c3b8d6f-6b0f-4c8f-a02b-7e4f3b2c1d02" });
    expect(within(nav).getByText("CASE-1003")).toBeTruthy();
    fireEvent.click(within(nav).getByText("Start investigation"));
    expect(transport.intents.find((i) => i.type === "queue.start")).toEqual({ type: "queue.start", caseNumber: "CASE-1003" });
    expect(within(nav).getByText("Capabilities · 1/3 available")).toBeTruthy();
    fireEvent.click(within(nav).getByText("set up"));
    expect(screen.getByRole("dialog", { name: "Enable dns-adapter" })).toBeTruthy();
  });

  it("Reasoning: verdict basis, one-line prediction rows that expand, the matrix, the pinned fold", () => {
    mount();
    expect(within(lens()).getByText("Verdict basis")).toBeTruthy();
    expect(within(lens()).getByText(/all discriminators tested|a discriminator is untested/)).toBeTruthy();
    const rows = lens().querySelectorAll(".prow");
    expect(rows.length).toBe(4);
    expect(rows[0].querySelector(".ptitle")?.textContent).toBe(data.snapshot.hypotheses[0].predictions[0].title); // the title of record (adr/0090), never a client-side clip
    expect(rows[0].querySelector(".psplits")?.textContent).toBe("splits H2");
    expect(rows[1].querySelector(".pcount")?.textContent).toBe("attested");
    expect(rows[0].querySelector(".pbody")).toBeNull();
    fireEvent.click(rows[0].querySelector(".pline")!);
    expect(rows[0].querySelector(".pbody .pstatement")).toBeTruthy();
    expect(within(lens()).getByText("⚡ Run tests")).toBeTruthy();
    expect(within(lens()).getByText("✓ Adopt")).toBeTruthy();
    fireEvent.click(within(lens()).getByRole("tab", { name: "Matrix" }));
    const cells = Array.from(lens().querySelectorAll("tbody tr:first-child td.cell")).map((c) => c.textContent);
    expect(cells).toEqual(["✓", "✕"]);
    expect(lens().querySelector("tfoot")?.textContent).toContain("Inconsistent02");
    expect(within(lens()).getByText(/Pinned evidence · 1/)).toBeTruthy();
  });

  it("Response: cards in every state, the approve lock, the notification draft → notify.request, the comms threads", () => {
    const { transport } = mount();
    tab("Response");
    const cards = lens().querySelectorAll(".actioncard.full");
    expect(cards.length).toBe(5);
    expect(within(lens()).getByText("Approve · Send")).toBeTruthy();
    expect(within(lens()).getByText("Approve (challenge)…")).toBeTruthy();
    expect(within(lens()).getByText("↻ Re-request")).toBeTruthy();
    expect(within(lens()).getByText("Reverse…")).toBeTruthy(); // SUCCEEDED + REVERSIBLE
    expect(within(lens()).getByText("✗ ticket is read-only")).toBeTruthy();
    expect(within(lens()).getAllByText(/No policy pre-authorizes/).length).toBeGreaterThan(0);
    // The policy fact is the record's (adr/0099): the shadow policy that would have fired is named.
    expect(within(lens()).getByText(/a shadow policy would have pre-authorized this \(policy\/shadow-disable-service-accounts\)/)).toBeTruthy();
    fireEvent.click(within(lens()).getAllByText("Reject…")[0]);
    const dlg = screen.getByRole("dialog");
    fireEvent.change(within(dlg).getByRole("textbox"), { target: { value: "not warranted" } });
    fireEvent.click(within(dlg).getByText("Reject"));
    expect(transport.intents.find((i) => i.type === "action.reject")).toMatchObject({ reason: "not warranted" });
    fireEvent.click(within(lens()).getByText("Approve · Send"));
    expect(transport.intents.find((i) => i.type === "action.approve")).toMatchObject({ actionId: "act-2", tier: "T1" });
    expect((within(lens()).getByText("Approve (challenge)…") as HTMLButtonElement).disabled).toBe(true);
    act(() => transport.emit({ type: "state.patch", seq: 15, patch: { actions: data.snapshot.actions!.filter((a) => a.actionId !== "act-2") } }));
    expect((within(lens()).getByText("Approve (challenge)…") as HTMLButtonElement).disabled).toBe(false);
    expect(within(lens()).getByText(/What happened: under investigation/)).toBeTruthy();
    fireEvent.click(within(lens()).getByText("Preview & send email…"));
    const send = screen.getByRole("dialog");
    fireEvent.change(send.querySelector("#promptExtra_target")!, { target: { value: "ops@example.test" } });
    fireEvent.click(within(send).getByText("Request to send"));
    expect(transport.intents.find((i) => i.type === "notify.request")).toMatchObject({ channel: "notify.email", target: "ops@example.test" });
    expect(within(lens()).getByText("External work · 1 open")).toBeTruthy();
    expect(within(lens()).getByText("Acknowledge")).toBeTruthy();
    expect(within(lens()).getByText(/gap 00 §6.17/)).toBeTruthy(); // owner items: honest gap line
  });

  it("Summary: four tiles, key entities with derivable words, still to do, procedures followed, the handoff note", () => {
    const { transport } = mount();
    tab("Summary");
    expect(within(lens()).getByText("none yet")).toBeTruthy(); // verdict tile
    expect(within(lens()).getByText("2 of 4 approved")).toBeTruthy(); // containment: SUCCEEDED + FAILED were approved; REQUESTED and EXPIRED were not
    expect(within(lens()).getByText("isolated")).toBeTruthy(); // host-a's derivable status word
    expect(within(lens()).getByText(/No narrative has been drafted/)).toBeTruthy();
    expect(within(lens()).getByText("Procedure one")).toBeTruthy();
    fireEvent.click(within(lens()).getByText("Copy handoff note"));
    const copied = transport.intents.find((i) => i.type === "copy") as { text: string };
    expect(copied.text).toContain("Still to do:");
    fireEvent.click(within(lens()).getByText("Export report"));
    expect(transport.intents.some((i) => i.type === "export.open")).toBe(true);
  });

  it("Story: techniques, the timeline in event time with citing predictions and the start divider, not yet known", () => {
    mount();
    tab("Story");
    expect(within(lens()).getByText("T1078")).toBeTruthy();
    // The rows are the investigation's retrieved events (adr/0092): the three nightly logons collapse into one baseline row (adr/0095), the two others stay.
    expect(lens().querySelectorAll(".trow").length).toBe(3);
    expect(lens().querySelectorAll(".trow.baseline").length).toBe(1);
    expect(within(lens()).getByText("3 events over 3 days, typically around 02:00 UTC")).toBeTruthy();
    expect(within(lens()).getByText(/5 retrieved events in this investigation/)).toBeTruthy();
    expect(within(lens()).getByText(/No hypothesis is SUPPORTED yet, so there is no attack path/)).toBeTruthy();
    expect(within(lens()).getByText("Open full graph")).toBeTruthy();
    expect(within(lens()).getByText(/investigation starts/)).toBeTruthy();
    expect(within(lens()).getByText(/under H1 not-owner: No scheduled job/)).toBeTruthy();
    // The per-class sentence (adr/0090): the event reads as who did what where, its entities as chips.
    const sentence = lens().querySelector(".trow:not(.baseline) .esentence") as HTMLElement;
    expect(sentence.textContent).toContain("interactive logon to");
    expect(sentence.querySelectorAll(".eslot").length).toBeGreaterThan(0);
    fireEvent.click(within(lens()).getByRole("tab", { name: "Auth" }));
    expect(lens().querySelectorAll(".trow .esentence").length).toBeGreaterThan(0);
    fireEvent.click(within(lens()).getByRole("tab", { name: "Network" }));
    expect(within(lens()).getByText("No cited events carry an event time yet.")).toBeTruthy();
  });

  it("Companion: the Knowledge section with toggles, search and the nudge; turn records as links; the activity disclosure; Thread mode", () => {
    const { transport } = mount();
    const comp = document.querySelector("#companion") as HTMLElement;
    expect(within(comp).getByText("2 retrieved · 0 included")).toBeTruthy();
    expect(within(lens()).getByText(/1 procedure and 1 past case the auto posture would include are not included/)).toBeTruthy();
    fireEvent.click(comp.querySelector("#knowledgeBox input[type=checkbox]")!);
    expect(within(comp).getByText("2 retrieved · 1 included")).toBeTruthy();
    expect(within(lens()).getByText(/1 past case the auto posture would include is not included/)).toBeTruthy();
    fireEvent.change(comp.querySelector(".ksearch input")!, { target: { value: "isolation" } });
    fireEvent.keyDown(comp.querySelector(".ksearch input")!, { key: "Enter" });
    expect(transport.intents.find((i) => i.type === "knowledge.search")).toEqual({ type: "knowledge.search", text: "isolation" });
    expect(within(comp).getByText("3 retrieved · 1 included")).toBeTruthy(); // the hit joined, excluded
    // The first turn's records and activity.
    expect(within(comp).getByText(/Activity · 1 tool call/)).toBeTruthy();
    expect(within(comp).getByText("followed: Procedure one")).toBeTruthy();
    expect(within(comp).getByText(/committed · /)).toBeTruthy(); // the aside, collapsed
    expect(comp.querySelector(".actline")).toBeNull(); // no act lines in Conversation mode
    fireEvent.click(within(comp).getByRole("tab", { name: "Thread" }));
    expect(within(comp).getByText("approved host.isolate")).toBeTruthy();
    expect(within(comp).getByText(/context reset — agent re-grounded/)).toBeTruthy();
    const pin = comp.querySelector(".summaryPin") as HTMLButtonElement; // a restored tool row's pin
    fireEvent.click(pin);
    expect(screen.getByRole("dialog", { name: "Pin as evidence" })).toBeTruthy();
  });

  it("sends a turn with the included knowledge set and parks the composer during the turn", () => {
    const { transport } = mount();
    fireEvent.click(document.querySelector("#knowledgeBox input[type=checkbox]")!);
    const input = document.querySelector("#input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "continue" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(transport.intents.find((i) => i.type === "send")).toEqual({ type: "send", text: "continue", includedKnowledge: ["sop-1"] });
    expect(input.disabled).toBe(true);
  });

  it("records a prediction with its discriminators and the manual flag, and routes adopt / rename / abandon through the card", () => {
    const { transport } = mount();
    fireEvent.click(within(lens()).getAllByText("+ prediction")[0]);
    let dlg = screen.getByRole("dialog");
    fireEvent.change(dlg.querySelector("#promptInput")!, { target: { value: "a new test" } });
    fireEvent.change(dlg.querySelector("#promptExtra_title")!, { target: { value: "A new test, titled." } });
    fireEvent.click(within(dlg).getByLabelText(/A person tests this/));
    fireEvent.click(within(dlg).getByText("Record prediction"));
    expect(transport.intents.find((i) => i.type === "pred.new")).toMatchObject({ statement: "a new test", testKind: "manual", title: "A new test, titled." });
    fireEvent.click(lens().querySelector(".hypcard .hyphandle")!);
    expect(screen.getByRole("dialog", { name: "Rename H1 not-owner" })).toBeTruthy();
    fireEvent.click(screen.getByText("Cancel"));
    fireEvent.click(within(lens()).getAllByText("Abandon")[0]);
    dlg = screen.getByRole("dialog", { name: "Abandon H1 not-owner" });
    fireEvent.click(within(dlg).getByText("Abandon"));
    expect(transport.intents.find((i) => i.type === "hyp.abandon")).toMatchObject({ reason: "" });
    fireEvent.click(within(lens()).getByText("+ Add a hypothesis"));
    dlg = screen.getByRole("dialog", { name: "New hypothesis" });
    fireEvent.change(within(dlg).getByRole("textbox"), { target: { value: "another explanation" } });
    fireEvent.click(within(dlg).getByText("Record hypothesis"));
    expect(transport.intents.find((i) => i.type === "hyp.new")).toEqual({ type: "hyp.new", statement: "another explanation", handle: "H3" });
  });

  it("gates the verdict on pins and a rationale, and cites the active pins", () => {
    const { transport } = mount();
    fireEvent.click(within(header()).getByText("Verdict…"));
    const dlg = screen.getByRole("dialog");
    const submit = within(dlg).getByRole("button", { name: "Record verdict" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.click(within(dlg).getByLabelText("MALICIOUS"));
    fireEvent.change(dlg.querySelector("#verdictRationale")!, { target: { value: "because" } });
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);
    const v = transport.intents.find((i) => i.type === "verdict.submit") as { refs: string[] };
    expect(v.refs.length).toBe(2);
  });

  it("renames inline: Enter commits, Escape cancels", () => {
    const { transport } = mount();
    fireEvent.doubleClick(screen.getByRole("heading", { level: 1 }));
    const edit = document.querySelector(".titleedit") as HTMLInputElement;
    fireEvent.change(edit, { target: { value: "Renamed" } });
    fireEvent.keyDown(edit, { key: "Enter" });
    expect(transport.intents.find((i) => i.type === "rename")).toEqual({ type: "rename", title: "Renamed" });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Renamed");
  });

  it("renders the draft surface and guards the double submit", () => {
    const { transport } = mount(null);
    const input = document.querySelector("#draftInput") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "host-b" } });
    expect(screen.getByText(/investigate host/)).toBeTruthy();
    fireEvent.click(screen.getByText("hunt this as a question instead"));
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(transport.intents.filter((i) => i.type === "seed.submit")).toEqual([{ type: "seed.submit", value: "host-b", kind: "question" }]);
  });

  it("parks the composer, opens on Summary, and hides the verdict and rename affordances on a concluded record", () => {
    const concluded: HarnessSnapshot = { ...data, snapshot: { ...data.snapshot, investigation: { ...data.snapshot.investigation, state: "CONCLUDED", verdict: { disposition: "BENIGN", verdictAt: "" } } } };
    mount(concluded);
    expect((document.querySelector("#input") as HTMLTextAreaElement).disabled).toBe(true);
    expect(screen.getByRole("tab", { name: /^Summary/ }).getAttribute("aria-selected")).toBe("true");
    expect(within(header()).queryByText("Verdict…")).toBeNull();
    expect(header().querySelector(".iconbtn")).toBeNull();
    expect(within(header()).getByText("Reopen…")).toBeTruthy();
    expect(within(header()).getByText("Archive…")).toBeTruthy();
    // Close is done; Contain and Notify honestly stay undone (the actions never settled); nothing is current.
    expect(document.querySelector(".stage .step:last-child")!.classList.contains("done")).toBe(true);
    expect(document.querySelector(".stage .step.current")).toBeNull();
  });
});
