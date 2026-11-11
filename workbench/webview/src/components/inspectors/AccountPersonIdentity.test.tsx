// An account's and a person's identity in the Entity inspector (adr/0324): a
// cut owner is listed with its mark and the cut's Withdraw, never hidden; a
// directory's owner offers no Withdraw, only "Not this person's…"; the other
// end of an act goes by ref when typed as one and by hint otherwise; a
// refusal over a hint that named several lists them for the analyst to pick;
// a person reads as a person, not a vendor.
import { act, cleanup, fireEvent, render, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AppProvider } from "../../app";
import { App } from "../App";
import { HarnessTransport, type HarnessSnapshot } from "../../../../harness/transport";
import synthetic from "../../../../harness/snapshots/synthetic.json";
import type { AccountIdentity, EvidenceDoc, PersonIdentity } from "../../../../src/viewTypes";
import type { InboundMessage, RenderMessage } from "../../../../src/protocol";

const base = synthetic as unknown as HarnessSnapshot;
const acct = "user-account--5a9b3c2d-8c4d-4f60-ab2c-3d4e5f6a7b02";
const alias = "user-account--00000000-0000-4000-8000-000000000010";
const other = "user-account--00000000-0000-4000-8000-000000000011";
const personA = "identity--00000000-0000-4000-8000-0000000000a1";
const personB = "identity--00000000-0000-4000-8000-0000000000b2";
const personC = "identity--00000000-0000-4000-8000-0000000000c3";
const acct2 = "user-account--00000000-0000-4000-8000-000000000012";
const hostX = "x-host--00000000-0000-4000-8000-0000000000f1";
const personD = "identity--00000000-0000-4000-8000-0000000000d4";
const personE = "identity--00000000-0000-4000-8000-0000000000e5";
const personF = "identity--00000000-0000-4000-8000-0000000000f6";
const personG = "identity--00000000-0000-4000-8000-0000000000a7";
const personH = "identity--00000000-0000-4000-8000-0000000000a8";
const acct3 = "user-account--00000000-0000-4000-8000-000000000013";
const acct3login = "user-account--00000000-0000-4000-8000-000000000014";
const hostY = "x-host--00000000-0000-4000-8000-0000000000f2";
const at = "2026-01-02T03:04:05Z";

function accountView(owners = true): AccountIdentity {
  return {
    aliases: [{ edgeId: "relationship--al", otherRef: alias, kind: "observed", source: "dc", at, withdrawn: false }],
    distinct: [
      { edgeId: "relationship--cut", otherRef: personB, kind: "asserted", by: "u-1", byName: "Ana Analyst", at, note: "shared desk", withdrawn: false },
      { edgeId: "relationship--apart", otherRef: other, kind: "asserted", by: "u-1", byName: "Ana Analyst", at, withdrawn: false },
    ],
    seenAs: { logins: ["CONTOSO\\jdoe"], ids: [{ tool: "dc", uid: "S-1-5-21-1001" }], principalNames: ["jdoe@contoso.example"] },
    owners: owners ? [
      { personRef: personA, accountRef: acct, edgeId: "relationship--o1", kind: "observed", source: "idp", at, withdrawn: false, cut: false },
      { personRef: personB, accountRef: acct, edgeId: "relationship--o2", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--cut", accountRef: acct, personRef: personB } },
      { personRef: personC, accountRef: alias, edgeId: "relationship--o3", kind: "asserted", by: "u-1", byName: "Ana Analyst", at, withdrawn: false, cut: false },
      // A directory's owner edge on an account aliased to this one.
      { personRef: personD, accountRef: alias, edgeId: "relationship--o4", kind: "observed", source: "idp", at, withdrawn: false, cut: false },
      // Cut by an edge on an aliased account, and by one against an aliased person.
      { personRef: personE, accountRef: acct, edgeId: "relationship--o5", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--viaacct", accountRef: alias, personRef: personE } },
      { personRef: personF, accountRef: acct, edgeId: "relationship--o6", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--viaperson", accountRef: acct, personRef: personG } },
    ] : [],
  };
}

const personView: PersonIdentity = {
  aliases: [],
  distinct: [
    { edgeId: "relationship--pd", otherRef: personB, kind: "asserted", by: "u-2", byName: "Ben Analyst", at, note: "namesakes", withdrawn: false },
    { edgeId: "relationship--pcut", otherRef: acct2, kind: "asserted", by: "u-2", byName: "Ben Analyst", at, withdrawn: false },
  ],
  accounts: [
    { accountRef: acct, personRef: personA, accountAliases: [acct], edgeId: "relationship--o1", kind: "observed", source: "idp", at, withdrawn: false, cut: false },
    { accountRef: acct2, personRef: personA, accountAliases: [acct2], edgeId: "relationship--p2", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--pcut", accountRef: acct2, personRef: personA } },
    // Cut by an edge against a person aliased to this one, on an account aliased to the row's.
    { accountRef: other, personRef: personA, accountAliases: [other], edgeId: "relationship--p3", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--pvia", accountRef: alias, personRef: personB, by: "u-2", byName: "Ben Analyst", at, note: "the login was recycled" } },
    // Attached to a person aliased to this one: the account a read of this person walks, with an alias of its own.
    { accountRef: acct3, personRef: personH, accountAliases: [acct3, acct3login], edgeId: "relationship--p4", kind: "observed", source: "idp", at, withdrawn: false, cut: false },
  ],
  // What a read of the person walks: the cut accounts are not in it.
  readAccounts: [acct, acct3, acct3login],
};

const personDoc = (ref: string, view?: PersonIdentity): EvidenceDoc => ({
  ref, kind: "stix", type: "identity", payload: { type: "identity", properties: { identity_class: "individual", name: "Jane Doe" } }, ...(view ? { personIdentity: view } : {}),
});

function mount(view: AccountIdentity = accountView()) {
  const event = base.snapshot.events![0]!;
  const data: HarnessSnapshot = {
    ...base,
    // One retrieved event names the account and a host the person's section lists.
    snapshot: { ...base.snapshot, events: [...base.snapshot.events!,
      { ...event, id: "2c3d4e5f-6071-4283-94a5-b6c7d8e9f0a1", entityRefs: [acct, hostX] },
      // An event that names an alias of an account a person aliased to this one owns.
      { ...event, id: "3d4e5f60-7182-4394-a5b6-c7d8e9f0a1b2", entityRefs: [acct3login, hostY] }] },
    docs: {
      ...base.docs,
      [acct]: { ref: acct, kind: "stix", type: "user-account", payload: { type: "user-account", properties: { account_login: "CONTOSO\\jdoe" } }, accountIdentity: view },
      [personA]: personDoc(personA, personView),
      [personB]: personDoc(personB),
      [personC]: { ref: personC, kind: "stix", type: "identity", payload: { type: "identity", properties: { name: "a vendor" } } },
    },
    labels: { ...base.labels, [personA]: "Jane Doe", [personH]: "J. Doe (hr)", [hostX]: "ws-jane", [hostY]: "ws-jane-2" },
  };
  const transport = new HarnessTransport(data);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  return transport;
}
const pane = () => document.querySelector(".inspector.active") as HTMLElement;
const open = (ref: string) => fireEvent.click(document.querySelector(`.refchip[data-ref="${ref}"]`) as HTMLElement);
const edge = (id: string) => pane().querySelector(`[data-edge="${id}"]`) as HTMLElement;
type IdentityIntent = Extract<InboundMessage, { type: "identity.assert" | "identity.withdraw" | "identity.owner" }>;
const lastAct = (t: HarnessTransport): IdentityIntent | undefined =>
  t.intents.filter((i): i is IdentityIntent => i.type === "identity.assert" || i.type === "identity.withdraw" || i.type === "identity.owner").at(-1);
const emit = (t: HarnessTransport, m: RenderMessage) => act(() => { (t as unknown as { emit(m: RenderMessage): void }).emit(m); });
const fill = (form: HTMLElement, value: string) => fireEvent.change(form.querySelector("input")!, { target: { value } });

afterEach(cleanup);

describe("an account's identity in the inspector (adr/0324)", () => {
  it("lists its aliases, what it was seen as, and every owner with its origin", () => {
    mount();
    open(acct);
    const p = pane();
    expect(within(edge("relationship--al")).getByText("from dc")).toBeTruthy();
    expect(within(p).getByText("Not the same account as")).toBeTruthy();
    expect(within(p).getByText("logins").nextElementSibling!.textContent).toBe("CONTOSO\\jdoe");
    expect(within(p).getByText("dc S-1-5-21-1001")).toBeTruthy();
    expect(within(p).getByText("jdoe@contoso.example")).toBeTruthy();
    expect(within(p).getByText("Belongs to")).toBeTruthy();
    expect(within(edge("relationship--o1")).getByText("from idp")).toBeTruthy();
    expect(within(edge("relationship--o3")).getByText(/^asserted by Ana Analyst · /)).toBeTruthy();
    expect(within(p).queryByText("No person owns this account.")).toBeNull();
  });

  it("offers no Withdraw on a directory's owner: the row offers Not this person's…, sent as the cut on the account", () => {
    const transport = mount();
    open(acct);
    const row = edge("relationship--o1");
    expect(within(row).queryByText("Withdraw…")).toBeNull();
    fireEvent.click(within(row).getByText("Not this person's…"));
    const form = within(pane()).getByRole("form", { name: "Not this person's account" });
    fireEvent.click(within(form).getByText("Cut"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct, requestId: expect.any(String), relation: "distinct", other: { ref: personA } });
  });

  it("shows a cut owner with its mark and the cut's Withdraw, never hidden", () => {
    const transport = mount();
    open(acct);
    const row = edge("relationship--o2");
    expect(within(row).getByText("cut · not this person's, by assertion")).toBeTruthy();
    expect(within(row).queryByText("Not this person's…")).toBeNull(); // already cut
    expect(within(row).queryByText("Withdraw…")).toBeNull(); // a directory's record
    fireEvent.click(within(row).getByText("Withdraw the cut…"));
    const form = within(pane()).getByRole("form", { name: "Withdraw this assertion" });
    fireEvent.click(within(form).getByText("Withdraw"));
    expect(lastAct(transport)).toEqual({ type: "identity.withdraw", ref: acct, requestId: expect.any(String), edgeId: "relationship--cut" });
    // The cut placed on its owner row is not listed a second time.
    expect(edge("relationship--cut")).toBeNull();
  });

  it("withdraws an asserted owner on the account of the set it starts from", () => {
    const transport = mount();
    open(acct);
    fireEvent.click(within(edge("relationship--o3")).getByText("Withdraw…"));
    fireEvent.click(within(within(pane()).getByRole("form", { name: "Withdraw this assertion" })).getByText("Withdraw"));
    expect(lastAct(transport)).toEqual({ type: "identity.withdraw", ref: alias, requestId: expect.any(String), edgeId: "relationship--o3" });
  });

  it("says so in words when no person owns the account", () => {
    mount(accountView(false));
    open(acct);
    expect(within(pane()).getByText("No person owns this account.")).toBeTruthy();
    // The cut with no owner row to sit on is listed on its own, with its Withdraw.
    expect(within(edge("relationship--cut")).getByText("Withdraw…")).toBeTruthy();
  });

  it("sends the other account by hint unless typed as a ref, and the owner the same way", () => {
    const transport = mount();
    open(acct);
    const p = pane();
    fireEvent.click(within(p).getByText("Same as another account…"));
    let form = within(p).getByRole("form", { name: "Same as another account" });
    fill(form, "jdoe");
    fireEvent.click(within(form).getByText("Join"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct, requestId: expect.any(String), relation: "aliases", other: { hint: "jdoe" } });
    emit(transport, { type: "inspector.identity", ref: acct, requestId: (lastAct(transport) as { requestId: string }).requestId, views: { accountIdentity: accountView() } });

    fireEvent.click(within(p).getByText("Not the same account as…"));
    form = within(p).getByRole("form", { name: "Not the same account as" });
    fill(form, other);
    fireEvent.click(within(form).getByText("Split"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct, requestId: expect.any(String), relation: "distinct", other: { ref: other } });
    emit(transport, { type: "inspector.identity", ref: acct, requestId: (lastAct(transport) as { requestId: string }).requestId, views: { accountIdentity: accountView() } });

    fireEvent.click(within(p).getByText("Set owner…"));
    form = within(p).getByRole("form", { name: "Set owner" });
    fill(form, "jdoe@contoso.example");
    fireEvent.click(within(form).getByText("Set"));
    expect(lastAct(transport)).toEqual({ type: "identity.owner", ref: acct, requestId: expect.any(String), person: { hint: "jdoe@contoso.example" } });
    emit(transport, { type: "inspector.identity", ref: acct, requestId: (lastAct(transport) as { requestId: string }).requestId, views: { accountIdentity: accountView() } });

    fireEvent.click(within(p).getByText("Set owner…"));
    form = within(p).getByRole("form", { name: "Set owner" });
    fill(form, personA);
    fireEvent.click(within(form).getByText("Set"));
    expect(lastAct(transport)).toEqual({ type: "identity.owner", ref: acct, requestId: expect.any(String), person: { ref: personA } });
  });

  it("lists the accounts a hint named under the form: picking one fills its ref, and nothing is sent until the analyst does", () => {
    const transport = mount();
    open(acct);
    const p = pane();
    fireEvent.click(within(p).getByText("Same as another account…"));
    const form = within(p).getByRole("form", { name: "Same as another account" });
    fill(form, "j.doe");
    fireEvent.click(within(form).getByText("Join"));
    const sent = lastAct(transport) as { requestId: string };
    emit(transport, {
      type: "identity.error", ref: acct, requestId: sent.requestId, message: "\"j.doe\" names 2 accounts; name one by other_ref",
      accounts: [{ ref: alias, accountType: "windows-domain", accountLogin: "contoso\\j.doe" }, { ref: other, accountType: "cloud", accountLogin: "j.doe@contoso.example" }],
    });
    expect(within(form).getByText("\"j.doe\" names 2 accounts; name one by other_ref")).toBeTruthy();
    const before = transport.intents.length;
    fireEvent.click(within(form).getByText("j.doe@contoso.example · cloud"));
    expect((form.querySelector("input") as HTMLInputElement).value).toBe(other);
    expect(transport.intents.length).toBe(before);
    fireEvent.click(within(form).getByText("Join"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct, requestId: expect.any(String), relation: "aliases", other: { ref: other } });
  });

  it("sends an act from a row on the account the row's edge is on", () => {
    const transport = mount();
    open(acct);
    fireEvent.click(within(edge("relationship--o4")).getByText("Not this person's…"));
    fireEvent.click(within(within(pane()).getByRole("form", { name: "Not this person's account" })).getByText("Cut"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: alias, requestId: expect.any(String), relation: "distinct", other: { ref: personD } });
  });

  it("reaches a cut from the row that shows it, when the edge is on an aliased account or against an aliased person", () => {
    const transport = mount();
    open(acct);
    for (const [row, edgeId, end, via] of [["relationship--o5", "relationship--viaacct", alias, alias], ["relationship--o6", "relationship--viaperson", acct, personG]] as const) {
      const r = edge(row);
      expect(within(r).getByText("cut · not this person's, by assertion")).toBeTruthy();
      expect(within(r).getByText(/^cut via/).querySelector(`[data-ref="${via}"]`)).toBeTruthy();
      fireEvent.click(within(r).getByText("Withdraw the cut…"));
      const form = within(pane()).getByRole("form", { name: "Withdraw this assertion" });
      fireEvent.click(within(form).getByText("Withdraw"));
      expect(lastAct(transport)).toEqual({ type: "identity.withdraw", ref: end, requestId: expect.any(String), edgeId });
      emit(transport, { type: "inspector.identity", ref: end, requestId: (lastAct(transport) as { requestId: string }).requestId, views: null });
      fireEvent.click(within(form).getByText("Close"));
    }
    // A row cut by its own edge names no other end.
    expect(within(edge("relationship--o2")).queryByText(/^cut via/)).toBeNull();
  });

  it("shows a refusal of the server's verbatim, and keeps the form open", () => {
    const transport = mount();
    open(acct);
    fireEvent.click(within(edge("relationship--o3")).getByText("Withdraw…"));
    const form = within(pane()).getByRole("form", { name: "Withdraw this assertion" });
    fireEvent.click(within(form).getByText("Withdraw"));
    const sent = lastAct(transport) as { requestId: string };
    emit(transport, { type: "identity.error", ref: alias, requestId: sent.requestId, message: "a directory's record; assert that the account is not this person's instead" });
    expect(within(form).getByText("a directory's record; assert that the account is not this person's instead")).toBeTruthy();
  });
});

describe("a person's identity in the inspector (adr/0324)", () => {
  const openPerson = () => {
    open(acct);
    fireEvent.click(edge("relationship--o1").querySelector(`[data-ref="${personA}"]`) as HTMLElement);
  };

  it("reads as a person, with its accounts, the cut one marked with the cut's Withdraw, and the hosts beside them", () => {
    const transport = mount();
    openPerson();
    const p = pane();
    expect(within(p).getByRole("heading", { level: 2 }).textContent).toBe("Jane Doe");
    expect(p.querySelector(".isub .kicker")!.textContent).toBe("person");
    expect(within(p).getByText("Not the same person as")).toBeTruthy();
    expect(within(edge("relationship--pd")).getByText("“namesakes”")).toBeTruthy();
    const cut = edge("relationship--p2");
    expect(within(cut).getByText("cut · not this person's, by assertion")).toBeTruthy();
    fireEvent.click(within(cut).getByText("Withdraw the cut…"));
    fireEvent.click(within(within(p).getByRole("form", { name: "Withdraw this assertion" })).getByText("Withdraw"));
    expect(lastAct(transport)).toEqual({ type: "identity.withdraw", ref: acct2, requestId: expect.any(String), edgeId: "relationship--pcut" });
    expect(within(p).getByText("Hosts used")).toBeTruthy();
    expect(within(p).getByText("ws-jane")).toBeTruthy();
  });

  it("asks for a shown person and account again when an act elsewhere marks them stale, and says the view may be out of date", () => {
    const transport = mount();
    openPerson();
    transport.send = (i) => { transport.intents.push(i); };
    const before = transport.intents.length;
    emit(transport, { type: "identity.changed", ref: acct });
    expect(within(pane()).getByText("This view may be out of date.")).toBeTruthy();
    expect(transport.intents.slice(before)).toContainEqual({ type: "inspector.open", target: { kind: "ref", ref: personA } });
    // Back reveals the account, held from before the act: read again.
    const back = transport.intents.length;
    fireEvent.click(within(pane()).getByText("‹ back"));
    expect(transport.intents.slice(back)).toContainEqual({ type: "inspector.open", target: { kind: "ref", ref: acct } });
  });

  it("lists an account attached to an aliased person with a via chip, acts on the row's own ends, and counts hosts its aliases name", () => {
    const transport = mount();
    openPerson();
    const row = edge("relationship--p4");
    expect(within(row).getByText(/^via/).querySelector(`[data-ref="${personH}"]`)).toBeTruthy();
    expect(within(edge("relationship--o1")).queryByText(/^via/)).toBeNull(); // the pane's own edge
    fireEvent.click(within(row).getByText("Not this person's…"));
    fireEvent.click(within(within(pane()).getByRole("form", { name: "Not this person's account" })).getByText("Cut"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct3, requestId: expect.any(String), relation: "distinct", other: { ref: personH } });
    expect(within(pane()).getByText("ws-jane-2")).toBeTruthy();
  });

  it("shows who cut an account, when and why, on the row the cut reaches through an alias", () => {
    mount();
    openPerson();
    const r = edge("relationship--p3");
    expect(within(r).getByText(/^cut by Ben Analyst · /)).toBeTruthy();
    expect(within(r).getByText("“the login was recycled”")).toBeTruthy();
  });

  it("reaches a cut asserted against an aliased person on an aliased account from the account's row", () => {
    const transport = mount();
    openPerson();
    const r = edge("relationship--p3");
    expect(within(r).getByText(/^cut via/).querySelector(`[data-ref="${personB}"]`)).toBeTruthy();
    fireEvent.click(within(r).getByText("Withdraw the cut…"));
    fireEvent.click(within(within(pane()).getByRole("form", { name: "Withdraw this assertion" })).getByText("Withdraw"));
    expect(lastAct(transport)).toEqual({ type: "identity.withdraw", ref: alias, requestId: expect.any(String), edgeId: "relationship--pvia" });
  });

  it("re-reads a pane already stale once when an act on another entity lands in this panel, and not again on a stale answer", () => {
    const transport = mount();
    openPerson();
    transport.send = (i) => { transport.intents.push(i); };
    // A degraded answer: the held view is reused and marked stale.
    emit(transport, { type: "inspector.evidence", ref: personA, doc: personDoc(personA), appearances: [] });
    expect(within(pane()).getByText("This view may be out of date.")).toBeTruthy();
    const opens = (from: number) => transport.intents.slice(from).filter((i) => i.type === "inspector.open");
    const before = transport.intents.length;
    // This panel's act on the account lands: its answer, then the change.
    emit(transport, { type: "inspector.identity", ref: acct, requestId: "elsewhere-in-pane", views: { accountIdentity: accountView() } });
    emit(transport, { type: "identity.changed", ref: acct, acted: true });
    expect(opens(before)).toEqual([{ type: "inspector.open", target: { kind: "ref", ref: personA } }]);
    // The answer comes back degraded again: nothing more.
    emit(transport, { type: "inspector.evidence", ref: personA, doc: personDoc(personA), appearances: [] });
    expect(opens(before)).toHaveLength(1);
  });

  it("re-reads a pane already stale once on another panel's change", () => {
    const transport = mount();
    openPerson();
    transport.send = (i) => { transport.intents.push(i); };
    emit(transport, { type: "inspector.evidence", ref: personA, doc: personDoc(personA), appearances: [] });
    const before = transport.intents.length;
    emit(transport, { type: "identity.changed", ref: acct2 });
    expect(transport.intents.slice(before).filter((i) => i.type === "inspector.open")).toEqual([{ type: "inspector.open", target: { kind: "ref", ref: personA } }]);
  });

  it("does not read the acted entity a second time in the pane that acted: its view came with the answer", () => {
    const transport = mount();
    openPerson();
    transport.send = (i) => { transport.intents.push(i); };
    const before = transport.intents.length;
    emit(transport, { type: "inspector.identity", ref: personA, requestId: "own", views: { personIdentity: personView } });
    emit(transport, { type: "identity.changed", ref: personA, acted: true });
    expect(transport.intents.slice(before).filter((i) => i.type === "inspector.open")).toEqual([]);
    expect(within(pane()).queryByText("This view may be out of date.")).toBeNull();
  });

  it("matches Hosts used on the set the read walks alone, never on a row's aliases", () => {
    // John owns SID1 and SID2 by record and SAM aliases both; SID2 is cut, so the
    // read walks SID1 alone, while SID1's row still lists SAM and SID2 for display.
    const sid1 = "user-account--00000000-0000-4000-8000-000000000021";
    const sid2 = "user-account--00000000-0000-4000-8000-000000000022";
    const sam = "user-account--00000000-0000-4000-8000-000000000023";
    const hostH = "x-host--00000000-0000-4000-8000-0000000000f3";
    const hostH2 = "x-host--00000000-0000-4000-8000-0000000000f4";
    const john: PersonIdentity = {
      aliases: [],
      distinct: [{ edgeId: "relationship--rc", otherRef: sid2, kind: "asserted", by: "u-1", byName: "Ana Analyst", at, withdrawn: false }],
      accounts: [
        { accountRef: sid1, personRef: personA, accountAliases: [sid1, sam, sid2], edgeId: "relationship--j1", kind: "observed", source: "idp", at, withdrawn: false, cut: false },
        { accountRef: sid2, personRef: personA, accountAliases: [sid2, sam, sid1], edgeId: "relationship--j2", kind: "observed", source: "idp", at, withdrawn: false, cut: true, cutBy: { edgeId: "relationship--rc", accountRef: sid2, personRef: personA } },
      ],
      readAccounts: [sid1],
    };
    const event = base.snapshot.events![0]!;
    const data: HarnessSnapshot = {
      ...base,
      snapshot: { ...base.snapshot, events: [
        { ...event, id: "4e5f6071-8293-44a5-b6c7-d8e9f0a1b2c3", entityRefs: [sam, hostH] },
        { ...event, id: "5f607182-93a4-45b6-c7d8-e9f0a1b2c3d4", entityRefs: [sid1, hostH2] },
      ] },
      docs: { ...base.docs, [acct]: { ref: acct, kind: "stix", type: "user-account", payload: {}, accountIdentity: accountView() }, [personA]: personDoc(personA, john) },
      labels: { ...base.labels, [hostH]: "ws-recycled", [hostH2]: "ws-john" },
    };
    render(<AppProvider transport={new HarnessTransport(data)}><App /></AppProvider>);
    openPerson();
    expect(within(pane()).getByText("ws-john")).toBeTruthy();
    expect(within(pane()).queryByText("ws-recycled")).toBeNull();
  });

  it("says Hosts used covers the held page of events when the record holds more", () => {
    const event = base.snapshot.events![0]!;
    const data: HarnessSnapshot = {
      ...base,
      snapshot: { ...base.snapshot, eventsTotal: 900, events: [{ ...event, id: "2c3d4e5f-6071-4283-94a5-b6c7d8e9f0a1", entityRefs: [acct, hostX] }] },
      docs: { ...base.docs, [acct]: { ref: acct, kind: "stix", type: "user-account", payload: {}, accountIdentity: accountView() }, [personA]: personDoc(personA, personView) },
      labels: { ...base.labels, [hostX]: "ws-jane" },
    };
    render(<AppProvider transport={new HarnessTransport(data)}><App /></AppProvider>);
    openPerson();
    expect(within(pane()).getByText("named by events that name one of these accounts, among the newest 1 of 900 retrieved events in this investigation")).toBeTruthy();
  });

  it("sends Not this person's… on the account, the person the other end", () => {
    const transport = mount();
    openPerson();
    const row = edge("relationship--o1");
    expect(within(row).queryByText("Withdraw…")).toBeNull();
    fireEvent.click(within(row).getByText("Not this person's…"));
    fireEvent.click(within(within(pane()).getByRole("form", { name: "Not this person's account" })).getByText("Cut"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: acct, requestId: expect.any(String), relation: "distinct", other: { ref: personA } });
  });

  it("joins and splits persons by hint or by ref", () => {
    const transport = mount();
    openPerson();
    const p = pane();
    fireEvent.click(within(p).getByText("Same person as…"));
    const form = within(p).getByRole("form", { name: "Same person as" });
    fill(form, "jane@contoso.example");
    fireEvent.click(within(form).getByText("Join"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: personA, requestId: expect.any(String), relation: "aliases", other: { hint: "jane@contoso.example" } });
    emit(transport, { type: "inspector.identity", ref: personA, requestId: (lastAct(transport) as { requestId: string }).requestId, views: { personIdentity: personView } });
    fireEvent.click(within(p).getByText("Not the same person as…"));
    const split = within(p).getByRole("form", { name: "Not the same person as" });
    fill(split, personB);
    fireEvent.click(within(split).getByText("Split"));
    expect(lastAct(transport)).toEqual({ type: "identity.assert", ref: personA, requestId: expect.any(String), relation: "distinct", other: { ref: personB } });
  });

  it("never invents an empty view for a person read without one", () => {
    mount();
    open(acct);
    fireEvent.click(edge("relationship--o2").querySelector(`[data-ref="${personB}"]`) as HTMLElement);
    expect(within(pane()).getByText("no identity view for this person")).toBeTruthy();
    expect(within(pane()).queryByText("No other person is joined to this one.")).toBeNull();
  });

  it("opens a vendor identity as evidence, with no person's acts", () => {
    mount();
    open(acct);
    fireEvent.click(edge("relationship--o3").querySelector(`[data-ref="${personC}"]`) as HTMLElement);
    expect(within(pane()).queryByText("Same person as…")).toBeNull();
    expect(within(pane()).queryByText("Accounts")).toBeNull();
  });
});
