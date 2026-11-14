// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BackendClient } from "./backend";
import { KnowledgeDocument } from "./knowledgeDocument";
import { createdPanels, fakeLog, resetVscodeFake } from "./test/vscode";

beforeEach(() => {
  resetVscodeFake();
  vi.clearAllMocks();
});

function knowledge(over: Record<string, unknown> = {}) {
  return {
    procedures: async () => [{ kind: "sop", ref: "sop-1", title: "Containment" }],
    caseList: async () => ({ items: [], total: 0 }),
    caseFacets: async () => ({ techniques: [], dispositions: [] }),
    pastCases: async () => ({ rows: [], total: 0 }),
    search: async () => ({ sops: [], cases: [] }),
    sop: async (id: string) => ({ id, title: "Containment", body: "b", tags: [], status: "published", revision: 4 }),
    sopCitations: async () => { throw new Error("no projection"); },
    revisionOutline: async (_id: string, revision: number) => ({ revision, cut: "1/16000", sections: [] }),
    caseSummary: async () => null,
    caseCitations: async () => ({ ref: "r", citedIn: 0, recalledIn: 0, investigations: [] }),
    ...over,
  };
}

function opened(k: Record<string, unknown>, incompatible: string | null = null) {
  const client = { status: async () => ({}), incompatibilityReason: () => incompatible, knowledge: knowledge(k) } as unknown as BackendClient;
  const view = new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog());
  view.open({ type: "knowledge.search" });
  const panel = createdPanels().at(-1)!;
  return { view, panel, posted: () => panel.webview.posted as { type: string }[] };
}

/** The search page says it is shown and ready, and the handshake completes. */
async function searchReady(panel: { webview: { receive: (m: unknown) => void } }) {
  panel.webview.receive({ type: "knowledge.showing" });
  panel.webview.receive({ type: "ready" });
  await new Promise((r) => setTimeout(r, 0));
}

describe("KnowledgeDocument (adr/0107)", () => {
  it("reads the corpus total and the facets on ready, after the handshake, and no procedure list (adr/0346)", async () => {
    const procedures = vi.fn();
    const { panel, posted } = opened({ procedures, caseList: async () => ({ items: [], total: 150 }) });
    await searchReady(panel);
    await vi.waitFor(() => expect(posted().filter((m) => !m.type.startsWith("knowledge.")).map((m) => m.type).sort()).toEqual(["pastCases.corpus", "pastCases.facets"]));
    expect(posted()).toContainEqual({ type: "pastCases.corpus", total: 150 });
    expect(procedures).not.toHaveBeenCalled();
  });

  it("shows why and reads nothing from an incompatible backend", async () => {
    const caseList = vi.fn();
    const { panel, posted } = opened({ caseList }, "backend API v9 unsupported");
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "error", message: "backend API v9 unsupported" }));
    expect(caseList).not.toHaveBeenCalled();
  });

  it("answers the latest filter only: an older answer landing late is dropped", async () => {
    let slow!: (v: unknown) => void;
    const pastCases = vi.fn()
      .mockImplementationOnce(() => new Promise((r) => { slow = r; }))
      .mockImplementationOnce(async () => ({ rows: [], total: 7 }));
    const { panel, posted } = opened({ pastCases });
    await searchReady(panel);
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1"] } });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T2"] } });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "pastCases", filter: { techniques: ["T2"] }, rows: [], total: 7 }));
    slow({ rows: [], total: 1 });
    await new Promise((r) => setTimeout(r, 0));
    expect(posted().filter((m) => m.type === "pastCases").length).toBe(1);
  });

  it("answers a failed listing with the error, and a procedure without the citations that failed", async () => {
    const { panel, posted } = opened({ pastCases: async () => { throw new Error("db down"); } });
    await searchReady(panel);
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: [] } });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "pastCases", filter: { techniques: [] }, rows: [], total: 0, error: "db down" }));
    panel.webview.receive({ type: "inspector.open", target: { kind: "sop", sopId: "sop-1" } });
    await vi.waitFor(() => expect(posted().find((m) => m.type === "inspector.procedure")).toMatchObject({ sopId: "sop-1", citations: undefined, reads: undefined, outline: { revision: 4 } })); // the outline is the body's revision
  });

  it("re-reads what is on screen after an import, under the last filter asked for", async () => {
    const pastCases = vi.fn(async () => ({ rows: [], total: 0 }));
    const { view, panel, posted } = opened({ pastCases });
    await searchReady(panel);
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1078"] } });
    await vi.waitFor(() => expect(pastCases).toHaveBeenCalledTimes(1));
    view.refresh();
    await vi.waitFor(() => expect(pastCases).toHaveBeenCalledTimes(2));
    expect(pastCases).toHaveBeenLastCalledWith({ techniques: ["T1078"] });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "pastCases.corpus")).toBe(true));
  });

  it("says a reference from a document's text cannot open here, rather than leaving the pane waiting", async () => {
    const { panel, posted } = opened({});
    panel.webview.receive({ type: "inspector.open", target: { kind: "ref", ref: "x-host--1" } });
    await vi.waitFor(() => expect(posted()).toContainEqual(expect.objectContaining({ type: "inspector.error", target: "x-host--1" })));
  });

  it("reads a listing asked for before the page's ready only after the handshake (adr/0244)", async () => {
    const pastCases = vi.fn(async () => ({ rows: [], total: 0 }));
    const incompatible = opened({ pastCases }, "backend API v9 unsupported");
    incompatible.panel.webview.receive({ type: "pastCases.get", filter: { techniques: [] } });
    incompatible.panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(incompatible.posted()).toContainEqual({ type: "error", message: "backend API v9 unsupported" }));
    expect(pastCases).not.toHaveBeenCalled();
    incompatible.panel.dispose();
    const later = vi.fn(async () => ({ rows: [], total: 0 }));
    const { panel, posted } = opened({ pastCases: later });
    panel.webview.receive({ type: "knowledge.showing" });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1"] } });
    expect(later).not.toHaveBeenCalled();
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "pastCases", filter: { techniques: ["T1"] }, rows: [], total: 0 }));
  });

  it("keeps only the latest ask while the handshake fails, and a retry reads that one (adr/0244)", async () => {
    let up = false;
    const pastCases = vi.fn(async () => ({ rows: [], total: 0 }));
    const client = { status: async () => { if (!up) throw new Error("backend down"); return {}; }, incompatibilityReason: () => null, knowledge: knowledge({ pastCases }) } as unknown as BackendClient;
    const view = new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog());
    view.open({ type: "knowledge.search" });
    const panel = createdPanels().at(-1)!;
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: [] } });
    await searchReady(panel);
    expect(panel.webview.posted).toContainEqual({ type: "error", message: "backend down" });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1078"] } }); // the analyst filters meanwhile
    expect(pastCases).not.toHaveBeenCalled();
    up = true;
    panel.webview.receive({ type: "refresh" }); // Retry
    await vi.waitFor(() => expect(panel.webview.posted).toContainEqual({ type: "pastCases", filter: { techniques: ["T1078"] }, rows: [], total: 0 }));
    expect(pastCases.mock.calls).toEqual([[{ techniques: ["T1078"] }]]);
  });

  it("reads nothing for the search page while an item is shown, and once when it is (adr/0346)", async () => {
    const caseList = vi.fn(async () => ({ items: [], total: 3 }));
    const caseFacets = vi.fn(async () => ({ techniques: [], dispositions: [] }));
    const pastCases = vi.fn(async () => ({ rows: [], total: 0 }));
    const client = { status: async () => ({}), incompatibilityReason: () => null, knowledge: knowledge({ caseList, caseFacets, pastCases }) } as unknown as BackendClient;
    const view = new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog());
    view.open({ type: "knowledge.show", target: { kind: "sop", sopId: "sop-1" } });
    const panel = createdPanels().at(-1)!;
    panel.webview.receive({ type: "knowledge.showing", target: { kind: "sop", sopId: "sop-1" } });
    panel.webview.receive({ type: "ready" });
    await new Promise((r) => setTimeout(r, 0));
    view.refresh(); // the library changed while the item is shown
    await new Promise((r) => setTimeout(r, 0));
    expect(caseList).not.toHaveBeenCalled();
    expect(caseFacets).not.toHaveBeenCalled();
    expect(pastCases).not.toHaveBeenCalled();
    // The search page, then an item and back: read once.
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: [] } });
    panel.webview.receive({ type: "knowledge.showing" });
    panel.webview.receive({ type: "knowledge.showing", target: { kind: "case", investigationRef: "grouping--a" } });
    panel.webview.receive({ type: "knowledge.showing" });
    await new Promise((r) => setTimeout(r, 0));
    expect([caseList.mock.calls.length, caseFacets.mock.calls.length, pastCases.mock.calls.length]).toEqual([1, 1, 1]);
  });

  it("sends no corpus total when the case read failed, so the page claims no corpus size", async () => {
    const { panel, posted } = opened({ caseList: async () => { throw new Error("down"); } });
    await searchReady(panel);
    await vi.waitFor(() => expect(posted().some((m) => m.type === "pastCases.facets")).toBe(true));
    await new Promise((r) => setTimeout(r, 0));
    expect(posted().some((m) => m.type === "pastCases.corpus")).toBe(false);
  });

  it("opens in the active group and takes the focus; an open tab is revealed where it is, never a second (adr/0346)", () => {
    const { view, panel } = opened({});
    expect(vscode.window.createWebviewPanel).toHaveBeenLastCalledWith("reckon.knowledge", "Knowledge", vscode.ViewColumn.Active, expect.anything());
    expect(panel.active).toBe(true);
    panel.viewColumn = vscode.ViewColumn.Two; // dragged to another group
    panel.setActive(false);
    view.open({ type: "knowledge.show", target: { kind: "sop", sopId: "sop-1" } });
    expect(panel.reveals.at(-1)).toEqual({ column: vscode.ViewColumn.Two, preserveFocus: undefined });
    expect(panel.active).toBe(true);
    expect(createdPanels().length).toBe(1);
  });

  it("opens on the item the sidebar named: once the new view is ready, and at once on the open one (adr/0344)", async () => {
    const client = { status: async () => ({}), incompatibilityReason: () => null, knowledge: knowledge({}) } as unknown as BackendClient;
    const view = new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog());
    view.open({ type: "knowledge.show", target: { kind: "sop", sopId: "sop-1" } });
    const panel = createdPanels().at(-1)!;
    const cues = () => (panel.webview.posted as { type: string }[]).filter((m) => m.type.startsWith("knowledge."));
    expect(cues()).toEqual([]); // the page cannot hear it yet
    panel.webview.receive({ type: "ready" });
    expect(cues()).toEqual([{ type: "knowledge.show", target: { kind: "sop", sopId: "sop-1" } }]);
    panel.webview.receive({ type: "refresh" });
    expect(cues().length).toBe(1); // heard once, not on every reload
    view.open({ type: "knowledge.search" });
    expect(createdPanels().length).toBe(1);
    expect(panel.revealed).toBe(1);
    expect(cues().at(-1)).toEqual({ type: "knowledge.search" });
  });

  it("posts only the latest cue when two arrive before the page is ready (adr/0344)", () => {
    const client = { status: async () => ({}), incompatibilityReason: () => null, knowledge: knowledge({}) } as unknown as BackendClient;
    const view = new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog());
    view.open({ type: "knowledge.show", target: { kind: "sop", sopId: "sop-1" } });
    view.open({ type: "knowledge.show", target: { kind: "case", investigationRef: "grouping--a" } });
    const panel = createdPanels().at(-1)!;
    const cues = () => (panel.webview.posted as { type: string }[]).filter((m) => m.type.startsWith("knowledge."));
    expect(cues()).toEqual([]); // nothing posted to a page that cannot hear it
    panel.webview.receive({ type: "ready" });
    expect(cues()).toEqual([{ type: "knowledge.show", target: { kind: "case", investigationRef: "grouping--a" } }]);
  });

  it("reads nothing on a library change while no view is open", async () => {
    const caseList = vi.fn(async () => ({ items: [], total: 0 }));
    const client = { status: async () => ({}), incompatibilityReason: () => null, knowledge: knowledge({ caseList }) } as unknown as BackendClient;
    new KnowledgeDocument(vscode.Uri.file("/ext"), client, fakeLog()).refresh();
    await new Promise((r) => setTimeout(r, 0));
    expect(caseList).not.toHaveBeenCalled();
  });

  it("opens an investigation a case names", () => {
    const { panel } = opened({});
    panel.webview.receive({ type: "investigation.open", id: "inv-1", title: "T" });
    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.openInvestigation", "inv-1", "T");
  });
});
