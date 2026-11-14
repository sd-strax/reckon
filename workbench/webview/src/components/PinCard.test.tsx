import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import type { EventRow } from "../../../src/viewTypes";

// A pinned evidence card (adr/0352): what happened in one line, its parts
// behind one disclosure, an aliased account pair as one chip.
const OD = "observed-data--7c1d5e4f-ae6f-4182-cd4e-5f6a7b8c9d04";
const HOST = "x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";
const SID = "user-account--11111111-1111-4111-8111-111111111111";
const LOGIN = "user-account--22222222-2222-4222-8222-222222222222";
const EV = "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8";
const COPY = "0a1b2c3d-0000-4000-8000-0000000000ff"; // the same event, from another call (adr/0348)

function mount(pinRefs: string[], opts: { aliased?: boolean } = {}) {
  const d = structuredClone(synthetic as unknown as HarnessSnapshot);
  const s = d.snapshot;
  const ev = s.events!.find((e) => e.id === EV)!;
  ev.time = "2026-10-20T09:04:00Z";
  ev.view.sentence = [
    { t: "entity", ref: SID, text: "svc_backup" },
    { t: "text", text: " logged on to " },
    { t: "entity", ref: HOST, text: "host-a" },
  ];
  const copy: EventRow = { ...structuredClone(ev), id: COPY, callId: "another-call", recordedAt: "2026-10-20T09:10:00Z" };
  s.events = [...s.events!, copy];
  s.pins = [{ interpretationId: "interp-pin-x", finding: "svc_backup logged on to host-a.", inputRefs: pinRefs, actor: "AI_DELEGATED", pinnedAt: "2026-10-20T09:05:00.000Z", superseded: false }];
  s.derived = { ...s.derived!, ...(opts.aliased ? { accountAliases: [{ ref: LOGIN, also: [SID] }] } : {}) };
  d.labels[SID] = "S-1-5-21-1004";
  d.labels[LOGIN] = "CONTOSO\\svc_backup";
  const transport = new HarnessTransport(d);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  fireEvent.click(screen.getByRole("tab", { name: /^Reasoning/ }));
  return Object.assign(document.querySelector(".pinrow") as HTMLElement, { transport, d });
}

describe("a pinned evidence card (adr/0352)", () => {
  it("leads with the pinned event's sentence, its time and class before it", () => {
    const card = mount([OD, EV, HOST]);
    const line = card.querySelector(".pinevent")!;
    expect(line.textContent).toContain("svc_backup logged on to host-a");
    expect(line.querySelector(".pinclass")!.textContent).toBe("Authentication");
    expect(line.querySelector(".ttime")!.textContent).not.toBe("");
    expect(card.querySelectorAll(".pinevent").length).toBe(1);
  });

  it("resolves a pinned record of a merged event to its event", () => {
    const card = mount([COPY]);
    expect(card.querySelectorAll(".pinevent").length).toBe(1);
    expect(card.querySelector(".pinevent")!.textContent).toContain("logged on to host-a");
  });

  it("holds every pinned ref behind one disclosure, collapsed by default", () => {
    const refs = [OD, EV, HOST, SID];
    const card = mount(refs);
    const made = card.querySelector("details.pinmade") as HTMLDetailsElement;
    expect(made.open).toBe(false);
    expect(made.querySelector("summary")!.textContent).toBe("What this is made of · 4");
    expect(Array.from(made.querySelectorAll(".refchip")).map((c) => c.getAttribute("data-ref"))).toEqual(refs);
  });

  it("shows an aliased account pair once, the login's chip with the SID beside it", () => {
    const card = mount([OD, EV, SID, LOGIN], { aliased: true });
    const made = card.querySelector("details.pinmade")!;
    expect(made.querySelector("summary")!.textContent).toBe("What this is made of · 4");
    const chips = Array.from(made.querySelectorAll(".refchip")).map((c) => c.getAttribute("data-ref"));
    expect(chips).toEqual([OD, EV, LOGIN]);
    const part = made.querySelector(`.refchip[data-ref="${LOGIN}"]`)!.parentElement!;
    expect(part.textContent).toContain("CONTOSO\\svc_backup");
    expect(part.querySelector(".aliasof")!.textContent).toBe("S-1-5-21-1004");
    // In the sentence too: the account's chip, its SID beside it.
    expect(card.querySelector(".pinevent .aliasof")!.textContent).toBe("S-1-5-21-1004");
  });

  it("joins two accounts only when the record links them", () => {
    const card = mount([OD, EV, SID, LOGIN]);
    const chips = Array.from(card.querySelectorAll("details.pinmade .refchip")).map((c) => c.getAttribute("data-ref"));
    expect(chips).toEqual([OD, EV, SID, LOGIN]);
    expect(card.querySelector(".aliasof")).toBeNull();
  });

  it("keeps the flat chips for a pin with no event the workbench holds", () => {
    const refs = [OD, "8f0e0e0e-0000-4000-8000-000000000001", HOST];
    const card = mount(refs);
    expect(card.querySelector(".pinevent")).toBeNull();
    expect(card.querySelector("details.pinmade")).toBeNull();
    expect(Array.from(card.querySelectorAll(".refchip")).map((c) => c.getAttribute("data-ref"))).toEqual(refs);
  });

  it("joins the pair once the derived views say so after an identity act, with no thread change", () => {
    const card = mount([OD, EV, SID, LOGIN]);
    expect(card.querySelectorAll("details.pinmade .refchip").length).toBe(4);
    const { transport, d } = card;
    act(() => transport.emit({ type: "state.patch", seq: d.snapshot.seq, patch: { derived: { ...d.snapshot.derived!, accountAliases: [{ ref: LOGIN, also: [SID] }] } } }));
    const pinned = document.querySelector(".pinrow")!;
    expect(Array.from(pinned.querySelectorAll("details.pinmade .refchip")).map((c) => c.getAttribute("data-ref"))).toEqual([OD, EV, LOGIN]);
    expect(pinned.querySelector("details.pinmade .aliasof")!.textContent).toBe("S-1-5-21-1004");
  });
});
