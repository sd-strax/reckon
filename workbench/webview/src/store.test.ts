import { describe, expect, it } from "vitest";
import { identityActPending, identityViewUnavailable, initialState, reducer, type State } from "./store";
import type { InvestigationSnapshot, RenderMessage } from "../../src/protocol";

function snap(seq: number, title = "T"): InvestigationSnapshot {
  return {
    seq,
    investigation: { id: "inv-1", title, state: "ACTIVE", lastEventSequence: seq },
    hypotheses: [], actions: [], pins: [], comms: [], dependencies: [], thread: [], turns: [],
    capabilities: null, enablement: null, derived: null,
  };
}

function run(msgs: RenderMessage[], from: State = initialState): State {
  return msgs.reduce((s, msg) => reducer(s, { type: "msg", msg }), from);
}

describe("reducer: sequence discipline", () => {
  it("applies a snapshot and patches at or after its seq, drops older ones", () => {
    let s = run([{ type: "state.snapshot", seq: 5, snapshot: snap(5) }]);
    expect(s.seq).toBe(5);
    expect(s.loading).toBe(false);
    s = run([{ type: "state.patch", seq: 4, patch: { pins: [{ interpretationId: "p", finding: "stale", inputRefs: [], actor: "HUMAN", pinnedAt: "", superseded: false }] } }], s);
    expect(s.snap?.pins).toEqual([]); // stale patch dropped
    s = run([{ type: "state.patch", seq: 6, patch: { pins: [{ interpretationId: "p", finding: "fresh", inputRefs: [], actor: "HUMAN", pinnedAt: "", superseded: false }] } }], s);
    expect(s.seq).toBe(6);
    expect(s.snap?.pins[0]!.finding).toBe("fresh");
  });
  it("drops a snapshot older than the state it holds, for the same investigation only", () => {
    let s = run([
      { type: "state.snapshot", seq: 7, snapshot: snap(7, "newer") },
      { type: "loading" },
      { type: "state.snapshot", seq: 6, snapshot: snap(6, "older") },
    ]);
    expect(s.seq).toBe(7);
    expect(s.title).toBe("newer");
    expect(s.loading).toBe(false);
    const other = snap(2, "another case");
    other.investigation = { ...other.investigation, id: "inv-2" };
    s = run([{ type: "state.snapshot", seq: 2, snapshot: other }], s);
    expect(s.seq).toBe(2);
    expect(s.title).toBe("another case");
  });
  it("ignores a patch before any snapshot", () => {
    const s = run([{ type: "state.patch", seq: 1, patch: { pins: [] } }]);
    expect(s.snap).toBeNull();
  });
});

describe("reducer: decisions in flight", () => {
  it("marks an action in flight when a decision is sent, until the host settles it", () => {
    let s = reducer(initialState, { type: "action.sent", actionId: "act-1" });
    s = reducer(s, { type: "action.sent", actionId: "act-2" });
    expect(s.actionsInFlight).toEqual({ "act-1": true, "act-2": true });
    s = run([{ type: "action.settled", actionId: "act-1" }], s);
    expect(s.actionsInFlight).toEqual({ "act-2": true });
  });
});

describe("reducer: the live conversation", () => {
  it("streams text into one segment, settles tool rows FIFO, keeps a failed turn", () => {
    let s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "turn.user", text: "hi" },
      { type: "turn.start" },
      { type: "turn.text", delta: "Hel" },
      { type: "turn.text", delta: "lo" },
      { type: "turn.tool", name: "a", input: { x: 1 } },
      { type: "turn.tool", name: "b", input: {} },
      { type: "turn.toolResult", name: "a", isError: false, coverage: "COMPLETE", events: 2, refs: ["r1"], callId: "c-1" },
      { type: "turn.text", delta: "after" },
    ]);
    expect(s.streaming).toBe(true);
    const turn = s.live[1];
    if (turn!.kind !== "assistant") throw new Error("expected a turn");
    expect(turn!.segments.map((x) => x.kind)).toEqual(["text", "tool", "tool", "text"]);
    const first = turn!.segments[1];
    if (first!.kind !== "tool") throw new Error();
    expect(first!.row.result?.detail).toBe("COMPLETE · 2 event(s)");
    expect(first!.row.result?.callId).toBe("c-1");
    const second = turn!.segments[2];
    if (second!.kind !== "tool") throw new Error();
    expect(second!.row.result).toBeUndefined();
    // A thread re-post while streaming must not clear the live view.
    s = run([{ type: "state.patch", seq: 2, patch: { thread: [] } }], s);
    expect(s.live.length).toBe(2);
    // A failed turn is preserved across the next thread render.
    s = run([{ type: "turn.error", message: "boom" }, { type: "turn.end" }, { type: "state.patch", seq: 3, patch: { thread: [] } }], s);
    expect(s.preserveLive).toBe(true);
    expect(s.live.some((i) => i.kind === "error")).toBe(true);
    // A new turn supersedes the preserved view.
    s = run([{ type: "turn.user", text: "again" }], s);
    expect(s.preserveLive).toBe(false);
  });
  it("shows a context compaction notice in the live turn", () => {
    const s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "turn.user", text: "hi" }, { type: "turn.start" },
      { type: "turn.notice", text: "Context condensed to fit the model's window: 2 earlier turns reduced to their text." },
    ]);
    const turn = s.live[1];
    if (turn!.kind !== "assistant") throw new Error();
    expect(turn!.segments).toEqual([
      { kind: "notice", text: "Context condensed to fit the model's window: 2 earlier turns reduced to their text." },
    ]);
  });
  it("clears the live view once the committed thread re-posts after an idle turn", () => {
    let s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "turn.user", text: "hi" }, { type: "turn.start" }, { type: "turn.text", delta: "x" },
      { type: "turn.end", usage: { input: 10, output: 5, cacheRead: 0 }, rounds: 1 },
    ]);
    expect(s.streaming).toBe(false);
    const turn = s.live[1];
    if (turn!.kind !== "assistant") throw new Error();
    expect(turn!.segments[turn!.segments.length - 1]).toEqual({ kind: "usage", text: "1 tool round · 10 in / 5 out" });
    s = run([{ type: "state.patch", seq: 2, patch: { thread: [] } }], s);
    expect(s.live).toEqual([]);
  });
  it("renders an aside collapsed and marks it done", () => {
    const s = run([{ type: "state.snapshot", seq: 1, snapshot: snap(1) }, { type: "aside.start", label: "⚡ decide · H1" }, { type: "turn.end" }]);
    const t = s.live[0];
    if (t!.kind !== "assistant") throw new Error();
    expect(t!.aside).toEqual({ label: "⚡ decide · H1", done: true });
  });
});

describe("reducer: the library", () => {
  it("holds the browse lists and one search's hits, and clears the search", () => {
    const item = { kind: "sop" as const, ref: "sop-1", title: "S" };
    let s = run([{ type: "library", sops: [item], cases: [] }]);
    expect(s.library.loaded).toBe(true);
    s = reducer(s, { type: "library.searching", query: "rdp" });
    s = run([{ type: "library.results", query: "rdp", sops: [{ ...item, relevance: "related" }], cases: [] }], s);
    expect(s.library.results?.query).toBe("rdp");
    s = reducer(s, { type: "library.clear" });
    expect(s.library.results).toBeNull();
    expect(s.library.sops).toEqual([item]);
  });

  it("shows only the answer to the search asked last, and none after it was dismissed (adr/0107)", () => {
    let s = reducer(initialState, { type: "library.searching", query: "old" });
    s = reducer(s, { type: "library.searching", query: "new" });
    s = run([{ type: "library.results", query: "old", sops: [], cases: [] }], s);
    expect(s.library.results).toBeNull(); // an older search's answer, landing late
    s = reducer(s, { type: "library.clear" }); // dismissed while "new" is in flight
    s = run([{ type: "library.results", query: "new", sops: [], cases: [] }], s);
    expect(s.library.results).toBeNull();
  });

  it("keeps a failed procedures read apart from an empty library, and the list read before it", () => {
    const item = { kind: "sop" as const, ref: "sop-1", title: "S" };
    let s = run([{ type: "library", sops: [item], cases: [] }]);
    s = run([{ type: "library", sops: [], cases: [], sopsError: "HTTP 500" }], s);
    expect(s.library.sopsError).toBe("HTTP 500");
    expect(s.library.sops).toEqual([item]);
    s = run([{ type: "library", sops: [], cases: [] }], s);
    expect(s.library.sopsError).toBeUndefined();
    expect(s.library.sops).toEqual([]); // a successful empty read is empty
  });
});
describe("reducer: rename", () => {
  it("updates the title in place and bumps a tree-initiated request", () => {
    let s = run([{ type: "state.snapshot", seq: 1, snapshot: snap(1, "Old") }, { type: "renamed", title: "New" }]);
    expect(s.title).toBe("New");
    expect(s.snap?.investigation.title).toBe("New");
    s = run([{ type: "rename.begin", title: "New" }, { type: "rename.begin", title: "New" }], s);
    expect(s.renameRequest?.nonce).toBe(2);
  });
});

describe("reducer: a host's identity acts (adr/0211)", () => {
  const ref = "x-host--1";
  const identity = { aliases: [], distinct: [], seenAs: { domains: [], agents: [], ips: [] } };
  const prior = { ...identity, aliases: [{ edgeId: "relationship--1", otherRef: "x-host--2", kind: "observed" as const, source: "edr", at: "2026-01-02T03:04:05Z", withdrawn: false }] };
  it("settles each act by its own request id: in flight, refused, landed", () => {
    let s = reducer(initialState, { type: "identity.sent", ref, requestId: "r1" });
    expect(s.identityActs.r1).toEqual({ ref, state: "pending" });
    expect(identityActPending(s, ref)).toBe(true);
    expect(identityActPending(s, "x-host--other")).toBe(false);
    s = run([{ type: "identity.error", ref, requestId: "r1", message: "the signed-in person changed" }], s);
    expect(s.identityActs.r1).toEqual({ ref, state: "failed", message: "the signed-in person changed" });
    expect(identityActPending(s, ref)).toBe(false);
    s = reducer(s, { type: "identity.sent", ref, requestId: "r2" });
    s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] }, { type: "inspector.identity", ref, requestId: "r2", views: { identity } }], s);
    expect(s.identityActs.r2).toEqual({ ref, state: "landed", message: undefined });
    expect(s.identityActs.r1!.state).toBe("failed"); // the other act is untouched
    expect(s.docs[ref]!.identity).toEqual(identity);
  });
  it("keeps a host's last view when a read answers it without one, marking it stale though it was not", () => {
    let s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] }]);
    expect(s.staleEntities[ref]).toBeUndefined();
    s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: { fresh: true } }, appearances: [] }], s);
    expect(s.docs[ref]!.identity).toEqual(prior);
    expect(s.docs[ref]!.payload).toEqual({ fresh: true });
    expect(s.staleEntities[ref]).toBe(true);
  });
  it("marks every other host held stale after an act, until it is read again", () => {
    const other = "x-host--2";
    let s = run([
      { type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] },
      { type: "inspector.evidence", ref: other, doc: { ref: other, kind: "stix", type: "x-host", payload: {}, identity }, appearances: [] },
      { type: "inspector.evidence", ref: "ipv4-addr--1", doc: { ref: "ipv4-addr--1", kind: "stix", type: "ipv4-addr", payload: {} }, appearances: [] },
    ]);
    s = reducer(s, { type: "identity.sent", ref, requestId: "r4" });
    s = run([{ type: "inspector.identity", ref, requestId: "r4", views: { identity } }], s);
    expect(s.staleEntities).toEqual({ [other]: true });
    s = run([{ type: "inspector.evidence", ref: other, doc: { ref: other, kind: "stix", type: "x-host", payload: {}, identity }, appearances: [] }], s);
    expect(s.staleEntities).toEqual({});
  });
  it("a load marks every held host stale, so a pane showing one reads it afresh", () => {
    let s = run([
      { type: "state.snapshot", seq: 1, snapshot: snap(1) },
      { type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] },
      { type: "inspector.evidence", ref: "ipv4-addr--1", doc: { ref: "ipv4-addr--1", kind: "stix", type: "ipv4-addr", payload: {} }, appearances: [] },
    ]);
    expect(s.staleEntities).toEqual({});
    s = run([{ type: "state.snapshot", seq: 1, snapshot: snap(1) }], s);
    expect(s.staleEntities).toEqual({ [ref]: true });
  });
  it("counts every applied load in reloadSeq, and not a snapshot dropped as older", () => {
    let s = run([{ type: "state.snapshot", seq: 5, snapshot: snap(5) }]);
    expect(s.reloadSeq).toBe(1);
    s = run([{ type: "state.snapshot", seq: 5, snapshot: snap(5) }], s);
    expect(s.reloadSeq).toBe(2);
    s = run([{ type: "state.snapshot", seq: 4, snapshot: snap(4) }], s);
    expect(s.reloadSeq).toBe(2);
  });
  it("identity.changed marks all hosts stale, the acted one included", () => {
    const other = "x-host--2";
    let s = run([
      { type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] },
      { type: "inspector.evidence", ref: other, doc: { ref: other, kind: "stix", type: "x-host", payload: {}, identity }, appearances: [] },
      { type: "inspector.evidence", ref: "ipv4-addr--1", doc: { ref: "ipv4-addr--1", kind: "stix", type: "ipv4-addr", payload: {} }, appearances: [] },
    ]);
    s = run([{ type: "identity.changed", ref }], s);
    expect(s.staleEntities).toEqual({ [ref]: true, [other]: true });
  });
  it("keeps a stale host marked when its re-read reuses the held view, and clears it on a fresh view", () => {
    let s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] }, { type: "identity.changed", ref }]);
    s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {} }, appearances: [] }], s);
    expect(s.docs[ref]!.identity).toEqual(prior);
    expect(s.staleEntities[ref]).toBe(true);
    s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity }, appearances: [] }], s);
    expect(s.staleEntities[ref]).toBeUndefined();
  });
  it("forgets a closed form's act: at once when settled, once it settles when in flight", () => {
    let s = reducer(initialState, { type: "identity.sent", ref, requestId: "r5" });
    s = run([{ type: "identity.error", ref, requestId: "r5", message: "no" }], s);
    s = reducer(s, { type: "identity.forget", requestId: "r5" });
    expect(s.identityActs.r5).toBeUndefined();
    s = reducer(s, { type: "identity.sent", ref, requestId: "r6" });
    s = reducer(s, { type: "identity.forget", requestId: "r6" });
    expect(identityActPending(s, ref)).toBe(true); // still counts against the host
    s = run([{ type: "inspector.identity", ref, requestId: "r6", views: { identity } }], s);
    expect(s.identityActs.r6).toBeUndefined();
    expect(identityActPending(s, ref)).toBe(false);
  });
  it("keeps the last view when the act's view could not be read, and says the act was recorded", () => {
    let s = run([{ type: "inspector.evidence", ref, doc: { ref, kind: "stix", type: "x-host", payload: {}, identity: prior }, appearances: [] }]);
    s = reducer(s, { type: "identity.sent", ref, requestId: "r3" });
    s = run([{ type: "inspector.identity", ref, requestId: "r3", views: null }], s);
    expect(s.identityActs.r3).toEqual({ ref, state: "landed", message: identityViewUnavailable });
    expect(s.docs[ref]!.identity).toEqual(prior);
  });
});

describe("reducer: an account's and a person's identity (adr/0324)", () => {
  const acct = "user-account--1";
  const person = "identity--1";
  const vendor = "identity--2";
  const edge = { edgeId: "relationship--1", otherRef: "user-account--2", kind: "observed" as const, source: "dc", at: "2026-01-02T03:04:05Z", withdrawn: false };
  const accountView = { aliases: [edge], distinct: [], seenAs: { logins: ["jdoe"], ids: [], principalNames: [] }, owners: [] };
  const personView = { aliases: [], distinct: [], accounts: [{ accountRef: acct, edgeId: "relationship--o", kind: "observed" as const, source: "idp", at: "2026-01-02T03:04:05Z", withdrawn: false, cut: false }] };
  const person$ = { type: "identity", payload: { properties: { identity_class: "individual" } } };
  const docs: RenderMessage[] = [
    { type: "inspector.evidence", ref: acct, doc: { ref: acct, kind: "stix", type: "user-account", payload: {}, accountIdentity: accountView }, appearances: [] },
    { type: "inspector.evidence", ref: person, doc: { ref: person, kind: "stix", ...person$, personIdentity: personView }, appearances: [] },
    { type: "inspector.evidence", ref: vendor, doc: { ref: vendor, kind: "stix", type: "identity", payload: { properties: { name: "edr vendor" } } }, appearances: [] },
  ];

  it("keeps an account's and a person's last view when a read answers without it, marking each stale", () => {
    let s = run(docs);
    expect(s.staleEntities).toEqual({});
    s = run([
      { type: "inspector.evidence", ref: acct, doc: { ref: acct, kind: "stix", type: "user-account", payload: { fresh: true } }, appearances: [] },
      { type: "inspector.evidence", ref: person, doc: { ref: person, kind: "stix", ...person$ }, appearances: [] },
    ], s);
    expect(s.docs[acct]!.accountIdentity).toEqual(accountView);
    expect(s.docs[acct]!.payload).toEqual({ fresh: true });
    expect(s.docs[person]!.personIdentity).toEqual(personView);
    expect(s.staleEntities).toEqual({ [acct]: true, [person]: true });
    // A fresh view clears the mark.
    s = run([{ type: "inspector.evidence", ref: acct, doc: { ref: acct, kind: "stix", type: "user-account", payload: {}, accountIdentity: accountView }, appearances: [] }], s);
    expect(s.staleEntities).toEqual({ [person]: true });
  });

  it("identity.changed and a load mark accounts and persons stale, never a vendor identity", () => {
    let s = run([...docs, { type: "identity.changed", ref: "x-host--9" }]);
    expect(s.staleEntities).toEqual({ [acct]: true, [person]: true });
    s = run([...docs, { type: "state.snapshot", seq: 1, snapshot: snap(1) }]);
    expect(s.staleEntities).toEqual({ [acct]: true, [person]: true });
  });

  it("writes an act's view in the field of its kind and marks every other entity held stale", () => {
    let s = run(docs);
    const after = { ...accountView, owners: [{ personRef: person, accountRef: acct, edgeId: "relationship--o", kind: "asserted" as const, by: "u-1", at: "2026-01-02T03:04:05Z", withdrawn: false, cut: false }] };
    s = reducer(s, { type: "identity.sent", ref: acct, requestId: "a1" });
    s = run([{ type: "inspector.identity", ref: acct, requestId: "a1", views: { accountIdentity: after } }], s);
    expect(s.docs[acct]!.accountIdentity).toEqual(after);
    expect(s.docs[acct]!.identity).toBeUndefined();
    expect(s.staleEntities).toEqual({ [person]: true });
  });

  it("keeps the last view and marks it stale when an act's view could not be read: recorded, never an empty view", () => {
    let s = run(docs);
    s = reducer(s, { type: "identity.sent", ref: acct, requestId: "a2" });
    s = run([{ type: "inspector.identity", ref: acct, requestId: "a2", views: null }], s);
    expect(s.identityActs.a2).toEqual({ ref: acct, state: "landed", message: identityViewUnavailable });
    expect(s.docs[acct]!.accountIdentity).toEqual(accountView);
    expect(s.staleEntities[acct]).toBe(true);
  });

  it("holds a refusal's candidates on the act, for its form to list", () => {
    let s = reducer(initialState, { type: "identity.sent", ref: acct, requestId: "a3" });
    s = run([{ type: "identity.error", ref: acct, requestId: "a3", message: "names 2 accounts", accounts: [{ ref: "user-account--2", accountType: "cloud", accountLogin: "j@x" }] }], s);
    expect(s.identityActs.a3).toEqual({ ref: acct, state: "failed", message: "names 2 accounts", accounts: [{ ref: "user-account--2", accountType: "cloud", accountLogin: "j@x" }] });
  });
});

describe("reducer: an identity change re-reads as a load does (adr/0211, adr/0324)", () => {
  const a = "user-account--1";
  const h = "x-host--1";
  const held: RenderMessage[] = [
    { type: "inspector.evidence", ref: a, doc: { ref: a, kind: "stix", type: "user-account", payload: {}, accountIdentity: { aliases: [], distinct: [], seenAs: { logins: [], ids: [], principalNames: [] }, owners: [] } }, appearances: [] },
    { type: "inspector.evidence", ref: h, doc: { ref: h, kind: "stix", type: "x-host", payload: {}, identity: { aliases: [], distinct: [], seenAs: { domains: [], agents: [], ips: [] } } }, appearances: [] },
  ];
  it("bumps the change on every identity.changed, stale or not, and on nothing else", () => {
    let s = run(held);
    expect(s.identityChange.seq).toBe(0);
    s = run([{ type: "identity.changed", ref: h }, { type: "identity.changed", ref: h }], s);
    expect(s.identityChange).toEqual({ seq: 2, except: undefined });
    s = run([{ type: "inspector.identity", ref: h, requestId: "r", views: null }, { type: "identity.error", ref: h, requestId: "q", message: "no" }], s);
    expect(s.identityChange.seq).toBe(2);
  });
  it("names the acted entity in the panel that acted, and keeps its fresh view unmarked", () => {
    let s = run(held);
    s = run([{ type: "identity.changed", ref: a, acted: true }], s);
    expect(s.identityChange).toEqual({ seq: 1, except: a });
    expect(s.staleEntities).toEqual({ [h]: true });
  });
});
