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
import * as os from "os";
import * as path from "path";
import * as vscode from "vscode";
import { ActionRow, Appearance, BackendClient, EvidenceDoc, InvestigationDetail, ThreadEntry } from "./backend";
import { AgentTransport, SOPCandidate } from "./agentTransport";
import type { InboundMessage, InspectorTarget, InvestigationSnapshot, RenderMessage } from "./protocol";
import { buildDerivedViews, type CitingRecords } from "./derivedViews";
import { deriveEvidenceLabel } from "./evidenceLabel";
import { InvestigationCache } from "./investigationCache";
import { candidateSOPMarkdown, errText, makeNonce, slugifyTitle } from "./documentText";
import { routeInbound, type InboundHandlers } from "./documentRouter";
import { TurnDriver } from "./turnDriver";
import { decideTurn, proposeTestsTurn, runTestsTurn, testPredictionTurn, type ScopedTurn } from "./scopedTurns";

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
  private readonly turns: TurnDriver;

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
    this.turns = new TurnDriver(transport, (panel, msg) => this.post(panel, msg), (id, panel) => this.refresh(id, panel));
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

  /** The panel is ready or asked to refresh: the draft surface, or a load of the record. */
  private opened(ref: { id: string }, panel: vscode.WebviewPanel): void {
    const id = ref.id;
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
        void this.seedDraft(ref, panel, { value: msg.value, kind: msg.kind });
      },
      "seed.alert": (msg) => {
        void this.seedDraft(ref, panel, { alertId: msg.alertId, source: msg.source });
      },
      "seed.caseSearch": (msg) => {
        void this.searchCases(panel, msg.filter, msg.queue);
      },
      "seed.case": (msg) => {
        void this.seedDraft(ref, panel, { value: msg.caseNumber, kind: "case" });
      },
      "caseNote.post": (msg) => {
        void this.postCaseNote(id, panel, msg.body);
      },
      send: (msg) => {
        void this.turns.run(id, panel, msg.text, undefined, msg.includedKnowledge);
      },
      "action.approve": (msg) => {
        void this.approve(id, panel, msg);
      },
      "action.reject": (msg) => {
        void this.reject(id, panel, msg);
      },
      "action.edit": (msg) => {
        void this.edit(id, panel, msg);
      },
      "action.rerequest": (msg) => {
        void this.rerequest(id, panel, msg);
      },
      "action.reverse": (msg) => {
        void this.reverse(id, panel, msg);
      },
      "pin.add": (msg) => {
        void this.pinCommit(id, panel, msg.refs, msg.finding);
      },
      "pin.unpin": (msg) => {
        void this.unpin(id, panel, msg.interpretationId, msg.reason);
      },
      "verdict.submit": (msg) => {
        void this.submitVerdict(id, panel, msg);
      },
      "hyp.ack": (msg) => {
        void this.ackHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
      },
      "hyp.revise": (msg) => {
        void this.reviseHypothesis(id, panel, msg.hypothesisRef, msg.restatement, msg.handle);
      },
      "hyp.abandon": (msg) => {
        void this.abandonHypothesis(id, panel, msg.hypothesisRef, msg.reason);
      },
      "hyp.new": (msg) => {
        void this.newHypothesis(id, panel, msg.statement, msg.handle);
      },
      "pred.new": (msg) => {
        void this.newPrediction(id, panel, msg.hypothesisRef, msg.statement, msg.discriminates, msg.testKind);
      },
      "pred.result": (msg) => {
        void this.recordPredictionResult(id, panel, msg);
      },
      "pred.withdraw": (msg) => {
        void this.withdrawPrediction(id, panel, msg.predictionRef, msg.reason);
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
        void this.commsAct(id, panel, msg.threadId, msg.verb);
      },
      "comms.snooze": (msg) => {
        void this.commsSnooze(id, panel, msg.threadId, msg.hours);
      },
      "comms.followup": (msg) => {
        void this.commsFollowUp(id, panel, msg);
      },
      lifecycle: (msg) => {
        void this.lifecycleTransition(id, panel, msg);
      },
      "evidence.resolve": (msg) => {
        void this.resolveLabel(panel, msg.ref);
      },
      "inspector.open": (msg) => {
        // An action record is re-sent by reads (resendActionRecords); answering
        // it in the same order keeps an older answer from overwriting a newer one.
        void (msg.target.kind === "action"
          ? this.cache.exclusive(id, () => this.answerInspector(id, panel, msg.target))
          : this.answerInspector(id, panel, msg.target));
      },
      "queue.get": () => {
        void this.cache.exclusive(id, () => this.postQueue(panel));
      },
      "queue.start": (msg) => {
        void this.startFromCase(msg.caseNumber);
      },
      "knowledge.search": (msg) => {
        void this.searchKnowledge(id, panel, msg.text);
      },
      "view.state": (msg) => {
        this.onViewState?.(id, msg.lens, msg.companion);
      },
      "export.open": () => {
        void this.openMarkdownExport(id);
      },
      copy: (msg) => {
        void vscode.env.clipboard.writeText(msg.text);
      },
      "investigation.open": (msg) => {
        void vscode.commands.executeCommand("reckon.openInvestigation", msg.id, msg.title);
      },
      rename: (msg) => {
        // The in-document rename card confirmed. Drafts have no aggregate id yet
        // (the pencil is hidden there), but guard so a synthetic __draft__ key
        // never reaches the backend.
        if (!InvestigationDocuments.isDraft(id)) {
          void this.applyRename(id, panel, msg.title);
        }
      },
    };
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
      void this.postDerivedViews(panel, snapshot.thread ?? [], snapshot.seq, {
        hypotheses: snapshot.hypotheses, pins: snapshot.pins, actions: snapshot.actions ?? [], seed: snapshot.investigation.seed,
      });
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
        void this.postDerivedViews(panel, patch.thread, seq, { // sequenced: a stale one is dropped
          hypotheses: patch.hypotheses, pins: patch.pins, actions: patch.actions ?? undefined, seed: patch.investigation?.seed,
        });
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
      // A follow-up rests on the grounds of the message it follows up on: the
      // engine refuses an outbound action that cites no evidence.
      const thread = (await this.client.comms(id)).find((t) => t.threadId === msg.threadId);
      const origin = thread?.actionId ? (await this.client.actionRecord(thread.actionId)).action : undefined;
      if (!origin?.evidenceRefs.length) {
        void vscode.window.showErrorMessage("reckon: follow-up not requested — the original message's evidence could not be read");
        return;
      }
      const res = await this.client.requestAction({
        investigationRef: id,
        actionType: msg.actionType,
        targets: [{ resolvedIdentifier: msg.target }],
        parameters,
        evidenceRefs: origin.evidenceRefs,
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
   * The derived lenses over the thread's citations (the event-time strip and
   * the evidence graph, src/derivedViews.ts), sharing one round of cached
   * evidence fetches — bounded; failures just leave gaps (both are lenses,
   * never load-bearing). Posted as a slice patch.
   */
  private async postDerivedViews(panel: vscode.WebviewPanel, thread: ThreadEntry[], seq: number, citing: CitingRecords): Promise<void> {
    const derived = await buildDerivedViews(thread, (ref) => this.getDoc(ref), citing);
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
  private async approve(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; challenge?: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      // The server requires the challenge too; checking the record's tier here
      // just saves the round trip when the prompt came back empty.
      if (a.tier === "T3" && (!msg.challenge || msg.challenge.trim() === "")) {
        return;
      }
      const decision = await this.client.approveAction(a.actionId, msg.challenge);
      void vscode.window.showInformationMessage(
        `reckon: ${a.actionType} → ${decision.status}${decision.stage ? ` (${decision.stage})` : ""}`,
      );
    }, "approve");
    // The refresh armed the dispatch watch: the approved action is now
    // APPROVED/EXECUTING, so the ledger keeps refreshing until it settles.
  }

  /**
   * Run one decision on an action the record holds, then refresh and tell the
   * webview the decision settled, whatever its outcome — that clears the
   * in-flight state every card for the action shares. Tier and type come from
   * the record, never from the message.
   */
  private async decide(
    id: string,
    panel: vscode.WebviewPanel,
    actionId: string,
    act: (a: ActionRow) => Promise<void>,
    verb: string,
  ): Promise<void> {
    try {
      let a = this.cache.action(id, actionId);
      if (!a) {
        await this.refresh(id, panel);
        a = this.cache.action(id, actionId);
      }
      if (!a) {
        void vscode.window.showErrorMessage(`reckon: ${verb} failed — the action is not in this investigation's record`);
        return;
      }
      await act(a);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: ${verb} failed — ${errText(err)}`);
    } finally {
      await this.refresh(id, panel);
      void this.post(panel, { type: "action.settled", actionId });
    }
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
  private async reject(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; reason: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      const decision = await this.client.rejectAction(a.actionId, msg.reason.trim() === "" ? "rejected from the workbench" : msg.reason.trim());
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} → ${decision.status}`);
    }, "reject");
  }

  /**
   * Re-request an expired action — a fresh request (same targets & evidence,
   * new window, retry_of lineage) the analyst then approves. Not a bypass of
   * expiry: the conscious re-affirmation it exists to elicit.
   */
  /** Edit a pending request before approving it (adr/0127): the same action, new parameters, on the record. */
  private async edit(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; parameters: Record<string, unknown>; reason?: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      await this.client.editAction(a.actionId, msg.parameters, msg.reason);
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} edited — review it and approve`);
    }, "edit");
  }

  private async rerequest(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; rationale: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      await this.client.rerequestAction(a.actionId, msg.rationale);
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} re-requested — approve the new request`);
    }, "re-request");
  }

  /**
   * Request the reversal of a SUCCEEDED action (04 §7): a NEW x-action of the
   * descriptor's declared inverse type, at the same tier, linked by
   * reversal_of_ref, which lands in the approval queue like any action. The
   * engine validates the pairing and the original's status; the host only
   * looks up the inverse type so the request names it.
   */
  private async reverse(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; rationale: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async () => {
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
    }, "reversal");
  }

  /**
   * The live markdown export (binding §6 item 10): rendered backend-side from
   * the projections and shown as a rendered preview beside the panel. The
   * preview reads a scratch file, not an untitled doc, so no unsaved source
   * tab is left behind; Save report… writes it to a file the analyst picks.
   */
  private async openMarkdownExport(id: string): Promise<void> {
    let md: string;
    try {
      md = await this.client.exportMarkdown(id);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: export failed — ${errText(err)}`);
      return;
    }
    const name = `reckon-report-${id.slice(0, 8)}.md`;
    const scratch = vscode.Uri.file(path.join(os.tmpdir(), "reckon-exports", name));
    try {
      await vscode.workspace.fs.createDirectory(vscode.Uri.file(path.dirname(scratch.fsPath)));
      await vscode.workspace.fs.writeFile(scratch, Buffer.from(md, "utf8"));
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: export failed — ${errText(err)}`);
      return;
    }
    await vscode.commands.executeCommand("markdown.showPreviewToSide", scratch);
    const save = "Save report…";
    if ((await vscode.window.showInformationMessage("reckon: report exported", save)) !== save) return;
    const target = await vscode.window.showSaveDialog({
      defaultUri: vscode.Uri.file(path.join(os.homedir(), name)),
      filters: { Markdown: ["md"] },
      saveLabel: "Save report",
    });
    if (!target) return;
    try {
      await vscode.workspace.fs.writeFile(target, Buffer.from(md, "utf8"));
      void vscode.window.showInformationMessage(`reckon: report saved to ${target.fsPath}`);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: saving the report failed — ${errText(err)}`);
    }
  }

  /** Run a ⚡ scoped turn: rendered collapsed under its label. */
  private runScoped(id: string, panel: vscode.WebviewPanel, t: ScopedTurn): Promise<void> {
    return this.turns.run(id, panel, t.instruction, { label: t.label });
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
