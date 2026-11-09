// The investigation's narrative (adr/0103): a draft the model writes on the
// analyst's own key, and the analyst's own revisions, both recorded on the
// investigation with their authorship. The draft goes through the sidecar,
// the only place the model key lives, which posts it on the agent token so
// the record says the model wrote it under this person; an edit is posted
// here on the human token. The text is read back from the record on the
// refresh that follows, never from what was sent.
import type * as vscode from "vscode";
import type { DocumentHost } from "./documentHost";
import { errText } from "./util";

export class NarrativeFlow {
  constructor(private readonly host: DocumentHost) {}

  /** Ask the model for a draft; the status rides the panel, the text the refresh. */
  async draft(id: string, panel: vscode.WebviewPanel): Promise<void> {
    void this.host.post(panel, { type: "narrative.status", state: "drafting" });
    const current = this.host.sameIdentity();
    try {
      await this.host.transport.draftNarrative(id);
    } catch (err) {
      if (current()) void this.host.post(panel, { type: "narrative.status", state: "failed", message: errText(err) });
      return;
    } finally {
      this.host.releaseSession(id);
    }
    if (!current()) return;
    try {
      await this.host.refresh(id, panel);
    } finally {
      // The draft is on the record whatever the re-read did; the lens must
      // not stay on "drafting".
      if (current()) void this.host.post(panel, { type: "narrative.status", state: "written" });
    }
  }

  /**
   * Record the analyst's revision, then re-read. The editor keeps the text
   * until the record carries it: saving, then saved after the re-read (so the
   * lens shows the new text as the editor closes), or save-failed with the
   * reason and the text still in the editor. A refusal re-reads nothing: the
   * record did not move.
   */
  async edit(id: string, panel: vscode.WebviewPanel, body: string): Promise<void> {
    const text = body.trim();
    if (!text) return;
    void this.host.post(panel, { type: "narrative.status", state: "saving" });
    const current = this.host.sameIdentity();
    try {
      await this.host.client.investigations.recordNarrative(id, text);
    } catch (err) {
      if (current()) void this.host.post(panel, { type: "narrative.status", state: "save-failed", message: errText(err) });
      return;
    }
    if (!current()) return;
    try {
      await this.host.refresh(id, panel);
    } finally {
      // The revision is on the record whatever the re-read did.
      if (current()) void this.host.post(panel, { type: "narrative.status", state: "saved" });
    }
  }
}
