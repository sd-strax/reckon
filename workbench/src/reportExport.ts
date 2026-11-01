// The live markdown export (binding §6 item 10): a rendered preview beside the
// panel from a scratch file, and Save report… to a file the analyst picks. The
// scratch files and their previews are reckon's to close when the person
// changes (adr/0241).
import { randomUUID } from "crypto";
import * as os from "os";
import * as path from "path";
import * as vscode from "vscode";
import type { DocumentHost } from "./documentHost";
import { errText } from "./util";

export class ReportExport {
  constructor(private readonly host: DocumentHost) {}

  // In the scratch name of this window's reports: their preview tabs are found
  // by that name, and a report the analyst saved must not match it.
  private readonly exportTag = randomUUID().slice(0, 8);

  private readonly reports = new Map<string, vscode.Uri>();

  private async closeReports(reports: vscode.Uri[]): Promise<void> {
    const names = reports.map((uri) => path.basename(uri.fsPath));
    const previews = vscode.window.tabGroups.all.flatMap((group) => group.tabs).filter((tab) =>
      tab.input instanceof vscode.TabInputWebview && tab.input.viewType.includes("markdown.preview")
      && names.some((name) => tab.label.includes(name)));
    try {
      await vscode.window.tabGroups.close(previews);
    } catch (err) {
      this.host.log.warn(`closing a report preview: ${errText(err)}`);
    }
    for (const uri of reports) {
      await Promise.resolve(vscode.workspace.fs.delete(uri)).catch((err: unknown) => {
        this.host.log.warn(`deleting a report's scratch file: ${errText(err)}`);
      });
    }
  }

  /**
   * The live markdown export (binding §6 item 10): rendered backend-side from
   * the projections and shown as a rendered preview beside the panel. The
   * preview reads a scratch file, not an untitled doc, so no unsaved source
   * tab is left behind; Save report… writes it to a file the analyst picks.
   */
  async openMarkdownExport(id: string): Promise<void> {
    const current = this.host.sameIdentity();
    let md: string;
    try {
      md = await this.host.client.investigations.exportMarkdown(id);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: export failed — ${errText(err)}`);
      return;
    }
    if (!current()) return;
    const name = `reckon-report-${id.slice(0, 8)}`;
    const scratch = vscode.Uri.file(path.join(os.tmpdir(), "reckon-exports", `${name}-${this.exportTag}.md`));
    try {
      await vscode.workspace.fs.createDirectory(vscode.Uri.file(path.dirname(scratch.fsPath)));
      await vscode.workspace.fs.writeFile(scratch, Buffer.from(md, "utf8"));
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: export failed — ${errText(err)}`);
      return;
    }
    if (!current()) {
      await this.closeReports([scratch]);
      return;
    }
    this.reports.set(scratch.fsPath, scratch);
    await vscode.commands.executeCommand("markdown.showPreviewToSide", scratch);
    if (!current()) {
      await this.closeReports([scratch]);
      return;
    }
    const save = "Save report…";
    // The prompt cannot be dismissed from here, so it can outlive the identity.
    if ((await vscode.window.showInformationMessage("reckon: report exported", save)) !== save || !current()) return;
    const target = await vscode.window.showSaveDialog({
      defaultUri: vscode.Uri.file(path.join(os.homedir(), `${name}.md`)),
      filters: { Markdown: ["md"] },
      saveLabel: "Save report",
    });
    if (!target || !current()) return;
    try {
      await vscode.workspace.fs.writeFile(target, Buffer.from(md, "utf8"));
      void vscode.window.showInformationMessage(`reckon: report saved to ${target.fsPath}`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: saving the report failed — ${errText(err)}`);
    }
  }

  /** The person changed: close every preview and delete its scratch file. */
  async clear(): Promise<void> {
    const reports = [...this.reports.values()];
    this.reports.clear();
    await this.closeReports(reports);
  }
}
