// The case document (adr/0030-cases-pane.md): one system-of-record case,
// read-only, with the one act the analyst can take on it here — start an
// investigation rooted on it (14 §3), or open the one already rooted on it.
// Nothing on this surface writes to the case system: a write is an x-action
// inside an investigation, and before the investigation exists there is
// nothing to record it against.

import * as vscode from "vscode";
import { BackendClient, CaseRow, InvestigationDetail } from "./backend";

type Outbound =
  | { type: "case"; case: CaseRow }
  | { type: "error"; message: string }
  | { type: "starting" };

type Inbound =
  | { type: "ready" }
  | { type: "start" }
  | { type: "open"; id: string; title?: string }
  | { type: "link"; url: string }
  | { type: "refresh" };

export class CaseDocuments {
  private readonly panels = new Map<string, vscode.WebviewPanel>();
  private readonly starting = new Set<string>();

  constructor(
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
    // Called with the created investigation: the extension opens it, applies
    // the case write-back posture (adr/0032) and refreshes the trees.
    private readonly onInvestigationCreated: (created: InvestigationDetail) => void,
  ) {}

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
      { enableScripts: true, retainContextWhenHidden: true },
    );
    this.panels.set(number, panel);
    panel.webview.html = this.html(panel.webview);
    panel.webview.onDidReceiveMessage((msg: Inbound) => this.route(number, panel, msg));
    panel.onDidDispose(() => this.panels.delete(number));
  }

  private route(number: string, panel: vscode.WebviewPanel, msg: Inbound): void {
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
      case "link":
        if (/^https?:\/\//i.test(msg.url)) {
          void vscode.env.openExternal(vscode.Uri.parse(msg.url));
        }
        return;
    }
  }

  private async load(number: string, panel: vscode.WebviewPanel): Promise<void> {
    try {
      const c = await this.client.caseDetail(number);
      void panel.webview.postMessage({ type: "case", case: c } satisfies Outbound);
    } catch (err) {
      void panel.webview.postMessage({ type: "error", message: errText(err) } satisfies Outbound);
    }
  }

  /**
   * Root a new investigation on the case — the same server flow the picker
   * uses (seed_input kind "case"), which fails closed on anything short of
   * exactly one case. Guarded against a double click; on success the
   * investigation opens and this document reloads, now pointing at it.
   */
  private async start(number: string, panel: vscode.WebviewPanel): Promise<void> {
    if (this.starting.has(number)) {
      return;
    }
    this.starting.add(number);
    void panel.webview.postMessage({ type: "starting" } satisfies Outbound);
    try {
      const created = await this.client.createInvestigationFromInput(number, "case");
      this.log.info(`case ${number}: investigation ${created.id} started`);
      this.onInvestigationCreated(created);
      void this.load(number, panel);
    } catch (err) {
      void panel.webview.postMessage({ type: "error", message: errText(err) } satisfies Outbound);
    } finally {
      this.starting.delete(number);
    }
  }

  private html(webview: vscode.Webview): string {
    const nonce = makeNonce();
    const csp = [
      `default-src 'none'`,
      `style-src ${webview.cspSource} 'unsafe-inline'`,
      `script-src 'nonce-${nonce}'`,
    ].join("; ");
    return /* html */ `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <style>
    :root { --fg: var(--vscode-foreground); --muted: var(--vscode-descriptionForeground);
            --border: var(--vscode-panel-border, rgba(128,128,128,.35)); --accent: #6366f1; }
    body { margin: 0; padding: 24px 28px; color: var(--fg); background: var(--vscode-editor-background);
           font: 13px/1.5 var(--vscode-font-family); max-width: 880px; }
    .num { font-family: var(--vscode-editor-font-family); color: var(--muted); font-size: 12px; }
    h1 { font-size: 18px; font-weight: 600; margin: 4px 0 10px; }
    .meta { display: flex; flex-wrap: wrap; gap: 6px 14px; color: var(--muted); margin-bottom: 18px; }
    .meta b { color: var(--fg); font-weight: 500; }
    .flag { border-left: 3px solid var(--accent); padding: 8px 12px; margin: 0 0 18px;
            background: color-mix(in srgb, var(--fg) 6%, transparent); }
    .flag a { color: var(--vscode-textLink-foreground); cursor: pointer; }
    pre { white-space: pre-wrap; font: inherit; margin: 0 0 22px; padding: 14px 16px;
          border: 1px solid var(--border); border-radius: 6px; }
    .actions { display: flex; align-items: center; gap: 14px; }
    button { font: inherit; padding: 6px 14px; border-radius: 4px; cursor: pointer; border: 1px solid transparent;
             background: var(--vscode-button-background); color: var(--vscode-button-foreground); }
    button:disabled { opacity: .6; cursor: default; }
    button.secondary { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); }
    .linklike { background: none; border: none; padding: 0; color: var(--vscode-textLink-foreground); text-decoration: underline; }
    .err { color: var(--vscode-errorForeground); margin-top: 12px; }
    .hint { color: var(--muted); font-style: italic; }
  </style>
</head>
<body>
  <div id="root"><p class="hint">Reading the case…</p></div>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
    let current = null, starting = false;
    function render() {
      const root = document.getElementById("root");
      if (!current) return;
      const c = current;
      const meta = [
        c.priority ? "<span><b>" + esc(c.priority) + "</b></span>" : "",
        c.status ? "<span>" + esc(c.status) + "</span>" : "",
        c.assignmentGroup ? "<span>" + esc(c.assignmentGroup) + "</span>" : "",
        c.openedAt ? "<span>opened " + esc(new Date(c.openedAt).toLocaleString()) + "</span>" : "",
        c.source ? "<span>source " + esc(c.source) + "</span>" : "",
        c.link ? '<span><button class="linklike" id="link">open in the system of record</button></span>' : "",
      ].filter(Boolean).join("");
      const inv = c.investigation;
      const flag = inv
        ? '<div class="flag">Under investigation: <a id="openInv">' + esc(inv.title) + "</a> <span class=\\"hint\\">(" + esc(inv.status) + ")</span></div>"
        : "";
      const action = inv
        ? '<button id="open">Open investigation</button><span class="hint">A second investigation on this case can still be started from New Investigation → From a case…</span>'
        : '<button id="start"' + (starting ? " disabled" : "") + ">" + (starting ? "Starting…" : "Start investigation") + '</button><span class="hint">Roots a new investigation on this case; the case body is treated as a claim to verify.</span>';
      root.innerHTML =
        '<div class="num">' + esc(c.number) + "</div>" +
        "<h1>" + esc(c.title || c.number) + "</h1>" +
        '<div class="meta">' + meta + "</div>" + flag +
        "<pre>" + esc(c.summary || "(no description)") + "</pre>" +
        '<div class="actions">' + action + "</div>" +
        '<div class="err" id="err"></div>';
      const start = document.getElementById("start");
      if (start) start.addEventListener("click", () => { if (starting) return; vscode.postMessage({ type: "start" }); });
      const open = document.getElementById("open");
      if (open && inv) open.addEventListener("click", () => vscode.postMessage({ type: "open", id: inv.id, title: inv.title }));
      const openInv = document.getElementById("openInv");
      if (openInv && inv) openInv.addEventListener("click", () => vscode.postMessage({ type: "open", id: inv.id, title: inv.title }));
      const link = document.getElementById("link");
      if (link) link.addEventListener("click", () => vscode.postMessage({ type: "link", url: c.link }));
    }
    window.addEventListener("message", (e) => {
      const m = e.data;
      if (m.type === "case") { current = m.case; starting = false; render(); }
      else if (m.type === "starting") { starting = true; render(); }
      else if (m.type === "error") {
        starting = false;
        if (current) { render(); document.getElementById("err").textContent = m.message; }
        else document.getElementById("root").innerHTML = '<p class="err">' + esc(m.message) + '</p><p><button class="secondary" id="retry">Retry</button></p>',
          document.getElementById("retry").addEventListener("click", () => vscode.postMessage({ type: "refresh" }));
      }
    });
    vscode.postMessage({ type: "ready" });
  </script>
</body>
</html>`;
  }
}

function makeNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
