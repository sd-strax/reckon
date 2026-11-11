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
import type { EvidenceDoc, HostIdentity } from "./viewTypes";

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
function harness(gets: Record<string, (EvidenceDoc | Promise<EvidenceDoc>)[]>, acts: Promise<HostIdentity | null>[] = []) {
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
    }, [Promise.resolve(view("relationship--a-after"))]);
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
    const act = deferred<HostIdentity | null>();
    const { reads, posted, answers } = harness({
      [hostA]: [stale.promise, doc(hostA, view("relationship--after")), doc(hostA, view("relationship--after"))],
    }, [act.promise]);
    const open = reads.answerInspector("inv-1", panel, { kind: "ref", ref: hostA });
    const acting = reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    stale.resolve(doc(hostA, view("relationship--before")));
    act.resolve(null);
    await Promise.all([open, acting]);
    expect(posted.find((m) => m.type === "inspector.identity")).toEqual({ type: "inspector.identity", ref: hostA, requestId: "r1", identity: null });
    expect(answers(hostA)).not.toContain("relationship--before");
    expect(answers(hostA).length).toBeGreaterThan(0);
  });

  it("frees the host the moment its act lands: a next act sent on the outcome is accepted", async () => {
    const reread = deferred<EvidenceDoc>();
    const { reads, posted, actCalls } = harness({ [hostA]: [reread.promise, doc(hostA, view("relationship--split")), doc(hostA, view("relationship--split"))] },
      [Promise.resolve(view("relationship--join")), Promise.resolve(view("relationship--split"))]);
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
    const second = deferred<HostIdentity | null>();
    const after = doc(hostA, view("relationship--after"));
    const { reads, posted, actCalls } = harness({ [hostA]: [reread.promise, after, after, after] },
      [Promise.resolve(view("relationship--one")), second.promise, Promise.resolve(view("relationship--three"))]);
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

  it("tells every other open panel that a host's identity changed, and not the sender", async () => {
    const { reads, posted, elsewhere } = harness({ [hostA]: [doc(hostA, view("relationship--after"))] }, [Promise.resolve(view("relationship--after"))]);
    await reads.assertIdentity("inv-1", panel, { ref: hostA, requestId: "r1", relation: "aliases", other: "ws9" });
    expect(elsewhere).toEqual([{ type: "identity.changed", ref: hostA }]);
    expect(posted.some((m) => m.type === "identity.changed")).toBe(false);
  });

  it("holds one act per host across panels: a second act on the host is refused while the first is in flight", async () => {
    const first = deferred<HostIdentity | null>();
    const { reads, posted, actCalls } = harness({ [hostA]: [doc(hostA, view("relationship--after"))], [hostB]: [doc(hostB, view("relationship--b"))] },
      [first.promise, Promise.resolve(view("relationship--b"))]);
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
