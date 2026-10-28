// The investigation document — the analyst's primary workspace (design/13 §4,
// §7 steps 2–3). A reckon-owned webview panel addressed by investigation id,
// never a file the extension decorates (workbench discipline, §3: custom
// surfaces in reckon real estate, no workspace-folder assumption).
//
// It renders the reasoning thread (hypotheses + predictions, read side) and
// hosts the interactive turn loop. The loop itself is the canonical Go
// implementation, reached through the AgentTransport seam (the sidecar,
// implementation/agent-sidecar.md) — this file is the renderer + message
// bridge, nothing more.
//
// The extension host holds the tokens and the Anthropic key and does every
// call; the webview only renders messages posted to it. The CSP forbids all
// network from the page.

import { randomUUID } from "crypto";
import * as vscode from "vscode";
import { ActionRow, Appearance, BackendClient, EvidenceDoc, InvestigationDetail, ThreadEntry } from "./backend";
import { AgentTransport, SOPCandidate } from "./agentTransport";
import type { InboundMessage, InspectorTarget, InvestigationSnapshot, RenderMessage, RivalBrief } from "./protocol";
import { buildDerivedViews } from "./derivedViews";
import { deriveEvidenceLabel } from "./evidenceLabel";
import { InvestigationCache } from "./investigationCache";

export class InvestigationDocuments {
  private readonly open = new Map<string, vscode.WebviewPanel>();
  // Per-investigation refresh timers for in-flight dispatches (APPROVED/
  // EXECUTING → poll until terminal; see scheduleDispatchWatch).
  private readonly dispatchWatch = new Map<string, { timer: ReturnType<typeof setTimeout>; ticks: number }>();
  // Monotonic key for not-yet-seeded draft panels (design/ui/02 §2.7): a draft
  // has no investigation id until its seed is typed, so it lives under a
  // synthetic key until seedDraft re-keys it to the real aggregate id.
  private draftSeq = 0;
  // Draft keys with a create in flight (the host half of the double-submit guard).
  private readonly seeding = new Set<string>();
  // A draft opened in "from a case" mode (via reckon.seedFromCase) — consumed in
  // the ready handshake so the panel opens with the case search already expanded.
  private readonly draftMode = new Map<string, "case">();
  // Investigations whose rename card should pop once their panel goes live
  // (set by beginRename from the tree; consumed in the ready handshake).
  private readonly pendingRename = new Map<string, string>();
  // Resolved evidence-ref labels, shared across panels — deterministic ids
  // mean a ref resolves to the same thing everywhere, so cache once.
  private readonly labelCache = new Map<string, string>();
  // Full evidence docs, cached for the same reason (null = unfetchable). Feeds
  // both label resolution and the event-time strip without duplicate fetches.
  private readonly docCache = new Map<string, EvidenceDoc | null>();
  // The projection reads, their watermark, and the per-investigation ordering
  // that keeps an older read from being posted after a newer one (impl §3.1–§3.2).
  private readonly cache: InvestigationCache;
  // Investigations whose knowledge rail has been read since the panel opened.
  private readonly knowledgeRead = new Set<string>();

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly client: BackendClient,
    private readonly log: vscode.LogOutputChannel,
    private readonly transport: AgentTransport,
    private readonly me: () => Promise<{ name: string; principal: string } | null>,
    /**
     * Called after each successful load with the last event sequence the
     * analyst has now seen — feeds the tree's unseen-changes cue (02 §2.12).
     * Last-seen is view state, never investigation state.
     */
    private readonly onLoaded?: (id: string, lastEventSequence: number) => void,
    /** The lens / companion mode the analyst chose (view state the tree may deep-link). */
    private readonly onViewState?: (id: string, lens?: string, companion?: string) => void,
  ) {
    this.cache = new InvestigationCache(client, log);
  }

  /** Open (or reveal) the document for one investigation. */
  show(id: string, titleHint?: string): void {
    const existing = this.open.get(id);
    if (existing) {
      existing.reveal();
      void this.load(id, existing);
      return;
    }

    const panel = vscode.window.createWebviewPanel(
      "reckon.investigation",
      titleHint ? `⚖ ${titleHint}` : "Investigation",
      vscode.ViewColumn.Active,
      this.panelOptions(),
    );
    this.open.set(id, panel);
    const ref = { id };
    panel.webview.html = this.html(panel.webview);
    panel.webview.onDidReceiveMessage((msg: InboundMessage) => this.route(ref, panel, msg));
    panel.onDidDispose(() => this.forget(ref.id));
  }

  /**
   * Open a not-yet-seeded investigation (design/ui/02 §2.7): the same document,
   * rendered in its unseeded state, where the analyst roots the investigation by
   * typing a host/IP/hash or a question into the composer. Nothing is persisted
   * until that first act — which keeps "an investigation always begins from
   * something concrete" honest without a wizard.
   */
  showDraft(mode?: "case"): void {
    const key = `__draft__${++this.draftSeq}`;
    if (mode) {
      this.draftMode.set(key, mode);
    }
    const panel = vscode.window.createWebviewPanel(
      "reckon.investigation",
      "New investigation",
      vscode.ViewColumn.Active,
      this.panelOptions(),
    );
    this.open.set(key, panel);
    const ref = { id: key };
    panel.webview.html = this.html(panel.webview);
    // The webview asks for its initial state via "ready"; a draft key answers
    // with the unseeded render instead of a load().
    panel.webview.onDidReceiveMessage((msg: InboundMessage) => this.route(ref, panel, msg));
    panel.onDidDispose(() => this.forget(ref.id));
  }

  /** The page may load only the bundle and the vendored fonts (impl §5.1). */
  private panelOptions(): vscode.WebviewPanelOptions & vscode.WebviewOptions {
    return {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [
        vscode.Uri.joinPath(this.extensionUri, "out", "webview"),
        vscode.Uri.joinPath(this.extensionUri, "media"),
      ],
    };
  }

  private static isDraft(key: string): boolean {
    return key.startsWith("__draft__");
  }

  /**
   * Rename an investigation on its own document surface: reveal the panel and
   * pop the in-document rename card (never the top-of-window quick input). Used
   * by the tree's context menu; the document header's pencil opens the card
   * directly in the webview. An already-open panel gets the card now; a freshly
   * opened one gets it after the webview's ready handshake.
   */
  beginRename(id: string, titleHint?: string): void {
    const existing = this.open.get(id);
    this.show(id, titleHint);
    if (existing) {
      void this.post(existing, { type: "rename.begin", title: titleHint ?? "" });
    } else {
      this.pendingRename.set(id, titleHint ?? "");
    }
  }

  /**
   * Re-load an investigation's panel from the backend if it is open — used
   * after an out-of-panel act that changed the thread (e.g. a context reset
   * from the tree), so the chronicle shows the committed marker immediately
   * instead of on the next manual refresh.
   */
  refreshOpen(id: string): void {
    const panel = this.open.get(id);
    if (panel) {
      void this.refresh(id, panel);
    }
  }

  /** Apply a confirmed rename, then update the tab + webview title in place. */
  private async applyRename(id: string, panel: vscode.WebviewPanel, title: string): Promise<void> {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    try {
      const applied = await this.client.renameInvestigation(id, trimmed);
      panel.title = `⚖ ${applied}`;
      void this.post(panel, { type: "renamed", title: applied });
      // The rename appended an event: the refresh advances the watermark and
      // the SeenTracker (via onLoaded), so the analyst's own rename does not
      // show in the tree as an unseen "● new" change.
      await this.refresh(id, panel);
      void vscode.commands.executeCommand("reckon.refreshInvestigations");
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: rename failed — ${errText(err)}`);
    }
  }

  /** Route one webview message using the panel's current (mutable) id. */
  private route(ref: { id: string }, panel: vscode.WebviewPanel, msg: InboundMessage): void {
    const id = ref.id;
    if (msg.type === "ready" || msg.type === "refresh") {
      if (InvestigationDocuments.isDraft(id)) {
        const mode = this.draftMode.get(id);
        this.draftMode.delete(id);
        void this.postDraft(panel, mode);
      } else {
        // A rename invoked from the tree opened this panel; start the in-place
        // edit only AFTER the load has posted its data (so the title field is
        // populated), preserving message order to the webview.
        const pending = this.pendingRename.get(id);
        this.pendingRename.delete(id);
        void this.load(id, panel).then(() => {
          if (pending !== undefined) {
            void this.post(panel, { type: "rename.begin", title: pending });
          }
        });
      }
    } else if (msg.type === "seed.submit") {
      void this.seedDraft(ref, panel, { value: msg.value, kind: msg.kind });
    } else if (msg.type === "seed.alert") {
      void this.seedDraft(ref, panel, { alertId: msg.alertId, source: msg.source });
    } else if (msg.type === "seed.caseSearch") {
      void this.searchCases(panel, msg.filter, msg.queue);
    } else if (msg.type === "seed.case") {
      void this.seedDraft(ref, panel, { value: msg.caseNumber, kind: "case" });
    } else if (msg.type === "caseNote.post") {
      void this.postCaseNote(id, panel, msg.body);
    } else if (msg.type === "send") {
      void this.runTurn(id, panel, msg.text, undefined, msg.includedKnowledge);
    } else if (msg.type === "action.approve") {
      void this.approve(id, panel, msg);
    } else if (msg.type === "action.reject") {
      void this.reject(id, panel, msg);
    } else if (msg.type === "action.rerequest") {
      void this.rerequest(id, panel, msg);
    } else if (msg.type === "action.reverse") {
      void this.reverse(id, panel, msg);
    } else if (msg.type === "pin.add") {
      void this.pinCommit(id, panel, msg.refs, msg.finding);
    } else if (msg.type === "pin.unpin") {
      void this.unpin(id, panel, msg.interpretationId, msg.reason);
    } else if (msg.type === "verdict.submit") {
      void this.submitVerdict(id, panel, msg);
    } else if (msg.type === "hyp.ack") {
      void this.ackHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
    } else if (msg.type === "hyp.revise") {
      void this.reviseHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
    } else if (msg.type === "hyp.abandon") {
      void this.abandonHypothesis(id, panel, msg.hypothesisRef, msg.reason);
    } else if (msg.type === "hyp.new") {
      void this.newHypothesis(id, panel, msg.statement, msg.handle);
    } else if (msg.type === "pred.new") {
      void this.newPrediction(id, panel, msg.hypothesisRef, msg.statement, msg.discriminates, msg.testKind);
    } else if (msg.type === "pred.result") {
      void this.recordPredictionResult(id, panel, msg);
    } else if (msg.type === "pred.withdraw") {
      void this.withdrawPrediction(id, panel, msg.predictionRef, msg.reason);
    } else if (msg.type === "hyp.proposeTests") {
      void this.proposeTests(id, panel, msg.hypothesisRef, msg.statement, msg.handle, msg.rivals);
    } else if (msg.type === "pred.test") {
      void this.testPrediction(id, panel, msg);
    } else if (msg.type === "hyp.runTests") {
      void this.runTests(id, panel, msg.hypothesisRef, msg.statement, msg.handle, msg.rivals);
    } else if (msg.type === "hyp.decide") {
      void this.decideHypothesis(id, panel, msg.hypothesisRef, msg.statement, msg.handle, msg.rivals);
    } else if (msg.type === "enablement.apply") {
      void this.applyEnablement(id, panel, msg);
    } else if (msg.type === "comms.act") {
      void this.commsAct(id, panel, msg.threadId, msg.verb);
    } else if (msg.type === "comms.snooze") {
      void this.commsSnooze(id, panel, msg.threadId, msg.hours);
    } else if (msg.type === "comms.followup") {
      void this.commsFollowUp(id, panel, msg);
    } else if (msg.type === "lifecycle") {
      void this.lifecycleTransition(id, panel, msg);
    } else if (msg.type === "evidence.resolve") {
      void this.resolveLabel(panel, msg.ref);
    } else if (msg.type === "inspector.open") {
      // An action record is re-sent by reads (resendActionRecords); answering
      // it in the same order keeps an older answer from overwriting a newer one.
      void (msg.target.kind === "action"
        ? this.cache.exclusive(id, () => this.answerInspector(id, panel, msg.target))
        : this.answerInspector(id, panel, msg.target));
    } else if (msg.type === "queue.get") {
      void this.cache.exclusive(id, () => this.postQueue(panel));
    } else if (msg.type === "queue.start") {
      void this.startFromCase(msg.caseNumber);
    } else if (msg.type === "knowledge.search") {
      void this.searchKnowledge(id, panel, msg.text);
    } else if (msg.type === "notify.request") {
      void this.requestNotification(id, panel, msg);
    } else if (msg.type === "view.state") {
      this.onViewState?.(id, msg.lens, msg.companion);
    } else if (msg.type === "export.open") {
      void this.openMarkdownExport(id);
    } else if (msg.type === "copy") {
      void vscode.env.clipboard.writeText(msg.text);
    } else if (msg.type === "investigation.open") {
      void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
    } else if (msg.type === "rename") {
      // The in-document rename card confirmed. Drafts have no aggregate id yet
      // (the pencil is hidden there), but guard so a synthetic __draft__ key
      // never reaches the backend.
      if (!InvestigationDocuments.isDraft(id)) {
        void this.applyRename(id, panel, msg.title);
      }
    }
  }

  /**
   * Persist a draft's seed and become the live investigation. On success the
   * panel is re-keyed from its synthetic draft key to the real aggregate id and
   * loaded in place — the surface never closes, it transforms.
   */
  private async seedDraft(
    ref: { id: string },
    panel: vscode.WebviewPanel,
    seed: { value?: string; kind?: string; alertId?: string; source?: string },
  ): Promise<void> {
    // Second line of the double-submit defense (the webview also guards): once
    // this draft key has a create in flight, or has already been re-keyed to a
    // real id, further seed messages are dropped — never a duplicate POST.
    if (!InvestigationDocuments.isDraft(ref.id) || this.seeding.has(ref.id)) {
      return;
    }
    this.seeding.add(ref.id);
    const draftKey = ref.id;
    try {
      const created = seed.alertId !== undefined
        ? await this.client.createInvestigation("", {
            type: "alert", alertId: seed.alertId, source: seed.source,
          })
        : await this.client.createInvestigationFromInput(seed.value ?? "", seed.kind ?? "");
      this.open.delete(ref.id);
      ref.id = created.id;
      this.open.set(created.id, panel);
      panel.title = `⚖ ${created.title}`;
      void this.post(panel, { type: "seeded" });
      void this.load(created.id, panel);
      // Case-rooted: apply the write-back posture (adr/0032) — the offer
      // opens once the load above has rendered.
      this.offerCaseNote(created);
      // The trees should show the new investigation immediately — and, for a
      // case seed, the Cases view should now point the case at it.
      void vscode.commands.executeCommand("reckon.refreshInvestigations");
      void vscode.commands.executeCommand("reckon.refreshCases");
    } catch (err) {
      void this.post(panel, { type: "draft.error", message: errText(err) });
    } finally {
      this.seeding.delete(draftKey);
    }
  }

  /**
   * Search the case system of record for the draft's "from a case" surface
   * (14 §4.1): the query and its results live on the panel, never the
   * top-of-window quick input (13 §3). A read failure surfaces inline in the
   * sub-form; seeding from a picked case runs the ordinary seedDraft path
   * (kind "case"), which fails closed server-side on a bad case id.
   */
  private async searchCases(panel: vscode.WebviewPanel, filter: string, queue?: string): Promise<void> {
    try {
      const cases = await this.client.queryExternalCases(filter.trim() || undefined, undefined, queue || undefined);
      void this.post(panel, { type: "draft.cases", cases });
    } catch (err) {
      void this.post(panel, { type: "draft.caseError", message: errText(err) });
    }
  }

  /**
   * Apply the tenant's case write-back posture to a just-created
   * investigation (adr/0032 as amended). Only "auto" acts here: it requests
   * the note now. Under "confirm" (the default) nothing happens at creation —
   * the document carries a "Note the case" action on its seed line, usable
   * whenever the analyst wants, once. "off" (or a non-case seed) sends no
   * offer at all, so the absence of created.caseNote is the whole test.
   */
  offerCaseNote(created: InvestigationDetail): void {
    if (!created.caseNote || created.caseNoteOnSeed !== "auto") {
      return;
    }
    void this.client.requestCaseNote(created.id).then(
      (r) => void vscode.window.showInformationMessage(`reckon: note on ${created.caseNote?.caseId} requested (${r.status.toLowerCase().replace(/_/g, " ")})`),
      (err: unknown) => void vscode.window.showWarningMessage(`reckon: could not request the case note: ${errText(err)}`),
    );
  }

  /** The analyst confirmed the note: request it as an action, then reload. */
  private async postCaseNote(id: string, panel: vscode.WebviewPanel, body: string): Promise<void> {
    try {
      const r = await this.client.requestCaseNote(id, body.trim() || undefined);
      void vscode.window.showInformationMessage(`reckon: case note requested (${r.status.toLowerCase().replace(/_/g, " ")})`);
      void this.refresh(id, panel);
    } catch (err) {
      void vscode.window.showWarningMessage(`reckon: could not request the case note: ${errText(err)}`);
    }
  }

  /**
   * Render the unseeded draft, carrying the tenant's configured case queue
   * (14 §4.2) when the capability layer reports one, so the "From a case…"
   * path opens on the analyst's queue instead of a blank search. A missing or
   * unreachable capability layer is not an error here — the draft simply
   * behaves as before.
   */
  private async postDraft(panel: vscode.WebviewPanel, mode?: "case"): Promise<void> {
    let caseQueue: string | undefined;
    try {
      caseQueue = await this.client.caseQueue();
    } catch {
      caseQueue = undefined;
    }
    void this.post(panel, { type: "draft", mode, caseQueue });
  }

  /** Refresh every open document (e.g. after sign-out flips to sign-in). */
  refreshAll(): void {
    for (const [id, panel] of this.open) {
      // Drafts have no aggregate id — loading one is a guaranteed 4xx.
      if (!InvestigationDocuments.isDraft(id)) {
        void this.load(id, panel);
      }
    }
  }

  /** Drop everything held for a closed panel, including a pending dispatch watch. */
  private forget(id: string): void {
    this.open.delete(id);
    this.cache.forget(id);
    this.knowledgeRead.delete(id);
    const watch = this.dispatchWatch.get(id);
    if (watch) {
      clearTimeout(watch.timer);
      this.dispatchWatch.delete(id);
    }
  }

  /** Whether this panel is still the open document for the investigation. */
  private isOpen(id: string, panel: vscode.WebviewPanel): boolean {
    return this.open.get(id) === panel;
  }

  /**
   * Build the investigation's snapshot (impl §3.1) and post it whole; the
   * optional layers' failures become null slices, never a broken panel. The
   * bulkier lenses (the derived views, the knowledge rail, enablement) follow
   * as slice patches so the document renders as soon as the core is in.
   */
  private load(id: string, panel: vscode.WebviewPanel): Promise<void> {
    return this.cache.exclusive(id, async () => {
      void this.post(panel, { type: "loading" });
      let snapshot: InvestigationSnapshot;
      try {
        snapshot = await this.cache.snapshot(id);
      } catch (err) {
        const message = errText(err);
        this.log.error(`load investigation ${id}: ${message}`);
        void this.post(panel, { type: "error", message });
        return;
      }
      if (!this.isOpen(id, panel)) {
        return; // closed while reading
      }
      panel.title = `⚖ ${snapshot.investigation.title}`;
      void this.post(panel, { type: "state.snapshot", seq: snapshot.seq, snapshot });
      this.onLoaded?.(id, snapshot.seq);
      void this.me().then((m) => { if (m) void this.post(panel, { type: "me", name: m.name, principal: m.principal }); }).catch(() => undefined);
      // Dispatch is asynchronous (a Temporal workflow) and its result events are
      // recorded by the worker, not the HTTP path — keep refreshing while any
      // action is mid-dispatch so DISPATCHING never sticks past the outcome.
      this.scheduleDispatchWatch(id, panel, snapshot.actions ?? []);
      // The event-time strip + evidence graph: lenses over the cited refs,
      // stamped with the sequence of the thread they were built from, so the
      // webview drops them if a newer read lands first.
      void this.postDerivedViews(panel, snapshot.thread ?? [], snapshot.seq);
      // The queue and the inspector's action records carry no sequence: they
      // finish inside this operation, so a later read cannot be overtaken. A
      // reload keeps the inspector open with its records, so a record with
      // newer events is re-sent like after any other read.
      await Promise.all([this.postQueue(panel), this.resendActionRecords(id, panel, snapshot.seq)]);
      // The enablement view (gap hints + form schemas). Absence is honest.
      const latestEnablement = this.cache.beginRequest(id, "enablement");
      void this.client.enablement()
        .then((data) => data, () => null)
        .then((data) => { if (latestEnablement()) void this.patch(id, panel, { enablement: data }); });
      // A reload re-reads the knowledge rail.
      this.knowledgeRead.delete(id);
      this.readKnowledge(id, panel, snapshot.investigation.state);
    });
  }

  /**
   * The implicit-retrieval knowledge rail (design/06 §5.1), once per panel and
   * only while the investigation is workable — it spawns the sidecar and runs
   * retrieval, so it is not worth it for a concluded or archived case. A
   * refresh calls it too, so a case reopened in place gets its rail.
   */
  private readKnowledge(id: string, panel: vscode.WebviewPanel, state: string): void {
    if (this.knowledgeRead.has(id) || !["DRAFT", "ACTIVE", "PAUSED"].includes(state)) {
      return;
    }
    this.knowledgeRead.add(id);
    const latest = this.cache.beginRequest(id, "knowledge");
    void this.transport.getKnowledge(id)
      .then((items) => { if (latest()) void this.patch(id, panel, { knowledge: items }); })
      .catch((err: unknown) => this.log.debug(`knowledge rail unavailable: ${errText(err)}`));
  }

  /**
   * Post slices that are not read from the event log (knowledge, enablement)
   * with the current watermark, so the webview applies them over what it holds.
   */
  private patch(id: string, panel: vscode.WebviewPanel, patch: Partial<InvestigationSnapshot>): Thenable<boolean> {
    return this.post(panel, { type: "state.patch", seq: this.cache.watermark(id), patch });
  }

  /**
   * Re-read the event-sourced slices after a write (the analyst's, the
   * agent's, or a dispatch landing) and post them as one patch with the
   * investigation's sequence — no loading state, no re-read of the layers a
   * write cannot change. The work queue, the derived views and any action
   * record the inspector holds follow when they changed.
   */
  private refresh(id: string, panel: vscode.WebviewPanel): Promise<void> {
    return this.cache.exclusive(id, async () => {
      let read: Awaited<ReturnType<InvestigationCache["refresh"]>>;
      try {
        read = await this.cache.refresh(id);
      } catch (err) {
        this.log.debug(`refresh ${id}: ${errText(err)}`);
        return;
      }
      if (!this.isOpen(id, panel)) {
        return; // closed while reading: no title, no seen-tracker advance, no re-armed watch
      }
      const { seq, patch } = read;
      if (patch.investigation) {
        panel.title = `⚖ ${patch.investigation.title}`;
        this.readKnowledge(id, panel, patch.investigation.state);
      }
      void this.post(panel, { type: "state.patch", seq, patch });
      this.onLoaded?.(id, seq);
      if (patch.actions) {
        this.scheduleDispatchWatch(id, panel, patch.actions);
      }
      if (patch.thread) {
        void this.postDerivedViews(panel, patch.thread, seq); // sequenced: a stale one is dropped
      }
      // Unsequenced follow-ups finish inside this operation (see load).
      await Promise.all([this.postQueue(panel), this.resendActionRecords(id, panel, seq)]);
    });
  }

  /** Re-send the inspector's action records read before the investigation's newest event. */
  private async resendActionRecords(id: string, panel: vscode.WebviewPanel, seq: number): Promise<void> {
    for (const actionId of this.cache.staleRecords(id, seq)) {
      try {
        const record = await this.client.actionRecord(actionId);
        this.cache.servedRecord(id, actionId, seq);
        void this.post(panel, { type: "inspector.action", actionId, record });
      } catch (err) {
        this.log.debug(`action record ${actionId}: ${errText(err)}`);
      }
    }
  }

  private async commsAct(id: string, panel: vscode.WebviewPanel, threadId: string, verb: "ack" | "done"): Promise<void> {
    try {
      await this.client.commsAct(threadId, verb);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: comms ${verb} failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Push a thread's follow-up clock. */
  private async commsSnooze(id: string, panel: vscode.WebviewPanel, threadId: string, hours: number): Promise<void> {
    try {
      await this.client.commsSnooze(threadId, hours);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: snooze failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Send a follow-up: a NEW notify.* action carrying thread_ref (binding §4 —
   * every outbound message is an action through Gate 2), which lands in the
   * approval queue as a preview card. On its SUCCEEDED dispatch the thread's
   * counter increments and its clock restarts.
   */
  private async commsFollowUp(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { threadId: string; actionType: string; target: string; subject: string; message: string; followUpHours: number },
  ): Promise<void> {
    const parameters: Record<string, unknown> = {
      thread_ref: msg.threadId,
      follow_up_hours: msg.followUpHours,
    };
    if (msg.actionType === "notify.email") {
      parameters.subject = msg.subject ? `Re: ${msg.subject}` : "Follow-up";
      parameters.body = msg.message;
    } else {
      parameters.message = msg.message;
    }
    try {
      const res = await this.client.requestAction({
        investigationRef: id,
        actionType: msg.actionType,
        targets: [{ resolvedIdentifier: msg.target }],
        parameters,
        rationale: `follow-up on comms thread ${msg.threadId}`,
      });
      void vscode.window.showInformationMessage(
        `reckon: follow-up requested (${res.status}) — approve to send`,
      );
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: follow-up failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Pin evidence — the analyst's curation act, on the human token. The finding
   * note is collected in-webview (the pin card), so this just records it.
   */
  private async pinCommit(id: string, panel: vscode.WebviewPanel, refs: string[], finding: string): Promise<void> {
    if (!refs.length || finding.trim() === "") {
      return;
    }
    try {
      await this.client.pinEvidence(id, finding.trim(), refs);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: pin failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Fetch one evidence doc through the cache (null = unfetchable). */
  private async getDoc(ref: string): Promise<EvidenceDoc | null> {
    const hit = this.docCache.get(ref);
    if (hit !== undefined) {
      return hit;
    }
    let doc: EvidenceDoc | null;
    try {
      doc = await this.client.evidence(ref);
    } catch {
      doc = null;
    }
    this.docCache.set(ref, doc);
    return doc;
  }

  /**
   * Resolve a human label for an evidence ref (host · WIN-FILE01, account ·
   * svc_backup) from the evidence endpoint, cached. Posts back only on a hit;
   * a ref that can't be opened keeps its type-only label in the webview.
   */
  private async resolveLabel(panel: vscode.WebviewPanel, ref: string): Promise<void> {
    let label = this.labelCache.get(ref);
    if (label === undefined) {
      const doc = await this.getDoc(ref);
      label = doc ? deriveEvidenceLabel(doc) : "";
      this.labelCache.set(ref, label);
    }
    if (label) {
      void this.post(panel, { type: "evidence.label", ref, label });
    }
  }

  /**
   * The work queue (08 §8.3): the analyst's investigations, needs-me-first,
   * and the case system's queue filtered to cases with no investigation. Both
   * reads are best-effort: a missing case layer leaves the Cases section
   * absent, never a broken document.
   */
  private async postQueue(panel: vscode.WebviewPanel): Promise<void> {
    try {
      const [investigations, queue] = await Promise.all([
        this.client.investigations(),
        this.client.cases().catch(() => null),
      ]);
      const cases = (queue?.cases ?? []).filter((c) => !c.investigation);
      void this.post(panel, { type: "queue", investigations, cases, caseSource: queue?.queue });
    } catch (err) {
      this.log.debug(`queue: ${errText(err)}`);
    }
  }

  /** Start an investigation from a case row: the seed path, then open it. */
  private async startFromCase(caseNumber: string): Promise<void> {
    try {
      const created = await this.client.createInvestigationFromInput(caseNumber, "case");
      this.show(created.id, created.title);
      this.offerCaseNote(created);
      void vscode.commands.executeCommand("reckon.refreshInvestigations");
      void vscode.commands.executeCommand("reckon.refreshCases");
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: could not start from ${caseNumber} — ${errText(err)}`);
    }
  }

  /** The Knowledge section's scoped search (09 §9.6): the session merges the hits. */
  private async searchKnowledge(id: string, panel: vscode.WebviewPanel, text: string): Promise<void> {
    // The search's answer is the session's whole set with the hits merged in,
    // so it supersedes any knowledge read still in flight.
    this.knowledgeRead.add(id);
    const latest = this.cache.beginRequest(id, "knowledge");
    try {
      const items = await this.transport.searchKnowledge(id, text);
      if (latest()) void this.patch(id, panel, { knowledge: items });
    } catch (err) {
      void vscode.window.showWarningMessage(`reckon: knowledge search failed — ${errText(err)}`);
    }
  }

  /**
   * The outbound notification (08 §8.4 Response): a notify.* action whose
   * pre-send preview is the approval. Through Gate 2 like any action; it lands
   * in the queue as a preview card.
   */
  private async requestNotification(id: string, panel: vscode.WebviewPanel, msg: { channel: string; target: string; subject?: string; body: string }): Promise<void> {
    const parameters: Record<string, unknown> = msg.channel === "notify.email"
      ? { subject: msg.subject || "Investigation update", body: msg.body }
      : { message: msg.body };
    try {
      const res = await this.client.requestAction({
        investigationRef: id,
        actionType: msg.channel,
        targets: [{ resolvedIdentifier: msg.target }],
        parameters,
        rationale: "stakeholder notification from the investigation record",
      });
      void vscode.window.showInformationMessage(`reckon: notification requested (${res.status}) — approve to send`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: notification failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * The derived lenses over the thread's citations (the event-time strip and
   * the evidence graph, src/derivedViews.ts), sharing one round of cached
   * evidence fetches — bounded; failures just leave gaps (both are lenses,
   * never load-bearing). Posted as a slice patch.
   */
  private async postDerivedViews(panel: vscode.WebviewPanel, thread: ThreadEntry[], seq: number): Promise<void> {
    const derived = await buildDerivedViews(thread, (ref) => this.getDoc(ref));
    void this.post(panel, { type: "state.patch", seq, patch: { derived } });
  }

  /**
   * Answer an inspector pane (impl §2.4): the webview opened it from the
   * snapshot and asks for what the record holds beyond it — the evidence
   * document with its cross-investigation memory (current investigation
   * excluded: "also seen in" means OTHERS), a procedure, a committed
   * transcript, an action's record, a case summary. A failed fetch posts an
   * honest error for that target; nothing else in the document is affected.
   */
  private async answerInspector(id: string, panel: vscode.WebviewPanel, target: InspectorTarget): Promise<void> {
    try {
      switch (target.kind) {
        case "ref": {
          const doc = await this.getDoc(target.ref);
          if (!doc) {
            void this.post(panel, { type: "inspector.error", target: target.ref, message: "no persisted record for this ref — it may predate telemetry persistence" });
            return;
          }
          let apps: Appearance[] = [];
          try {
            apps = (await this.client.appearances(target.ref)).filter((a) => a.investigationId !== id);
          } catch (err) {
            this.log.debug(`appearances ${target.ref}: ${errText(err)}`);
          }
          void this.post(panel, { type: "inspector.evidence", ref: target.ref, doc, appearances: apps });
          return;
        }
        case "sop":
          void this.post(panel, { type: "inspector.procedure", sopId: target.sopId, sop: await this.client.sop(target.sopId) });
          return;
        case "turn": {
          const body = await this.cache.transcript(target.interpretationId);
          void this.post(panel, { type: "inspector.turn", interpretationId: target.interpretationId, body });
          return;
        }
        case "action": {
          const record = await this.client.actionRecord(target.actionId);
          this.cache.servedRecord(id, target.actionId, this.cache.watermark(id));
          void this.post(panel, { type: "inspector.action", actionId: target.actionId, record });
          return;
        }
        case "case":
          void this.post(panel, { type: "inspector.case", investigationRef: target.investigationRef, summary: await this.client.caseSummary(target.investigationRef) });
          return;
        default:
          return; // thread, person: rendered from the snapshot alone
      }
    } catch (err) {
      const key = "ref" in target ? target.ref : "sopId" in target ? target.sopId : "interpretationId" in target ? target.interpretationId
        : "actionId" in target ? target.actionId : "investigationRef" in target ? target.investigationRef : "";
      void this.post(panel, { type: "inspector.error", target: key, message: errText(err) });
    }
  }

  /**
   * Un-pin = supersession with a reason; the pin stays visible, struck. The
   * reason is collected in the webview prompt card, so this just records it.
   */
  private async unpin(id: string, panel: vscode.WebviewPanel, interpretationId: string, reason: string): Promise<void> {
    if (reason.trim() === "") {
      return;
    }
    try {
      await this.client.supersedeInterpretation(interpretationId, id, reason.trim());
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: un-pin failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Acknowledge an AI-PROPOSED hypothesis — the human taking ownership of the
   * line of inquiry (02 §2.9). The aggregate enforces that this is a human act;
   * a rejection surfaces verbatim.
   */
  private async ackHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, restatement?: string, handle?: string): Promise<void> {
    try {
      await this.client.acknowledgeHypothesis(id, hypothesisRef, {
        restatement: restatement?.trim() || undefined,
        handle: handle?.trim() || undefined,
      });
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: adopt failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Rename / restate an OPEN hypothesis (01 §x-hypothesis; human, untested-only for the statement). */
  private async reviseHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, restatement?: string, handle?: string): Promise<void> {
    const r = restatement?.trim() || undefined;
    const h = handle?.trim() || undefined;
    if (!r && !h) {
      return;
    }
    try {
      await this.client.reviseHypothesis(id, hypothesisRef, { restatement: r, handle: h });
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: revise failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Abandon a live hypothesis the analyst is not pursuing (terminal; the thread keeps it). */
  private async abandonHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, reason: string): Promise<void> {
    try {
      await this.client.abandonHypothesis(id, hypothesisRef, reason.trim() || "abandoned from the workbench — not pursued");
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: abandon failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Record the analyst's own test under a hypothesis (01 §x-prediction: either actor authors predictions). */
  private async newPrediction(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, statement: string, discriminates: string[], testKind?: string): Promise<void> {
    if (statement.trim() === "") {
      return;
    }
    try {
      await this.client.recordPrediction(id, hypothesisRef, statement.trim(), discriminates ?? [], testKind);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: prediction failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * The analyst records a prediction's result themselves (adr/0054): the
   * outcome plus what was observed — a person's answer, a check made by hand —
   * which the engine keeps as an attestation the outcome cites. Rejections
   * (e.g. the prediction is no longer UNTESTED) surface verbatim.
   */
  private async recordPredictionResult(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { predictionRef: string; status: string; content: string; source?: string; channel?: string },
  ): Promise<void> {
    if (msg.content.trim() === "") {
      return;
    }
    const how = msg.channel?.trim() ? ` by ${msg.channel.trim()}` : "";
    try {
      await this.client.recordPredictionResult(id, msg.predictionRef, msg.status, `result recorded by the analyst${how}`, {
        content: msg.content.trim(),
        source: msg.source?.trim() || undefined,
        channel: msg.channel?.trim() || undefined,
      });
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: record result failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Withdraw an untested prediction — a status, never a deletion. */
  private async withdrawPrediction(id: string, panel: vscode.WebviewPanel, predictionRef: string, reason: string): Promise<void> {
    try {
      await this.client.withdrawPrediction(id, predictionRef, reason.trim() || "withdrawn from the workbench");
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: withdraw failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Record an analyst-authored hypothesis — e.g. the alternate explanation
   * that competes with the AI's. Lands OPEN (human-authored); rejections
   * surface verbatim.
   */
  private async newHypothesis(id: string, panel: vscode.WebviewPanel, statement: string, handle?: string): Promise<void> {
    if (statement.trim() === "") {
      return;
    }
    try {
      await this.client.recordHypothesis(id, statement.trim(), handle?.trim() || undefined);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: hypothesis failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /** Record the verdict of record — always the analyst's act here (human token). */
  private async submitVerdict(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { disposition: string; rationale: string; refs: string[] },
  ): Promise<void> {
    try {
      await this.client.recordVerdict(id, msg.disposition, msg.rationale, msg.refs);
      void vscode.window.showInformationMessage(`reckon: verdict recorded — ${msg.disposition}`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: verdict failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Apply an enablement change (11 §5.1). The webview form collected the
   * non-secret fields; here the flow becomes NATIVE: a modal confirm bound to
   * the human's identity, and secret fields captured through a secure input —
   * out-of-band of the webview and the conversation, so a secret value never
   * touches chat content or the transcript store. What is captured for an
   * x-secret field is a secret REFERENCE (keychain:// env:// vault://), per
   * 11 §4.3 — the engine refuses literals at config load.
   */
  private async applyEnablement(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { adapter: string; enabled: boolean; config: Record<string, string>; secretFields: string[] },
  ): Promise<void> {
    const verb = msg.enabled ? "Enable" : "Disable";
    const pick = await vscode.window.showWarningMessage(
      `reckon: ${verb} adapter "${msg.adapter}"? This edits the tenant config file (the source of truth) and records an attributed config-plane audit entry.`,
      { modal: true },
      verb,
    );
    if (pick !== verb) {
      return;
    }
    const config = { ...msg.config };
    for (const field of msg.secretFields ?? []) {
      const v = await vscode.window.showInputBox({
        title: `reckon — ${msg.adapter}.${field} (secret reference)`,
        prompt: "A secret REFERENCE (keychain://…, env://NAME, vault://…) — never the literal value; the engine refuses literals at config load.",
        password: true,
        ignoreFocusOut: true,
      });
      if (v === undefined) {
        return; // cancelled — nothing applied
      }
      if (v.trim() !== "") {
        config[field] = v.trim();
      }
    }
    try {
      await this.client.applyEnablement(msg.adapter, msg.enabled, config);
      void vscode.window.showInformationMessage(
        `reckon: ${msg.adapter} ${msg.enabled ? "enabled" : "disabled"} — capability surface rebuilt`,
      );
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: enablement failed — ${errText(err)}`);
    }
    await this.load(id, panel);
  }

  /**
   * Drive one lifecycle transition — always the analyst's act on the human
   * token (the aggregate's actor allowlist bars an AI delegate from
   * conclude/reopen/archive). Conclude mints the STIX Report id here — the
   * Report object itself is v1; the aggregate requires the reference — and the
   * aggregate refuses to conclude without a verdict of record, an error we
   * surface verbatim. A refresh follows: status, thread, and (after conclude)
   * the export trail all changed.
   */
  private async lifecycleTransition(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { transition: string; reason?: string; summary?: string },
  ): Promise<void> {
    try {
      const body: { transition: string; reason?: string; reportRef?: string; summary?: string } = {
        transition: msg.transition,
        reason: msg.reason?.trim() || undefined,
      };
      if (msg.transition === "conclude") {
        body.reportRef = `report--${randomUUID()}`;
        body.summary = msg.summary?.trim() || undefined;
      }
      const res = await this.client.lifecycle(id, body);
      const exportNote = res.exportWorkflowId ? " · export started" : "";
      void vscode.window.showInformationMessage(`reckon: investigation → ${res.status}${exportNote}`);
      if (msg.transition === "conclude") {
        // The client-side tier of the two-tier knowledge summary (design/06
        // §3.2): the sidecar — the only place the BYOK model key lives —
        // writes the narrative from its own conversation and posts it.
        // Fire-and-forget: the server's structured baseline stands either
        // way; a narrative failure must never look like a conclude failure.
        void this.transport.summarizeConcluded(id).then(
          () => vscode.window.setStatusBarMessage("reckon: knowledge summary written", 5000),
          (err: unknown) => console.warn("reckon: knowledge summary narrative failed (baseline stands):", err),
        );
        // Candidate-SOP generalization (design/06: the compounding loop) — the
        // model drafts a reusable SOP from this case + the SOPs and similar past
        // cases it consulted; the analyst reviews and accepts or discards it.
        // Fire-and-forget, like the summary: a failed draft never touches the
        // conclude UX.
        void this.proposeCandidateSOP(id);
      }
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: ${msg.transition} failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Generalize a candidate SOP from the concluded investigation and let the
   * analyst accept or discard it (design/06: the compounding loop). The draft
   * opens as an editable markdown document — the review surface — so the analyst
   * can refine it before accepting; "Add to SOP library" imports the CURRENT
   * editor content through the same markdown path as `Import SOPs…`, so edits are
   * honored and the source URL keys the SOP to this investigation (re-accepting
   * revises rather than duplicates). Discarding leaves nothing behind. The whole
   * thing is best-effort — a failed or unwarranted draft is silent.
   */
  private async proposeCandidateSOP(id: string): Promise<void> {
    let candidate: SOPCandidate;
    try {
      candidate = await this.transport.proposeSOP(id);
    } catch (err) {
      console.warn("reckon: candidate SOP generation failed:", err);
      return;
    }
    if (!candidate?.warranted || !candidate.title.trim() || !candidate.body.trim()) {
      // The model judged no new procedure worth capturing (an existing SOP
      // already covers it, or the case was too specific). Nothing to surface.
      return;
    }

    const markdown = candidateSOPMarkdown(candidate, id);
    const doc = await vscode.workspace.openTextDocument({ content: markdown, language: "markdown" });
    await vscode.window.showTextDocument(doc, { preview: false });
    const choice = await vscode.window.showInformationMessage(
      `reckon: drafted a candidate SOP from this investigation — “${candidate.title.trim()}”. Review it, then add it to the library or discard.`,
      "Add to SOP library",
      "Discard",
    );
    if (choice !== "Add to SOP library") {
      return; // discarded or dismissed — leave the untitled doc for the analyst to close
    }
    try {
      const content = doc.getText(); // honor any edits the analyst made while reviewing
      const filename = slugifyTitle(candidate.title) + ".md";
      const res = await this.client.importSOPMarkdown(filename, content);
      void vscode.window.showInformationMessage(
        `reckon: SOP “${res.title}” ${res.outcome === "revised" ? "updated" : "added"} — consultable in future investigations.`,
      );
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: adding the SOP failed — ${errText(err)}`);
    }
  }

  /**
   * Approve one action — the analyst's own act, extension→backend on the
   * HUMAN token, never through the sidecar (implementation/agent-sidecar.md
   * §5). A T3 approval carries the typed challenge (04 §5.5), collected in the
   * webview prompt card before this fires.
   */
  private async approve(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { actionId: string; tier: string; actionType: string; challenge?: string },
  ): Promise<void> {
    if (msg.tier === "T3" && (!msg.challenge || msg.challenge.trim() === "")) {
      await this.refresh(id, panel); // no challenge — re-enable the buttons
      return;
    }
    try {
      const decision = await this.client.approveAction(msg.actionId, msg.challenge);
      void vscode.window.showInformationMessage(
        `reckon: ${msg.actionType} → ${decision.status}${decision.stage ? ` (${decision.stage})` : ""}`,
      );
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: approve failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
    // The refresh arms the dispatch watch: the approved action is now
    // APPROVED/EXECUTING, so the ledger keeps refreshing until it settles.
  }

  /**
   * Keep the ledger honest through an async dispatch: while any action sits in
   * a transient state (APPROVED/EXECUTING), re-fetch on a short interval until
   * everything is terminal — regardless of which surface (this panel, the
   * agent, auto-approval) started the dispatch. Bounded (~3 min) so a truly
   * wedged workflow cannot poll forever; the ledger then shows the honest
   * transient state until the next interaction.
   */
  private scheduleDispatchWatch(id: string, panel: vscode.WebviewPanel, actions: ActionRow[]): void {
    const existing = this.dispatchWatch.get(id);
    const inFlight = actions.some((a) => a.status === "APPROVED" || a.status === "EXECUTING");
    if (!inFlight) {
      if (existing) {
        clearTimeout(existing.timer);
        this.dispatchWatch.delete(id);
      }
      return;
    }
    const ticks = existing ? existing.ticks + 1 : 0;
    if (existing) {
      clearTimeout(existing.timer);
    }
    if (ticks >= 45 || this.open.get(id) !== panel) {
      this.dispatchWatch.delete(id);
      return;
    }
    const timer = setTimeout(() => {
      void this.refresh(id, panel); // re-arms (or clears) the watch from fresh state
    }, 4000);
    this.dispatchWatch.set(id, { timer, ticks });
  }

  /** Reject one action — human token only; reason from the webview prompt card. */
  private async reject(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { actionId: string; actionType: string; reason: string },
  ): Promise<void> {
    try {
      const decision = await this.client.rejectAction(
        msg.actionId,
        msg.reason.trim() === "" ? "rejected from the workbench" : msg.reason.trim(),
      );
      void vscode.window.showInformationMessage(`reckon: ${msg.actionType} → ${decision.status}`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: reject failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Re-request an expired action — a fresh request (same targets & evidence,
   * new window, retry_of lineage) the analyst then approves. Not a bypass of
   * expiry: the conscious re-affirmation it exists to elicit.
   */
  private async rerequest(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { actionId: string; actionType: string; rationale: string },
  ): Promise<void> {
    try {
      await this.client.rerequestAction(msg.actionId, msg.rationale);
      void vscode.window.showInformationMessage(`reckon: ${msg.actionType} re-requested — approve the new request`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: re-request failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * Request the reversal of a SUCCEEDED action (04 §7): a NEW x-action of the
   * descriptor's declared inverse type, at the same tier, linked by
   * reversal_of_ref, which lands in the approval queue like any action. The
   * engine validates the pairing and the original's status; the host only
   * looks up the inverse type so the request names it.
   */
  private async reverse(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; rationale: string }): Promise<void> {
    try {
      const [record, types] = await Promise.all([this.client.actionRecord(msg.actionId), this.client.actionTypes()]);
      const original = record.action;
      const inverse = types.find((t) => t.actionType === original.actionType)?.reversibleBy;
      if (!inverse) {
        void vscode.window.showWarningMessage(`reckon: ${original.actionType} declares no reversal in the action catalog`);
        return;
      }
      const res = await this.client.requestAction({
        investigationRef: id,
        actionType: inverse,
        targets: original.targets.map((t) => ({ resolvedIdentifier: t })),
        evidenceRefs: original.evidenceRefs,
        rationale: msg.rationale.trim() || `reversal of ${original.actionType} ${original.actionId}`,
        reversalOfRef: original.actionId,
      });
      void vscode.window.showInformationMessage(`reckon: reversal requested as ${inverse} (${res.status}) — approve to run it`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: reversal failed — ${errText(err)}`);
    }
    await this.refresh(id, panel);
  }

  /**
   * The live markdown export in an editor tab (binding §6 item 10): rendered
   * backend-side from the projections, opened as an untitled markdown doc the
   * analyst can read, save, or paste into a ticket unedited.
   */
  private async openMarkdownExport(id: string): Promise<void> {
    try {
      const md = await this.client.exportMarkdown(id);
      const doc = await vscode.workspace.openTextDocument({ content: md, language: "markdown" });
      await vscode.window.showTextDocument(doc, { preview: false, viewColumn: vscode.ViewColumn.Beside });
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: export failed — ${errText(err)}`);
    }
  }

  /**
   * Fire the propose-tests aside (ui/02 §2.9, the "btw" lens): a scoped
   * agent turn the analyst ordered with one click. Same audit record as any
   * turn — the transcript commits to the one thread — but rendered collapsed
   * in the conversation; the predictions it records land on the tracker.
   * The "⚡ Propose tests:" prefix doubles as the cold-restore marker.
   */
  private async proposeTests(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): Promise<void> {
    const name = hypLabel(handle, statement);
    const instruction = `⚡ Propose tests: for the hypothesis ${hypCite(handle, statement, hypothesisRef)}, `
      + "record the falsifiable predictions that would decide it — one prediction per test, "
      + "each with a concrete test query against the available capability verbs. Prove it or break it. "
      + rivalsClause(rivals, "Read the live rivals too and make at least one prediction DISCRIMINATE — "
        + "opposite expected outcome under this hypothesis and a rival — and set its discriminates to that rival's id. "
        + "A test that comes out the same under both tells us nothing.")
      + "Record them with the tools ONLY. This turn ENDS when the predictions are recorded: do not run any test, "
      + "do not record any prediction outcome, do not record any hypothesis outcome — Run tests and Decide are "
      + "separate steps the analyst orders. Keep your final message to one sentence and do not "
      + "restate the predictions in prose — the tracker renders them.";
    await this.runTurn(id, panel, instruction, { label: `⚡ propose tests · ${name}` });
  }

  /**
   * Run the untested predictions — the execute half of the drivable loop.
   * They are independent claims, so the agent runs them in parallel where
   * possible; outcomes must cite what was observed (the aggregate refuses a
   * CONFIRMED/DISCONFIRMED without test_result_refs).
   */
  private async runTests(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): Promise<void> {
    const name = hypLabel(handle, statement);
    const instruction = `⚡ Run tests: execute the UNTESTED predictions under the hypothesis ${hypCite(handle, statement, hypothesisRef)}. `
      + "They are independent — run their declared test queries in parallel where possible, then record each "
      + "outcome (CONFIRMED / DISCONFIRMED / INCONCLUSIVE) citing the observed evidence as test_result_refs. "
      + "Skip any prediction marked manual — a person tests it and I record its result; name it in one clause and move on. "
      + rivalsClause(rivals, "Where a prediction discriminates against a rival, say in one clause which way the result cuts for that rival. ")
      + "This turn ENDS when the outcomes are recorded: do not record a hypothesis outcome — Decide is a separate "
      + "step the analyst orders. Keep your final message to one or two sentences on what was decisive — do not restate the outcomes; the tracker renders them.";
    await this.runTurn(id, panel, instruction, { label: `⚡ run tests · ${name}` });
  }

  /**
   * Run ONE prediction's declared test — the per-row member of the ⚡ family
   * (adr/0075). Same contract as Run tests, scoped to a single prediction: the
   * outcome is recorded citing evidence, and the turn ends there.
   */
  private async testPrediction(id: string, panel: vscode.WebviewPanel, m: {
    predictionRef: string; predictionName: string; statement: string; queryText?: string; tool?: string;
    hypothesisRef: string; hypStatement: string; handle?: string; rivals?: RivalBrief[];
  }): Promise<void> {
    const name = hypLabel(m.handle, m.hypStatement);
    const declared = m.queryText
      ? `Its declared test${m.tool ? " via " + m.tool : ""}: ${m.queryText} `
      : "";
    const instruction = `⚡ Test ${m.predictionName}: run the test for prediction ${m.predictionRef} under the hypothesis ${hypCite(m.handle, m.hypStatement, m.hypothesisRef)} — "${m.statement}". `
      + declared
      + "Record its outcome (CONFIRMED / DISCONFIRMED / INCONCLUSIVE) citing the observed evidence as test_result_refs. "
      + rivalsClause(m.rivals, "If it discriminates against a rival, say in one clause which way the result cuts for that rival. ")
      + "This turn ENDS when that one outcome is recorded: do not run other tests and do not record a hypothesis outcome — Decide is a separate step the analyst orders. "
      + "Keep your final message to one or two sentences on what was decisive; the tracker renders the outcome.";
    await this.runTurn(id, panel, instruction, { label: `⚡ test ${m.predictionName} · ${name}` });
  }

  /**
   * Decide the hypothesis — the judgment rung, ordered by the analyst once
   * every prediction is decided. The agent weighs the outcomes AGAINST THE
   * RIVALS and records support / refutation / inconclusive citing the
   * decisive test evidence; it declines while a rival's discriminator is
   * untested unless the analyst overrides. Terminal in v0 (01): a changed
   * judgment later is a NEW hypothesis chained via parent_ref.
   */
  private async decideHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): Promise<void> {
    const name = hypLabel(handle, statement);
    const instruction = `⚡ Decide: every prediction under the hypothesis ${hypCite(handle, statement, hypothesisRef)} has been tested. `
      + "Weigh the outcomes and record the hypothesis outcome — support, refutation, or inconclusive — citing the "
      + "decisive test evidence. Be honest about mixed results: confirmed predictions support only what they actually tested. "
      + rivalsClause(rivals, "Decide against the rivals, not by counting confirmations: if a live rival still has an UNTESTED prediction "
        + "that discriminates between you, do NOT record an outcome — the engine refuses it; name that test and ask to run it first (I can run it, withdraw it, or record the outcome myself). ")
      + "Keep your final message to one or two sentences on the judgment and why — the tracker renders the status.";
    await this.runTurn(id, panel, instruction, { label: `⚡ decide · ${name}` });
  }

  /** Drive one analyst turn through the transport, streaming progress to the webview. */
  private async runTurn(id: string, panel: vscode.WebviewPanel, text: string, aside?: { label: string }, includedKnowledge?: string[]): Promise<void> {
    if (aside) {
      void this.post(panel, { type: "aside.start", label: aside.label });
    } else {
      void this.post(panel, { type: "turn.user", text });
      void this.post(panel, { type: "turn.start" });
    }
    try {
      const outcome = await this.transport.turn(id, text, {
        // Both text paths land on the same appendable turn.text case: deltas
        // stream token-by-token (E.4); round-complete text arrives only from a
        // non-streaming provider (mutually exclusive per completion, enforced
        // sidecar-side — never both for the same text).
        onText: (chunk) => void this.post(panel, { type: "turn.text", delta: chunk }),
        onTextDelta: (chunk) => void this.post(panel, { type: "turn.text", delta: chunk }),
        onStep: (round) => void this.post(panel, { type: "turn.step", round }),
        onToolCall: (name, input) => void this.post(panel, { type: "turn.tool", name, input }),
        // coverage/events come distilled from the sidecar (from the full,
        // unclipped payload) — absent for non-envelope results, and the
        // webview renders that honestly instead of a bogus "? · 0".
        onToolResult: (name, _content, isError, coverage, events, refs) =>
          void this.post(panel, { type: "turn.toolResult", name, isError, coverage, events, refs }),
      }, includedKnowledge);
      if (outcome.interpretationId) {
        void this.post(panel, { type: "turn.committed", interpretationId: outcome.interpretationId });
      }
      // Pending actions are NOT posted from the outcome: the finally-load
      // re-fetches the durable queue, so the panel renders the backend's
      // truth (including actions from earlier turns/sessions) with the
      // approve/reject affordances attached.
      if (outcome.error) {
        void this.post(panel, { type: "turn.error", message: outcome.error });
      }
      void this.post(panel, {
        type: "turn.end",
        rounds: outcome.toolRounds,
        usage: {
          input: outcome.usage.input + outcome.usage.cacheRead + outcome.usage.cacheWrite,
          output: outcome.usage.output,
          cacheRead: outcome.usage.cacheRead,
        },
      });
    } catch (err) {
      void this.post(panel, { type: "turn.error", message: errText(err) });
      void this.post(panel, { type: "turn.end" });
    } finally {
      // Reasoning nodes may have changed — refresh the thread panel.
      void this.refresh(id, panel);
    }
  }

  private post(panel: vscode.WebviewPanel, msg: RenderMessage): Thenable<boolean> {
    try {
      return panel.webview.postMessage(msg);
    } catch {
      return Promise.resolve(false); // the panel was disposed while a read was in flight
    }
  }

  /**
   * The panel shell (impl §5.1): one bundled script + stylesheet loaded through
   * asWebviewUri, the vendored fonts beside them. All content is rendered from
   * posted messages; the CSP forbids any other load and any network from the
   * page. No inline styles, so style-src carries no 'unsafe-inline'.
   */
  private html(webview: vscode.Webview): string {
    const nonce = makeNonce();
    const base = vscode.Uri.joinPath(this.extensionUri, "out", "webview");
    const script = webview.asWebviewUri(vscode.Uri.joinPath(base, "investigation.js"));
    const style = webview.asWebviewUri(vscode.Uri.joinPath(base, "investigation.css"));
    const csp = [
      `default-src 'none'`,
      `style-src ${webview.cspSource}`,
      `font-src ${webview.cspSource}`,
      `script-src 'nonce-${nonce}'`,
    ].join("; ");
    return /* html */ `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="${style.toString()}" />
  <title>Investigation</title>
</head>
<body class="loading">
  <div id="root"></div>
  <script nonce="${nonce}" src="${script.toString()}"></script>
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

/** The short name a hypothesis is referred to by: its handle, else a clipped statement. */
function hypLabel(handle: string | undefined, statement: string): string {
  if (handle && handle.trim()) {
    return handle.trim();
  }
  return statement.length > 60 ? statement.slice(0, 59) + "…" : statement;
}

/** How a scoped turn cites the hypothesis it acts on: handle + claim + id. */
function hypCite(handle: string | undefined, statement: string, ref: string): string {
  const h = handle && handle.trim() ? `${handle.trim()}: ` : "";
  return `"${h}${statement}" (${ref})`;
}

/**
 * The rivals clause of a scoped turn: the live competing hypotheses, named by
 * handle and id, followed by the set-wide instruction. Empty when there is no
 * rival — a single-hypothesis investigation is still a legal shape.
 */
function rivalsClause(rivals: RivalBrief[] | undefined, instruction: string): string {
  if (!rivals || !rivals.length) {
    return "";
  }
  const list = rivals.map((r) => {
    const name = r.handle && r.handle.trim() ? `${r.handle.trim()}: ` : "";
    const pending = r.untestedDiscriminators > 0
      ? ` [${r.untestedDiscriminators} untested discriminating prediction${r.untestedDiscriminators === 1 ? "" : "s"}]`
      : "";
    return `${name}"${r.statement}" (${r.id})${pending}`;
  });
  return `Live rivals: ${list.join("; ")}. ${instruction}`;
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Render a candidate SOP as markdown + YAML frontmatter — the exact shape the
 * server's markdown import parses (design/06 §2.4). The source URL keys the SOP
 * to its originating investigation, so re-accepting a re-drafted candidate
 * revises the same SOP rather than creating a duplicate.
 */
function candidateSOPMarkdown(c: SOPCandidate, investigationId: string): string {
  const tags = (c.tags ?? []).map((t) => t.trim()).filter(Boolean);
  const lines = [
    "---",
    `title: ${yamlScalar(c.title.trim())}`,
    `tags: [${tags.map(yamlScalar).join(", ")}]`,
  ];
  if (c.recommendation?.trim()) {
    lines.push(`recommendation: ${yamlScalar(c.recommendation.trim())}`);
  }
  lines.push(
    "source:",
    "  system: reckon",
    `  url: reckon://investigation/${investigationId}`,
    "---",
    "",
    c.body.trim(),
    "",
  );
  return lines.join("\n");
}

/** Minimal YAML scalar quoting — quote when the value could be misread. */
function yamlScalar(s: string): string {
  return /^[\w .\-/]+$/.test(s) ? s : JSON.stringify(s);
}

/** A filesystem-safe slug from a SOP title, for the import filename. */
function slugifyTitle(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return slug || "candidate-sop";
}
