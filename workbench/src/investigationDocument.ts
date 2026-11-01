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
// This controller owns the panels, the routing, load and refresh, and the
// identity; the document's acts live in domain classes (draftFlow,
// actionDecisions, reasoningWrites, evidenceReads, knowledgeFlow,
// reportExport) that reach it only through DocumentHost (adr/0243).
//
// The extension host holds the tokens and the Anthropic key and does every
// call; the webview only renders messages posted to it. The CSP forbids all
// network from the page.

import { randomUUID } from "crypto";
import * as vscode from "vscode";
import { BackendClient, InvestigationDetail } from "./backend";
import { AgentTransport } from "./agentTransport";
import type { InboundMessage, InvestigationSnapshot, RenderMessage } from "./protocol";
import { InvestigationCache } from "./investigationCache";
import { errText, panelTitle } from "./util";
import { panelHtml, panelOptions } from "./panelHtml";
import { routeInbound, type InboundHandlers } from "./documentRouter";
import { TurnDriver } from "./turnDriver";
import { decideTurn, proposeTestsTurn, runTestsTurn, testPredictionTurn, type ScopedTurn } from "./scopedTurns";
import type { DocumentHost } from "./documentHost";
import { DraftFlow, isDraftKey } from "./draftFlow";
import { ActionDecisions } from "./actionDecisions";
import { ReasoningWrites } from "./reasoningWrites";
import { EvidenceReads } from "./evidenceReads";
import { KnowledgeFlow } from "./knowledgeFlow";
import { ReportExport } from "./reportExport";

export class InvestigationDocuments implements DocumentHost {
  private readonly open = new Map<string, vscode.WebviewPanel>();
  // Monotonic key for not-yet-seeded draft panels (design/ui/02 §2.7): a draft
  // has no investigation id until its seed is typed, so it lives under a
  // synthetic key until seedDraft re-keys it to the real aggregate id.
  private draftSeq = 0;
  // Investigations with a conclude-time summary running: it outlives the panel.
  private readonly summarizing = new Set<string>();
  // A draft opened in "from a case" mode (via reckon.seedFromCase) — consumed in
  // the ready handshake so the panel opens with the case search already expanded.
  private readonly draftMode = new Map<string, "case">();
  // Investigations whose rename card should pop once their panel goes live
  // (set by beginRename from the tree; consumed in the ready handshake).
  private readonly pendingRename = new Map<string, string>();
  // Bumped when the identity changes: a read begun before it is not cached, and
  // work begun before it neither opens anything nor writes (see sameIdentity).
  private generation = 0;
  // The projection reads, their watermark, and the per-investigation ordering
  // that keeps an older read from being posted after a newer one (impl §3.1–§3.2).
  readonly cache: InvestigationCache;
  private readonly turns: TurnDriver;
  // The document's domains (adr/0243), each given this controller as its host.
  private readonly draft: DraftFlow;
  private readonly actions: ActionDecisions;
  private readonly reasoning: ReasoningWrites;
  private readonly evidence: EvidenceReads;
  private readonly knowledge: KnowledgeFlow;
  private readonly export: ReportExport;

  constructor(
    private readonly extensionUri: vscode.Uri,
    readonly client: BackendClient,
    readonly log: vscode.LogOutputChannel,
    readonly transport: AgentTransport,
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
    this.turns = new TurnDriver(transport, (panel, msg) => this.post(panel, msg), (id, panel) => this.refresh(id, panel));
    this.draft = new DraftFlow(this);
    this.actions = new ActionDecisions(this);
    this.reasoning = new ReasoningWrites(this);
    this.evidence = new EvidenceReads(this);
    this.knowledge = new KnowledgeFlow(this);
    this.export = new ReportExport(this);
  }

  /** Apply the tenant's case write-back posture to a just-created investigation (adr/0032). */
  offerCaseNote(created: InvestigationDetail): void {
    this.draft.offerCaseNote(created);
  }

  /** A draft panel became the live investigation: it is held under the real id now. */
  rekey(from: string, to: string, panel: vscode.WebviewPanel): void {
    this.open.delete(from);
    this.open.set(to, panel);
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
      titleHint ? panelTitle(titleHint) : "Investigation",
      vscode.ViewColumn.Active,
      panelOptions(this.extensionUri),
    );
    this.open.set(id, panel);
    const ref = { id };
    panel.webview.html = panelHtml(panel.webview, this.extensionUri, "investigation", "Investigation");
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
      panelOptions(this.extensionUri),
    );
    this.open.set(key, panel);
    const ref = { id: key };
    panel.webview.html = panelHtml(panel.webview, this.extensionUri, "investigation", "Investigation");
    // The webview asks for its initial state via "ready"; a draft key answers
    // with the unseeded render instead of a load().
    panel.webview.onDidReceiveMessage((msg: InboundMessage) => this.route(ref, panel, msg));
    panel.onDidDispose(() => this.forget(ref.id));
  }

  /** The page may load only the bundle and the vendored fonts (impl §5.1). */

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

  /** The trees re-read: Investigations always, Cases too when a case changed hands. */
  refreshTrees(opts: { cases?: boolean } = {}): void {
    void vscode.commands.executeCommand("reckon.refreshInvestigations");
    if (opts.cases) {
      void vscode.commands.executeCommand("reckon.refreshCases");
    }
  }

  /**
   * One write from the panel: run it, toast a failure as `<verb> failed`, and
   * re-read the record either way, since a refused write still leaves the panel
   * to show what the server holds. `reload` re-reads the whole snapshot, for a
   * write that changes slices the refresh does not carry.
   */
  async mutate(id: string, panel: vscode.WebviewPanel, verb: string, fn: () => Promise<void>, opts: { reload?: boolean } = {}): Promise<void> {
    try {
      await fn();
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: ${verb} failed — ${errText(err)}`);
    }
    await (opts.reload ? this.load(id, panel) : this.refresh(id, panel));
  }

  /** Apply a confirmed rename, then update the tab + webview title in place. */
  private async applyRename(id: string, panel: vscode.WebviewPanel, title: string): Promise<void> {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    try {
      const applied = await this.client.investigations.rename(id, trimmed);
      if (this.isOpen(id, panel)) {
        panel.title = panelTitle(applied);
        void this.post(panel, { type: "renamed", title: applied });
        // The rename appended an event: the refresh advances the watermark and
        // the SeenTracker (via onLoaded), so the analyst's own rename does not
        // show in the tree as an unseen "● new" change.
        await this.refresh(id, panel);
      }
      this.refreshTrees();
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: rename failed — ${errText(err)}`);
    }
  }

  /** The panel is ready or asked to refresh: the draft surface, or a load of the record. */
  private opened(ref: { id: string }, panel: vscode.WebviewPanel): void {
    const id = ref.id;
    if (isDraftKey(id)) {
      const mode = this.draftMode.get(id);
      this.draftMode.delete(id);
      void this.draft.postDraft(panel, mode);
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
  }

  /** Route one webview message using the panel's current (mutable) id. */
  private route(ref: { id: string }, panel: vscode.WebviewPanel, msg: InboundMessage): void {
    routeInbound(this.handlers(ref, panel), msg);
  }

  /** Every inbound message's handler for one panel (the router's table). */
  private handlers(ref: { id: string }, panel: vscode.WebviewPanel): InboundHandlers {
    const id = ref.id;
    return {
      ready: () => this.opened(ref, panel),
      refresh: () => this.opened(ref, panel),
      "seed.submit": (msg) => {
        void this.draft.seedDraft(ref, panel, { value: msg.value, kind: msg.kind, allowUnknown: msg.allowUnknown });
      },
      "seed.alert": (msg) => {
        void this.draft.seedDraft(ref, panel, { alertId: msg.alertId, source: msg.source });
      },
      "seed.caseSearch": (msg) => {
        void this.draft.searchCases(panel, msg.filter, msg.queue);
      },
      "seed.case": (msg) => {
        void this.draft.seedDraft(ref, panel, { value: msg.caseNumber, kind: "case" });
      },
      "caseNote.post": (msg) => {
        void this.draft.postCaseNote(id, panel, msg.body);
      },
      "turn.stop": () => {
        this.transport.cancel(id).catch((err: unknown) => {
          void vscode.window.showErrorMessage(`reckon: stopping the turn failed — ${errText(err)}`);
        });
      },
      send: (msg) => {
        void this.turns.run(id, panel, msg.text).finally(() => this.releaseSession(id));
      },
      "action.approve": (msg) => {
        void this.actions.approve(id, panel, msg);
      },
      "action.reject": (msg) => {
        void this.actions.reject(id, panel, msg);
      },
      "action.edit": (msg) => {
        void this.actions.edit(id, panel, msg);
      },
      "action.request": (msg) => {
        void this.actions.requestFromPin(id, panel, msg);
      },
      "action.rerequest": (msg) => {
        void this.actions.rerequest(id, panel, msg);
      },
      "action.resolve": (msg) => {
        void this.actions.resolve(id, panel, msg);
      },
      "action.reverse": (msg) => {
        void this.actions.reverse(id, panel, msg);
      },
      "pin.add": (msg) => {
        void this.reasoning.pinCommit(id, panel, msg.refs, msg.finding);
      },
      "pin.unpin": (msg) => {
        void this.reasoning.unpin(id, panel, msg.interpretationId, msg.reason);
      },
      "verdict.submit": (msg) => {
        void this.reasoning.submitVerdict(id, panel, msg);
      },
      "hyp.ack": (msg) => {
        void this.reasoning.ackHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
      },
      "hyp.revise": (msg) => {
        void this.reasoning.reviseHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
      },
      "hyp.abandon": (msg) => {
        void this.reasoning.abandonHypothesis(id, panel, msg.hypothesisRef, msg.reason);
      },
      "hyp.new": (msg) => {
        void this.reasoning.newHypothesis(id, panel, msg.statement, msg.handle);
      },
      "pred.new": (msg) => {
        void this.reasoning.newPrediction(id, panel, msg.hypothesisRef, msg.statement, msg.discriminates, msg.testKind);
      },
      "pred.result": (msg) => {
        void this.reasoning.recordPredictionResult(id, panel, msg);
      },
      "pred.withdraw": (msg) => {
        void this.reasoning.withdrawPrediction(id, panel, msg.predictionRef, msg.reason);
      },
      "hyp.proposeTests": (msg) => {
        void this.runScoped(id, panel, proposeTestsTurn(msg.hypothesisRef, msg.statement, msg.handle, msg.rivals));
      },
      "pred.test": (msg) => {
        void this.runScoped(id, panel, testPredictionTurn(msg));
      },
      "hyp.runTests": (msg) => {
        void this.runScoped(id, panel, runTestsTurn(msg.hypothesisRef, msg.statement, msg.handle, msg.rivals));
      },
      "hyp.decide": (msg) => {
        void this.runScoped(id, panel, decideTurn(msg.hypothesisRef, msg.statement, msg.handle, msg.rivals));
      },
      "enablement.apply": (msg) => {
        void this.applyEnablement(id, panel, msg);
      },
      "comms.act": (msg) => {
        void this.actions.commsAct(id, panel, msg.threadId, msg.verb);
      },
      "comms.snooze": (msg) => {
        void this.actions.commsSnooze(id, panel, msg.threadId, msg.hours);
      },
      "comms.followup": (msg) => {
        void this.actions.commsFollowUp(id, panel, msg);
      },
      lifecycle: (msg) => {
        void this.lifecycleTransition(id, panel, msg);
      },
      "evidence.resolve": (msg) => {
        void this.evidence.resolveLabel(panel, msg.ref);
      },
      "inspector.open": (msg) => {
        // An action record is re-sent by reads (resendActionRecords); answering
        // it in the same order keeps an older answer from overwriting a newer one.
        void (msg.target.kind === "action"
          ? this.cache.exclusive(id, () => this.evidence.answerInspector(id, panel, msg.target))
          : this.evidence.answerInspector(id, panel, msg.target));
      },
      "queue.get": () => {
        void this.cache.exclusive(id, () => this.postQueue(panel));
      },
      "queue.start": (msg) => {
        void this.draft.startFromCase(msg.caseNumber);
      },
      "sop.draft": (msg) => {
        void this.knowledge.draftSOP(id, panel, msg.force);
      },
      "sop.accept": () => {
        void this.knowledge.acceptSOP(id, panel);
      },
      "sop.discard": () => {
        void this.knowledge.discardSOP(id, panel);
      },
      "library.get": () => {
        void this.knowledge.postLibrary(panel);
      },
      "library.search": (msg) => {
        void this.knowledge.searchLibrary(panel, msg.text);
      },
      "library.import": () => {
        void vscode.commands.executeCommand("reckon.importSOP");
      },
      "view.state": (msg) => {
        this.onViewState?.(id, msg.lens, msg.companion);
      },
      "export.open": () => {
        void this.export.openMarkdownExport(id);
      },
      copy: (msg) => {
        void vscode.env.clipboard.writeText(msg.text);
      },
      "investigation.new": () => {
        void vscode.commands.executeCommand("reckon.newInvestigation");
      },
      "investigation.open": (msg) => {
        void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
      },
      rename: (msg) => {
        // The in-document rename card confirmed. Drafts have no aggregate id yet
        // (the pencil is hidden there), but guard so a synthetic __draft__ key
        // never reaches the backend.
        if (!isDraftKey(id)) {
          void this.applyRename(id, panel, msg.title);
        }
      },
    };
  }

  /** Refresh every open document (e.g. after sign-out flips to sign-in). */
  refreshAll(): void {
    for (const [id, panel] of this.open) {
      // Drafts have no aggregate id — loading one is a guaranteed 4xx.
      if (!isDraftKey(id)) {
        void this.load(id, panel);
      }
    }
  }

  /**
   * The person behind the session changed (adr/0241). Everything here was read
   * as someone else, so it goes, and everything reckon opened closes: a panel
   * the new person cannot read would otherwise keep the last snapshot on screen,
   * and its tab the title.
   */
  identityChanged(): void {
    this.generation++;
    this.evidence.clear();
    this.cache.clear();
    this.pendingRename.clear();
    for (const panel of [...this.open.values()]) {
      panel.dispose();
    }
    void this.closeEditors();
  }

  /**
   * Close what reckon opened outside its panels, and only that. A draft's
   * unsaved text is reverted, not offered to the next person to save, and a
   * report's scratch file goes with its preview.
   */
  private async closeEditors(): Promise<void> {
    await this.knowledge.clear();
    await this.export.clear();
  }

  /**
   * A check that holds until the identity changes (adr/0241). Taken before an
   * await, it stops what the previous person began from opening anything for
   * the next one, or from writing on the next one's token.
   */
  sameIdentity(): () => boolean {
    const generation = this.generation;
    return () => generation === this.generation;
  }

  /** Drop everything held for a closed panel, including a pending dispatch watch. */
  private forget(id: string): void {
    this.open.delete(id);
    this.cache.forget(id);
    this.knowledge.forget(id);
    this.actions.forget(id);
    this.pendingRename.delete(id);
    this.draftMode.delete(id);
    this.releaseSession(id);
  }

  /**
   * End the agent session of an investigation no panel holds (adr/0237). Runs
   * when a panel closes, and after work that outlived its panel or never had
   * one, which would otherwise leave a session nothing ever ends.
   */
  releaseSession(id: string): void {
    if (!this.open.has(id) && !this.summarizing.has(id)) {
      void this.transport.closeSession(id);
    }
  }

  /** Whether this panel is still the open document for the investigation. */
  isOpen(id: string, panel: vscode.WebviewPanel): boolean {
    return this.open.get(id) === panel;
  }

  /**
   * Build the investigation's snapshot (impl §3.1) and post it whole; the
   * optional layers' failures become null slices, never a broken panel. The
   * bulkier lenses (the derived views, the knowledge rail, enablement) follow
   * as slice patches so the document renders as soon as the core is in.
   */
  load(id: string, panel: vscode.WebviewPanel): Promise<void> {
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
      panel.title = panelTitle(snapshot.investigation.title);
      void this.post(panel, { type: "state.snapshot", seq: snapshot.seq, snapshot });
      this.onLoaded?.(id, snapshot.seq);
      void this.me().then((m) => { if (m) void this.post(panel, { type: "me", name: m.name, principal: m.principal }); }).catch(() => undefined);
      // Dispatch is asynchronous (a Temporal workflow) and its result events are
      // recorded by the worker, not the HTTP path — keep refreshing while any
      // action is mid-dispatch so DISPATCHING never sticks past the outcome.
      this.actions.scheduleDispatchWatch(id, panel, snapshot.actions ?? []);
      // The event-time strip + evidence graph: lenses over the cited refs,
      // stamped with the sequence of the thread they were built from, so the
      // webview drops them if a newer read lands first.
      void this.evidence.postDerivedViews(panel, snapshot.thread ?? [], snapshot.seq, {
        hypotheses: snapshot.hypotheses, pins: snapshot.pins, actions: snapshot.actions ?? [], seed: snapshot.investigation.seed,
      });
      // The queue and the inspector's action records carry no sequence: they
      // finish inside this operation, so a later read cannot be overtaken. A
      // reload keeps the inspector open with its records, so a record with
      // newer events is re-sent like after any other read.
      await Promise.all([this.postQueue(panel), this.actions.resendActionRecords(id, panel, snapshot.seq)]);
      // The enablement view (gap hints + form schemas). Absence is honest.
      const latestEnablement = this.cache.beginRequest(id, "enablement");
      void this.client.capabilities.enablement()
        .then((data) => data, () => null)
        .then((data) => { if (latestEnablement()) void this.patch(id, panel, { enablement: data }); });
      void this.knowledge.postLibrary(panel);
      void this.actions.postActionTypes(panel);
      if (String(snapshot.investigation.state).toUpperCase() === "CONCLUDED") {
        void this.knowledge.postSummaryStatus(id, panel);
      }
    });
  }

  /**
   * Post slices that are not read from the event log (enablement)
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
  refresh(id: string, panel: vscode.WebviewPanel): Promise<void> {
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
        panel.title = panelTitle(patch.investigation.title);
      }
      void this.post(panel, { type: "state.patch", seq, patch });
      this.onLoaded?.(id, seq);
      if (patch.actions) {
        this.actions.scheduleDispatchWatch(id, panel, patch.actions);
      }
      if (patch.thread) {
        void this.evidence.postDerivedViews(panel, patch.thread, seq, { // sequenced: a stale one is dropped
          hypotheses: patch.hypotheses, pins: patch.pins, actions: patch.actions ?? undefined, seed: patch.investigation?.seed,
        });
      }
      // Unsequenced follow-ups finish inside this operation (see load).
      await Promise.all([this.postQueue(panel), this.actions.resendActionRecords(id, panel, seq)]);
    });
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
        this.client.investigations.list(),
        this.client.cases.queue().catch(() => null),
      ]);
      const cases = (queue?.cases ?? []).filter((c) => !c.investigation);
      void this.post(panel, { type: "queue", investigations, cases, caseSource: queue?.queue });
    } catch (err) {
      this.log.debug(`queue: ${errText(err)}`);
    }
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
    const current = this.sameIdentity();
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
    if (!current()) return;
    await this.mutate(id, panel, "enablement", async () => {
      await this.client.capabilities.applyEnablement(msg.adapter, msg.enabled, config);
      void vscode.window.showInformationMessage(
        `reckon: ${msg.adapter} ${msg.enabled ? "enabled" : "disabled"} — capability surface rebuilt`,
      );
    }, { reload: true });
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
    const current = this.sameIdentity();
    await this.mutate(id, panel, msg.transition, async () => {
      const body: { transition: string; reason?: string; reportRef?: string; summary?: string } = {
        transition: msg.transition,
        reason: msg.reason?.trim() || undefined,
      };
      if (msg.transition === "conclude") {
        body.reportRef = `report--${randomUUID()}`;
        body.summary = msg.summary?.trim() || undefined;
      }
      const res = await this.client.investigations.lifecycle(id, body);
      const exportNote = res.exportWorkflowId ? " · export started" : "";
      void vscode.window.showInformationMessage(`reckon: investigation → ${res.status}${exportNote}`);
      // Every transition moves the investigation between the tree's groups (a
      // discarded draft leaves them altogether).
      this.refreshTrees();
      if (msg.transition === "conclude" && current()) {
        // The client-side tier of the two-tier knowledge summary (design/06
        // §3.2): the sidecar — the only place the BYOK model key lives —
        // writes the narrative from its own conversation and posts it.
        // Fire-and-forget: the server's structured baseline stands either
        // way; a narrative failure must never look like a conclude failure.
        // The summary's outcome is shown beside the report (adr/0156), not only
        // in a status bar message that fades.
        this.summarizing.add(id);
        void this.transport.summarizeConcluded(id).then(
          () => this.post(panel, { type: "summary.status", state: "written" }),
          (err: unknown) => this.post(panel, { type: "summary.status", state: "failed", detail: errText(err) }),
        ).finally(() => {
          this.summarizing.delete(id);
          this.releaseSession(id);
        });
        // Drafting a procedure from the case is the analyst's step now
        // (adr/0156): Draft SOP from this case, in the Summary lens.
      }
    });
  }

  /** Run a ⚡ scoped turn: rendered collapsed under its label. */
  private runScoped(id: string, panel: vscode.WebviewPanel, t: ScopedTurn): Promise<void> {
    return this.turns.run(id, panel, t.instruction, { label: t.label }).finally(() => this.releaseSession(id));
  }

  post(panel: vscode.WebviewPanel, msg: RenderMessage): Thenable<boolean> {
    try {
      return panel.webview.postMessage(msg);
    } catch {
      return Promise.resolve(false); // the panel was disposed while a read was in flight
    }
  }
}
