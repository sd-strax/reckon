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
  view.open();
  const panel = createdPanels().at(-1)!;
  return { view, panel, posted: () => panel.webview.posted as { type: string }[] };
}

describe("KnowledgeDocument (adr/0107)", () => {
  it("reads the library and the facets on ready, after the handshake", async () => {
    const { panel, posted } = opened({});
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted().map((m) => m.type).sort()).toEqual(["library", "pastCases.facets"]));
  });

  it("shows why and reads nothing from an incompatible backend", async () => {
    const procedures = vi.fn();
    const { panel, posted } = opened({ procedures }, "backend API v9 unsupported");
    panel.webview.receive({ type: "ready" });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "error", message: "backend API v9 unsupported" }));
    expect(procedures).not.toHaveBeenCalled();
  });

  it("answers the latest filter only: an older answer landing late is dropped", async () => {
    let slow!: (v: unknown) => void;
    const pastCases = vi.fn()
      .mockImplementationOnce(() => new Promise((r) => { slow = r; }))
      .mockImplementationOnce(async () => ({ rows: [], total: 7 }));
    const { panel, posted } = opened({ pastCases });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1"] } });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T2"] } });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "pastCases", filter: { techniques: ["T2"] }, rows: [], total: 7 }));
    slow({ rows: [], total: 1 });
    await new Promise((r) => setTimeout(r, 0));
    expect(posted().filter((m) => m.type === "pastCases").length).toBe(1);
  });

  it("answers a failed listing with the error, and a procedure without the citations that failed", async () => {
    const { panel, posted } = opened({ pastCases: async () => { throw new Error("db down"); } });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: [] } });
    await vi.waitFor(() => expect(posted()).toContainEqual({ type: "pastCases", filter: { techniques: [] }, rows: [], total: 0, error: "db down" }));
    panel.webview.receive({ type: "inspector.open", target: { kind: "sop", sopId: "sop-1" } });
    await vi.waitFor(() => expect(posted().find((m) => m.type === "inspector.procedure")).toMatchObject({ sopId: "sop-1", citations: undefined, reads: undefined, outline: { revision: 4 } })); // the outline is the body's revision
  });

  it("re-reads what is on screen after an import, under the last filter asked for", async () => {
    const pastCases = vi.fn(async () => ({ rows: [], total: 0 }));
    const { view, panel, posted } = opened({ pastCases });
    panel.webview.receive({ type: "pastCases.get", filter: { techniques: ["T1078"] } });
    await vi.waitFor(() => expect(pastCases).toHaveBeenCalledTimes(1));
    view.refresh();
    await vi.waitFor(() => expect(pastCases).toHaveBeenCalledTimes(2));
    expect(pastCases).toHaveBeenLastCalledWith({ techniques: ["T1078"] });
    await vi.waitFor(() => expect(posted().some((m) => m.type === "library")).toBe(true));
  });

  it("says a reference from a document's text cannot open here, rather than leaving the pane waiting", async () => {
    const { panel, posted } = opened({});
    panel.webview.receive({ type: "inspector.open", target: { kind: "ref", ref: "x-host--1" } });
    await vi.waitFor(() => expect(posted()).toContainEqual(expect.objectContaining({ type: "inspector.error", target: "x-host--1" })));
  });

  it("reports a failed procedures read as a failure, not an empty library", async () => {
    const { panel, posted } = opened({ procedures: async () => { throw new Error("HTTP 500"); } });
    panel.webview.receive({ type: "library.get" });
    await vi.waitFor(() => expect(posted().find((m) => m.type === "library")).toMatchObject({ sops: [], sopsError: "HTTP 500" }));
  });

  it("sends the corpus total beside the library rows, and none when the case read failed", async () => {
    const { panel, posted } = opened({ caseList: async () => ({ items: [], total: 150 }) });
    panel.webview.receive({ type: "library.get" });
    await vi.waitFor(() => expect(posted().find((m) => m.type === "library")).toMatchObject({ casesTotal: 150 }));
    const failed = opened({ caseList: async () => { throw new Error("down"); } });
    failed.panel.webview.receive({ type: "library.get" });
    await vi.waitFor(() => expect(failed.posted().find((m) => m.type === "library")).toMatchObject({ cases: [], casesTotal: undefined }));
  });

  it("opens an investigation a case names, and imports through the command", () => {
    const { panel } = opened({});
    panel.webview.receive({ type: "investigation.open", id: "inv-1", title: "T" });
    panel.webview.receive({ type: "library.import" });
    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.openInvestigation", "inv-1", "T");
    expect(vscode.commands.executeCommand).toHaveBeenCalledWith("reckon.importSOP");
  });
});
