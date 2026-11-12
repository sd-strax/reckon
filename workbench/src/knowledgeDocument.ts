// The Knowledge tab (adr/0107, adr/0346, ui/09 §9.1): one document outside
// any investigation, a top-level tab whose page is one procedure or past case,
// or the search page (the query bar, its results, and the filtered past
// cases). It shows knowledge only and records nothing in any investigation; a
// past case names the investigation it came from as a link, which opens that
// investigation's own document.
//
// The page is the bundle's `knowledge` surface (webview/src/surfaces/
// KnowledgeApp.tsx); this host answers its subset of the messages. The
// sidebar's Knowledge view (adr/0344) is its only navigation: it opens the tab
// on one item or on the search page.

import * as vscode from "vscode";
import type { BackendClient } from "./backend";
import { caseAnswer, procedureAnswer } from "./knowledgeAnswers";
import { panelHtml, panelOptions } from "./panelHtml";
import type { InspectorTarget, KnowledgeCue, KnowledgeInboundMessage, KnowledgeRenderMessage, KnowledgeTarget } from "./protocol";
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
  // The listing the page asked for before a handshake succeeded: only the
  // latest ask is kept, and it is read once one does (adr/0244).
  private heldFilter: PastCaseFilter | undefined;
  // Whether the open panel's handshake has succeeded: until then nothing is read.
  private handshakeOk = false;
  // The page the open panel shows (adr/0346): its item, or the search page.
  // Only what the shown page uses is read.
  private showing: KnowledgeTarget | "search" | undefined;
  // The search page's corpus total and facets, and its listing, need reading
  // when it is next shown: set on a load and a refresh, cleared once read.
  private searchStale = true;
  private listingStale = true;
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

  /**
   * The library changed (an import, a procedure added, a case concluded), or
   * the tab came back into view: re-read what the shown page uses; the search
   * page, if not shown, is read when it next is.
   */
  refresh(): void {
    const panel = this.panel;
    if (!panel || !this.handshakeOk) return;
    this.searchStale = true;
    this.listingStale = true;
    if (this.showing && this.showing !== "search") void this.answerInspector(panel, this.showing);
    else this.readShown(panel);
  }

  /** Read what of the search page's corpus total, facets and listing is stale, if it is the page shown. */
  private readShown(panel: vscode.WebviewPanel): void {
    if (panel !== this.panel || !this.handshakeOk || this.showing !== "search") return;
    if (this.searchStale) {
      this.searchStale = false;
      void this.postCorpus(panel);
      void this.postFacets(panel);
    }
    if (this.listingStale && this.lastFilter) void this.postPastCases(panel, this.lastFilter);
  }

  /**
   * Open (or reveal) the one Knowledge tab on one item or on the search page
   * (adr/0346). It opens in the active editor group and takes the focus; an
   * open tab is revealed where it is. A tab just created hears the cue once
   * it is ready.
   */
  open(cue: KnowledgeCue): void {
    if (this.panel) {
      this.panel.reveal(this.panel.viewColumn);
      if (this.pageReady) this.post(this.panel, cue);
      else this.pendingCue = cue;
      return;
    }
    this.pageReady = false;
    this.pendingCue = cue;
    this.handshakeOk = false;
    this.showing = undefined;
    this.searchStale = true;
    this.listingStale = true;
    this.heldFilter = undefined;
    this.lastFilter = undefined;
    const panel = vscode.window.createWebviewPanel("reckon.knowledge", "Knowledge", vscode.ViewColumn.Active, panelOptions(this.extensionUri));
    this.panel = panel;
    panel.webview.html = panelHtml(panel.webview, this.extensionUri, "knowledge", "Knowledge");
    panel.webview.onDidReceiveMessage((msg: KnowledgeInboundMessage) => this.route(panel, msg));
    panel.onDidDispose(() => {
      if (this.panel === panel) {
        this.panel = undefined;
        this.pendingCue = undefined;
        this.heldFilter = undefined;
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
      case "library.search":
        void this.search(panel, msg.text);
        return;
      case "pastCases.get":
        // The search page asks for its listing as it mounts, before its
        // ready: an ask before a handshake succeeded is held, the latest
        // replacing any before it, and read by load.
        if (panel === this.panel && !this.handshakeOk) this.heldFilter = msg.filter;
        else void this.postPastCases(panel, msg.filter);
        return;
      case "knowledge.showing":
        if (panel !== this.panel) return;
        this.showing = msg.target ?? "search";
        this.readShown(panel);
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
        if (panel === this.panel) this.handshakeOk = false;
        this.post(panel, { type: "error", message: incompatible });
        return;
      }
    } catch (err) {
      if (generation !== this.generation) return;
      if (panel === this.panel) this.handshakeOk = false;
      this.post(panel, { type: "error", message: errText(err) });
      return;
    }
    if (panel !== this.panel) return;
    this.handshakeOk = true;
    // A held ask is the latest one: it is what the page shows.
    if (this.heldFilter) this.lastFilter = this.heldFilter;
    this.heldFilter = undefined;
    this.searchStale = true;
    this.listingStale = true;
    this.readShown(panel);
  }

  /** A guard for one list: true while this request is still the latest of its kind and the identity has not changed. */
  private begin(kind: string): () => boolean {
    const n = (this.requests.get(kind) ?? 0) + 1;
    this.requests.set(kind, n);
    const generation = this.generation;
    return () => this.requests.get(kind) === n && this.generation === generation;
  }

  /** How many past cases there are in all, for "n of m match"; a failed read sends nothing, so the page claims no corpus size. */
  private async postCorpus(panel: vscode.WebviewPanel): Promise<void> {
    const latest = this.begin("corpus");
    try {
      const { total } = await this.client.knowledge.caseList();
      if (latest()) this.post(panel, { type: "pastCases.corpus", total });
    } catch (err) {
      this.log.debug(`knowledge view cases: ${errText(err)}`);
    }
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
    this.listingStale = false;
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
