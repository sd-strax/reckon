// The knowledge library in the document (adr/0129) and the procedure drafted
// from a concluded case (adr/0156): browse, search, the summary's status, and
// the draft's life from proposal to the library or the bin. The drafts reckon
// opens as editors are closed when the person changes (adr/0241).
import * as vscode from "vscode";
import type { SOPCandidate } from "./agentTransport";
import type { DocumentHost } from "./documentHost";
import { candidateSOPMarkdown, slugifyTitle } from "./documentText";
import { errText } from "./util";

/** The status line after a draft was added, by what the import did. A word the
 * server answers that this workbench does not know is not guessed at: it is
 * reported as the failure, the draft kept. */
function acceptedLine(outcome: string): string {
  switch (outcome) {
    case "created":
      return "added to the library";
    case "revised":
      return "updated in the library";
    case "unchanged":
      return "already in the library, unchanged";
    default:
      throw new Error(`the server answered an import outcome this workbench does not know: "${outcome}"`);
  }
}

export class KnowledgeFlow {
  constructor(private readonly host: DocumentHost) {}

  // What reckon opened outside its panels, closed when the identity changes:
  // candidate SOP drafts, and exported reports (their scratch files, by path).
  private readonly draftEditors = new Set<vscode.TextDocument>();

  /**
   * Revert and close a draft in every group that holds it. The command acts on
   * the active editor, so each group's tab is made active by column: shown with
   * no column, the draft would open again in the active group and only that copy
   * would close. `shownIn` is a column the tabs may not report yet.
   */
  private async closeDraft(doc: vscode.TextDocument, shownIn?: vscode.ViewColumn): Promise<void> {
    const uri = doc.uri.toString();
    const columns = new Set(vscode.window.tabGroups.all
      .filter((group) => group.tabs.some((tab) => tab.input instanceof vscode.TabInputText && tab.input.uri.toString() === uri))
      .map((group) => group.viewColumn));
    if (shownIn !== undefined) columns.add(shownIn);
    for (const viewColumn of columns) {
      if (doc.isClosed) {
        return;
      }
      try {
        await vscode.window.showTextDocument(doc, { viewColumn });
        await vscode.commands.executeCommand("workbench.action.revertAndCloseActiveEditor");
      } catch (err) {
        this.host.log.warn(`closing a SOP draft: ${errText(err)}`);
      }
    }
  }

  /** The knowledge library (adr/0129): every procedure and past case, to browse. */
  async postLibrary(panel: vscode.WebviewPanel): Promise<void> {
    try {
      const [sops, cases] = await Promise.all([
        this.host.client.knowledge.procedures().catch(() => []),
        this.host.client.knowledge.cases().catch(() => []),
      ]);
      void this.host.post(panel, { type: "library", sops, cases });
    } catch (err) {
      this.host.log.debug(`library: ${errText(err)}`);
    }
  }

  /** Whether a concluded case's knowledge summary carries a narrative (adr/0156). */
  async postSummaryStatus(id: string, panel: vscode.WebviewPanel): Promise<void> {
    try {
      const sum = await this.host.client.knowledge.caseSummary(id);
      if (sum) void this.host.post(panel, sum.generatorModel
        ? { type: "summary.status", state: "written", detail: sum.generatorModel }
        : { type: "summary.status", state: "baseline" });
    } catch (err) {
      this.host.log.debug(`summary status: ${errText(err)}`);
    }
  }

  /** One library search over both corpora; the hits come back as their own list. */
  async searchLibrary(panel: vscode.WebviewPanel, text: string): Promise<void> {
    try {
      const found = await this.host.client.knowledge.search(text);
      void this.host.post(panel, { type: "library.results", query: text, ...found });
    } catch (err) {
      void vscode.window.showWarningMessage(`reckon: library search failed — ${errText(err)}`);
    }
  }

  /** The open candidate drafts, per investigation: the editor document and its title. */
  private readonly sopDrafts = new Map<string, { doc: vscode.TextDocument; title: string }>();

  /**
   * Draft a procedure from this concluded case (adr/0156) — the analyst's
   * step, from the Summary lens. One completion over the session's own
   * conversation; the draft opens as an editable document and the panel offers
   * Add to SOP library / Discard. A decline says why and can be overridden.
   */
  async draftSOP(id: string, panel: vscode.WebviewPanel, force?: boolean): Promise<void> {
    void this.host.post(panel, { type: "sop.status", state: "drafting" });
    const current = this.host.sameIdentity();
    let candidate: SOPCandidate;
    let usable: boolean;
    try {
      candidate = await this.host.transport.proposeSOP(id, force);
      usable = candidate.warranted && !!candidate.title.trim() && !!candidate.body.trim();
    } catch (err) {
      void this.host.post(panel, { type: "sop.status", state: "failed", message: errText(err) });
      return;
    } finally {
      this.host.releaseSession(id);
    }
    if (!current()) return;
    if (!usable) {
      void this.host.post(panel, { type: "sop.status", state: "declined", message: candidate.rationale.trim() || "No new procedure: the model judged nothing here to generalize." });
      return;
    }
    const doc = await vscode.workspace.openTextDocument({ content: candidateSOPMarkdown(candidate, id), language: "markdown" });
    if (!current()) return;
    const editor = await vscode.window.showTextDocument(doc, { preview: false, viewColumn: vscode.ViewColumn.Beside });
    if (!current()) {
      await this.closeDraft(doc, editor.viewColumn);
      return;
    }
    this.sopDrafts.set(id, { doc, title: candidate.title.trim() });
    this.draftEditors.add(doc);
    void this.host.post(panel, { type: "sop.status", state: "ready", title: candidate.title.trim(), message: candidate.rationale?.trim() || undefined });
  }

  /** Add the draft — as it reads now, with the analyst's edits — to the library. */
  async acceptSOP(id: string, panel: vscode.WebviewPanel): Promise<void> {
    const draft = this.sopDrafts.get(id);
    if (!draft) return;
    try {
      const res = await this.host.client.knowledge.importMarkdown(slugifyTitle(draft.title) + ".md", draft.doc.getText());
      const message = acceptedLine(res.outcome);
      this.sopDrafts.delete(id);
      void this.host.post(panel, { type: "sop.status", state: "added", title: res.title, message });
    } catch (err) {
      void this.host.post(panel, { type: "sop.status", state: "ready", title: draft.title, message: "adding it failed — " + errText(err) });
    }
  }

  /** Discard the draft: nothing is kept. */
  async discardSOP(id: string, panel: vscode.WebviewPanel): Promise<void> {
    this.sopDrafts.delete(id);
    void this.host.post(panel, { type: "sop.status", state: "discarded" });
  }

  /** Drop a closed panel's draft. */
  forget(id: string): void {
    this.sopDrafts.delete(id);
  }

  /**
   * The person changed: forget every draft and close what was opened as an
   * editor, reverting its unsaved text rather than offering it to the next
   * person to save.
   */
  async clear(): Promise<void> {
    const drafts = [...this.draftEditors];
    this.sopDrafts.clear();
    this.draftEditors.clear();
    for (const doc of drafts) {
      await this.closeDraft(doc);
    }
  }
}
