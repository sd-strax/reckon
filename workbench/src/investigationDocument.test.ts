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
  summarizeConcluded: vi.fn(async () => undefined),
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
});
