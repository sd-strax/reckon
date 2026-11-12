// The case document (adr/0030-cases-pane.md): one system-of-record case,
// read-only, with the one act the analyst can take on it here — start an
// investigation rooted on it (14 §3), or open the one already rooted on it.
// It does not link out: the document IS the read, and the case number is
// enough for anyone who needs the SoR's own page.
// Nothing on this surface writes to the case system: a write is an x-action
// inside an investigation, and before the investigation exists there is
// nothing to record it against.
//
// The page is the bundle's `case` surface (webview/src/surfaces/CaseApp.tsx);
// this host reads the case, starts the investigation and routes the clicks.

import * as vscode from "vscode";
import { BackendClient, InvestigationDetail, existingInvestigation } from "./backend";
import { CaseStarts } from "./caseStarts";
import { panelHtml, panelOptions } from "./panelHtml";
import type { CaseInboundMessage, CaseRenderMessage } from "./protocol";
import { errText } from "./util";

export class CaseDocuments {
  private readonly panels = new Map<string, vscode.WebviewPanel>();
  // Bumped when the identity changes: a start begun before it opens nothing.
  private generation = 0;

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
    // Called with the created investigation: the extension opens it, applies
    // the case write-back posture (adr/0032) and refreshes the trees.
    private readonly onInvestigationCreated: (created: InvestigationDetail) => void,
    // The one start from a case (adr/0342): its in-flight guard is shared with
    // the sidebar's and the draft's starts, and any start of a case shown here
    // re-reads it, so the page says Under investigation wherever it began.
    private readonly starts: CaseStarts = new CaseStarts(),
  ) {
    starts.onDidStart((number) => this.reloadIfOpen(number));
  }

  /** Re-read the case's document if it is open. */
  reloadIfOpen(number: string): void {
    const panel = this.panels.get(number);
    if (panel) {
      void this.load(number, panel);
    }
  }

  /** The person behind the session changed: a case on screen was read as someone else (adr/0241). */
  closeAll(): void {
    this.generation++;
    for (const panel of [...this.panels.values()]) {
      panel.dispose();
    }
  }

  /** Open (or reveal) the document for one case number and (re)load it. */
  open(number: string): void {
    const existing = this.panels.get(number);
    if (existing) {
      existing.reveal();
      void this.load(number, existing);
      return;
    }
    const panel = vscode.window.createWebviewPanel(
      "reckon.case",
      `Case ${number}`,
      vscode.ViewColumn.Active,
      panelOptions(this.extensionUri),
    );
    this.panels.set(number, panel);
    panel.webview.html = panelHtml(panel.webview, this.extensionUri, "case", `Case ${number}`);
    panel.webview.onDidReceiveMessage((msg: CaseInboundMessage) => this.route(number, panel, msg));
    panel.onDidDispose(() => this.panels.delete(number));
  }

  private route(number: string, panel: vscode.WebviewPanel, msg: CaseInboundMessage): void {
    switch (msg.type) {
      case "ready":
      case "refresh":
        void this.load(number, panel);
        return;
      case "start":
        void this.start(number, panel);
        return;
      case "open":
        void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
        return;
    }
  }

  private post(panel: vscode.WebviewPanel, msg: CaseRenderMessage): void {
    void panel.webview.postMessage(msg);
  }

  private async load(number: string, panel: vscode.WebviewPanel): Promise<void> {
    try {
      // The handshake, on every load (adr/0244): a backend upgraded under a
      // running window is caught here, before anything is read from it.
      const incompatible = this.client.incompatibilityReason(await this.client.status());
      if (incompatible) {
        this.post(panel, { type: "error", message: incompatible });
        return;
      }
      const c = await this.client.cases.get(number);
      this.post(panel, { type: "case", case: c });
    } catch (err) {
      this.post(panel, { type: "error", message: errText(err) });
    }
  }

  /**
   * Root a new investigation on the case — the same server flow the picker
   * uses (seed_input kind "case"), which fails closed on anything short of
   * exactly one case. Guarded against a double start from any surface (the
   * shared CaseStarts); on success the investigation opens and this document
   * reloads (through the start's notice), now pointing at it.
   */
  private async start(number: string, panel: vscode.WebviewPanel): Promise<void> {
    if (this.starts.inFlight(number)) {
      return;
    }
    const generation = this.generation;
    this.post(panel, { type: "starting" });
    try {
      const created = await this.starts.run(number, () => this.client.investigations.createFromInput(number, "case"));
      if (!created || generation !== this.generation) return;
      this.log.info(`case ${number}: investigation ${created.id} started`);
      this.onInvestigationCreated(created);
    } catch (err) {
      if (generation !== this.generation) return;
      // Already investigated (#158): open that investigation instead.
      const existing = existingInvestigation(err);
      if (existing) {
        void vscode.commands.executeCommand("reckon.openInvestigation", existing);
        return;
      }
      this.post(panel, { type: "error", message: errText(err) });
    }
  }
}
