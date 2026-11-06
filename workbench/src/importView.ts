// Import SOPs — a reckon-owned panel (design/06 §2.4, design/13 §3). Choosing
// WHAT becomes institutional knowledge is an operational decision, so it gets a
// visible surface the analyst can read, revise, and see the outcome of — not a
// quick-pick that evaporates on focus loss. The panel lists the markdown/text
// documents already open in the editor (what is imported is each buffer as
// shown, unsaved edits included — nothing is re-read behind the analyst's
// back), lets them add files from disk, and renders each document's result
// (created / revised / unchanged / failed) in place. Attribution comes from each
// document's YAML frontmatter (title, author, tags, source), parsed server-side.
//
// The page is the bundle's `import` surface (webview/src/surfaces/ImportApp.tsx);
// this host owns the rows and the import.

import * as vscode from "vscode";
import { BackendClient } from "./backend";
import { panelHtml, panelOptions } from "./panelHtml";
import type { ImportInboundMessage, ImportOutcome, ImportRenderMessage } from "./protocol";
import { errText } from "./util";

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
  result?: { outcome: ImportOutcome; title: string; message?: string };
}

/** The server's outcome word as the row's. A word this panel does not know is
 * not guessed at: the row reports it as a failure naming the word. */
function importOutcome(word: string): ImportOutcome {
  switch (word) {
    case "created":
    case "revised":
    case "unchanged":
      return word;
    default:
      throw new Error(`the server answered an import outcome this workbench does not know: "${word}"`);
  }
}

export class ImportSOPsView {
  private panel?: vscode.WebviewPanel;
  private rows: Row[] = [];
  private disposables: vscode.Disposable[] = [];
  // Bumped when the identity changes: what was begun before it goes no further.
  private generation = 0;

  constructor(
    private readonly extensionUri: vscode.Uri,
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
        panelOptions(this.extensionUri),
      );
      this.panel.webview.html = panelHtml(this.panel.webview, this.extensionUri, "import", "Import SOPs");
      this.panel.webview.onDidReceiveMessage((m: ImportInboundMessage) => {
        switch (m?.type) {
          case "ready":
          case "refresh":
            this.rescan();
            this.push();
            return;
          case "toggle": {
            const row = this.rows.find((r) => r.uri.toString() === m.id);
            if (row) {
              row.checked = m.checked === true;
            }
            return;
          }
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

  /**
   * The person behind the session changed (adr/0241). The panel closes, and an
   * import still running stops: each document takes a token as it goes, so the
   * rest of one person's selection would land as the next person's.
   */
  identityChanged(): void {
    this.generation++;
    this.dispose();
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
    const generation = this.generation;
    const uris = await vscode.window.showOpenDialog({
      title: "Add SOP documents",
      canSelectMany: true,
      openLabel: "Add",
      filters: { "Markdown / text": SOP_EXTENSIONS },
    });
    if (!uris || uris.length === 0 || generation !== this.generation) {
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
    const generation = this.generation;
    let ok = 0;
    for (const row of selected) {
      try {
        const content = row.fromDisk
          ? Buffer.from(await vscode.workspace.fs.readFile(row.uri)).toString("utf8")
          // openTextDocument returns the live buffer when the file is open —
          // unsaved edits included — so what is imported is what is on screen.
          : (await vscode.workspace.openTextDocument(row.uri)).getText();
        if (generation !== this.generation) {
          return;
        }
        // The FULL path goes as the filename: when the document carries no
        // frontmatter source, the server keys the re-import lineage on it, and
        // a bare basename would make two different "runbook.md" files silently
        // revise each other. Matches the CLI, which keys on the cleaned path.
        const res = await this.client.knowledge.importMarkdown(row.uri.fsPath, content);
        row.result = { outcome: importOutcome(res.outcome), title: res.title };
        row.checked = false;
        ok++;
        this.log.info(`imported SOP ${res.outcome}: ${res.title} (${row.name})`);
      } catch (err) {
        row.result = { outcome: "failed", title: row.name, message: errText(err) };
        this.log.warn(`SOP import failed: ${row.name}: ${row.result.message}`);
      }
      if (generation !== this.generation) {
        return;
      }
      this.push({ busy: true });
    }
    this.push({ busy: false, done: { ok, failed: selected.length - ok } });
  }

  private push(extra: { busy?: boolean; done?: { ok: number; failed: number } } = {}): void {
    if (!this.panel) {
      return;
    }
    const msg: ImportRenderMessage = {
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
    };
    void this.panel.webview.postMessage(msg);
  }
}

function isSOPFile(uri: vscode.Uri): boolean {
  if (uri.scheme !== "file" && uri.scheme !== "untitled") {
    return false; // reckon's own virtual documents, remote/virtual fs, etc.
  }
  const ext = uri.path.split(".").pop()?.toLowerCase() ?? "";
  return SOP_EXTENSIONS.includes(ext);
}
