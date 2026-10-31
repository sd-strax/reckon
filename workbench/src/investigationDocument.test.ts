// @vitest-environment node
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTransport } from "./agentTransport";
import type { ActionRow, BackendClient, InvestigationDetail } from "./backend";
import { InvestigationDocuments } from "./investigationDocument";
import type { RenderMessage } from "./protocol";
import { createdPanels, fakeLog, resetVscodeFake } from "./test/vscode";

const detail = (seq = 3): InvestigationDetail =>
  ({ id: "inv-1", title: "RDP from host-a", state: "ACTIVE", lastEventSequence: seq }) as InvestigationDetail;

/**
 * A backend whose named reads the test sets; every other read answers an
 * empty list, which each optional layer of the document renders as absent.
 */
function backend(over: Record<string, (...args: never[]) => Promise<unknown>> = {}) {
  const calls: string[] = [];
  const named: Record<string, (...args: never[]) => Promise<unknown>> = {
    getInvestigation: async () => detail(),
    enablement: async () => null,
    cases: async () => null,
    ...over,
  };
  const client = new Proxy({}, {
    get: (_t, prop: string) => (...args: never[]) => {
      calls.push(prop);
      return (named[prop] ?? (async () => []))(...args);
    },
  }) as BackendClient;
  return { client, calls };
}

const transport = {
  closeSession: vi.fn(async () => undefined),
  cancel: vi.fn(async () => undefined),
  summarizeConcluded: vi.fn(async () => undefined),
  proposeSOP: vi.fn(async () => ({ warranted: true, title: "T", body: "B", tags: [], recommendation: "", rationale: "" })),
} as unknown as AgentTransport;

function documents(client: BackendClient) {
  const onLoaded = vi.fn();
  const docs = new InvestigationDocuments(vscode.Uri.file("/ext"), client, fakeLog(), transport, async () => null, onLoaded);
  return { docs, onLoaded };
}

const posted = (i = 0) => createdPanels()[i].webview.posted as RenderMessage[];
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
    expect(panel.title).toBe("⚖ RDP from host-a");
    expect(panel.webview.options.enableScripts).toBe(true);
    expect(panel.webview.html).toContain("default-src 'none'");
    expect(panel.webview.html).not.toContain("unsafe-inline");
    expect(panel.webview.posted).toEqual([]); // nothing until the page asks

    panel.webview.receive({ type: "ready" });
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
    expect(createdPanels()[0].revealed).toBe(1);
  });
});

describe("InvestigationDocuments: closing", () => {
  it("posts nothing more once the panel closes mid-read", async () => {
    let answer!: (d: InvestigationDetail) => void;
    const { client } = backend({ getInvestigation: () => new Promise((r) => { answer = r; }) });
    const { docs, onLoaded } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(answer).toBeDefined());

    panel.dispose();
    answer(detail());
    await new Promise((r) => setTimeout(r, 0));

    expect(types()).toEqual(["loading"]);
    expect(onLoaded).not.toHaveBeenCalled();
  });

  it("polls while a dispatch is in flight, and stops when the panel closes", async () => {
    vi.useFakeTimers();
    const approved = [{ actionId: "a-1", status: "APPROVED" }] as ActionRow[];
    const { client, calls } = backend({ actions: async () => approved });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(types()).toContain("state.snapshot"));

    const reads = () => calls.filter((c) => c === "getInvestigation").length;
    const before = reads();
    await vi.advanceTimersByTimeAsync(4000);
    expect(reads()).toBe(before + 1);

    panel.dispose();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(reads()).toBe(before + 1);
  });

  it("ends the agent session when the panel closes", async () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0].dispose();
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
    panel.webview.receive({ type: "lifecycle", transition: "conclude", summary: "done" });
    await vi.waitFor(() => expect(transport.summarizeConcluded).toHaveBeenCalledWith("inv-1"));

    panel.dispose();
    expect(transport.closeSession).not.toHaveBeenCalled();

    finish();
    await vi.waitFor(() => expect(transport.closeSession).toHaveBeenCalledWith("inv-1"));
  });

  it("stops the running turn on the analyst's word", async () => {
    const { client } = backend();
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0].webview.receive({ type: "turn.stop" });
    expect(transport.cancel).toHaveBeenCalledWith("inv-1");
  });

  const evidenceDoc = { ref: "ref-1", kind: "observed-data", type: "observed-data", payload: {} };

  it("does not keep a failed evidence read, and tells the panel", async () => {
    let down = true;
    const evidence = vi.fn(async () => {
      if (down) throw new Error("503");
      return evidenceDoc;
    });
    const { client } = backend({ evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    const panel = createdPanels()[0];

    panel.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(types()).toContain("evidence.unresolved"));

    down = false;
    panel.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("closes every panel when the identity changes, and reads again for the next person", async () => {
    const evidence = vi.fn(async () => evidenceDoc);
    const { client } = backend({ evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    docs.showDraft();
    const [first, draft] = createdPanels();
    first.webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledOnce());

    docs.identityChanged();

    expect(first.disposed).toBe(true);
    expect(draft.disposed).toBe(true);
    docs.show("inv-1");
    createdPanels()[2].webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("does not cache an evidence read that lands after the identity changed", async () => {
    let land!: (d: typeof evidenceDoc) => void;
    const evidence = vi.fn((): Promise<typeof evidenceDoc> => new Promise((r) => { land = r; }));
    const { client } = backend({ evidence });
    const { docs } = documents(client);
    docs.show("inv-1");
    createdPanels()[0].webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledOnce());

    docs.identityChanged();
    land(evidenceDoc);
    await new Promise((r) => setTimeout(r, 0));

    docs.show("inv-1");
    createdPanels()[1].webview.receive({ type: "evidence.resolve", ref: "ref-1" });
    await vi.waitFor(() => expect(evidence).toHaveBeenCalledTimes(2));
  });

  it("does not pop a rename from a panel closed before it was ready", async () => {
    const { docs } = documents(backend().client);
    docs.beginRename("inv-1", "T");
    createdPanels()[0].dispose();

    docs.show("inv-1");
    const panel = createdPanels()[1];
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(panel.webview.posted.map((m) => (m as RenderMessage).type)).toContain("state.snapshot"));
    await new Promise((r) => setTimeout(r, 0));

    expect(panel.webview.posted.map((m) => (m as RenderMessage).type)).not.toContain("rename.begin");
  });

  it("closes a SOP draft when the identity changes, unsaved text included, and nobody is asked to save it", async () => {
    const { docs } = documents(backend().client);
    docs.show("inv-1");
    createdPanels()[0].webview.receive({ type: "sop.draft" });
    await vi.waitFor(() => expect(vscode.window.showTextDocument).toHaveBeenCalledOnce());

    docs.identityChanged();

    await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith("workbench.action.revertAndCloseActiveEditor"));
  });

  it("closes a report's preview and deletes its scratch file when the identity changes, and leaves other tabs", async () => {
    const { docs } = documents(backend({ exportMarkdown: async () => "# report" }).client);
    docs.show("inv-12345678");
    createdPanels()[0].webview.receive({ type: "export.open" });
    await vi.waitFor(() => expect(vscode.commands.executeCommand).toHaveBeenCalledWith("markdown.showPreviewToSide", expect.anything()));
    const preview = { label: "Preview reckon-report-inv-1234.md", input: new vscode.TabInputWebview("mainThreadWebview-markdown.preview") };
    const mine = { label: "Preview notes.md", input: new vscode.TabInputWebview("mainThreadWebview-markdown.preview") };
    (vscode.window.tabGroups as unknown as { all: unknown[] }).all = [{ tabs: [preview, mine] }];

    docs.identityChanged();

    await vi.waitFor(() => expect(vscode.window.tabGroups.close).toHaveBeenCalledWith([preview]));
    await vi.waitFor(() => expect(vscode.workspace.fs.delete).toHaveBeenCalledOnce());
    (vscode.window.tabGroups as unknown as { all: unknown[] }).all = [];
  });
});
