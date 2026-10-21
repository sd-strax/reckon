// Import SOPs — a reckon-owned panel (design/06 §2.4, design/13 §3). Choosing
// WHAT becomes institutional knowledge is an operational decision, so it gets a
// visible surface the analyst can read, revise, and see the outcome of — not a
// quick-pick that evaporates on focus loss. The panel lists the markdown/text
// documents already open in the editor (what is imported is each buffer as
// shown, unsaved edits included — nothing is re-read behind the analyst's
// back), lets them add files from disk, and renders each document's result
// (created / revised / failed) in place. Attribution comes from each
// document's YAML frontmatter (title, author, tags, source), parsed server-side.

import * as vscode from "vscode";
import { BackendClient } from "./backend";

const SOP_EXTENSIONS = ["md", "markdown", "txt"];

/** One candidate document. `uri` is the identity; `fromDisk` rows were added
 * via the file dialog and are read from disk, open-editor rows from the buffer. */
interface Row {
  uri: vscode.Uri;
  name: string;
  dir: string;
  fromDisk: boolean;
  dirty: boolean;
  checked: boolean;
  result?: { outcome: "created" | "revised" | "failed"; title: string; message?: string };
}

export class ImportSOPsView {
  private panel?: vscode.WebviewPanel;
  private rows: Row[] = [];
  private disposables: vscode.Disposable[] = [];

  constructor(
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
  ) {}

  /** Open (or reveal) the panel and rescan the open editors. Disk rows added
   * earlier in this panel's life are kept until it is closed. */
  open(): void {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "reckon.importSOPs",
        "Import SOPs",
        { viewColumn: vscode.ViewColumn.Beside, preserveFocus: false },
        { enableScripts: true, retainContextWhenHidden: true },
      );
      this.panel.webview.html = this.html(this.panel.webview);
      this.panel.webview.onDidReceiveMessage((m: { type?: string; ids?: unknown; id?: unknown; checked?: unknown }) => {
        switch (m?.type) {
          case "ready":
          case "refresh":
            this.rescan();
            this.push();
            return;
          case "toggle":
            if (typeof m.id === "string") {
              const row = this.rows.find((r) => r.uri.toString() === m.id);
              if (row) {
                row.checked = m.checked === true;
              }
            }
            return;
          case "addFiles":
            void this.addFromDisk();
            return;
          case "import":
            void this.importSelected();
            return;
        }
      });
      // Tabs opening/closing while the panel is up: keep the list honest.
      this.disposables.push(
        vscode.window.tabGroups.onDidChangeTabs(() => {
          this.rescan();
          this.push();
        }),
      );
      this.panel.onDidDispose(() => {
        this.panel = undefined;
        this.rows = [];
        for (const d of this.disposables.splice(0)) {
          d.dispose();
        }
      });
    }
    this.rescan();
    this.panel.reveal(vscode.ViewColumn.Beside, false);
    this.push();
  }

  dispose(): void {
    this.panel?.dispose();
  }

  /** Rebuild the open-editor rows from the live tab set, preserving the
   * analyst's checkbox choices and results for rows that are still open. */
  private rescan(): void {
    const activeKey = vscode.window.activeTextEditor?.document.uri.toString();
    const prior = new Map(this.rows.map((r) => [r.uri.toString(), r]));
    const next: Row[] = [];
    const seen = new Set<string>();
    for (const group of vscode.window.tabGroups.all) {
      for (const tab of group.tabs) {
        if (!(tab.input instanceof vscode.TabInputText)) {
          continue;
        }
        const uri = tab.input.uri;
        const key = uri.toString();
        if (seen.has(key) || !isSOPFile(uri)) {
          continue;
        }
        seen.add(key);
        const old = prior.get(key);
        const parts = uri.path.split("/");
        const name = parts.pop() || tab.label;
        next.push({
          uri,
          name,
          dir: parts.join("/"),
          fromDisk: false,
          dirty: vscode.workspace.textDocuments.find((d) => d.uri.toString() === key)?.isDirty === true,
          // First sighting: pre-check the document the analyst is looking at.
          checked: old ? old.checked : key === activeKey,
          result: old?.result,
        });
      }
    }
    // Disk rows persist for the panel's lifetime unless the same file is now
    // also open in the editor, in which case the editor row (live buffer) wins.
    for (const r of this.rows) {
      if (r.fromDisk && !seen.has(r.uri.toString())) {
        next.push(r);
      }
    }
    this.rows = next;
  }

  private async addFromDisk(): Promise<void> {
    const uris = await vscode.window.showOpenDialog({
      title: "Add SOP documents",
      canSelectMany: true,
      openLabel: "Add",
      filters: { "Markdown / text": SOP_EXTENSIONS },
    });
    if (!uris || uris.length === 0) {
      return;
    }
    const have = new Set(this.rows.map((r) => r.uri.toString()));
    for (const uri of uris) {
      if (have.has(uri.toString())) {
        continue;
      }
      const parts = uri.path.split("/");
      this.rows.push({
        uri,
        name: parts.pop() || "untitled.md",
        dir: parts.join("/"),
        fromDisk: true,
        dirty: false,
        checked: true,
      });
    }
    this.push();
  }

  private async importSelected(): Promise<void> {
    const selected = this.rows.filter((r) => r.checked);
    if (selected.length === 0) {
      return;
    }
    this.push({ busy: true });
    let ok = 0;
    for (const row of selected) {
      try {
        const content = row.fromDisk
          ? Buffer.from(await vscode.workspace.fs.readFile(row.uri)).toString("utf8")
          // openTextDocument returns the live buffer when the file is open —
          // unsaved edits included — so what is imported is what is on screen.
          : (await vscode.workspace.openTextDocument(row.uri)).getText();
        const res = await this.client.importSOPMarkdown(row.name, content);
        row.result = { outcome: res.outcome === "created" ? "created" : "revised", title: res.title };
        row.checked = false;
        ok++;
        this.log.info(`imported SOP ${res.outcome}: ${res.title} (${row.name})`);
      } catch (err) {
        row.result = { outcome: "failed", title: row.name, message: err instanceof Error ? err.message : String(err) };
        this.log.warn(`SOP import failed: ${row.name}: ${row.result.message}`);
      }
      this.push({ busy: true });
    }
    this.push({ busy: false, done: { ok, failed: selected.length - ok } });
  }

  private push(extra: { busy?: boolean; done?: { ok: number; failed: number } } = {}): void {
    if (!this.panel) {
      return;
    }
    void this.panel.webview.postMessage({
      type: "state",
      busy: extra.busy === true,
      done: extra.done,
      rows: this.rows.map((r) => ({
        id: r.uri.toString(),
        name: r.name,
        dir: r.dir,
        fromDisk: r.fromDisk,
        dirty: r.dirty,
        checked: r.checked,
        result: r.result,
      })),
    });
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
    :root {
      --fg: var(--vscode-foreground);
      --bg: var(--vscode-editor-background);
      --mono: var(--vscode-editor-font-family, ui-monospace, monospace);
      --border: var(--vscode-panel-border, color-mix(in srgb, var(--fg) 16%, transparent));
      --muted: color-mix(in srgb, var(--fg) 55%, transparent);
      --fill: color-mix(in srgb, var(--fg) 5%, transparent);
      --accent: var(--vscode-textLink-foreground, #4aa8ff);
      --brand: #7371fc;
      --ok: #3fb950;
      --warn: #d29922;
      --bad: #e5534b;
    }
    * { box-sizing: border-box; }
    body { color: var(--fg); background: var(--bg); font-family: var(--vscode-font-family); font-size: 13px; padding: 14px 18px 40px; line-height: 1.5; }
    .kicker { font-size: 10px; font-weight: 700; letter-spacing: 0.09em; text-transform: uppercase; color: var(--muted); }
    h1 { font-size: 19px; margin: 2px 0 4px; font-weight: 600; }
    .sub { color: var(--muted); font-size: 12px; margin-bottom: 16px; max-width: 64ch; }
    section { margin: 0 0 18px; }
    section > .kicker { display: block; margin-bottom: 7px; }
    .row { display: grid; grid-template-columns: auto 1fr auto; gap: 4px 10px; align-items: start; border: 1px solid var(--border); border-radius: 5px; padding: 8px 11px; margin-bottom: 6px; }
    .row.done { border-left: 2px solid var(--ok); }
    .row.failed { border-left: 2px solid var(--bad); }
    .row input { margin: 4px 0 0; accent-color: var(--brand); }
    .row label { cursor: pointer; }
    .name { font-weight: 600; word-break: break-all; }
    .dir { color: var(--muted); font-family: var(--mono); font-size: 11px; word-break: break-all; }
    .tags { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
    .badge { display: inline-block; font-family: var(--mono); font-size: 10.5px; padding: 1px 6px; border: 1px solid var(--border); border-radius: 4px; background: var(--fill); white-space: nowrap; }
    .badge.unsaved { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 50%, var(--border)); }
    .badge.created, .badge.revised { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 50%, var(--border)); }
    .badge.failed { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 50%, var(--border)); }
    .result { grid-column: 2 / -1; font-size: 12px; color: var(--muted); }
    .result.failed { color: var(--bad); }
    .empty { color: var(--muted); font-style: italic; }
    .toolbar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-top: 6px; }
    button { font: inherit; font-size: 12px; color: var(--accent); background: none; border: 1px solid var(--border); border-radius: 4px; padding: 4px 11px; cursor: pointer; }
    button:hover { background: var(--fill); }
    button.primary { color: #fff; background: var(--brand); border-color: var(--brand); font-weight: 600; }
    button.primary:hover { filter: brightness(1.08); }
    button:disabled { opacity: 0.5; cursor: default; }
    .foot { border-top: 1px solid var(--border); margin-top: 14px; padding-top: 12px; display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
    .note { color: var(--muted); font-size: 12px; }
  </style>
</head>
<body>
  <div class="kicker">Knowledge · institutional SOPs</div>
  <h1>Import SOPs</h1>
  <p class="sub">Markdown or text, with optional YAML frontmatter (title, author, tags, source). Imported documents are consultable immediately on the knowledge rail. Re-importing a document with the same source revises it rather than duplicating it.</p>

  <section>
    <span class="kicker" id="open-kicker">Open in the editor</span>
    <div id="open"></div>
    <div class="toolbar"><button id="refresh">Rescan open editors</button></div>
  </section>

  <section>
    <span class="kicker">From disk</span>
    <div id="disk"></div>
    <div class="toolbar"><button id="add">Add files…</button></div>
  </section>

  <div class="foot">
    <button class="primary" id="import" disabled>Import</button>
    <span class="note" id="note"></span>
  </div>

  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    const $ = (id) => document.getElementById(id);
    let state = { rows: [], busy: false };

    function el(tag, cls, text) {
      const e = document.createElement(tag);
      if (cls) e.className = cls;
      if (text !== undefined) e.textContent = text;
      return e;
    }

    function rowEl(r) {
      const box = el("div", "row" + (r.result ? (r.result.outcome === "failed" ? " failed" : " done") : ""));
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.id = "cb-" + btoa(r.id).replace(/[^a-z0-9]/gi, "");
      cb.checked = r.checked;
      cb.disabled = state.busy;
      cb.addEventListener("change", () => {
        r.checked = cb.checked;
        vscode.postMessage({ type: "toggle", id: r.id, checked: cb.checked });
        updateFoot();
      });
      box.appendChild(cb);
      const lbl = el("label");
      lbl.htmlFor = cb.id;
      lbl.appendChild(el("div", "name", r.name));
      if (r.dir) lbl.appendChild(el("div", "dir", r.dir));
      box.appendChild(lbl);
      const tags = el("div", "tags");
      if (r.dirty) tags.appendChild(el("span", "badge unsaved", "unsaved edits · imported as shown"));
      if (r.result) tags.appendChild(el("span", "badge " + r.result.outcome, r.result.outcome));
      box.appendChild(tags);
      if (r.result) {
        const res = el("div", "result" + (r.result.outcome === "failed" ? " failed" : ""));
        res.textContent = r.result.outcome === "failed"
          ? (r.result.message || "import failed")
          : (r.result.outcome === "created" ? "Added to the SOP library as " : "Revised existing SOP ") + "“" + r.result.title + "”";
        box.appendChild(res);
      }
      return box;
    }

    function render() {
      const open = state.rows.filter((r) => !r.fromDisk);
      const disk = state.rows.filter((r) => r.fromDisk);
      const o = $("open"); o.textContent = "";
      if (!open.length) o.appendChild(el("p", "empty", "No markdown or text documents are open. Open one to import it as shown, or add files from disk below."));
      for (const r of open) o.appendChild(rowEl(r));
      $("open-kicker").textContent = "Open in the editor" + (open.length ? " · " + open.length : "");
      const d = $("disk"); d.textContent = "";
      if (!disk.length) d.appendChild(el("p", "empty", "No files added."));
      for (const r of disk) d.appendChild(rowEl(r));
      $("add").disabled = state.busy;
      $("refresh").disabled = state.busy;
      updateFoot();
    }

    function updateFoot() {
      const n = state.rows.filter((r) => r.checked).length;
      const b = $("import");
      b.disabled = state.busy || n === 0;
      b.textContent = state.busy ? "Importing…" : (n ? "Import " + n + " document" + (n === 1 ? "" : "s") : "Import");
      const note = $("note");
      if (state.done) {
        note.textContent = state.done.failed
          ? state.done.ok + " imported, " + state.done.failed + " failed — see each row."
          : state.done.ok + " imported — consultable now on the knowledge rail.";
      } else if (!state.busy) {
        note.textContent = n ? "" : "Select the documents to import.";
      } else {
        note.textContent = "";
      }
    }

    $("add").addEventListener("click", () => vscode.postMessage({ type: "addFiles" }));
    $("refresh").addEventListener("click", () => vscode.postMessage({ type: "refresh" }));
    $("import").addEventListener("click", () => vscode.postMessage({ type: "import" }));

    window.addEventListener("message", (ev) => {
      const m = ev.data;
      if (m.type === "state") {
        state = { rows: m.rows, busy: m.busy, done: m.done };
        render();
      }
    });
    vscode.postMessage({ type: "ready" });
  </script>
</body>
</html>`;
  }
}

function isSOPFile(uri: vscode.Uri): boolean {
  if (uri.scheme !== "file" && uri.scheme !== "untitled") {
    return false; // reckon's own virtual documents, remote/virtual fs, etc.
  }
  const ext = uri.path.split(".").pop()?.toLowerCase() ?? "";
  return SOP_EXTENSIONS.includes(ext);
}

function makeNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}
