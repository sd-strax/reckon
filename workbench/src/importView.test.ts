// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BackendClient } from "./backend";
import { ImportSOPsView } from "./importView";
import { createdPanels, fakeLog, resetVscodeFake } from "./test/vscode";

const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  resetVscodeFake();
  vi.clearAllMocks();
});

async function viewWithTwoFiles(importSOPMarkdown: () => Promise<unknown>) {
  const view = new ImportSOPsView(vscode.Uri.file("/ext"), { importSOPMarkdown } as unknown as BackendClient, fakeLog());
  vi.mocked(vscode.window.showOpenDialog).mockResolvedValueOnce([vscode.Uri.file("/a.md"), vscode.Uri.file("/b.md")]);
  view.open();
  createdPanels()[0].webview.receive({ type: "addFiles" });
  await settle();
  return view;
}

describe("ImportSOPsView: a change of identity", () => {
  it("stops an import: the rest of the selection is not written on the next person's token", async () => {
    let land!: (r: unknown) => void;
    const importSOPMarkdown = vi.fn((): Promise<unknown> => new Promise((r) => { land = r; }));
    const view = await viewWithTwoFiles(importSOPMarkdown);
    createdPanels()[0].webview.receive({ type: "import" });
    await vi.waitFor(() => expect(importSOPMarkdown).toHaveBeenCalledOnce());

    view.identityChanged();
    land({ outcome: "created", title: "A" });
    await settle();

    expect(importSOPMarkdown).toHaveBeenCalledOnce();
  });

  it("imports the whole selection otherwise", async () => {
    const importSOPMarkdown = vi.fn(async () => ({ outcome: "created", title: "A" }));
    const view = await viewWithTwoFiles(importSOPMarkdown);
    createdPanels()[0].webview.receive({ type: "import" });

    await vi.waitFor(() => expect(importSOPMarkdown).toHaveBeenCalledTimes(2));
    view.dispose();
  });

  it("does not add files picked before the change to the next person's panel", async () => {
    let pick!: (u: vscode.Uri[]) => void;
    vi.mocked(vscode.window.showOpenDialog).mockImplementationOnce(() => new Promise((r) => { pick = r; }));
    const view = new ImportSOPsView(vscode.Uri.file("/ext"), {} as BackendClient, fakeLog());
    view.open();
    createdPanels()[0].webview.receive({ type: "addFiles" });

    view.identityChanged();
    pick([vscode.Uri.file("/alice.md")]);
    await settle();

    view.open();
    expect(JSON.stringify(createdPanels()[1].webview.posted)).not.toContain("alice.md");
  });
});
