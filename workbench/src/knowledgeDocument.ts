// The Knowledge view (adr/0107, ui/09 §9.1): one document outside any
// investigation, to browse and search the procedures and past cases, filter
// the past cases, read either in the inspector, and import. It shows
// knowledge only and records nothing in any investigation; a past case
// names the investigation it came from as a link, which opens that
// investigation's own document.
//
// The page is the bundle's `knowledge` surface (webview/src/surfaces/
// KnowledgeApp.tsx); this host answers its subset of the messages. The
// sidebar's Knowledge view (adr/0344) opens it on one item or with its query
// bar focused.

import * as vscode from "vscode";
import type { BackendClient } from "./backend";
import { caseAnswer, procedureAnswer } from "./knowledgeAnswers";
import { panelHtml, panelOptions } from "./panelHtml";
import type { InspectorTarget, KnowledgeCue, KnowledgeInboundMessage, KnowledgeRenderMessage } from "./protocol";
import type { PastCaseFilter } from "./viewTypes";
import { errText } from "./util";

export class KnowledgeDocument {
  private panel: vscode.WebviewPanel | undefined;
  // The latest request per list: an older answer landing after a newer one
  // is dropped, never posted over it (the filter the analyst sees is the last
  // one asked for).
  private readonly requests = new Map<string, number>();
  // Bumped when the identity changes: a read begun before it posts nothing.
  private generation = 0;
  // The filter the page last asked for, so a refresh re-reads what is on screen.
  private lastFilter: PastCaseFilter | undefined;
  // What the sidebar asked of a view not yet ready to hear it: posted on its ready.
  private pendingCue: KnowledgeCue | undefined;
  // Whether the open panel's page has said ready: until then a cue is held, the latest one kept.
  private pageReady = false;

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
  ) {}

  /** The person behind the session changed: what is on screen was read as someone else (adr/0241). */
  closeAll(): void {
    this.generation++;
    this.pendingCue = undefined;
    this.panel?.dispose();
  }

  /** The library changed (an import, a procedure added, a case concluded): re-read what the open view lists. */
  refresh(): void {
    const panel = this.panel;
    if (!panel) return;
    void this.postLibrary(panel);
    void this.postFacets(panel);
    if (this.lastFilter) void this.postPastCases(panel, this.lastFilter);
  }

  /**
   * Open (or reveal) the one Knowledge view; with a cue, on one item or with
   * its query bar focused. A view just created hears the cue once it is ready.
   *
   * `beside` is the column of the investigation that is the active editor
   * (adr/0344): the view never covers it. An open view in another group is
   * revealed where it is; otherwise it goes Beside. `focus` says whether the
   * view takes the keyboard (Search) or the investigation keeps it (a row).
   */
  open(cue?: KnowledgeCue, opts: { beside?: vscode.ViewColumn; focus?: boolean } = {}): void {
    const { beside, focus = false } = opts;
    if (this.panel) {
      if (beside === undefined) this.panel.reveal();
      else if (this.panel.viewColumn !== undefined && this.panel.viewColumn !== beside) this.panel.reveal(this.panel.viewColumn, !focus);
      else this.panel.reveal(vscode.ViewColumn.Beside, !focus);
      if (cue && this.pageReady) this.post(this.panel, cue);
      else if (cue) this.pendingCue = cue;
      return;
    }
    this.pageReady = false;
    this.pendingCue = cue;
    const column = beside !== undefined ? { viewColumn: vscode.ViewColumn.Beside, preserveFocus: !focus } : vscode.ViewColumn.Active;
    const panel = vscode.window.createWebviewPanel("reckon.knowledge", "Knowledge", column, panelOptions(this.extensionUri));
    this.panel = panel;
    panel.webview.html = panelHtml(panel.webview, this.extensionUri, "knowledge", "Knowledge");
    panel.webview.onDidReceiveMessage((msg: KnowledgeInboundMessage) => this.route(panel, msg));
    panel.onDidDispose(() => {
      if (this.panel === panel) {
        this.panel = undefined;
        this.pendingCue = undefined;
      }
    });
    // The corpus moves while the view is hidden (an investigation concludes,
    // a summary is written): coming back into view re-reads what it lists.
    panel.onDidChangeViewState((e) => {
      if (e.webviewPanel.visible) this.refresh();
    });
  }

  private route(panel: vscode.WebviewPanel, msg: KnowledgeInboundMessage): void {
    switch (msg.type) {
      case "ready":
      case "refresh": {
        if (panel === this.panel) this.pageReady = true;
        void this.load(panel);
        const cue = this.pendingCue;
        this.pendingCue = undefined;
        if (cue) this.post(panel, cue);
        return;
      }
      case "library.get":
        void this.postLibrary(panel);
        void this.postFacets(panel);
        return;
      case "library.search":
        void this.search(panel, msg.text);
        return;
      case "library.import":
        void vscode.commands.executeCommand("reckon.importSOP");
        return;
      case "pastCases.get":
        void this.postPastCases(panel, msg.filter);
        return;
      case "inspector.open":
        void this.answerInspector(panel, msg.target);
        return;
      case "investigation.open":
        void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
        return;
      case "copy":
        void vscode.env.clipboard.writeText(msg.text);
        return;
    }
  }

  private post(panel: vscode.WebviewPanel, msg: KnowledgeRenderMessage): void {
    void panel.webview.postMessage(msg);
  }

  /**
   * The handshake, on every load (adr/0244): a backend upgraded under a
   * running window is caught here, before anything is read from it.
   */
  private async load(panel: vscode.WebviewPanel): Promise<void> {
    const generation = this.generation;
    try {
      const incompatible = this.client.incompatibilityReason(await this.client.status());
      if (generation !== this.generation) return;
      if (incompatible) {
        this.post(panel, { type: "error", message: incompatible });
        return;
      }
    } catch (err) {
      if (generation === this.generation) this.post(panel, { type: "error", message: errText(err) });
      return;
    }
    void this.postLibrary(panel);
    void this.postFacets(panel);
  }

  /** A guard for one list: true while this request is still the latest of its kind and the identity has not changed. */
  private begin(kind: string): () => boolean {
    const n = (this.requests.get(kind) ?? 0) + 1;
    this.requests.set(kind, n);
    const generation = this.generation;
    return () => this.requests.get(kind) === n && this.generation === generation;
  }

  private async postLibrary(panel: vscode.WebviewPanel): Promise<void> {
    const latest = this.begin("library");
    let sopsError: string | undefined;
    const [sops, cases] = await Promise.all([
      this.client.knowledge.procedures().catch((err: unknown) => { sopsError = errText(err); this.log.debug(`knowledge view procedures: ${sopsError}`); return []; }),
      this.client.knowledge.caseList().catch((err: unknown) => { this.log.debug(`knowledge view cases: ${errText(err)}`); return undefined; }),
    ]);
    // A failed case read sends no total: the view then claims no corpus size.
    if (latest()) this.post(panel, { type: "library", sops, cases: cases?.items ?? [], casesTotal: cases?.total, sopsError });
  }

  private async postFacets(panel: vscode.WebviewPanel): Promise<void> {
    const latest = this.begin("facets");
    try {
      const facets = await this.client.knowledge.caseFacets();
      if (latest()) this.post(panel, { type: "pastCases.facets", facets });
    } catch (err) {
      // The filters then offer nothing to pick; the listing still reads.
      this.log.debug(`knowledge view facets: ${errText(err)}`);
    }
  }

  private async postPastCases(panel: vscode.WebviewPanel, filter: PastCaseFilter): Promise<void> {
    this.lastFilter = filter;
    const latest = this.begin("pastCases");
    try {
      const { rows, total } = await this.client.knowledge.pastCases(filter);
      if (latest()) this.post(panel, { type: "pastCases", filter, rows, total });
    } catch (err) {
      if (latest()) this.post(panel, { type: "pastCases", filter, rows: [], total: 0, error: errText(err) });
    }
  }

  private async search(panel: vscode.WebviewPanel, text: string): Promise<void> {
    const latest = this.begin("search");
    try {
      const found = await this.client.knowledge.search(text);
      if (latest()) this.post(panel, { type: "library.results", query: text, ...found });
    } catch (err) {
      void vscode.window.showWarningMessage(`reckon: knowledge search failed — ${errText(err)}`);
    }
  }

  /** The procedure and past-case panes; nothing else opens here. */
  private async answerInspector(panel: vscode.WebviewPanel, target: InspectorTarget): Promise<void> {
    const host = { client: this.client, log: this.log };
    const generation = this.generation;
    if (target.kind !== "sop" && target.kind !== "case") {
      // A reference in a document's text (an entity, an event) belongs to an
      // investigation's record; there is none here, and the pane says so
      // rather than waiting.
      const key = "ref" in target ? target.ref : "";
      if (key) this.post(panel, { type: "inspector.error", target: key, message: "Not available outside an investigation: open the investigation it belongs to." });
      return;
    }
    const key = target.kind === "sop" ? target.sopId : target.investigationRef;
    try {
      const answer = target.kind === "sop" ? await procedureAnswer(host, target.sopId) : await caseAnswer(host, target.investigationRef);
      if (generation === this.generation) this.post(panel, answer);
    } catch (err) {
      if (key && generation === this.generation) this.post(panel, { type: "inspector.error", target: key, message: errText(err) });
    }
  }
}
