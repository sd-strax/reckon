// @vitest-environment node
// A host's identity view is mutable (adr/0211): every open of a host reads it
// fresh, never from the document cache; a read an identity act overtook is
// read again rather than posted; one act per host is in flight at a time,
// whichever panel sent it.
import { describe, expect, it } from "vitest";
import type * as vscode from "vscode";
import type { DocumentHost } from "./documentHost";
import { EvidenceReads } from "./evidenceReads";
import type { RenderMessage } from "./protocol";
import { BackendRequestError } from "./client/http";
import type { AccountIdentity, EvidenceDoc, HostIdentity, IdentityViews, PersonIdentity } from "./viewTypes";

const hostA = "x-host--00000000-0000-4000-8000-00000000000a";
const hostB = "x-host--00000000-0000-4000-8000-00000000000b";
const panel = { name: "sender" } as unknown as vscode.WebviewPanel;
const otherPanel = { name: "another investigation" } as unknown as vscode.WebviewPanel;
const view = (edgeId: string): HostIdentity => ({
  aliases: [{ edgeId, otherRef: "x-host--00000000-0000-4000-8000-000000000002", kind: "asserted", by: "u-1", at: "2026-01-02T03:04:05Z", withdrawn: false }],
  distinct: [], seenAs: { domains: [], agents: [], ips: [] },
});
const doc = (ref: string, identity: HostIdentity): EvidenceDoc => ({ ref, kind: "stix", type: "x-host", payload: { properties: { hostname: "ws1" } }, identity });

const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
};

/**
 * A document host whose evidence reads are answered per ref, in order, from
 * `gets` (each a value or a promise the test resolves), and whose identity
 * acts answer from `acts`, in order.
 */
function harness(gets: Record<string, (EvidenceDoc | Promise<EvidenceDoc>)[]>, acts: Promise<IdentityViews | null>[] = []) {
  const posted: RenderMessage[] = [];
  const elsewhere: RenderMessage[] = []; // what the other open panel was posted
  const reads: Record<string, number> = {};
  let actCalls = 0;
  const fake = {
    client: {
      evidence: {
        get: async (ref: string) => {
          const i = reads[ref] ?? 0;
          reads[ref] = i + 1;
          const g = gets[ref]?.[i];
          if (!g) throw new Error(`no read ${i} of ${ref}`);
          return g;
        },
        appearances: async () => [],
        events: async () => ({ events: [], total: 0, groups: [], foldedOver: 0 }),
        assertIdentity: async () => { const a = acts[actCalls++]; if (!a) throw new Error("no act"); return a; },
        assertOwner: async () => { const a = acts[actCalls++]; if (!a) throw new Error("no act"); return a; },
      },
    },
    post: async (p: vscode.WebviewPanel, m: RenderMessage) => { (p === panel ? posted : elsewhere).push(m); return true; },
    openPanels: () => [panel, otherPanel],
    sameIdentity: () => () => true,
    log: { debug: () => {} },
  } as unknown as DocumentHost;
  const answers = (ref: string) => posted
    .filter((m): m is Extract<RenderMessage, { type: "inspector.evidence" }> => m.type === "inspector.evidence" && m.ref === ref)
    .map((m) => m.doc.identity?.aliases[0]?.edgeId);
  return { reads: new EvidenceReads(fake), posted, elsewhere, readsOf: (ref: string) => reads[ref] ?? 0, answers, actCalls: () => actCalls };
}

describe("a host's identity in the inspector reads (adr/0211)", () => {
  it("reads a host's identity fresh on every open, never from the document cache", async () => {
    const { reads, readsOf, answers } = harness({ [hostA]: [doc(hostA, view("relationship--old")), doc(hostA, view("relationship--new"))] });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    expect(readsOf(hostA)).toBe(2);
    expect(answers(hostA)).toEqual(["relationship--old", "relationship--new"]);
  });

  it("reads an open again when an act on another host overtakes it: the open still posts, and only the fresh read", async () => {
    const first = deferred<EvidenceDoc>();
    const { reads, answers, readsOf } = harness({
      [hostB]: [first.promise, doc(hostB, view("relationship--b-after"))],
      [hostA]: [doc(hostA, view("relationship--a-after"))],
    }, [Promise.resolve({ identity: view("relationship--a-after") })]);
    const open = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostB });
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    first.resolve(doc(hostB, view("relationship--b-before")));
    await open;
    expect(readsOf(hostB)).toBe(2);
    expect(answers(hostB)).toEqual(["relationship--b-after"]);
    expect(answers(hostA)).toEqual(["relationship--a-after"]);
  });

  it("reads the acted-on host again when the act overtakes its open, and posts the act's outcome under its request id", async () => {
    const stale = deferred<EvidenceDoc>();
    const act = deferred<IdentityViews | null>();
    const { reads, posted, answers } = harness({
      [hostA]: [stale.promise, doc(hostA, view("relationship--after")), doc(hostA, view("relationship--after"))],
    }, [act.promise]);
    const open = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    const acting = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    stale.resolve(doc(hostA, view("relationship--before")));
    act.resolve(null);
    await Promise.all([open, acting]);
    expect(posted.find((m) => m.type === "inspector.identity")).toEqual({ type: "inspector.identity", ref: hostA, requestId: "r1", views: null });
    // Only the act's own read posts: the overtaken open is superseded by it, not read again.
    expect(answers(hostA)).toEqual(["relationship--after"]);
  });

  it("frees the host the moment its act lands: a next act sent on the outcome is accepted", async () => {
    const reread = deferred<EvidenceDoc>();
    const { reads, posted, actCalls } = harness({ [hostA]: [reread.promise, doc(hostA, view("relationship--split")), doc(hostA, view("relationship--split"))] },
      [Promise.resolve({ identity: view("relationship--join") }), Promise.resolve({ identity: view("relationship--split") })]);
    // The join lands; its re-read is still in flight when the split is sent.
    const joining = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "join", relation: "aliases", other: "ws9" });
    await new Promise((r) => setTimeout(r, 0));
    expect(posted.some((m) => m.type === "inspector.identity" && m.requestId === "join")).toBe(true);
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "split", relation: "distinct", other: "ws8" });
    expect(actCalls()).toBe(2);
    expect(posted.filter((m) => m.type === "identity.error")).toEqual([]);
    reread.resolve(doc(hostA, view("relationship--join")));
    await joining;
    expect(posted.filter((m) => m.type === "inspector.error")).toEqual([]);
  });

  it("lets an act's lock go once: its late finally never frees the lock a next act on the host holds", async () => {
    const reread = deferred<EvidenceDoc>();
    const second = deferred<IdentityViews | null>();
    const after = doc(hostA, view("relationship--after"));
    const { reads, posted, actCalls } = harness({ [hostA]: [reread.promise, after, after, after] },
      [Promise.resolve({ identity: view("relationship--one") }), second.promise, Promise.resolve({ identity: view("relationship--three") })]);
    // Act 1 lands and releases; its re-read (and so its finally) is still pending.
    const one = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "one", relation: "aliases", other: "ws9" });
    await new Promise((r) => setTimeout(r, 0));
    // Act 2 takes the lock and stays in flight.
    const two = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "two", relation: "distinct", other: "ws8" });
    expect(actCalls()).toBe(2);
    // Act 1 finishes: its finally runs.
    reread.resolve(after);
    await one;
    // A third act on the host is still refused: act 2 holds the lock.
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "three", relation: "aliases", other: "ws7" });
    expect(actCalls()).toBe(2);
    expect(posted).toContainEqual({ type: "identity.error", ref: hostA, requestId: "three", message: "an act on this host is in flight" });
    second.resolve(null);
    await two;
  });

  it("tells every open panel that a host's identity changed: the sender as the one that acted, after the act's answer", async () => {
    const { reads, posted, elsewhere } = harness({ [hostA]: [doc(hostA, view("relationship--after"))] }, [Promise.resolve({ identity: view("relationship--after") })]);
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    expect(elsewhere).toEqual([{ type: "identity.changed", ref: hostA }]);
    const order = posted.map((m) => m.type).filter((t) => t === "inspector.identity" || t === "identity.changed");
    expect(order).toEqual(["inspector.identity", "identity.changed"]);
    expect(posted).toContainEqual({ type: "identity.changed", ref: hostA, acted: true });
  });

  it("tells no panel of a change when the act was refused", async () => {
    const { reads, posted, elsewhere } = harness({}, [Promise.reject(new Error("refused"))]);
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    expect([...posted, ...elsewhere].some((m) => m.type === "identity.changed")).toBe(false);
  });

  it("holds one act per host across panels: a second act on the host is refused while the first is in flight", async () => {
    const first = deferred<IdentityViews | null>();
    const { reads, posted, actCalls } = harness({ [hostA]: [doc(hostA, view("relationship--after"))], [hostB]: [doc(hostB, view("relationship--b"))] },
      [first.promise, Promise.resolve({ identity: view("relationship--b") })]);
    const acting = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    await reads.assertIdentity("inv-2", panel, { ref: hostA.toUpperCase(), requestId: "r2", relation: "distinct", other: "ws9" });
    expect(posted).toContainEqual({ type: "identity.error", ref: hostA.toUpperCase(), requestId: "r2", message: "an act on this host is in flight" });
    expect(actCalls()).toBe(1);
    // Another host is not held up.
    await reads.assertIdentity("inv-1", panel, { ref: hostB, requestId: "r3", relation: "aliases", other: "ws9" });
    expect(actCalls()).toBe(2);
    first.resolve(null);
    await acting;
    expect(posted.filter((m) => m.type === "inspector.identity").map((m) => (m as { requestId: string }).requestId)).toEqual(["r3", "r1"]);
  });
});

describe("overlapping inspector reads: the latest issued for a target wins", () => {
  const cached = (reads: EvidenceReads, ref: string) => (reads as unknown as { docCache: Map<string, EvidenceDoc> }).docCache.get(ref);
  const rejecting = () => {
    let reject!: (e: Error) => void;
    const promise = new Promise<EvidenceDoc>((_r, j) => { reject = j; });
    return { promise, reject };
  };

  it("posts only the newer of two reads of one host, and lets the older write no cache", async () => {
    const slow = deferred<EvidenceDoc>();
    const { reads, answers } = harness({ [hostA]: [slow.promise, doc(hostA, view("relationship--new"))] });
    const r1 = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    expect(answers(hostA)).toEqual(["relationship--new"]);
    const r1Doc = { ...doc(hostA, view("relationship--old")), payload: { properties: { hostname: "old" } } };
    slow.resolve(r1Doc);
    await r1;
    expect(answers(hostA)).toEqual(["relationship--new"]);
    expect(cached(reads, hostA)?.payload).toEqual({ properties: { hostname: "ws1" } });
  });

  it("drops an older read's late error after the newer read's answer", async () => {
    const slow = rejecting();
    const { reads, posted, answers } = harness({ [hostA]: [slow.promise, doc(hostA, view("relationship--new"))] });
    const r1 = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    slow.reject(new Error("backend unreachable"));
    await r1;
    expect(answers(hostA)).toEqual(["relationship--new"]);
    expect(posted.filter((m) => m.type === "inspector.error")).toEqual([]);
  });

  it("does not let reads of two hosts supersede each other", async () => {
    const slowA = deferred<EvidenceDoc>();
    const { reads, answers } = harness({ [hostA]: [slowA.promise], [hostB]: [doc(hostB, view("relationship--b"))] });
    const a = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostB });
    slowA.resolve(doc(hostA, view("relationship--a")));
    await a;
    expect(answers(hostA)).toEqual(["relationship--a"]);
    expect(answers(hostB)).toEqual(["relationship--b"]);
  });

  it("orders reads per panel: one host read in two panels answers each panel", async () => {
    const slow = deferred<EvidenceDoc>();
    const { reads, answers, elsewhere } = harness({ [hostA]: [slow.promise, doc(hostA, view("relationship--other-panel"))] });
    const mine = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    await reads.answerInspector("inv-2", otherPanel, { kind: "ref", ref: hostA });
    slow.resolve(doc(hostA, view("relationship--mine")));
    await mine;
    expect(answers(hostA)).toEqual(["relationship--mine"]);
    expect(elsewhere.filter((m) => m.type === "inspector.evidence")).toHaveLength(1);
  });
});

describe("an account's and a person's identity in the inspector reads (adr/0324)", () => {
  const acct = "user-account--00000000-0000-4000-8000-0000000000a1";
  const person = "identity--00000000-0000-4000-8000-0000000000b1";
  const vendor = "identity--00000000-0000-4000-8000-0000000000c1";
  const cached = (reads: EvidenceReads, ref: string) => (reads as unknown as { docCache: Map<string, EvidenceDoc> }).docCache.get(ref);
  const accountView = (edgeId: string): AccountIdentity => ({
    aliases: [{ edgeId, otherRef: "user-account--2", kind: "observed", source: "dc", at: "2026-01-02T03:04:05Z", withdrawn: false }],
    distinct: [], seenAs: { logins: [], ids: [], principalNames: [] }, owners: [],
  });
  const personView = (edgeId: string): PersonIdentity => ({
    aliases: [{ edgeId, otherRef: "identity--2", kind: "asserted", by: "u-1", at: "2026-01-02T03:04:05Z", withdrawn: false }], distinct: [], accounts: [],
  });
  const accountDoc = (edgeId: string): EvidenceDoc => ({ ref: acct, kind: "stix", type: "user-account", payload: { properties: { account_login: "jdoe" } }, accountIdentity: accountView(edgeId) });
  const personDoc = (edgeId: string): EvidenceDoc => ({ ref: person, kind: "stix", type: "identity", payload: { properties: { identity_class: "individual" } }, personIdentity: personView(edgeId) });
  const vendorDoc: EvidenceDoc = { ref: vendor, kind: "stix", type: "identity", payload: { properties: { name: "edr vendor" } } };
  const answered = (posted: RenderMessage[], ref: string) => posted
    .filter((m): m is Extract<RenderMessage, { type: "inspector.evidence" }> => m.type === "inspector.evidence" && m.ref === ref)
    .map((m) => (m.doc.accountIdentity ?? m.doc.personIdentity)?.aliases[0]?.edgeId);

  it("reads an account and a person fresh on every open, and caches each without its view", async () => {
    const { reads, readsOf, posted } = harness({ [acct]: [accountDoc("relationship--a1"), accountDoc("relationship--a2")], [person]: [personDoc("relationship--p1"), personDoc("relationship--p2")] });
    for (const ref of [acct, acct, person, person]) await reads.answerInspector("inv-1", panel, { kind: "ref", ref });
    expect(readsOf(acct)).toBe(2);
    expect(readsOf(person)).toBe(2);
    expect(answered(posted, acct)).toEqual(["relationship--a1", "relationship--a2"]);
    expect(answered(posted, person)).toEqual(["relationship--p1", "relationship--p2"]);
    for (const ref of [acct, person]) {
      const held = cached(reads, ref)!;
      expect(held).toBeDefined();
      expect(held.identity).toBeUndefined();
      expect(held.accountIdentity).toBeUndefined();
      expect(held.personIdentity).toBeUndefined();
    }
  });

  it("serves a vendor identity from the cache once its doc says it is no person", async () => {
    const { reads, readsOf } = harness({ [vendor]: [vendorDoc] });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: vendor });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: vendor });
    expect(readsOf(vendor)).toBe(1);
  });

  it("posts only the newer of two reads of one account", async () => {
    const slow = deferred<EvidenceDoc>();
    const { reads, posted } = harness({ [acct]: [slow.promise, accountDoc("relationship--new")] });
    const r1 = reads.answerInspector("inv-1", panel, { kind: "ref", ref: acct });
    await reads.answerInspector("inv-1", panel, { kind: "ref", ref: acct });
    slow.resolve(accountDoc("relationship--old"));
    await r1;
    expect(answered(posted, acct)).toEqual(["relationship--new"]);
  });

  it("reads a person again when an act on an account overtakes its open", async () => {
    const first = deferred<EvidenceDoc>();
    const { reads, posted, readsOf } = harness({ [person]: [first.promise, personDoc("relationship--after")], [acct]: [accountDoc("relationship--a")] },
      [Promise.resolve({ accountIdentity: accountView("relationship--a") })]);
    const open = reads.answerInspector("inv-1", panel, { kind: "ref", ref: person });
    await reads.assertOwner("inv-1", panel, { ref: acct, requestId: "o1", person: { ref: person } });
    first.resolve(personDoc("relationship--before"));
    await open;
    expect(readsOf(person)).toBe(2);
    expect(answered(posted, person)).toEqual(["relationship--after"]);
  });

  it("holds one act per entity across kinds of act and panels: an owner act beside a join on the account is refused", async () => {
    const first = deferred<IdentityViews | null>();
    const { reads, posted, actCalls } = harness({ [acct]: [accountDoc("relationship--a")], [person]: [personDoc("relationship--p")] },
      [first.promise, Promise.resolve({ personIdentity: personView("relationship--p") })]);
    const joining = reads.assertIdentity("inv-1", panel, { ref: acct, requestId: "j1", relation: "aliases", other: { hint: "jdoe" } });
    await reads.assertOwner("inv-2", otherPanel, { ref: acct, requestId: "o1", person: { hint: "jdoe@contoso.example" } });
    expect(actCalls()).toBe(1);
    // A person is not held up.
    await reads.assertIdentity("inv-1", panel, { ref: person, requestId: "p1", relation: "distinct", other: { ref: "identity--2" } });
    expect(actCalls()).toBe(2);
    first.resolve(null);
    await joining;
    // The answer is under the act's request id; null keeps the last view in the webview.
    expect(posted).toContainEqual({ type: "inspector.identity", ref: acct, requestId: "j1", views: null });
  });

  it("refuses a second act on the account with the in-flight message", async () => {
    const first = deferred<IdentityViews | null>();
    const { reads, elsewhere } = harness({ [acct]: [accountDoc("relationship--a")] }, [first.promise]);
    const joining = reads.assertIdentity("inv-1", panel, { ref: acct, requestId: "j1", relation: "aliases", other: { hint: "jdoe" } });
    await reads.assertOwner("inv-2", otherPanel, { ref: acct, requestId: "o1", person: { hint: "jdoe@contoso.example" } });
    expect(elsewhere).toContainEqual({ type: "identity.error", ref: acct, requestId: "o1", message: "an act on this account is in flight" });
    first.resolve(null);
    await joining;
  });

  it("says a refusal in the server's words, with the candidates a hint named", async () => {
    const conflict = new BackendRequestError("POST /api/entities/x/aliases → 409: \"j.doe\" names 2 accounts; name one by other_ref", 409, {
      error: "\"j.doe\" names 2 accounts; name one by other_ref",
      accounts: [{ ref: "user-account--2", account_type: "windows-domain", account_login: "contoso\\j.doe" }, { ref: "user-account--3", account_type: "cloud", account_login: "j.doe@contoso.example" }],
    });
    const observed = new BackendRequestError("POST /api/identity/e/withdraw → 409: a directory's record; assert that the account is not this person's instead", 409, {
      error: "a directory's record; assert that the account is not this person's instead",
    });
    const { reads, posted } = harness({}, [Promise.reject(conflict), Promise.reject(observed)]);
    await reads.assertIdentity("inv-1", panel, { ref: acct, requestId: "j1", relation: "aliases", other: { hint: "j.doe" } });
    expect(posted).toContainEqual({
      type: "identity.error", ref: acct, requestId: "j1", message: "\"j.doe\" names 2 accounts; name one by other_ref",
      accounts: [{ ref: "user-account--2", accountType: "windows-domain", accountLogin: "contoso\\j.doe" }, { ref: "user-account--3", accountType: "cloud", accountLogin: "j.doe@contoso.example" }],
    });
    await reads.assertOwner("inv-1", panel, { ref: acct, requestId: "o1", person: { ref: person } });
    expect(posted).toContainEqual({ type: "identity.error", ref: acct, requestId: "o1", message: "a directory's record; assert that the account is not this person's instead" });
  });

  it("tells every other open panel that an account's identity changed", async () => {
    const { reads, elsewhere } = harness({ [acct]: [accountDoc("relationship--a")] }, [Promise.resolve({ accountIdentity: accountView("relationship--a") })]);
    await reads.assertOwner("inv-1", panel, { ref: acct, requestId: "o1", person: { hint: "jdoe@contoso.example" } });
    expect(elsewhere).toEqual([{ type: "identity.changed", ref: acct }]);
  });
});
