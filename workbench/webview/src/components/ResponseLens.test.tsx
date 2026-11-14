import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { needsAttention } from "../derive/actions";
import { ActionCard, approvalFact, willDoKicker } from "./ActionCard";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const data = synthetic as unknown as HarnessSnapshot;
const lens = () => document.querySelector(".lensscroll") as HTMLElement;
const item = (id: string) => document.getElementById("obj-" + id)!;

function mount(d: HarnessSnapshot = data, transport = new HarnessTransport(d)) {
  render(<AppProvider transport={transport}><App /></AppProvider>);
  fireEvent.click(screen.getByRole("tab", { name: /^Response/ }));
  return transport;
}
const ids = (root: Element, sel: string) => Array.from(root.querySelectorAll(sel)).map((e) => e.id.replace(/^obj-/, ""));
const timeline = () => ids(document.getElementById("obj-timeline")!, ".tl > .tlitem");

/** The synthetic snapshot with its actions replaced. */
function withActions(rows: ActionRow[]): HarnessSnapshot {
  const d = structuredClone(data);
  d.snapshot.actions = rows;
  return d;
}
/** A settled host.isolate row, act-1's shape, overridden. */
const settled = (over: Partial<ActionRow>): ActionRow => ({ ...(data.snapshot.actions![0] as ActionRow), approvedAt: undefined, ...over });
/** Open an item's details: the row on a line, the Details link on a card. */
function openDetails(id: string) {
  const row = item(id).querySelector(".tlrow");
  fireEvent.click(row || within(item(id)).getByRole("button", { name: /^Details/ }));
}
const history = (id: string) => {
  const dt = within(item(id)).getByText("History");
  return dt.nextElementSibling!.textContent!;
};

// adr/0361: decisions on top, then one timeline in request order, detail on demand.
describe("the Response lens (adr/0361)", () => {
  it("puts the pending decisions first, the window that closes soonest first and no deadline last", () => {
    const d = structuredClone(data);
    d.snapshot.actions = [
      ...(d.snapshot.actions || []),
      { ...d.snapshot.actions![1]!, actionId: "act-8", expiresAt: undefined },
    ];
    mount(d);
    const sections = Array.from(lens().querySelectorAll("section > h3, section > .lsechead > h3")).map((h) => h.childNodes[0]!.textContent);
    expect(sections.slice(0, 2)).toEqual(["Needs your decision", "Timeline"]);
    expect(within(lens()).getByRole("heading", { level: 3, name: "Response" })).toBeTruthy();
    // act-3 closes at 18:52, act-2 at 19:50; act-8 has no deadline.
    expect(ids(document.getElementById("obj-waiting")!, ".actioncard.pending")).toEqual(["act-3", "act-2", "act-8"]);
  });

  it("orders the timeline by request time, oldest first, and remembers the newest-first choice", () => {
    const transport = mount();
    expect(timeline()).toEqual(["act-4", "act-1", "act-5"]);
    expect(screen.getByRole("button", { name: "Oldest first" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Newest first" }));
    expect(timeline()).toEqual(["act-5", "act-1", "act-4"]);
    expect(transport.getState<{ responseOrder?: string }>()?.responseOrder).toBe("newest");
    cleanup();
    mount(data, transport);
    expect(timeline()).toEqual(["act-5", "act-1", "act-4"]);
  });

  it("renders a failed item as an open card at its place, with the outcome at rest and the raw error in details", () => {
    mount();
    const failed = item("act-5");
    expect(failed.querySelector(".actioncard.attention")).toBeTruthy();
    expect(failed.querySelector(".tlrow")).toBeNull();
    expect(within(failed).getByText(/Did not complete — reckon cannot confirm any change on CASE-1001/)).toBeTruthy();
    expect(within(failed).getByText("FAILED")).toBeTruthy();
    expect(within(failed).queryByText("ticket is read-only")).toBeNull();
    fireEvent.click(within(failed).getByRole("button", { name: /^Details/ }));
    expect(within(failed).getByText("ticket is read-only")).toBeTruthy();
  });

  it("keeps details hidden at rest and shows them on toggle", () => {
    mount();
    const done = item("act-1");
    const row = done.querySelector(".tlrow") as HTMLButtonElement;
    expect(within(row).getByText("SUCCEEDED")).toBeTruthy();
    expect(row.getAttribute("aria-expanded")).toBe("false");
    expect(within(done).queryByText("History")).toBeNull();
    fireEvent.click(row);
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(within(done).getByText("History")).toBeTruthy();
    expect(within(done).getByText(/requested · approved .+$/)).toBeTruthy();
    expect(within(done).getByText("Reverse…")).toBeTruthy();
    fireEvent.click(row);
    expect(within(done).queryByText("History")).toBeNull();

    // A pending card says at rest what approving does; the evidence waits in its details.
    const pending = item("act-2");
    expect(within(pending).getByText(/via/).textContent).toContain("via fixture-mail");
    expect(within(pending).getByText(/^Can be undone: no$/)).toBeTruthy();
    expect(within(pending).queryByText("Based on")).toBeNull();
    expect(pending.querySelector(".ackv")).toBeNull();
    fireEvent.click(within(pending).getByRole("button", { name: /^Details/ }));
    const facts = pending.querySelector(".ackv") as HTMLElement;
    expect(within(facts).getByText("Based on")).toBeTruthy();
    expect(within(facts).getByText("notify.email")).toBeTruthy(); // the type id
    expect(within(pending).getByRole("button", { name: "Open action record ›" })).toBeTruthy();
  });

  it("has no Decide later and ignores a set-aside list saved before it was removed", () => {
    const transport = new HarnessTransport(data);
    transport.setState({ aside: ["act-2"] });
    mount(data, transport);
    expect(within(lens()).queryByText("Decide later")).toBeNull();
    expect(within(lens()).queryByText("Set aside")).toBeNull();
    expect(document.getElementById("obj-aside")).toBeNull();
    expect(document.getElementById("obj-waiting")!.contains(item("act-2"))).toBe(true);
  });

  it("takes the history's words from the status, and a recorded approval time only adds when", () => {
    mount(withActions([
      settled({ actionId: "rej", status: "REJECTED", approvedAt: "2026-10-27T18:31:00.000Z" }),
      settled({ actionId: "exp", status: "EXPIRED", expired: true, approvedAt: "2026-10-27T18:31:00.000Z" }),
      settled({ actionId: "ok", status: "SUCCEEDED" }),
    ]));
    for (const id of ["rej", "exp", "ok"]) openDetails(id);
    expect(history("rej")).toMatch(/requested · first approval .* · rejected$/);
    expect(history("exp")).toMatch(/requested · first approval .* · expired$/);
    expect(history("ok")).toMatch(/requested · approved$/);
  });

  it("dates a timeline time whenever the day changes", () => {
    mount(withActions([
      settled({ actionId: "d1", requestedAt: "2026-10-27T12:00:00.000Z" }),
      settled({ actionId: "d2", requestedAt: "2026-10-29T12:00:00.000Z" }),
    ]));
    const label = (id: string) => item(id).querySelector(".tltime")!;
    const day2 = new Date("2026-10-29T12:00:00.000Z").toLocaleDateString(undefined, { month: "short", day: "numeric" });
    expect(label("d1").textContent).not.toBe(label("d2").textContent);
    expect(label("d2").querySelector(".tldate")?.textContent).toBe(day2);
    expect(label("d1").querySelector(".tldate")).toBeTruthy(); // the first item, not today
    expect(label("d2").getAttribute("title")).toBe(new Date("2026-10-29T12:00:00.000Z").toLocaleString());
  });

  it("shows every action's parameters in its details, not only a notification's", () => {
    mount(withActions([
      settled({ actionId: "tc", actionType: "ticket.comment", status: "FAILED", targets: ["CASE-1001"], parameters: { body: "Containment started on host-a." } }),
    ]));
    expect(within(item("tc")).queryByText("Containment started on host-a.")).toBeNull();
    openDetails("tc");
    expect(within(item("tc")).getByText("Containment started on host-a.")).toBeTruthy();
  });

  it("opens an unknown outcome with Resolve… and a re-requestable expiry with Re-request at rest", () => {
    mount(withActions([
      settled({ actionId: "unk", status: "OUTCOME_UNKNOWN" }),
      settled({ actionId: "exp", status: "EXPIRED", expired: true }),
    ]));
    expect(item("unk").querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(item("unk")).getByRole("button", { name: "Resolve…" })).toBeTruthy();
    expect(within(item("unk")).getByText(/whether host-a changed is unknown/)).toBeTruthy();
    expect(item("exp").querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(item("exp")).getByRole("button", { name: "↻ Re-request" })).toBeTruthy();
  });

  const STATUSES = [
    "REQUESTED", "PENDING_SECONDARY", "APPROVED", "EXECUTING", "SUCCEEDED", "FAILED", "PARTIAL",
    "TIMEOUT", "OUTCOME_UNKNOWN", "REVERSED", "REJECTED", "EXPIRED",
  ];
  const LINEAGE = ["alone", "re-requested", "reversed again"] as const;
  it.each(STATUSES.flatMap((status) => LINEAGE.map((l) => [status, l] as const)))("opens a %s item (%s) at rest exactly when needsAttention says so", (status, lineage) => {
    // Lapsed, so a pending status lands on the timeline too.
    const lapsed = status === "REQUESTED" || status === "PENDING_SECONDARY";
    const reversal = lineage === "reversed again" ? { isReversal: true, reversalOfRef: "o", requestedAt: "2026-10-27T18:40:00.000Z" } : {};
    const row = settled({ actionId: "x", status, expired: status === "EXPIRED" || lapsed, pending: lapsed, ...reversal });
    const rows = lineage === "re-requested" ? [row, settled({ actionId: "r", status: "SUCCEEDED", retryOf: "x" })]
      : lineage === "reversed again" ? [row, settled({ actionId: "r", status: "SUCCEEDED", isReversal: true, reversalOfRef: "o", requestedAt: "2026-10-27T18:50:00.000Z" })]
      : [row];
    mount(withActions(rows));
    const open = Boolean(item("x").querySelector(".actioncard.attention"));
    expect(open).toBe(needsAttention(row, rows));
    expect(Boolean(item("x").querySelector(".tlrow"))).toBe(!open);
  });

  it("closes a failure already re-requested to one line with a marker to its retry", () => {
    const failed = settled({ actionId: "f", status: "FAILED", errorDetail: "tool refused" });
    const lone = settled({ actionId: "g", status: "FAILED" });
    const transport = mount(withActions([failed, settled({ actionId: "f-retry", status: "SUCCEEDED", retryOf: "f" }), lone]));
    expect(item("f").querySelector(".actioncard.attention")).toBeNull();
    const row = item("f").querySelector(".tlrow") as HTMLElement;
    expect(within(row).getByText("FAILED")).toBeTruthy();
    fireEvent.click(within(item("f")).getByRole("button", { name: "re-requested ↻" }));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "action", actionId: "f-retry" } });
    // A failure nobody re-requested still opens.
    expect(item("g").querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(item("g")).queryByRole("button", { name: "re-requested ↻" })).toBeNull();
  });

  it("states a two-party requirement at rest, and nothing for a plain manual request", () => {
    const later = new Date(Date.now() + 3600_000).toISOString();
    mount(withActions([
      settled({
        actionId: "tp", actionType: "account.disable", status: "PENDING_SECONDARY", pending: true, expired: false, expiresAt: later,
        pendingLabel: "PENDING_TWO_PARTY", requiredMode: "TWO_PARTY", secondaryApproverPool: ["analyst-b", "analyst-c"], primaryApproverRef: "analyst-b",
      }),
      settled({ actionId: "man", status: "REQUESTED", pending: true, expired: false, expiresAt: later, pendingLabel: "PENDING_MANUAL" }),
    ]));
    expect(item("tp").querySelector(".ackv")).toBeNull(); // details closed
    expect(within(item("tp")).getByText(/awaiting a second approval from one of analyst-c/)).toBeTruthy();
    expect(within(item("tp")).getByRole("button", { name: "policy decision ›" })).toBeTruthy();
    expect(within(item("man")).queryByText(/needs your approval/)).toBeNull();
  });

  it.each(STATUSES)("says approved in the history exactly when the Approval row does (%s)", (status) => {
    const row = settled({ actionId: "x", status, expired: status === "EXPIRED" || status === "REQUESTED" || status === "PENDING_SECONDARY", pending: status === "REQUESTED" || status === "PENDING_SECONDARY" });
    mount(withActions([row]));
    openDetails("x");
    const approval = within(item("x")).getByText("Approval").nextElementSibling!.textContent!;
    expect(/\bapproved\b/.test(history("x"))).toBe(approval === "approved");
  });

  it("labels each time on a two-party history with the event it records", () => {
    mount(withActions([settled({ actionId: "tp", status: "SUCCEEDED", requiredMode: "TWO_PARTY", approvedAt: "2026-10-27T18:31:00.000Z" })]));
    openDetails("tp");
    expect(history("tp")).toMatch(/first approval .* · approved$/);
  });

  it("closes a failed reversal that a later reversal of the same original took over", () => {
    const transport = mount(withActions([
      settled({ actionId: "orig", status: "SUCCEEDED" }),
      settled({ actionId: "rev1", actionType: "host.unisolate", status: "FAILED", isReversal: true, reversalOfRef: "orig", requestedAt: "2026-10-27T18:40:00.000Z" }),
      settled({ actionId: "rev2", actionType: "host.unisolate", status: "SUCCEEDED", isReversal: true, reversalOfRef: "orig", requestedAt: "2026-10-27T18:50:00.000Z" }),
    ]));
    expect(item("rev1").querySelector(".actioncard.attention")).toBeNull();
    expect(within(item("rev1").querySelector(".tlrow") as HTMLElement).getByText("FAILED")).toBeTruthy();
    fireEvent.click(within(item("rev1")).getByRole("button", { name: "reversal requested again ↺" }));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "action", actionId: "rev2" } });
  });

  it("opens a failed reversal no later reversal took over", () => {
    mount(withActions([
      settled({ actionId: "orig", status: "SUCCEEDED" }),
      settled({ actionId: "rev2", actionType: "host.unisolate", status: "SUCCEEDED", isReversal: true, reversalOfRef: "orig", requestedAt: "2026-10-27T18:40:00.000Z" }),
      settled({ actionId: "rev1", actionType: "host.unisolate", status: "FAILED", isReversal: true, reversalOfRef: "orig", requestedAt: "2026-10-27T18:50:00.000Z" }),
    ]));
    expect(item("rev1").querySelector(".actioncard.attention")).toBeTruthy(); // the earlier reversal did not take it over
  });

  it("names a request that never ran as asked, and a lapsed one as needing nothing", () => {
    mount(withActions([
      settled({ actionId: "rej", status: "REJECTED" }),
      settled({ actionId: "lap", status: "REQUESTED", pending: true, expired: true, pendingLabel: "PENDING_MANUAL", expiresAt: "2026-10-27T18:00:00.000Z" }),
    ]));
    openDetails("rej");
    expect(within(item("rej")).getByText("Asked to")).toBeTruthy();
    openDetails("lap");
    expect(within(item("lap")).getByText("Asked to")).toBeTruthy();
    expect(within(item("lap")).queryByText(/needs your approval/)).toBeNull();
  });

  it("gives a compact pending card the decision lines its header no longer carries", () => {
    const row = settled({
      actionId: "c", actionType: "account.disable", tier: "T3", status: "REQUESTED", pending: true, expired: false,
      expiresAt: new Date(Date.now() + 3600_000).toISOString(), pendingLabel: "PENDING_TWO_PARTY", requiredMode: "TWO_PARTY",
      secondaryApproverPool: ["analyst-b"], reversibility: "IRREVERSIBLE", tierEscalated: true, targets: ["u1", "u2"],
    });
    render(<AppProvider transport={new HarnessTransport(data)}><ActionCard a={row} variant="compact" /></AppProvider>);
    const card = document.querySelector(".actioncard.compact") as HTMLElement;
    expect(within(card).getByText("Can be undone: no")).toBeTruthy();
    expect(within(card).getByText(/escalated to T3: 2 targets/)).toBeTruthy();
    expect(within(card).getByText(/two-party · a second approval from one of analyst-b/)).toBeTruthy();
    expect(within(card).getByText("Approve (challenge)…")).toBeTruthy();
  });

  it("needs a live successor to take a failure over", () => {
    const rev = (id: string, status: string, minute: number, over: Partial<ActionRow> = {}) =>
      settled({ actionId: id, actionType: "host.unisolate", status, isReversal: true, reversalOfRef: "orig", requestedAt: `2026-10-27T18:${minute}:00.000Z`, ...over });
    mount(withActions([
      settled({ actionId: "orig", status: "SUCCEEDED" }),
      rev("a1", "FAILED", 40), rev("a2", "REJECTED", 41),
      settled({ actionId: "orig2", status: "SUCCEEDED", targets: ["host-b"] }),
      rev("b1", "FAILED", 42, { reversalOfRef: "orig2" }),
      rev("b2", "REQUESTED", 43, { reversalOfRef: "orig2", pending: true, expired: false, pendingLabel: "PENDING_MANUAL", expiresAt: new Date(Date.now() + 3600_000).toISOString() }),
      settled({ actionId: "c1", status: "FAILED", targets: ["host-c"] }),
      settled({ actionId: "c2", status: "REJECTED", targets: ["host-c"], retryOf: "c1" }),
    ]));
    // A rejected later reversal took nothing over.
    expect(item("a1").querySelector(".actioncard.attention")).toBeTruthy();
    // A pending later reversal did: one line, and the marker names a request, not an outcome.
    expect(item("b1").querySelector(".actioncard.attention")).toBeNull();
    const marker = within(item("b1")).getByRole("button", { name: /requested again/ });
    expect(marker.textContent).not.toContain("reversed");
    // A rejected retry took nothing over.
    expect(item("c1").querySelector(".actioncard.attention")).toBeTruthy();
  });

  it.each([
    ["REQUESTED", "Will do"], ["APPROVED", "Will do"], ["EXECUTING", "Doing"], ["SUCCEEDED", "Did"], ["REVERSED", "Did"],
    ["FAILED", "Tried to"], ["PARTIAL", "Tried to"], ["TIMEOUT", "Tried to"], ["OUTCOME_UNKNOWN", "Tried to"],
    ["REJECTED", "Asked to"], ["EXPIRED", "Asked to"],
  ])("kicks a %s action's will-do with %s", (status, word) => {
    const pending = status === "REQUESTED";
    expect(willDoKicker(settled({ status, pending, expired: status === "EXPIRED" }))).toBe(word);
  });

  it("reads expired on a stored EXPIRED row's Approval", () => {
    const row = settled({ actionId: "e", status: "EXPIRED", expired: true });
    expect(approvalFact(row)).toBe("expired");
    mount(withActions([row]));
    openDetails("e");
    expect(within(item("e")).getByText("Approval").nextElementSibling!.textContent).toBe("expired");
  });
});
