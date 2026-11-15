import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { attentionKind } from "../derive/actions";
import { ActionCard, approvalFact, willDoKicker } from "./ActionCard";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import { ActionsClient } from "../../../src/client/actions";
import type { BackendHttp } from "../../../src/client/http";
import { decode, type Decoder } from "../../../src/wire/decode";

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
/** The lifecycle status and recorded outcome the action list serves for a
 * status word: PARTIAL is SUCCEEDED with outcome PARTIAL, TIMEOUT is FAILED
 * with outcome TIMEOUT (08 §7.1, adr/0361). */
const served = (word: string): Pick<ActionRow, "status" | "outcome"> =>
  word === "PARTIAL" ? { status: "SUCCEEDED", outcome: "PARTIAL" }
    : word === "TIMEOUT" ? { status: "FAILED", outcome: "TIMEOUT" }
    : { status: word };

/** Rows as the workbench maps them from GET /api/investigations/{id}/actions. */
async function fromWire(rows: Record<string, unknown>[]): Promise<ActionRow[]> {
  const http = { get: async <T,>(p: string, shape: Decoder<T>) => decode(p, shape, { actions: rows }) } as unknown as BackendHttp;
  return new ActionsClient(http).list("inv-1");
}
/** A settled host.isolate list row as the backend sends it. */
const wireRow = (id: string, host: string, over: Record<string, unknown>) => ({
  action_id: id, action_type: "host.isolate", tier: "T2", required_mode: "MANUAL", mode: "MANUAL",
  targets: [{ entity_ref: "x-host--" + id, resolved_identifier: host }], evidence_refs: ["observed-data--1"],
  reversibility: "REVERSIBLE", requested_at: "2026-10-27T18:30:00.000Z", approved_at: "2026-10-27T18:31:00.000Z",
  name: "Isolate host", category: "containment", adapter: "fixture-edr", ...over,
});

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
  it.each(STATUSES.flatMap((status) => LINEAGE.map((l) => [status, l] as const)))("opens a %s item (%s) at rest exactly when attentionKind says so", (status, lineage) => {
    // Lapsed, so a pending status lands on the timeline too.
    const lapsed = status === "REQUESTED" || status === "PENDING_SECONDARY";
    const reversal = lineage === "reversed again" ? { isReversal: true, reversalOfRef: "o", requestedAt: "2026-10-27T18:40:00.000Z" } : {};
    const row = settled({ actionId: "x", ...served(status), expired: status === "EXPIRED" || lapsed, pending: lapsed, ...reversal });
    const rows = lineage === "re-requested" ? [row, settled({ actionId: "r", status: "SUCCEEDED", retryOf: "x" })]
      : lineage === "reversed again" ? [row, settled({ actionId: "r", status: "SUCCEEDED", isReversal: true, reversalOfRef: "o", requestedAt: "2026-10-27T18:50:00.000Z" })]
      : [row];
    mount(withActions(rows));
    const open = Boolean(item("x").querySelector(".actioncard.attention"));
    expect(open).toBe(attentionKind(row, rows) !== null);
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
    const row = settled({ actionId: "x", ...served(status), expired: status === "EXPIRED" || status === "REQUESTED" || status === "PENDING_SECONDARY", pending: status === "REQUESTED" || status === "PENDING_SECONDARY" });
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
    expect(willDoKicker(settled({ ...served(status), pending, expired: status === "EXPIRED" }))).toBe(word);
  });

  // The list serves PARTIAL as SUCCEEDED and TIMEOUT as FAILED, with the
  // recorded outcome beside them; the timeline reads the outcome (adr/0361).
  it("opens a partial and a timed-out result as the list serves them, each in its own words", async () => {
    const rows = await fromWire([
      wireRow("part", "WIN-A", { status: "SUCCEEDED", outcome: "PARTIAL" }),
      wireRow("tout", "WIN-B", { status: "FAILED", outcome: "TIMEOUT" }),
    ]);
    expect(rows.map((r) => [r.status, r.outcome])).toEqual([["SUCCEEDED", "PARTIAL"], ["FAILED", "TIMEOUT"]]);
    mount(withActions(rows));
    const part = item("part");
    expect(part.querySelector(".actioncard.attention")).toBeTruthy();
    expect(part.querySelector(".tlrow")).toBeNull();
    expect(within(part).getByText(/Partly done — some of WIN-A may be changed; check each before relying on it\./)).toBeTruthy();
    expect(within(part).getByText("PARTIAL")).toBeTruthy();
    expect(within(part).queryByText("SUCCEEDED")).toBeNull();
    const tout = item("tout");
    expect(tout.querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(tout).getByText(/No answer from the tool — whether WIN-B changed is unknown\./)).toBeTruthy();
    expect(within(tout).getByText("TIMEOUT")).toBeTruthy();
    expect(within(tout).queryByText(/Did not complete/)).toBeNull();
    fireEvent.click(within(tout).getByRole("button", { name: /^Details/ }));
    expect(within(tout).getByText("Tried to")).toBeTruthy();
  });

  it("keeps a full success, a reversed partial and a resolution to success on one line, and opens a resolution to failure", async () => {
    const rows = await fromWire([
      wireRow("ok", "WIN-A", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("rev", "WIN-B", { status: "REVERSED", outcome: "PARTIAL" }),
      wireRow("fixed", "WIN-C", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("bad", "WIN-D", { status: "FAILED", outcome: "FAILED" }),
      wireRow("old", "WIN-E", { status: "FAILED" }),
    ]);
    mount(withActions(rows));
    for (const [id, word] of [["ok", "SUCCEEDED"], ["rev", "REVERSED"], ["fixed", "SUCCEEDED"]] as const) {
      expect(item(id).querySelector(".actioncard.attention")).toBeNull();
      expect(within(item(id).querySelector(".tlrow") as HTMLElement).getByText(word)).toBeTruthy();
    }
    // A failure with no outcome on record (a row served before the field) still opens.
    for (const [id, host] of [["bad", "WIN-D"], ["old", "WIN-E"]] as const) {
      expect(item(id).querySelector(".actioncard.attention")).toBeTruthy();
      expect(within(item(id)).getByText(new RegExp(`Did not complete — reckon cannot confirm any change on ${host}`))).toBeTruthy();
    }
  });

  // A reversal of a partial result takes it over (adr/0361), unless it was
  // rejected or left to expire.
  it("closes a partial result a live reversal of it took over, and opens one whose reversal was rejected", async () => {
    const later = new Date(Date.now() + 3600_000).toISOString();
    const undo = (id: string, of: string, over: Record<string, unknown>) =>
      wireRow(id, "WIN-" + of, { action_type: "host.unisolate", name: "Lift isolation", category: "recovery", is_reversal: true, reversal_of_ref: of, requested_at: "2026-10-27T18:40:00.000Z", ...over });
    const rows = await fromWire([
      wireRow("pa", "WIN-pa", { status: "SUCCEEDED", outcome: "PARTIAL" }),
      undo("pa-undo", "pa", { status: "REQUESTED", expires_at: later }),
      // The reversal ran; a best-effort original stays SUCCEEDED with its outcome PARTIAL.
      wireRow("pb", "WIN-pb", { status: "SUCCEEDED", outcome: "PARTIAL", reversibility: "best_effort" }),
      undo("pb-undo", "pb", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("pc", "WIN-pc", { status: "SUCCEEDED", outcome: "PARTIAL" }),
      undo("pc-undo", "pc", { status: "REJECTED" }),
    ]);
    const transport = mount(withActions(rows));
    for (const id of ["pa", "pb"]) {
      expect(item(id).querySelector(".actioncard.attention")).toBeNull();
      expect(within(item(id).querySelector(".tlrow") as HTMLElement).getByText("PARTIAL")).toBeTruthy();
      const marker = within(item(id)).getByRole("button", { name: "reversal requested ↺" });
      expect(marker.textContent).not.toContain("reversed");
    }
    fireEvent.click(within(item("pa")).getByRole("button", { name: "reversal requested ↺" }));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "action", actionId: "pa-undo" } });
    expect(item("pc").querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(item("pc")).getByText(/Partly done — some of WIN-pc may be changed/)).toBeTruthy();
    expect(within(item("pc")).queryByRole("button", { name: /reversal requested/ })).toBeNull();
  });

  it("marks a partial reversal that a reversal of it took over as a reversal requested, not again", async () => {
    const rows = await fromWire([
      wireRow("orig", "WIN-A", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("undo", "WIN-A", { action_type: "host.unisolate", status: "SUCCEEDED", outcome: "PARTIAL", is_reversal: true, reversal_of_ref: "orig", requested_at: "2026-10-27T18:40:00.000Z" }),
      wireRow("redo", "WIN-A", { status: "SUCCEEDED", outcome: "SUCCEEDED", is_reversal: true, reversal_of_ref: "undo", requested_at: "2026-10-27T18:50:00.000Z" }),
    ]);
    mount(withActions(rows));
    expect(item("undo").querySelector(".actioncard.attention")).toBeNull();
    expect(within(item("undo")).getByRole("button", { name: "reversal requested ↺" })).toBeTruthy();
    expect(within(item("undo")).queryByRole("button", { name: /again/ })).toBeNull();
  });

  it("walks a re-request chain to its first live link, and keeps a failure open when the chain ends dead", async () => {
    const rows = await fromWire([
      wireRow("a", "WIN-A", { status: "FAILED", outcome: "FAILED" }),
      wireRow("r1", "WIN-A", { status: "EXPIRED", retry_of: "a", requested_at: "2026-10-27T18:40:00.000Z" }),
      wireRow("r2", "WIN-A", { status: "SUCCEEDED", outcome: "SUCCEEDED", retry_of: "r1", requested_at: "2026-10-27T18:50:00.000Z" }),
      wireRow("b", "WIN-B", { status: "FAILED", outcome: "FAILED" }),
      wireRow("b1", "WIN-B", { status: "REJECTED", retry_of: "b", requested_at: "2026-10-27T18:45:00.000Z" }),
    ]);
    const transport = mount(withActions(rows));
    expect(item("a").querySelector(".actioncard.attention")).toBeNull();
    fireEvent.click(within(item("a")).getByRole("button", { name: "re-requested ↻" }));
    expect(transport.intents).toContainEqual({ type: "inspector.open", target: { kind: "action", actionId: "r2" } });
    expect(item("b").querySelector(".actioncard.attention")).toBeTruthy();
    expect(within(item("b")).queryByRole("button", { name: "re-requested ↻" })).toBeNull();
  });

  it("says a reversal the tool never answered is unconfirmed on the original", async () => {
    const rows = await fromWire([
      wireRow("orig", "WIN-A", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("undo", "WIN-A", { action_type: "host.unisolate", status: "FAILED", outcome: "TIMEOUT", is_reversal: true, reversal_of_ref: "orig", requested_at: "2026-10-27T18:40:00.000Z" }),
      wireRow("o2", "WIN-B", { status: "SUCCEEDED", outcome: "SUCCEEDED" }),
      wireRow("u2", "WIN-B", { action_type: "host.unisolate", status: "FAILED", outcome: "FAILED", is_reversal: true, reversal_of_ref: "o2", requested_at: "2026-10-27T18:41:00.000Z" }),
    ]);
    mount(withActions(rows));
    openDetails("orig");
    expect(within(item("orig")).getByText(/↺ reversal unconfirmed:/)).toBeTruthy();
    expect(within(item("orig")).queryByText(/did not take effect/)).toBeNull();
    openDetails("o2");
    expect(within(item("o2")).getByText(/↺ reversal did not take effect:/)).toBeTruthy();
  });

  it("reads expired on a stored EXPIRED row's Approval", () => {
    const row = settled({ actionId: "e", status: "EXPIRED", expired: true });
    expect(approvalFact(row)).toBe("expired");
    mount(withActions([row]));
    openDetails("e");
    expect(within(item("e")).getByText("Approval").nextElementSibling!.textContent).toBe("expired");
  });
});
