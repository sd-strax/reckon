// The Knowledge document (design/ui/09 §9.1; implementation/workbench-react.md
// §2.6): one singleton webview panel in the main area rendering <KnowledgeApp>
// from the same bundle as the investigation document, revealed rather than
// re-created. It reads and writes only the knowledge service, shows no
// investigation state, and records nothing in any investigation: the only
// investigation facts it posts are links the record carries.
//
// The host owns a KnowledgeCache (impl §3): CRUD reads re-posted whole after
// every read and after its own writes, with no sequence (nothing here is
// event-sourced). Import keeps every behavior of the former Import SOPs panel
// (parity §N) and adds the server-side dry run so the one parser
// (internal/sopdoc) stays the only parser.

import * as vscode from "vscode";
import { randomUUID } from "node:crypto";
import type { AgentTransport } from "./agentTransport";
import { BackendClient, CandidateRow, CaseSummaryView, InvestigationDetail, SOPDoc } from "./backend";
import type { Answer, ImportDryRun, ImportRow, InboundMessage, InspectorTarget, KnowledgeSection, KnowledgeState, RenderMessage, SearchResults } from "./protocol";
import { panelOptions, webviewShell } from "./webviewShell";
import { openSOPOriginal } from "./investigationDocument";

const SOP_EXTENSIONS = ["md", "markdown", "txt"];

/** One chosen document; the URI is its identity across refreshes. */
interface Row {
  uri: vscode.Uri;
  name: string;
  dir: string;
  fromEditor: boolean;
  dirty: boolean;
  checked: boolean;
  preview?: ImportDryRun;
  previewError?: string;
  result?: ImportRow["result"];
}

export class KnowledgeDocument {
  private panel?: vscode.WebviewPanel;
  private ready = false;
  /** A reveal requested before the webview reported ready. */
  private pendingReveal?: { section: KnowledgeSection; id?: string };
  private rows: Row[] = [];
  private importing = false;
  private sops: SOPDoc[] = [];
  private cases: CaseSummaryView[] | null = null;
  private candidates: CandidateRow[] | null = null;
  private readonly disposables: vscode.Disposable[] = [];

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
    // Called with an investigation started from the work queue shown in this
    // document's left region: the extension opens it and refreshes the trees.
    private readonly onInvestigationCreated: (created: InvestigationDetail) => void,
    // The sidecar transport, for Ask (adr/0111): a knowledge-only loop with no
    // session and nothing recorded. Optional so the document stands without it.
    private readonly transport?: Pick<AgentTransport, "askKnowledge">,
  ) {}

  /** Open (or reveal) the document, at a section when one is named. */
  open(section?: KnowledgeSection, id?: string): void {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "reckon.knowledge",
        "Knowledge",
        vscode.ViewColumn.Active,
        panelOptions(this.extensionUri),
      );
      this.panel.webview.html = webviewShell(this.panel.webview, this.extensionUri, { title: "Knowledge", surface: "knowledge" });
      this.panel.webview.onDidReceiveMessage((msg: InboundMessage) => this.route(msg));
      // Tabs opening/closing while the document is up: keep the editor rows honest.
      this.disposables.push(vscode.window.tabGroups.onDidChangeTabs(() => { if (this.ready) void this.rescanEditors(); }));
      this.panel.onDidDispose(() => {
        this.panel = undefined;
        this.ready = false;
        this.rows = [];
        for (const d of this.disposables.splice(0)) d.dispose();
      });
    } else {
      this.panel.reveal(undefined, false);
    }
    if (section) {
      if (this.ready) void this.post({ type: "knowledge.reveal", section, id });
      else this.pendingReveal = { section, id };
    }
  }

  dispose(): void {
    this.panel?.dispose();
  }

  private route(msg: InboundMessage): void {
    switch (msg.type) {
      case "ready":
      case "refresh":
      case "ui.resync":
        void this.load();
        return;
      case "queue.get":
        void this.postQueue();
        return;
      case "queue.start":
        void this.startFromCase(msg.caseNumber);
        return;
      case "knowledge.query":
        if (msg.mode === "ask") {
          void this.ask(msg.text);
        } else {
          void this.search(msg.text, msg.filters);
        }
        return;
      case "sop.get":
        void this.getSOP(msg.sopId);
        return;
      case "sop.openOriginal":
        void openSOPOriginal(this.client, msg.sopId, msg.revision);
        return;
      case "sop.revision":
        void this.getRevision(msg.sopId, msg.hash);
        return;
      case "sop.save":
        void this.saveSOP(msg);
        return;
      case "sop.retire":
        void this.retireSOP(msg.sopId, msg.reason);
        return;
      case "cases.list":
        void this.listCases(msg.filters);
        return;
      case "candidates.list":
        void this.listCandidates(msg.status);
        return;
      case "candidate.get":
        void this.getCandidate(msg.id);
        return;
      case "candidate.accept":
        void this.acceptCandidate(msg.id, msg.body);
        return;
      case "candidate.discard":
        void this.discardCandidate(msg.id, msg.reason);
        return;
      case "import.choose":
        void this.choose(msg.folder);
        return;
      case "import.toggle": {
        const row = this.rows.find((r) => r.uri.toString() === msg.id);
        if (row) row.checked = msg.checked;
        void this.postImport();
        return;
      }
      case "import.run":
        void this.runImport();
        return;
      case "import.refresh":
        void this.refreshRows();
        return;
      case "inspector.open":
        void this.answerInspector(msg.target);
        return;
      case "investigation.open":
        void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
        return;
      case "copy":
        void vscode.env.clipboard.writeText(msg.text);
        return;
      default:
        // Every other intent is an investigation act; this document never
        // posts one, and the host ignores one it did not ask for.
        return;
    }
  }

  private async post(m: RenderMessage): Promise<void> {
    if (this.panel) await this.panel.webview.postMessage(m);
  }

  /** The whole cache: the procedure corpus, the counts, the import rows. */
  private async load(): Promise<void> {
    try {
      this.sops = await this.client.sops(true);
    } catch (err) {
      void this.post({ type: "knowledge.error", message: `Could not load the procedures — ${errText(err)}` });
      return;
    }
    await this.rescanEditors(false);
    // Past cases (09 §9.4, adr/0107): the listing is a read over the corpus; a
    // failure leaves the section honest (null) and the procedures intact.
    try {
      this.cases = await this.client.caseSummaries();
    } catch (err) {
      this.log.debug(`case_summaries: ${errText(err)}`);
      this.cases = null;
    }
    // "Cited in N investigations" (adr/0109): a read over the citation projection.
    let citations: Record<string, number> | null = null;
    try {
      citations = await this.client.citationCounts("sop");
    } catch (err) {
      this.log.debug(`citation counts: ${errText(err)}`);
    }
    // Candidates (adr/0112): the drafts waiting for review, with the decided ones.
    try {
      this.candidates = await this.client.candidates();
    } catch (err) {
      this.log.debug(`candidates: ${errText(err)}`);
      this.candidates = null;
    }
    const state: KnowledgeState = {
      counts: this.counts(),
      sops: this.sops,
      cases: this.cases,
      candidates: this.candidates,
      candidateDetail: null,
      citations,
      detailCitations: null,
      detail: null,
      detailRevisions: null,
      revisionBodies: {},
      results: null,
      answer: null,
      importRows: this.rows.map(toImportRow),
      importing: this.importing,
    };
    await this.post({ type: "knowledge.state", state });
    this.ready = true;
    if (this.pendingReveal) {
      const r = this.pendingReveal;
      this.pendingReveal = undefined;
      await this.post({ type: "knowledge.reveal", section: r.section, id: r.id });
    }
    // Dry-run the editor rows after the first paint.
    await this.previewRows(this.rows.filter((r) => !r.preview && !r.previewError));
  }

  private counts(): KnowledgeState["counts"] {
    return { procedures: this.sops.filter((s) => s.status !== "retired").length, cases: this.cases ? this.cases.length : null, candidates: this.candidates ? this.candidates.filter((c) => c.status === "NEW").length : null };
  }

  /** The candidates (adr/0112): the list, one draft with its closest procedure, accept through the import path, discard with a reason. */
  private async listCandidates(status?: string): Promise<void> {
    try {
      const candidates = await this.client.candidates(status);
      if (!status) this.candidates = candidates;
      await this.post({ type: "knowledge.state", state: { candidates, counts: this.counts() } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not list the candidates — ${errText(err)}` });
    }
  }

  private async getCandidate(id: string): Promise<void> {
    try {
      await this.post({ type: "knowledge.state", state: { candidateDetail: await this.client.candidate(id) } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not read the candidate — ${errText(err)}` });
    }
  }

  private async acceptCandidate(id: string, body: string): Promise<void> {
    try {
      const res = await this.client.acceptCandidate(id, body);
      void vscode.window.setStatusBarMessage(`reckon: procedure “${res.candidate.title}” ${res.outcome === "revised" ? "revised" : "added"} — consultable in future investigations`, 6000);
      this.sops = await this.client.sops(true).catch(() => this.sops);
      await this.listCandidates();
      await this.post({ type: "knowledge.state", state: { sops: this.sops, candidateDetail: await this.client.candidate(id).catch(() => null) } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not add the procedure — ${errText(err)}` });
    }
  }

  private async discardCandidate(id: string, reason: string): Promise<void> {
    try {
      await this.client.discardCandidate(id, reason);
      await this.listCandidates();
      await this.post({ type: "knowledge.state", state: { candidateDetail: await this.client.candidate(id).catch(() => null) } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not discard the candidate — ${errText(err)}` });
    }
  }

  /** Ask (09 §9.2, adr/0111): one knowledge-only loop on the client plane; the
   * answer streams by request id, citations resolve to inspector targets, and
   * the footer's "nothing recorded" is the sidecar's fact, not a promise. */
  private async ask(text: string): Promise<void> {
    const question = text.trim();
    if (!question) return;
    if (!this.transport) {
      await this.post({ type: "knowledge.error", message: "Ask needs the agent sidecar, which this document was opened without." });
      return;
    }
    const requestId = randomUUID();
    await this.post({ type: "knowledge.answer.start", requestId, question });
    try {
      const a = await this.transport.askKnowledge(requestId, question, (delta) => { void this.post({ type: "knowledge.answer.delta", requestId, text: delta }); });
      const answer: Answer = {
        requestId, question, text: a.text, model: a.model, toolsUsed: a.toolsUsed,
        citations: a.citations.map((c) => ({ n: c.n, title: c.title, target: c.kind === "case" ? { kind: "case", investigationRef: c.ref } : { kind: "sop", sopId: c.ref } })),
      };
      await this.post({ type: "knowledge.answer.done", requestId, answer });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Ask failed — ${errText(err)}` });
    }
  }

  /** The Past cases listing with the analyst's filters (09 §9.4, adr/0107). The
   * count stays the unfiltered total so the section's number means what it says. */
  private async listCases(filters?: { technique?: string; disposition?: string; from?: string; to?: string }): Promise<void> {
    try {
      const cases = await this.client.caseSummaries(filters);
      const filtered = !!(filters && (filters.technique || filters.disposition || filters.from || filters.to));
      if (!filtered) this.cases = cases;
      await this.post({ type: "knowledge.state", state: { cases, counts: this.counts() } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not list past cases — ${errText(err)}` });
    }
  }

  private async getSOP(id: string): Promise<void> {
    try {
      const [detail, detailRevisions, cited] = await Promise.all([
        this.client.sop(id),
        this.client.sopRevisions(id).catch((err: unknown) => { this.log.debug(`revisions ${id}: ${errText(err)}`); return null; }),
        this.client.citations("sop", id).catch((err: unknown) => { this.log.debug(`citations ${id}: ${errText(err)}`); return null; }),
      ]);
      await this.post({ type: "knowledge.state", state: { detail, detailRevisions, detailCitations: cited ? cited.investigations : null } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not read the procedure — ${errText(err)}` });
    }
  }

  private readonly revisionBodies: Record<string, import("./backend").RevisionContent> = {};

  /** One revision's content for the Versions compare (adr/0097). */
  private async getRevision(id: string, hash: string): Promise<void> {
    try {
      this.revisionBodies[hash] = await this.client.sopRevision(id, hash);
      await this.post({ type: "knowledge.state", state: { revisionBodies: { ...this.revisionBodies } } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Could not read the revision — ${errText(err)}` });
    }
  }

  private async saveSOP(msg: { sopId: string; title: string; body: string; tags: string[]; recommendation?: string }): Promise<void> {
    try {
      await this.client.updateSOP(msg.sopId, { title: msg.title, body: msg.body, tags: msg.tags, recommendation: msg.recommendation });
      this.sops = await this.client.sops(true);
      const [detail, detailRevisions] = await Promise.all([this.client.sop(msg.sopId), this.client.sopRevisions(msg.sopId).catch(() => null)]);
      await this.post({ type: "knowledge.state", state: { sops: this.sops, counts: this.counts(), detail, detailRevisions } });
      this.log.info(`SOP ${msg.sopId} revised: ${msg.title}`);
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Save failed — ${errText(err)}` });
    }
  }

  private async retireSOP(id: string, reason: string): Promise<void> {
    try {
      await this.client.retireSOP(id, reason);
      this.sops = await this.client.sops(true);
      const detail = await this.client.sop(id).catch(() => null);
      await this.post({ type: "knowledge.state", state: { sops: this.sops, counts: this.counts(), detail } });
      this.log.info(`SOP ${id} retired: ${reason}`);
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Retire failed — ${errText(err)}` });
    }
  }

  /** Search (09 §9.2): both recalls, direct against the knowledge service —
   * there is no session here, nothing is included anywhere. */
  private async search(text: string, filters?: { tags?: string[]; includeRetired?: boolean }): Promise<void> {
    const query = text.trim();
    if (!query) return;
    try {
      const [sops, cases] = await Promise.all([
        this.client.recallSOPs(query, filters?.tags, filters?.includeRetired === true),
        this.client.recallSimilar(query, filters?.tags).catch((err: unknown) => { this.log.debug(`recall_similar: ${errText(err)}`); return null; }),
      ]);
      const results: SearchResults = {
        query,
        procedures: (sops.results ?? []).map((r) => ({ sopId: r.sop_id, title: r.title, excerpt: r.excerpt, score: r.score, rationale: r.match_rationale, tags: r.tags ?? [], recommendation: r.recommendation, status: r.status, backend: r.backend })),
        cases: (cases?.results ?? []).map((r) => ({ investigationRef: r.investigation_ref, title: r.title, excerpt: r.excerpt, score: r.score, band: r.band, rationale: r.match_rationale, tags: r.tags ?? [], backend: r.backend })),
        coverage: { procedures: sops.coverage ?? "", cases: cases?.coverage ?? "" },
      };
      await this.post({ type: "knowledge.state", state: { results } });
    } catch (err) {
      await this.post({ type: "knowledge.error", message: `Search failed — ${errText(err)}` });
    }
  }

  // --- Import (09 §9.5; parity §N) -------------------------------------------

  /** Rebuild the open-editor rows from the live tab set, preserving choices,
   * previews and results for rows still open. Disk rows persist for the
   * document's lifetime unless the same file is now open, when the editor row
   * (the live buffer) wins. */
  private async rescanEditors(post = true): Promise<void> {
    const activeKey = vscode.window.activeTextEditor?.document.uri.toString();
    const prior = new Map(this.rows.map((r) => [r.uri.toString(), r]));
    const next: Row[] = [];
    const seen = new Set<string>();
    for (const group of vscode.window.tabGroups.all) {
      for (const tab of group.tabs) {
        if (!(tab.input instanceof vscode.TabInputText)) continue;
        const uri = tab.input.uri;
        const key = uri.toString();
        if (seen.has(key) || !isSOPFile(uri)) continue;
        seen.add(key);
        const old = prior.get(key);
        const parts = uri.path.split("/");
        const name = parts.pop() || tab.label;
        next.push({
          uri, name, dir: parts.join("/"), fromEditor: true,
          dirty: vscode.workspace.textDocuments.find((d) => d.uri.toString() === key)?.isDirty === true,
          checked: old ? old.checked : key === activeKey,
          preview: old?.preview, previewError: old?.previewError, result: old?.result,
        });
      }
    }
    for (const r of this.rows) {
      if (!r.fromEditor && !seen.has(r.uri.toString())) next.push(r);
    }
    this.rows = next;
    if (post) {
      await this.postImport();
      await this.previewRows(this.rows.filter((r) => r.fromEditor && !r.preview && !r.previewError));
    }
  }

  private async choose(folder: boolean): Promise<void> {
    const uris = await vscode.window.showOpenDialog({
      title: folder ? "Add a folder of SOP documents" : "Add SOP documents",
      canSelectMany: !folder,
      canSelectFiles: !folder,
      canSelectFolders: folder,
      openLabel: "Add",
      filters: folder ? undefined : { "Markdown / text": SOP_EXTENSIONS },
    });
    if (!uris || uris.length === 0) return;
    const files: vscode.Uri[] = [];
    for (const uri of uris) {
      if (folder) files.push(...(await listSOPFiles(uri, 3)));
      else files.push(uri);
    }
    const have = new Set(this.rows.map((r) => r.uri.toString()));
    const added: Row[] = [];
    for (const uri of files) {
      if (have.has(uri.toString())) continue;
      have.add(uri.toString());
      const parts = uri.path.split("/");
      const row: Row = { uri, name: parts.pop() || "untitled.md", dir: parts.join("/"), fromEditor: false, dirty: false, checked: true };
      this.rows.push(row);
      added.push(row);
    }
    await this.postImport();
    await this.previewRows(added);
  }

  /** Re-read every row: editor rows from the live buffer, disk rows from disk;
   * the dry run runs again so the preview matches what would be imported. */
  private async refreshRows(): Promise<void> {
    for (const r of this.rows) { r.preview = undefined; r.previewError = undefined; }
    await this.rescanEditors(false);
    await this.postImport();
    await this.previewRows(this.rows);
  }

  private async content(row: Row): Promise<string> {
    // openTextDocument returns the live buffer when the file is open — unsaved
    // edits included — so what is imported is what is on screen.
    if (row.fromEditor) return (await vscode.workspace.openTextDocument(row.uri)).getText();
    return Buffer.from(await vscode.workspace.fs.readFile(row.uri)).toString("utf8");
  }

  /** The server's reading of each document (09 §9.5): parsed frontmatter,
   * missing recommended fields, and the outcome an import would have. The
   * parser's error is shown verbatim. */
  private async previewRows(rows: Row[]): Promise<void> {
    for (const row of rows) {
      try {
        const raw = await this.client.importSOPDryRun(row.uri.fsPath, await this.content(row));
        row.preview = {
          title: raw.title, author: raw.author, tags: raw.tags ?? [], recommendation: raw.recommendation,
          source: raw.source, sourceDefaulted: raw.source_defaulted, missing: raw.missing ?? [],
          outcome: raw.outcome, existingId: raw.existing_id, existingTitle: raw.existing_title,
        };
        row.previewError = undefined;
      } catch (err) {
        row.previewError = errText(err);
      }
      await this.postImport();
    }
  }

  private async runImport(): Promise<void> {
    const selected = this.rows.filter((r) => r.checked);
    if (!selected.length || this.importing) return;
    this.importing = true;
    await this.postImport();
    let ok = 0;
    for (const row of selected) {
      try {
        // The FULL path goes as the filename: when the document carries no
        // frontmatter source, the server keys the re-import lineage on it, and
        // a bare basename would make two different "runbook.md" files silently
        // revise each other. Matches the CLI, which keys on the cleaned path.
        const res = await this.client.importSOPMarkdown(row.uri.fsPath, await this.content(row));
        row.result = { outcome: res.outcome === "created" ? "added" : "revised", title: res.title };
        row.checked = false;
        ok++;
        this.log.info(`imported SOP ${res.outcome}: ${res.title} (${row.name})`);
      } catch (err) {
        row.result = { outcome: "failed", title: row.name, message: errText(err) };
        this.log.warn(`SOP import failed: ${row.name}: ${row.result.message}`);
      }
      await this.postImport();
    }
    this.importing = false;
    // The corpus changed: the procedures and their counts, re-posted whole.
    try { this.sops = await this.client.sops(true); } catch { /* the next refresh reloads */ }
    await this.post({ type: "knowledge.state", state: { sops: this.sops, counts: this.counts(), importRows: this.rows.map(toImportRow), importing: false } });
    this.log.info(`import done: ${ok} of ${selected.length}`);
  }

  private async postImport(): Promise<void> {
    await this.post({ type: "knowledge.state", state: { importRows: this.rows.map(toImportRow), importing: this.importing } });
  }

  // --- The left region in Work mode (08 §8.3) --------------------------------

  private async postQueue(): Promise<void> {
    try {
      const [investigations, queue] = await Promise.all([
        this.client.investigations(),
        this.client.cases().catch(() => null),
      ]);
      const cases = (queue?.cases ?? []).filter((c) => !c.investigation);
      await this.post({ type: "queue", investigations, cases, caseSource: queue?.queue });
    } catch (err) {
      this.log.debug(`queue: ${errText(err)}`);
    }
  }

  private async startFromCase(caseNumber: string): Promise<void> {
    try {
      const created = await this.client.createInvestigationFromInput(caseNumber, "case");
      this.onInvestigationCreated(created);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: could not start from ${caseNumber} — ${errText(err)}`);
    }
  }

  // --- The inspector (Procedure and Similar-case only; impl §2.6) -----------

  private async answerInspector(target: InspectorTarget): Promise<void> {
    switch (target.kind) {
      case "sop":
        try {
          await this.post({ type: "inspector.procedure", sopId: target.sopId, sop: await this.client.sop(target.sopId) });
        } catch (err) {
          await this.post({ type: "inspector.error", target: target.sopId, message: errText(err) });
        }
        return;
      case "case":
        try {
          const [summary, citations] = await Promise.all([
            this.client.caseSummary(target.investigationRef),
            this.client.citations("case", target.investigationRef).catch((err: unknown) => { this.log.debug(`case citations: ${errText(err)}`); return null; }),
          ]);
          await this.post({ type: "inspector.case", investigationRef: target.investigationRef, summary, citations });
        } catch (err) {
          await this.post({ type: "inspector.error", target: target.investigationRef, message: errText(err) });
        }
        return;
      default:
        await this.post({ type: "inspector.error", target: "ref" in target ? target.ref : target.kind, message: "Only procedures and past cases open here; investigation objects open in their investigation." });
    }
  }
}

function toImportRow(r: Row): ImportRow {
  return {
    id: r.uri.toString(), name: r.name, dir: r.dir, fromEditor: r.fromEditor, dirty: r.dirty, checked: r.checked,
    preview: r.preview, previewError: r.previewError, result: r.result,
  };
}

function isSOPFile(uri: vscode.Uri): boolean {
  if (uri.scheme !== "file" && uri.scheme !== "untitled") {
    return false; // reckon's own virtual documents, remote/virtual fs, etc.
  }
  const ext = uri.path.split(".").pop()?.toLowerCase() ?? "";
  return SOP_EXTENSIONS.includes(ext);
}

/** The markdown/text files under a folder, a few levels deep, sorted. */
async function listSOPFiles(dir: vscode.Uri, depth: number): Promise<vscode.Uri[]> {
  const out: vscode.Uri[] = [];
  let entries: [string, vscode.FileType][] = [];
  try { entries = await vscode.workspace.fs.readDirectory(dir); } catch { return out; }
  entries.sort((a, b) => a[0].localeCompare(b[0]));
  for (const [name, type] of entries) {
    const child = vscode.Uri.joinPath(dir, name);
    if (type === vscode.FileType.Directory && depth > 0 && !name.startsWith(".")) out.push(...(await listSOPFiles(child, depth - 1)));
    else if (type === vscode.FileType.File && isSOPFile(child)) out.push(child);
  }
  return out;
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
