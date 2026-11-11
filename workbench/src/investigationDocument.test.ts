// @vitest-environment node
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTransport } from "./agentTransport";
import { BackendRequestError, type ActionRow, type BackendClient, type InvestigationDetail } from "./backend";
import { InvestigationDocuments } from "./investigationDocument";
import type { RenderMessage } from "./protocol";
import { createdPanels, fakeLog, resetVscodeFake } from "./test/vscode";

const detail = (seq = 3): InvestigationDetail =>
  ({ id: "inv-1", title: "RDP from host-a", state: "ACTIVE", lastEventSequence: seq }) as InvestigationDetail;

/**
 * A backend whose named reads the test sets; every other read answers an
 * empty list, which each optional layer of the document renders as absent.
 */
function backend(over: Record<string, (...args: never[]) => unknown> = {}) {
  const calls: string[] = [];
  const named: Record<string, (...args: never[]) => unknown> = {
    incompatibilityReason: () => null,
    "investigations.get": async () => detail(),
    "capabilities.enablement": async () => null,
    "cases.queue": async () => null,
    "evidence.events": async () => ({ events: [], total: 0 }),
    ...over,
  };
  const call = (name: string) => (...args: never[]) => {
    calls.push(name);
    return (named[name] ?? (async () => []))(...args);
  };
  // The connection-level calls are on the client itself; every read is named
  // by domain and method, as the client exposes them.
  const connection = new Set(["status", "incompatibilityReason", "me", "authConfig"]);
  const client = new Proxy({}, {
    get: (_t, prop: string) => connection.has(prop)
      ? call(prop)
      : new Proxy({}, { get: (_d, method: string) => call(`${prop}.${method}`) }),
  }) as BackendClient;
  return { client, calls };
}

const transport = {
  closeSession: vi.fn(async () => undefined),
  cancel: vi.fn(async () => undefined),
  summarizeConcluded: vi.fn(async () => undefined),
  proposeSOP: vi.fn(async () => ({ warranted: true, title: "T", body: "B", tags: [], recommendation: "", rationale: "" })),
  draftNarrative: vi.fn(async () => ({ revision: 1, chars: 10 })),
} as unknown as AgentTransport;

function documents(client: BackendClient) {
  const onLoaded = vi.fn();
  const docs = new InvestigationDocuments(vscode.Uri.file("/ext"), client, fakeLog(), transport, async () => null, onLoaded);
  return { docs, onLoaded };
}

const posted = (i = 0) => createdPanels()[i]!.webview.posted as RenderMessage[];
const types = (i = 0) => posted(i).map((m) => m.type);

beforeEach(() => {
  resetVscodeFake();
  vi.clearAllMocks();
});
afterEach(() => vi.useRealTimers());

describe("InvestigationDocuments: opening", () => {
  it("opens a scripted, locked-down panel and loads the record when the page is ready", async () => {
    const { docs, onLoaded } = documents(backend().client);
    docs.show("inv-1", "RDP from host-a");

    const panel = createdPanels()[0];
    expect(panel!.title).toBe("⚖ RDP from host-a");
    expect(panel!.webview.options.enableScripts).toBe(true);
    expect(panel!.webview.html).toContain("default-src 'none'");
    expect(panel!.webview.html).not.toContain("unsafe-inline");
    expect(panel!.webview.posted).toEqual([]); // nothing until the page asks

    panel!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(types()).toContain("state.snapshot"));

    expect(types()[0]).toBe("loading");
    const snap = posted().find((m) => m.type === "state.snapshot") as Extract<RenderMessage, { type: "state.snapshot" }>;
    expect(snap.seq).toBe(3);
    expect(snap.snapshot.investigation.title).toBe("RDP from host-a");
    expect(onLoaded).toHaveBeenCalledWith("inv-1", 3);
  });

  it("reveals the open panel instead of opening a second", () => {
    const { docs } = documents(backend().client);
    docs.show("inv-1");
    docs.show("inv-1");
    expect(createdPanels()).toHaveLength(1);
    expect(createdPanels()[0]!.revealed).toBe(1);
  });
});

describe("InvestigationDocuments: closing", () => {
  it("posts nothing more once the panel closes mid-read", async () => {
    let answer!: (d: InvestigationDetail) => void;
    const { client } = backend({ "investigations.get": () => new Promise((r) => { answer = r; }) });
    const { docs, onLoaded } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(answer).toBeDefined());

    panel!.dispose();
    answer(detail());
    await new Promise((r) => setTimeout(r, 0));

    expect(types()).toEqual(["loading"]);
    expect(onLoaded).not.toHaveBeenCalled();
  });

  it("polls while a dispatch is in flight, and stops when the panel closes", async () => {
    vi.useFakeTimers();
    const approved = [{ actionId: "a-1", status: "APPROVED" }] as ActionRow[];
    const { client, calls } = backend({ "actions.list": async () => approved });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(types()).toContain("state.snapshot"));

    const reads = () => calls.filter((c) => c === "investigations.get").length;
    const before = reads();
    await vi.advanceTimersByTimeAsync(4000);
    expect(reads()).toBe(before + 1);

    panel!.dispose();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(reads()).toBe(before + 1);
  });

  it("ends the agent session when the panel closes", async () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.dispose();
    expect(transport.closeSession).toHaveBeenCalledWith("inv-1");
  });

  it("leaves a session with a panel open alone", () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    docs.releaseSession("inv-1");
    expect(transport.closeSession).not.toHaveBeenCalled();
  });

  it("lets a conclude-time summary finish before ending the session of a closed panel", async () => {
    let finish!: () => void;
    vi.mocked(transport.summarizeConcluded).mockImplementationOnce(() => new Promise((r) => { finish = r; }));
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel!.webview.receive({ type: "lifecycle", transition: "conclude", summary: "done" });
    await vi.waitFor(() => expect(transport.summarizeConcluded).toHaveBeenCalledWith("inv-1"));

    panel!.dispose();
    expect(transport.closeSession).not.toHaveBeenCalled();

    finish();
    await vi.waitFor(() => expect(transport.closeSession).toHaveBeenCalledWith("inv-1"));
  });

  it("stops the running turn on the analyst's word", async () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "turn.stop" });
    expect(transport.cancel).toHaveBeenCalledWith("inv-1");
  });

  const evidenceDoc = { ref: "ref-1", kind: "observed-data", type: "observed-data", payload: {} };
  const eventRow = { id: "e-1", classUid: 3002, className: "Authentication", time: "2026-01-02T03:04:05Z", recordedAt: "2026-01-02T03:04:05Z", sourceTool: "fixture:s", callId: "c-1", verb: "enumerate_logons", coverage: "COMPLETE", observedDataRefs: [], entityRefs: [], view: { fields: [], category: "auth" } };

  it("posts the retrieved events after the snapshot, folded by the default key, stamped with its sequence (adr/0092, adr/0095)", async () => {
    const group = { classUid: 3002, key: [{ slot: "user", label: "Account", text: "svc_backup" }], count: 1, first: eventRow.time, last: eventRow.time, days: 1, eventIds: [eventRow.id] };
    const keys = { classUid: 3002, className: "Authentication", slots: [{ name: "user", label: "Account" }], default: ["user"], key: ["user"] };
    const events = vi.fn(async () => ({ events: [eventRow], total: 1, groups: [group], keys: [keys], foldedOver: 1 }));
    const { client } = backend({ "evidence.events": events });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "events" in m.patch)).toBe(true));
    const patch = posted().find((m) => m.type === "state.patch" && "events" in m.patch) as Extract<RenderMessage, { type: "state.patch" }>;
    expect(patch.seq).toBe(3);
    expect(patch.patch.events).toEqual([eventRow]);
    expect(patch.patch.eventsTotal).toBe(1);
    expect(patch.patch.eventsError).toBeNull();
    expect(patch.patch.eventsCursor).toBeNull();
    expect(patch.patch.eventGroups).toEqual([group]);
    expect(patch.patch.eventKeys).toEqual([keys]);
    expect(patch.patch.eventsFoldedOver).toBe(1);
    expect(events).toHaveBeenCalledWith("inv-1", { groupBy: "default" });
  });

  it("re-reads the record with the key the timeline chose, or flat when the fold is turned off (adr/0095)", async () => {
    const events = vi.fn(async (_id: string, q: { groupBy?: string }) => q.groupBy === undefined ? { events: [eventRow], total: 1, nextCursor: "c1" } : { events: [eventRow], total: 1, groups: [], keys: [], foldedOver: 1 });
    const { client } = backend({ "evidence.events": events });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "events" in m.patch)).toBe(true));
    panel.webview.receive({ type: "events.fold", keys: "3002:user,logon_type" });
    await vi.waitFor(() => expect(events).toHaveBeenCalledTimes(2));
    expect(events).toHaveBeenLastCalledWith("inv-1", { groupBy: "3002:user,logon_type" });
    panel.webview.receive({ type: "events.fold", keys: null });
    await vi.waitFor(() => expect(posted().filter((m) => m.type === "state.patch" && "events" in m.patch).length).toBe(3));
    expect(events).toHaveBeenLastCalledWith("inv-1", { limit: 500, order: "newest" });
    const flat = posted().filter((m) => m.type === "state.patch" && "events" in m.patch).pop() as Extract<RenderMessage, { type: "state.patch" }>;
    expect(flat.patch.eventGroups).toBeNull();
    expect(flat.patch.eventsCursor).toBe("c1");
  });

  it("appends the next older page when the timeline asks for it, and ignores a cursor it did not post", async () => {
    const older = { ...eventRow, id: "e-0", time: "2026-01-01T00:00:00Z" };
    const events = vi.fn(async (_id: string, q: { cursor?: string }) => q.cursor ? { events: [older], total: 2 } : { events: [eventRow], total: 2, nextCursor: "2026-01-02T03:04:05Z,e-1" });
    const { client } = backend({ "evidence.events": events });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "events" in m.patch)).toBe(true)); // the folded first read
    panel.webview.receive({ type: "events.fold", keys: null }); // the flat page, which is what pages
    const flat = () => posted().filter((m) => m.type === "state.patch" && "eventsCursor" in m.patch && !Array.isArray(m.patch.eventGroups));
    await vi.waitFor(() => expect(flat().length).toBe(1));
    panel.webview.receive({ type: "events.older", cursor: "stale" });
    panel.webview.receive({ type: "events.older", cursor: "2026-01-02T03:04:05Z,e-1" });
    await vi.waitFor(() => expect(flat().length).toBe(2));
    expect(events).toHaveBeenCalledTimes(3); // folded, flat, older
    expect(events).toHaveBeenLastCalledWith("inv-1", { limit: 500, order: "newest", cursor: "2026-01-02T03:04:05Z,e-1" });
    const last = flat().pop() as Extract<RenderMessage, { type: "state.patch" }>;
    expect(last.patch.events).toEqual([eventRow, older]);
    expect(last.patch.eventsCursor).toBeNull();
  });

  it("drops an older page that lands after a newer read of the record, so the next click is not ignored", async () => {
    let releaseOlder: (() => void) | undefined;
    const slow = new Promise<void>((r) => { releaseOlder = r; });
    const events = vi.fn(async (_id: string, q: { cursor?: string }) => {
      if (q.cursor === "c-old") { await slow; return { events: [{ ...eventRow, id: "e-old" }], total: 3, nextCursor: "c-older" }; }
      if (q.cursor === "c-new") return { events: [{ ...eventRow, id: "e-prev" }], total: 3 };
      return { events: [eventRow], total: 3, nextCursor: events.mock.calls.length > 2 ? "c-new" : "c-old" };
    });
    const { client } = backend({ "evidence.events": events });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "events" in m.patch)).toBe(true)); // the folded first read
    panel.webview.receive({ type: "events.fold", keys: null }); // the flat page; a refresh keeps the fold the timeline is in
    const cursorPatches = () => posted().filter((m) => m.type === "state.patch" && "eventsCursor" in m.patch && !Array.isArray(m.patch.eventGroups)) as Extract<RenderMessage, { type: "state.patch" }>[];
    await vi.waitFor(() => expect(cursorPatches().length).toBe(1));
    expect(cursorPatches()[0]!.patch.eventsCursor).toBe("c-old");
    panel.webview.receive({ type: "events.older", cursor: "c-old" }); // slow
    panel.webview.receive({ type: "refresh" }); // a newer read of the record: cursor c-new
    await vi.waitFor(() => expect(cursorPatches().length).toBe(2));
    expect(cursorPatches()[1]!.patch.eventsCursor).toBe("c-new");
    releaseOlder!();
    await new Promise((r) => setTimeout(r, 0));
    expect(cursorPatches().length).toBe(2); // the slow older page was dropped
    panel.webview.receive({ type: "events.older", cursor: "c-new" });
    await vi.waitFor(() => expect(cursorPatches().length).toBe(3));
    expect(cursorPatches()[2]!.patch.events!.map((e) => e.id)).toEqual(["e-1", "e-prev"]);
  });

  it("re-posts the Library when a refresh re-reads the thread, so a count follows a citing turn (adr/0109)", async () => {
    let citedIn = 0;
    const procedures = vi.fn(async () => [{ kind: "sop" as const, ref: "sop-1", title: "Procedure one", citedIn }]);
    const { client } = backend({ "knowledge.procedures": procedures });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    const libraries = () => posted().filter((m) => m.type === "library") as Extract<RenderMessage, { type: "library" }>[];
    await vi.waitFor(() => expect(libraries().length).toBe(1));
    expect(libraries()[0]!.sops[0]!.citedIn).toBe(0);
    citedIn = 1; // a turn committed a citation; the record now counts it
    panel.webview.receive({ type: "refresh" });
    await vi.waitFor(() => expect(libraries().length).toBe(2));
    expect(libraries()[1]!.sops[0]!.citedIn).toBe(1);
  });

  it("drops an older Library answer that lands after a newer one, so a stale count never overwrites the new one", async () => {
    let releaseOlder: (() => void) | undefined;
    const slow = new Promise<void>((r) => { releaseOlder = r; });
    let reads = 0;
    const procedures = vi.fn(async () => {
      const n = ++reads;
      if (n === 1) { await slow; return [{ kind: "sop" as const, ref: "sop-1", title: "Procedure one", citedIn: 0 }]; } // the load's read, slow
      return [{ kind: "sop" as const, ref: "sop-1", title: "Procedure one", citedIn: 1 }];
    });
    const { client } = backend({ "knowledge.procedures": procedures });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    const libraries = () => posted().filter((m) => m.type === "library") as Extract<RenderMessage, { type: "library" }>[];
    await vi.waitFor(() => expect(procedures).toHaveBeenCalledTimes(1));
    panel.webview.receive({ type: "refresh" }); // a newer read of the library, which answers first
    await vi.waitFor(() => expect(libraries().length).toBe(1));
    expect(libraries()[0]!.sops[0]!.citedIn).toBe(1);
    releaseOlder!();
    await new Promise((r) => setTimeout(r, 0));
    expect(libraries().length).toBe(1); // the older answer was dropped
  });

  it("does not report the failure of an older read retired by a newer read of the record", async () => {
    let failOlder: ((err: Error) => void) | undefined;
    const slow = new Promise<never>((_, reject) => { failOlder = reject; });
    const events = vi.fn(async (_id: string, q: { cursor?: string }) => {
      if (q.cursor === "c-old") return slow;
      return { events: [eventRow], total: 2, nextCursor: "c-old" };
    });
    const { client } = backend({ "evidence.events": events });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "events" in m.patch)).toBe(true)); // the folded first read
    panel.webview.receive({ type: "events.fold", keys: null });
    const cursorPatches = () => posted().filter((m) => m.type === "state.patch" && "eventsCursor" in m.patch && !Array.isArray(m.patch.eventGroups));
    await vi.waitFor(() => expect(cursorPatches().length).toBe(1));
    panel.webview.receive({ type: "events.older", cursor: "c-old" }); // hangs
    panel.webview.receive({ type: "refresh" }); // retires it
    await vi.waitFor(() => expect(cursorPatches().length).toBe(2));
    failOlder!(new Error("store down"));
    await new Promise((r) => setTimeout(r, 0));
    expect(posted().some((m) => m.type === "state.patch" && "eventsError" in m.patch && m.patch.eventsError)).toBe(false);
  });

  it("answers an event that is not a member of the investigation with an empty window, since its evidence is already loaded", async () => {
    const ocsf = { ref: "e-old", kind: "ocsf", type: "Authentication", payload: {}, classUid: 3002 };
    const { client } = backend({ "evidence.get": async () => ocsf, "evidence.events": async () => { throw new BackendRequestError("not a member", 404); } });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "inspector.open", target: { kind: "ref", ref: "e-old" } });
    await vi.waitFor(() => expect(types()).toContain("inspector.evidence"));
    const answer = posted().find((m) => m.type === "inspector.evidence") as Extract<RenderMessage, { type: "inspector.evidence" }>;
    expect(answer.around).toEqual([]);
    expect(types()).not.toContain("inspector.error");
  });

  it("tells the panel when the retrieved record could not be read, leaving the events slice as it was", async () => {
    const { client } = backend({ "evidence.events": async () => { throw new BackendRequestError("no route", 404); } });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "state.patch" && "eventsError" in m.patch)).toBe(true));
    const patch = posted().find((m) => m.type === "state.patch" && "eventsError" in m.patch) as Extract<RenderMessage, { type: "state.patch" }>;
    expect(patch.patch.eventsError).toMatch(/no route/);
    expect("events" in patch.patch).toBe(false);
  });

  it("reports a failed around read on the event's own pane, since an empty window means the record holds nothing", async () => {
    const ocsf = { ref: "e-1", kind: "ocsf", type: "Authentication", payload: {}, classUid: 3002 };
    const { client } = backend({ "evidence.get": async () => ocsf, "evidence.events": async () => { throw new BackendRequestError("store down", 503); } });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "inspector.open", target: { kind: "ref", ref: "e-1" } });
    await vi.waitFor(() => expect(types()).toContain("inspector.error"));
    expect(posted().find((m) => m.type === "inspector.error")).toMatchObject({ type: "inspector.error", target: "e-1", message: expect.stringMatching(/store down/) });
    expect(types()).not.toContain("inspector.evidence");
  });

  it("answers an event's inspector with the retrieved events around it, and a call pane with the call", async () => {
    const ocsf = { ref: "e-1", kind: "ocsf", type: "Authentication", payload: {}, classUid: 3002 };
    const events = vi.fn(async () => ({ events: [eventRow], total: 1 }));
    const call = { callId: "c-1", verb: "enumerate_logons", priority: 100, coverage: "COMPLETE", bindingsAnswered: 1, bindingsTotal: 1, degradationNotes: [], startedAt: "2026-01-02T03:04:05Z", durationMs: 7, outcomes: [] };
    const { client } = backend({ "evidence.get": async () => ocsf, "evidence.events": events, "evidence.call": async () => call });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0]!;
    panel.webview.receive({ type: "inspector.open", target: { kind: "ref", ref: "e-1" } });
    await vi.waitFor(() => expect(types()).toContain("inspector.evidence"));
    const answer = posted().find((m) => m.type === "inspector.evidence") as Extract<RenderMessage, { type: "inspector.evidence" }>;
    expect(answer.around).toEqual([eventRow]);
    expect(events).toHaveBeenCalledWith("inv-1", { around: "e-1", limit: 5 });

    panel.webview.receive({ type: "inspector.open", target: { kind: "call", callId: "c-1" } });
    await vi.waitFor(() => expect(types()).toContain("inspector.call"));
    expect(posted().find((m) => m.type === "inspector.call")).toEqual({ type: "inspector.call", callId: "c-1", call });
  });

  it("reports a call the record does not hold on its own pane", async () => {
    const { client } = backend({ "evidence.call": async () => { throw new BackendRequestError("no capability call", 404); } });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "inspector.open", target: { kind: "call", callId: "c-9" } });
    await vi.waitFor(() => expect(types()).toContain("inspector.error"));
    expect(posted().find((m) => m.type === "inspector.error")).toMatchObject({ type: "inspector.error", target: "c-9" });
  });

  it("does not keep a failed evidence read, and tells the panel", async () => {
    let down = true;
    const evidence = vi.fn(async () => {
      if (down) throw new Error("503");
      return evidenceDoc;
    });
    const { client } = backend({ "evidence.get": evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];

    panel!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(types()).toContain("evidence.unresolved"));

    down = false;
    panel!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("closes every panel when the identity changes, and reads again for the next person", async () => {
    const evidence = vi.fn(async () => evidenceDoc);
    const { client } = backend({ "evidence.get": evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    docs.showDraft();
    const [first, draft] = createdPanels();
    first!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledOnce());

    docs.identityChanged();

    expect(first!.disposed).toBe(true);
    expect(draft!.disposed).toBe(true);
    docs.show("inv-1");
    createdPanels()[2]!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("does not cache an evidence read that lands after the identity changed", async () => {
    let land!: (d: typeof evidenceDoc) => void;
    const evidence = vi.fn((): Promise<typeof evidenceDoc> => new Promise((r) => { land = r; }));
    const { client } = backend({ "evidence.get": evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledOnce());

    docs.identityChanged();
    land(evidenceDoc);
    await new Promise((r) => setTimeout(r, 0));

    docs.show("inv-1");
    createdPanels()[1]!.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("does not pop a rename from a panel closed before it was ready", async () => {
    const { docs } = documents(backend().client);
    docs.beginRename("inv-1", "T");
    createdPanels()[0]!.dispose();

    docs.show("inv-1");
    const panel = createdPanels()[1];
    panel!.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(panel!.webview.posted.map((m) => (m as RenderMessage).type)).toContain("state.snapshot"));
    await new Promise((r) => setTimeout(r, 0));

    expect(panel!.webview.posted.map((m) => (m as RenderMessage).type)).not.toContain("rename.begin");
  });

  describe("closing a SOP draft when the identity changes", () => {
    const uri = vscode.Uri.parse("untitled:Untitled-1");
    const draftTab = () => ({ input: new vscode.TabInputText(uri) });
    const other = { input: new vscode.TabInputText(vscode.Uri.file("/mine.md")) };
    const setGroups = (all: unknown[]) => { (vscode.window.tabGroups as unknown as { all: unknown[] }).all = all; };
    const revert = "workbench.action.revertAndCloseActiveEditor";
    afterEach(() => setGroups([]));

    async function drafted() {
      vi.mocked(vscode.workspace.openTextDocument).mockResolvedValueOnce({ uri, isClosed: false } as vscode.TextDocument);
      const { docs } = documents(backend().client);
      docs.show("inv-1");
      createdPanels()[0]!.webview.receive({ type: "sop.draft" });
      await vi.waitFor(() => expect(vscode.window.showTextDocument).toHaveBeenCalledOnce());
      await new Promise((r) => setTimeout(r, 0));
      vi.mocked(vscode.window.showTextDocument).mockClear();
      return docs;
    }

    it("reverts it in the group that holds it, not the active one, and nobody is asked to save it", async () => {
      const docs = await drafted();
      setGroups([{ viewColumn: 1, tabs: [other] }, { viewColumn: 2, tabs: [draftTab()] }]);

      docs.identityChanged();

      await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith(revert));
      expect(vscode.window.showTextDocument).toHaveBeenCalledOnce();
      expect(vscode.window.showTextDocument).toHaveBeenCalledWith(expect.objectContaining({ uri }), { viewColumn: 2 });
    });

    it("reverts it in every group that holds it", async () => {
      const docs = await drafted();
      setGroups([{ viewColumn: 1, tabs: [draftTab()] }, { viewColumn: 3, tabs: [draftTab()] }]);

      docs.identityChanged();

      await vi.waitFor(() => expect(vscode.window.showTextDocument).toHaveBeenCalledTimes(2));
      await vi.waitFor(() => expect(vi.mocked(vscode.commands.executeCommand).mock.calls.filter(([c]) => c === revert)).toHaveLength(2));
      expect(vscode.window.showTextDocument).toHaveBeenCalledWith(expect.anything(), { viewColumn: 3 });
    });

    it("does not reopen a draft no group holds", async () => {
      const docs = await drafted();
      setGroups([{ viewColumn: 1, tabs: [other] }]);

      docs.identityChanged();
      await new Promise((r) => setTimeout(r, 0));

      expect(vscode.window.showTextDocument).not.toHaveBeenCalled();
    });
  });

  it("closes a report's preview and deletes its scratch file when the identity changes, and leaves other tabs", async () => {
    const { docs } = documents(backend({ "investigations.exportMarkdown": async () => "# report" }).client);
    docs.show("inv-12345678");
    createdPanels()[0]!.webview.receive({ type: "export.open" });
    await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith("markdown.showPreviewToSide", expect.anything()));
    const scratch = vi.mocked(vscode.commands.executeCommand).mock.calls.find(([c]) => c === "markdown.showPreviewToSide")![1] as vscode.Uri;
    const preview = { label: `Preview ${scratch.path.split("/").pop()}`, input: new vscode.TabInputWebview("mainThreadWebview-markdown.preview") };
    // The analyst's saved copy of the same report is not reckon's to close.
    const mine = { label: "Preview reckon-report-inv-1234.md", input: new vscode.TabInputWebview("mainThreadWebview-markdown.preview") };
    (vscode.window.tabGroups as unknown as { all: unknown[] }).all = [{ tabs: [preview, mine] }];

    docs.identityChanged();

    await vi.waitFor(() => expect(vscode.window.tabGroups.close).toHaveBeenCalledWith([preview]));
    await vi.waitFor(() => expect(vscode.workspace.fs.delete).toHaveBeenCalledOnce());
    (vscode.window.tabGroups as unknown as { all: unknown[] }).all = [];
  });
});

describe("InvestigationDocuments: work in flight when the identity changes", () => {
  const gate = <T>() => {
    let open!: (v: T) => void;
    const opened = new Promise<T>((r) => { open = r; });
    return { opened, open };
  };
  const settle = () => new Promise((r) => setTimeout(r, 0));
  const fromCase = { id: "inv-2", title: "Alice's case", caseNote: { caseId: "C-1" }, caseNoteOnSeed: "auto" } as unknown as InvestigationDetail;

  it("does not open an export that lands after the change", async () => {
    const md = gate<string>();
    const { docs } = documents(backend({ "investigations.exportMarkdown": () => md.opened }).client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "export.open" });

    docs.identityChanged();
    md.open("# Alice's report");
    await settle();

    expect(vscode.workspace.fs.writeFile).not.toHaveBeenCalled();
    expect(vscode.commands.executeCommand).not.toHaveBeenCalledWith("markdown.showPreviewToSide", expect.anything());
  });

  it("closes a preview that opened across the change", async () => {
    const shown = gate<void>();
    vi.mocked(vscode.commands.executeCommand).mockImplementationOnce(() => shown.opened);
    const { docs } = documents(backend({ "investigations.exportMarkdown": async () => "# r" }).client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "export.open" });
    await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith("markdown.showPreviewToSide", expect.anything()));

    docs.identityChanged();
    await settle();
    vi.mocked(vscode.workspace.fs.delete).mockClear();
    shown.open();

    await vi.waitFor(() => expect(vscode.workspace.fs.delete).toHaveBeenCalledOnce());
    expect(vscode.window.showInformationMessage).not.toHaveBeenCalled();
  });

  it("does not save a report from a prompt that outlived the change", async () => {
    const answer = gate<string | undefined>();
    vi.mocked(vscode.window.showInformationMessage).mockImplementationOnce(() => answer.opened as never);
    const { docs } = documents(backend({ "investigations.exportMarkdown": async () => "# r" }).client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "export.open" });
    await vi.waitFor(() => expect(vscode.window.showInformationMessage).toHaveBeenCalledOnce());

    docs.identityChanged();
    answer.open("Save report…");
    await settle();

    expect(vscode.window.showSaveDialog).not.toHaveBeenCalled();
  });

  it("does not take over a closed draft when its seed lands, nor write its case note", async () => {
    const created = gate<InvestigationDetail>();
    const { client, calls } = backend({ "investigations.createFromInput": () => created.opened });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "seed.submit", value: "host-a", kind: "" });

    docs.identityChanged();
    created.open(fromCase);
    await settle();

    expect(createdPanels()).toHaveLength(1);
    expect(calls).not.toContain("investigations.requestCaseNote");
  });

  it("does not open an investigation started from a case row before the change", async () => {
    const created = gate<InvestigationDetail>();
    const { client, calls } = backend({ "investigations.createFromInput": () => created.opened });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "queue.start", caseNumber: "C-1" });
    await vi.waitFor(() => expect(calls).toContain("investigations.createFromInput"));

    docs.identityChanged();
    created.open(fromCase);
    await settle();

    expect(createdPanels()).toHaveLength(1);
    expect(calls).not.toContain("investigations.requestCaseNote");
  });

  it("does not open a SOP draft that lands after the change", async () => {
    const candidate = gate<{ warranted: boolean; title: string; body: string; tags: string[]; recommendation: string; rationale: string }>();
    vi.mocked(transport.proposeSOP).mockImplementationOnce(() => candidate.opened as never);
    const { docs } = documents(backend().client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "sop.draft" });
    await vi.waitFor(() => expect(transport.proposeSOP).toHaveBeenCalledOnce());

    docs.identityChanged();
    candidate.open({ warranted: true, title: "T", body: "B", tags: [], recommendation: "", rationale: "" });
    await settle();

    expect(vscode.window.showTextDocument).not.toHaveBeenCalled();
  });

  it("closes a SOP draft shown across the change, in the column it was shown in", async () => {
    const shown = gate<{ viewColumn: number }>();
    const doc = { uri: vscode.Uri.parse("untitled:Untitled-1"), isClosed: false } as vscode.TextDocument;
    vi.mocked(vscode.workspace.openTextDocument).mockResolvedValueOnce(doc);
    vi.mocked(vscode.window.showTextDocument).mockImplementationOnce(() => shown.opened as never);
    const { docs } = documents(backend().client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "sop.draft" });
    await vi.waitFor(() => expect(vscode.window.showTextDocument).toHaveBeenCalledOnce());

    docs.identityChanged();
    shown.open({ viewColumn: 2 });

    await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith("workbench.action.revertAndCloseActiveEditor"));
    expect(vscode.window.showTextDocument).toHaveBeenLastCalledWith(doc, { viewColumn: 2 });
  });

  it("does not approve on the next person's token when the change lands during the read before it", async () => {
    const actions = gate<ActionRow[]>();
    const approveAction = vi.fn(async () => ({ status: "APPROVED" }));
    const { client } = backend({ "actions.list": () => actions.opened, "actions.approve": approveAction });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "action.approve", actionId: "a-1" });
    await settle();

    docs.identityChanged();
    actions.open([{ actionId: "a-1", tier: "T1", status: "PROPOSED" } as ActionRow]);
    await settle();

    expect(approveAction).not.toHaveBeenCalled();
    expect(vscode.window.showErrorMessage).not.toHaveBeenCalled();
  });

  it("does not request a reversal on the next person's token", async () => {
    const record = gate<{ action: { actionType: string; targets: string[]; evidenceRefs: string[]; actionId: string } }>();
    const requestAction = vi.fn(async () => ({ status: "PROPOSED" }));
    const { client } = backend({
      "actions.list": async () => [{ actionId: "a-1", tier: "T1", status: "SUCCEEDED" } as ActionRow],
      "actions.record": () => record.opened,
      "actions.types": async () => [{ actionType: "x", reversibleBy: "y" }],
      "actions.request": requestAction,
    });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "action.reverse", actionId: "a-1", rationale: "r" });
    await settle();
    await settle();

    docs.identityChanged();
    record.open({ action: { actionType: "x", targets: ["t"], evidenceRefs: ["e"], actionId: "a-1" } });
    await settle();

    expect(requestAction).not.toHaveBeenCalled();
  });

  it("sends a dependency's resolution to the backend with the analyst's note (adr/0101)", async () => {
    const resolve = vi.fn(async () => undefined);
    const { client, calls } = backend({ "actions.resolveDependency": resolve });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "dependency.resolve", dependencyId: "dep-1", verb: "confirm", note: "the owner confirmed it", externalRef: "OPS-1" });
    await vi.waitFor(() => expect(calls).toContain("actions.resolveDependency"));
    expect(resolve).toHaveBeenCalledWith("dep-1", "confirm", "the owner confirmed it", "OPS-1");
  });

  it("does not request a comms follow-up on the next person's token", async () => {
    const comms = gate<{ threadId: string; actionId: string }[]>();
    const requestAction = vi.fn(async () => ({ status: "PROPOSED" }));
    const { client, calls } = backend({
      "actions.comms": () => comms.opened,
      "actions.record": async () => ({ action: { evidenceRefs: ["e"] } }),
      "actions.request": requestAction,
    });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "comms.followup", threadId: "t-1", actionType: "notify.chat", target: "x", subject: "", message: "m", followUpHours: 1 });
    await vi.waitFor(() => expect(calls).toContain("actions.comms"));

    docs.identityChanged();
    comms.open([{ threadId: "t-1", actionId: "a-1" }]);
    await settle();

    expect(requestAction).not.toHaveBeenCalled();
  });

  it("does not apply an enablement confirmed before the change", async () => {
    const answer = gate<string | undefined>();
    vi.mocked(vscode.window.showWarningMessage).mockImplementationOnce(() => answer.opened as never);
    const applyEnablement = vi.fn(async () => undefined);
    const { docs } = documents(backend({ "capabilities.applyEnablement": applyEnablement }).client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "enablement.apply", adapter: "a", enabled: true, config: {}, secretFields: [] });

    docs.identityChanged();
    answer.open("Enable");
    await settle();

    expect(applyEnablement).not.toHaveBeenCalled();
  });

  it("does not start the conclude summary for the next person", async () => {
    const concluded = gate<{ status: string }>();
    const { client, calls } = backend({ "investigations.lifecycle": () => concluded.opened });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "lifecycle", transition: "conclude", summary: "done" });
    await vi.waitFor(() => expect(calls).toContain("investigations.lifecycle"));

    docs.identityChanged();
    concluded.open({ status: "CONCLUDED" });
    await settle();

    expect(transport.summarizeConcluded).not.toHaveBeenCalled();
  });
});

describe("InvestigationDocuments: a panel closed during an await", () => {
  const gate = <T>() => {
    let open!: (v: T) => void;
    let fail!: (e: unknown) => void;
    const opened = new Promise<T>((res, rej) => { open = res; fail = rej; });
    return { opened, open, fail };
  };
  const settle = () => new Promise((r) => setTimeout(r, 0));
  const created = { id: "inv-2", title: "RDP from host-a" } as InvestigationDetail;

  function seeding() {
    const create = gate<InvestigationDetail>();
    const { client, calls } = backend({ "investigations.createFromInput": () => create.opened, "investigations.requestCaseNote": async () => ({ status: "PROPOSED" }) });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "seed.submit", value: "host-a", kind: "" });
    return { create, calls, docs };
  }

  it("does not register a draft closed while its seed was in flight, and the investigation still opens", async () => {
    const { create, docs } = seeding();

    createdPanels()[0]!.dispose();
    create.open(created);
    await settle();

    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.refreshInvestigations");
    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.refreshCases");
    expect(vscode.window.showErrorMessage).not.toHaveBeenCalled();
    docs.show("inv-2");
    expect(createdPanels()).toHaveLength(2);
  });

  it("still applies the case note posture when the draft closed: the create stands", async () => {
    const { create, calls } = seeding();

    createdPanels()[0]!.dispose();
    create.open({ ...created, caseNote: { caseId: "C-1" }, caseNoteOnSeed: "auto" } as unknown as InvestigationDetail);
    await settle();

    expect(calls).toContain("investigations.requestCaseNote");
  });

  it("re-keys a draft still open when its seed lands", async () => {
    const { create, docs } = seeding();

    create.open(created);
    await settle();

    expect(createdPanels()[0]!.title).toBe("⚖ RDP from host-a");
    docs.show("inv-2");
    expect(createdPanels()).toHaveLength(1);
  });

  it("opens nothing for an already investigated case when the draft closed", async () => {
    const { create } = seeding();

    createdPanels()[0]!.dispose();
    create.fail(new BackendRequestError("exists", 409, { investigation_id: "inv-9" }));
    await settle();

    expect(createdPanels()).toHaveLength(1);
    expect(vscode.window.showInformationMessage).toHaveBeenCalledOnce();
  });

  it("opens the existing investigation in place of a draft still open", async () => {
    const { create } = seeding();

    create.fail(new BackendRequestError("exists", 409, { investigation_id: "inv-9" }));
    await settle();

    expect(createdPanels()).toHaveLength(2);
    expect(createdPanels()[0]!.disposed).toBe(true);
  });

  it("does not report a rename that succeeded as failed when its panel closed, and refreshes the tree", async () => {
    const rename = gate<string>();
    const { client } = backend({ "investigations.rename": () => rename.opened });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel!.webview.receive({ type: "rename", title: "New" });
    await settle();

    panel!.dispose();
    rename.open("New");
    await settle();

    expect(vscode.window.showErrorMessage).not.toHaveBeenCalled();
    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.refreshInvestigations");
  });
});

describe("InvestigationDocuments: the wire boundary", () => {
  it("shows why when the backend is incompatible, and makes no other call", async () => {
    const reason = "backend API v9 unsupported (this workbench speaks v1) — align versions";
    const { client, calls } = backend({ incompatibilityReason: () => reason });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];

    panel!.webview.receive({ type: "ready" });

    await vi.waitFor(() => expect(panel!.webview.posted).toContainEqual({ type: "error", message: reason }));
    expect(calls.filter((c) => c !== "status" && c !== "incompatibilityReason")).toEqual([]);
  });

  it("says so on a draft too, and reads nothing for it", async () => {
    const reason = "backend API v9 unsupported";
    const { client, calls } = backend({ incompatibilityReason: () => reason });
    const { docs } = documents(client);
    docs.showDraft();

    createdPanels()[0]!.webview.receive({ type: "ready" });

    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "draft.error", message: reason }));
    expect(calls).not.toContain("cases.configuredQueue");
  });

  it("loads when the backend is compatible", async () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "ready" });

    await vi.waitFor(() => expect(types()).toContain("state.snapshot"));
  });

  it("does not leave the panel on drafting when a candidate has no title", async () => {
    vi.mocked(transport.proposeSOP).mockResolvedValueOnce({ warranted: true, body: "B" } as never);
    const { docs } = documents(backend().client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "sop.draft" });

    await vi.waitFor(() => expect(posted()).toContainEqual(expect.objectContaining({ type: "sop.status", state: "failed" })));
  });

  it("keeps a draft whose accept answered an outcome word the workbench does not know, and says so", async () => {
    const doc = { uri: vscode.Uri.parse("untitled:Untitled-2"), isClosed: false, getText: () => "# T\n\nB" } as unknown as vscode.TextDocument;
    vi.mocked(vscode.workspace.openTextDocument).mockResolvedValueOnce(doc);
    const importMarkdown = vi.fn(async () => ({ outcome: "merged", title: "T" }));
    const { docs } = documents(backend({ "knowledge.importMarkdown": importMarkdown }).client);
    docs.show("inv-1");
    createdPanels()[0]!.webview.receive({ type: "sop.draft" });
    await vi.waitFor(() => expect(posted()).toContainEqual(expect.objectContaining({ type: "sop.status", state: "ready", title: "T" })));

    createdPanels()[0]!.webview.receive({ type: "sop.accept" });
    await vi.waitFor(() => expect(importMarkdown).toHaveBeenCalledOnce());
    await vi.waitFor(() => expect(posted()).toContainEqual(expect.objectContaining({ type: "sop.status", state: "ready", message: expect.stringContaining("does not know") })));
    expect(posted()).not.toContainEqual(expect.objectContaining({ type: "sop.status", state: "added" }));
  });

  it("does not throw on a message of unknown type", () => {
    const { docs } = documents(backend().client);
    docs.show("inv-1");

    expect(() => createdPanels()[0]!.webview.receive({ type: "no.such.message" })).not.toThrow();
  });
});

describe("InvestigationDocuments: a seed's entity kind (adr/0324)", () => {
  it("passes the analyst's kind to the create", async () => {
    const seen: unknown[][] = [];
    const { client } = backend({ "investigations.createFromInput": ((...args: unknown[]) => { seen.push(args); return new Promise(() => {}); }) as never });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "seed.submit", value: "jdoe", kind: "entity", entityKind: "account" });
    await vi.waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0]).toEqual(["jdoe", "entity", undefined, undefined, "account", undefined]);
    // A listed account is seeded by its ref.
    const second = documents(client).docs;
    second.showDraft();
    createdPanels().at(-1)!.webview.receive({ type: "seed.submit", value: "root", kind: "entity", accountRef: "user-account--7" });
    await vi.waitFor(() => expect(seen).toHaveLength(2));
    expect(seen[1]).toEqual(["root", "entity", undefined, undefined, undefined, "user-account--7"]);
  });

  it("lists the accounts the record holds under an unknown account's name, and keeps start anyway", async () => {
    const refused = new BackendRequestError("POST /api/investigations → 422", 422, {
      error: "no account \"jdoe\" of that kind in the record; the record has 1 account(s) under that name — seed on one, or start anyway",
      unknown_entity: true,
      known_accounts: [{ ref: "user-account--1", account_type: "windows-domain", account_login: "CONTOSO\\jdoe" }, { bad: true }],
    });
    const { client } = backend({ "investigations.createFromInput": async () => { throw refused; } });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "seed.submit", value: "jdoe", kind: "entity", entityKind: "account" });
    await vi.waitFor(() => expect(types()).toContain("draft.error"));
    expect(posted().find((m) => m.type === "draft.error")).toEqual({
      type: "draft.error", message: refused.body!.error, unknownEntity: true,
      knownAccounts: [{ ref: "user-account--1", accountType: "windows-domain", accountLogin: "CONTOSO\\jdoe" }],
    });
  });

  it("says a person seed the record does not know in the server's words, with no start anyway", async () => {
    const refused = new BackendRequestError("POST /api/investigations → 404: resolve seed: no person known as \"jane\"", 404, {
      error: "resolve seed: no person known as \"jane\": a person is known from a directory's record of them",
    });
    const { client } = backend({ "investigations.createFromInput": async () => { throw refused; } });
    const { docs } = documents(client);
    docs.showDraft();
    createdPanels()[0]!.webview.receive({ type: "seed.submit", value: "jane", kind: "entity", entityKind: "person" });
    await vi.waitFor(() => expect(types()).toContain("draft.error"));
    expect(posted().find((m) => m.type === "draft.error")).toEqual({ type: "draft.error", message: refused.body!.error, unknownEntity: false });
  });
});
