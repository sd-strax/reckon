// Dependencies on others (adr/0101) over the harness snapshot: the Response
// section lists them with their status, a notification card names what it asks
// for, confirm and waive are the analyst's acts with a note, a new one is
// recorded with its owner, and the Summary's still-to-do carries them.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;
const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name: new RegExp("^" + name) }));
const lens = () => document.querySelector(".lensscroll") as HTMLElement;

function mount(shape: (s: HarnessSnapshot) => void = () => undefined) {
  const copy = JSON.parse(JSON.stringify(data)) as HarnessSnapshot;
  shape(copy);
  const transport = new HarnessTransport(copy);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}

describe("dependencies on others", () => {
  it("lists them in the Response lens with their status, and the notification names what it asks for", () => {
    mount();
    tab("Response");
    const section = document.getElementById("obj-dependencies")!;
    expect(within(section).getByText(/^Dependencies on others/).textContent).toContain("2");
    const sent = document.getElementById("obj-dep-1")!;
    expect(within(sent).getByText("asked")).toBeTruthy();
    expect(within(sent).getByText(/owed by/).textContent).toContain("it-ops");
    const open = document.getElementById("obj-dep-2")!;
    expect(within(open).getByText("not yet asked")).toBeTruthy();
    expect(within(open).getByText("OPS-4411")).toBeTruthy();
    // The pending notification act-2 carries the OPEN dependency riding on it.
    const card = document.getElementById("obj-act-2")!;
    expect(within(card).getByText(/asks for/).textContent).toContain("Re-image host-a");
  });

  it("confirms with the analyst's note, and the card settles", () => {
    const transport = mount();
    tab("Response");
    fireEvent.click(within(document.getElementById("obj-dep-2")!).getByText("Confirm…"));
    const dlg = screen.getByRole("dialog", { name: /^Confirm — / });
    fireEvent.change(within(dlg).getAllByRole("textbox")[0]!, { target: { value: "the desktop lead confirmed it on the ticket" } });
    fireEvent.click(within(dlg).getByText("Record confirmation"));
    const sent = transport.intents.find((m) => m.type === "dependency.resolve");
    expect(sent).toEqual({ type: "dependency.resolve", dependencyId: "dep-2", verb: "confirm", note: "the desktop lead confirmed it on the ticket", externalRef: "OPS-4411" });
    const card = document.getElementById("obj-dep-2")!;
    expect(within(card).getByText("confirmed")).toBeTruthy();
    expect(within(card).queryByText("Confirm…")).toBeNull();
    expect(within(document.getElementById("obj-dependencies")!).getByText(/^Dependencies on others/).textContent).toContain("1");
  });

  it("waives only with a reason", () => {
    const transport = mount();
    tab("Response");
    fireEvent.click(within(document.getElementById("obj-dep-1")!).getByText("Waive…"));
    const dlg = screen.getByRole("dialog", { name: /^Waive — / });
    const waive = within(dlg).getByText("Waive", { selector: "button" });
    expect((waive as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(within(dlg).getAllByRole("textbox")[0]!, { target: { value: "no maintenance window existed, the question is moot" } });
    fireEvent.click(waive);
    expect(transport.intents.find((m) => m.type === "dependency.resolve")).toMatchObject({ dependencyId: "dep-1", verb: "waive", note: "no maintenance window existed, the question is moot" });
    expect(within(document.getElementById("obj-dep-1")!).getByText("waived")).toBeTruthy();
  });

  it("records a new dependency with its owner, riding on a pending notification when chosen", () => {
    const transport = mount();
    tab("Response");
    fireEvent.click(within(document.getElementById("obj-dependencies")!).getByText("Record a dependency…"));
    const dlg = screen.getByRole("dialog", { name: "Record a dependency" });
    fireEvent.change(within(dlg).getByPlaceholderText("e.g. rotate the svc_backup credential"), { target: { value: "rotate the svc_backup credential" } });
    fireEvent.change(within(dlg).getByLabelText("Who owes it"), { target: { value: "platform team" } });
    fireEvent.change(within(dlg).getByLabelText("Ride on a pending notification"), { target: { value: "act-2" } });
    fireEvent.click(within(dlg).getByText("Record"));
    expect(transport.intents.find((m) => m.type === "dependency.create")).toEqual({ type: "dependency.create", title: "rotate the svc_backup credential", owner: "platform team", detail: undefined, externalRef: undefined, notifyActionId: "act-2" });
    // The new card, and the notification it rides on naming it.
    expect(within(document.getElementById("obj-dep-3")!).getByText("rotate the svc_backup credential")).toBeTruthy();
    expect(within(document.getElementById("obj-act-2")!).getByText(/asks for/).textContent).toContain("rotate the svc_backup credential");
  });

  it("moves an open dependency off a notification that will not send", () => {
    // dep-2 rides on a notification that expired; act-2 is still pending.
    const transport = mount((s) => {
      const notify = s.snapshot.actions!.find((a) => a.actionId === "act-2")!;
      s.snapshot.actions!.push({ ...notify, actionId: "act-9", status: "EXPIRED", pending: false, expired: true });
      s.snapshot.dependencies!.find((d) => d.dependencyId === "dep-2")!.notifyActionId = "act-9";
    });
    tab("Response");
    const card = document.getElementById("obj-dep-2")!;
    expect(within(card).queryByText("Attach to notification…")).toBeNull();
    fireEvent.click(within(card).getByText("Move to another notification…"));
    const dlg = screen.getByRole("dialog", { name: /^Ride on a notification — / });
    fireEvent.change(within(dlg).getByLabelText("Notification"), { target: { value: "act-2" } });
    fireEvent.click(within(dlg).getByText("Attach"));
    expect(transport.intents.find((m) => m.type === "dependency.attach")).toEqual({ type: "dependency.attach", dependencyId: "dep-2", notifyActionId: "act-2" });
  });

  it("keeps an open dependency on a send in flight, even with another notification pending", () => {
    // act-2 is approved and will send; act-8 is a second, pending notification.
    mount((s) => {
      const notify = s.snapshot.actions!.find((a) => a.actionId === "act-2")!;
      s.snapshot.actions!.push({ ...notify, actionId: "act-8" });
      notify.status = "APPROVED";
      notify.pending = false;
    });
    tab("Response");
    const card = document.getElementById("obj-dep-2")!;
    expect(within(card).queryByText("Move to another notification…")).toBeNull();
    expect(within(card).queryByText("Attach to notification…")).toBeNull();
  });

  it("offers no resolution on a concluded record, in the section or the inspector", () => {
    mount((s) => { s.snapshot.investigation.state = "CONCLUDED"; });
    tab("Response");
    const card = document.getElementById("obj-dep-2")!;
    expect(within(card).queryByText("Confirm…")).toBeNull();
    fireEvent.click(within(card).getByTitle("Open the dependency"));
    const inspector = document.querySelector(".inspector") as HTMLElement;
    expect(within(inspector).getAllByText("Re-image host-a once the isolation is lifted").length).toBeGreaterThan(0);
    expect(within(inspector).queryByText("Confirm…")).toBeNull();
    expect(within(inspector).queryByText("Waive…")).toBeNull();
  });

  it("says when the dependencies could not be read instead of counting zero", () => {
    mount((s) => { s.snapshot.dependencies = null; });
    tab("Response");
    const section = document.getElementById("obj-dependencies")!;
    expect(within(section).getByText(/could not be read/)).toBeTruthy();
    expect(within(section).queryByText("none recorded")).toBeNull();
  });

  it("puts the open ones on the Summary's still-to-do", () => {
    mount();
    tab("Summary");
    const todo = Array.from(lens().querySelectorAll(".todo li.dependency")).map((li) => li.textContent);
    expect(todo).toEqual([
      expect.stringContaining("Confirm: Confirm whether maintenance was scheduled on host-a (it-ops)"),
      expect.stringContaining("Ask and confirm: Re-image host-a once the isolation is lifted (desktop team)"),
    ]);
  });
});
