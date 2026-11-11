// The seed names its kind (adr/0324): the draft defaults it from the token's
// shape as the server does, the analyst switches host / account / person,
// an address and a hash stay what they are; a person the record does not
// know is said in the server's words with no "Start anyway"; an unknown
// account lists the accounts the record holds under the name.
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport } from "../../../harness/transport";
import { defaultEntityKind, effectiveEntityKind, entityKindChoosable } from "../derive/draft";

afterEach(cleanup);

function mount() {
  const transport = new HarnessTransport(null);
  render(<AppProvider transport={transport}><App /></AppProvider>);
  // From here the answers are the test's: the harness would leave the draft on a submit.
  transport.send = (i) => { transport.intents.push(i); };
  return transport;
}
const type = (value: string) => fireEvent.change(document.querySelector("#draftInput")!, { target: { value } });
const lastSeed = (t: HarnessTransport) => t.intents.filter((i) => i.type === "seed.submit").pop();
const interp = () => document.querySelector(".draftInterp") as HTMLElement;

describe("the seed's entity kind (adr/0324)", () => {
  it("defaults from the token's shape as the server does", () => {
    expect(defaultEntityKind("CONTOSO\\jdoe")).toBe("account");
    expect(defaultEntityKind("jdoe@contoso.example")).toBe("account");
    expect(defaultEntityKind("WIN-FILE01")).toBe("host");
    expect(entityKindChoosable("10.0.0.7")).toBe(false);
    expect(entityKindChoosable("d41d8cd98f00b204e9800998ecf8427e")).toBe(false);
    expect(effectiveEntityKind("10.0.0.7", "person")).toBeUndefined();
    expect(effectiveEntityKind("jdoe", "person")).toBe("person");
  });

  it("reads the interpretation with the kind, and sends it", () => {
    const transport = mount();
    type("CONTOSO\\jdoe");
    expect(interp().textContent).toContain("→ investigate account CONTOSO\\jdoe");
    fireEvent.click(screen.getByText("Start investigation"));
    expect(lastSeed(transport)).toEqual({ type: "seed.submit", value: "CONTOSO\\jdoe", kind: "entity", entityKind: "account" });
  });

  it("lets the analyst switch the kind before submitting", () => {
    const transport = mount();
    type("jdoe");
    expect(interp().textContent).toContain("→ investigate host jdoe");
    fireEvent.click(within(interp()).getByText("person"));
    expect(interp().textContent).toContain("→ investigate person jdoe");
    fireEvent.click(screen.getByText("Start investigation"));
    expect(lastSeed(transport)).toEqual({ type: "seed.submit", value: "jdoe", kind: "entity", entityKind: "person" });
  });

  it("offers no kind for an address or a hash, and sends none", () => {
    const transport = mount();
    type("10.0.0.7");
    expect(interp().textContent).toContain("→ investigate IP 10.0.0.7");
    expect(within(interp()).queryByText("account")).toBeNull();
    fireEvent.click(screen.getByText("Start investigation"));
    expect(lastSeed(transport)).toEqual({ type: "seed.submit", value: "10.0.0.7", kind: "entity" });
  });

  it("says a person the record does not know in the server's words, with no Start anyway", () => {
    const transport = mount();
    type("jane");
    fireEvent.click(within(interp()).getByText("person"));
    fireEvent.click(screen.getByText("Start investigation"));
    act(() => transport.emit({ type: "draft.error", message: "resolve seed: no person known as \"jane\": a person is known from a directory's record of them", unknownEntity: false }));
    expect(screen.getByText(/no person known as "jane"/)).toBeTruthy();
    expect(screen.queryByText("Start anyway")).toBeNull();
  });

  it("lists the accounts an unknown account's name holds: a click shows the login and seeds the listed account by its ref", () => {
    const transport = mount();
    type("jdoe");
    fireEvent.click(within(interp()).getByText("account"));
    fireEvent.click(screen.getByText("Start investigation"));
    act(() => transport.emit({
      type: "draft.error", message: "no account \"jdoe\" of that kind in the record; the record has 1 account(s) under that name — seed on one, or start anyway", unknownEntity: true,
      knownAccounts: [{ ref: "user-account--1", accountType: "windows-domain", accountLogin: "CONTOSO\\jdoe" }],
    }));
    expect(screen.getByText("Start anyway")).toBeTruthy();
    const before = transport.intents.length;
    fireEvent.click(screen.getByText("CONTOSO\\jdoe · windows-domain"));
    expect((document.querySelector("#draftInput") as HTMLTextAreaElement).value).toBe("CONTOSO\\jdoe");
    expect(interp().textContent).toContain("→ investigate account CONTOSO\\jdoe");
    expect(transport.intents.length).toBe(before);
    fireEvent.click(screen.getByText("Start investigation"));
    expect(lastSeed(transport)).toEqual({ type: "seed.submit", value: "CONTOSO\\jdoe", kind: "entity", accountRef: "user-account--1" });
  });

  it("drops the picked account's ref once the analyst edits the box", () => {
    const transport = mount();
    type("root");
    fireEvent.click(screen.getByText("Start investigation"));
    act(() => transport.emit({
      type: "draft.error", message: "no account \"root\" of that kind in the record", unknownEntity: true,
      knownAccounts: [{ ref: "user-account--7", accountType: "unix", accountLogin: "root" }],
    }));
    fireEvent.click(screen.getByText("root · unix"));
    type("root2");
    fireEvent.click(screen.getByText("Start investigation"));
    const sent = lastSeed(transport) as Record<string, unknown>;
    expect(sent.accountRef).toBeUndefined();
    expect(sent).toEqual({ type: "seed.submit", value: "root2", kind: "entity", entityKind: "host" });
  });
});
