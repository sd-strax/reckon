// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BackendRequestError, type BackendClient, type InvestigationDetail } from "./backend";
import { CaseDocuments } from "./caseDocument";
import { createdPanels, fakeLog, resetVscodeFake } from "./test/vscode";

const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  resetVscodeFake();
  vi.clearAllMocks();
});

describe("CaseDocuments: a change of identity", () => {
  const created = { id: "inv-1", title: "T" } as InvestigationDetail;

  function started(createInvestigationFromInput: () => Promise<InvestigationDetail>) {
    const onCreated = vi.fn();
    const client = {
      status: async () => ({}), incompatibilityReason: () => null,
      investigations: { createFromInput: createInvestigationFromInput }, cases: { get: async () => ({ number: "C-1" }) },
    } as unknown as BackendClient;
    const docs = new CaseDocuments(vscode.Uri.file("/ext"), client, fakeLog(), onCreated);
    docs.open("C-1");
    createdPanels()[0].webview.receive({ type: "start" });
    return { docs, onCreated };
  }

  it("opens nothing when a start lands after the change", async () => {
    let land!: (d: InvestigationDetail) => void;
    const { docs, onCreated } = started(() => new Promise((r) => { land = r; }));

    docs.closeAll();
    land(created);
    await settle();

    expect(onCreated).not.toHaveBeenCalled();
  });

  it("opens nothing when a start that fails after the change finds the case investigated", async () => {
    let fail!: (e: BackendRequestError) => void;
    const { docs } = started(() => new Promise((_, r) => { fail = r; }));

    docs.closeAll();
    fail(new BackendRequestError("exists", 409, { investigation_id: "inv-9" }));
    await settle();

    expect(vscode.commands.executeCommand).not.toHaveBeenCalled();
  });

  it("still opens what a start creates with the same person signed in", async () => {
    const { onCreated } = started(async () => created);

    await vi.waitFor(() => expect(onCreated).toHaveBeenCalledWith(created));
  });
});

describe("CaseDocuments: an incompatible backend", () => {
  it("shows why, and does not read the case", async () => {
    const get = vi.fn();
    const client = { status: async () => ({}), incompatibilityReason: () => "backend API v9 unsupported", cases: { get } } as unknown as BackendClient;
    new CaseDocuments(vscode.Uri.file("/ext"), client, fakeLog(), vi.fn()).open("C-1");

    createdPanels()[0].webview.receive({ type: "ready" });

    await vi.waitFor(() => expect(createdPanels()[0].webview.posted).toContainEqual({ type: "error", message: "backend API v9 unsupported" }));
    expect(get).not.toHaveBeenCalled();
  });
});
