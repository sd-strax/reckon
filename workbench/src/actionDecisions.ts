// The analyst's decisions on actions (design/04 §5, adr/0127, adr/0216) and
// the writes that are actions through Gate 2: a request from a pin, a comms
// act or follow-up, a reversal. Every decision is extension→backend on the
// HUMAN token, never through the sidecar. The dispatch watch keeps the ledger
// honest while a workflow runs.
import * as vscode from "vscode";
import type { ActionRow, ArtifactLocation } from "./backend";
import type { DocumentHost } from "./documentHost";
import { errText } from "./util";

export class ActionDecisions {
  constructor(private readonly host: DocumentHost) {}

  // Per-investigation refresh timers for in-flight dispatches (APPROVED/
  // EXECUTING → poll until terminal; see scheduleDispatchWatch).
  private readonly dispatchWatch = new Map<string, { timer: ReturnType<typeof setTimeout>; ticks: number }>();

  /** The action types the analyst can request directly (#146), available ones only. */
  async postActionTypes(panel: vscode.WebviewPanel): Promise<void> {
    try {
      const types = (await this.host.client.actions.types()).filter((t) => t.available)
        .map((t) => ({ actionType: t.actionType, name: t.name, category: t.category, inputs: t.inputs }));
      void this.host.post(panel, { type: "actionTypes", types });
    } catch (err) {
      this.host.log.debug(`action types: ${errText(err)}`);
    }
  }

  /**
   * The analyst requests an action from a pinned finding (#146), when the
   * agent cannot or should not: the pin's evidence is the citation, and the
   * request is the analyst's — same approval policies, approval and dispatch.
   */
  requestFromPin(id: string, panel: vscode.WebviewPanel, msg: { actionType: string; target: string; parameters: Record<string, unknown>; rationale: string; evidenceRefs: string[] }): Promise<void> {
    return this.host.mutate(id, panel, `requesting ${msg.actionType}`, async () => {
      const res = await this.host.client.actions.request({
        investigationRef: id, actionType: msg.actionType,
        targets: [{ resolvedIdentifier: msg.target }],
        parameters: Object.keys(msg.parameters).length ? msg.parameters : undefined,
        evidenceRefs: msg.evidenceRefs, rationale: msg.rationale,
      });
      void vscode.window.showInformationMessage(`reckon: ${msg.actionType} on ${msg.target} requested (${res.status}) — approve it in Response`);
    });
  }

  /** Re-send the inspector's action records read before the investigation's newest event. */
  async resendActionRecords(id: string, panel: vscode.WebviewPanel, seq: number): Promise<void> {
    for (const actionId of this.host.cache.staleRecords(id, seq)) {
      try {
        const record = await this.host.client.actions.record(actionId);
        this.host.cache.servedRecord(id, actionId, seq);
        void this.host.post(panel, { type: "inspector.action", actionId, record });
      } catch (err) {
        this.host.log.debug(`action record ${actionId}: ${errText(err)}`);
      }
    }
  }

  commsAct(id: string, panel: vscode.WebviewPanel, threadId: string, verb: "ack" | "done"): Promise<void> {
    return this.host.mutate(id, panel, `comms ${verb}`, () => this.host.client.actions.commsAct(threadId, verb));
  }

  /** Push a thread's follow-up clock. */
  commsSnooze(id: string, panel: vscode.WebviewPanel, threadId: string, hours: number): Promise<void> {
    return this.host.mutate(id, panel, "snooze", () => this.host.client.actions.commsSnooze(threadId, hours));
  }

  /**
   * Send a follow-up: a NEW notify.* action carrying thread_ref (binding §4 —
   * every outbound message is an action through Gate 2), which lands in the
   * approval queue as a preview card. On its SUCCEEDED dispatch the thread's
   * counter increments and its clock restarts.
   */
  async commsFollowUp(
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
    const current = this.host.sameIdentity();
    await this.host.mutate(id, panel, "follow-up", async () => {
      // A follow-up rests on the grounds of the message it follows up on: the
      // engine refuses an outbound action that cites no evidence.
      const thread = (await this.host.client.actions.comms(id)).find((t) => t.threadId === msg.threadId);
      const origin = thread?.actionId ? (await this.host.client.actions.record(thread.actionId)).action : undefined;
      if (!origin?.evidenceRefs.length) {
        void vscode.window.showErrorMessage("reckon: follow-up not requested — the original message's evidence could not be read");
        return;
      }
      if (!current()) return;
      const res = await this.host.client.actions.request({
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
    });
  }

  /**
   * Approve one action — the analyst's own act, extension→backend on the
   * HUMAN token, never through the sidecar (implementation/agent-sidecar.md
   * §5). A T3 approval carries the typed challenge (04 §5.5), collected in the
   * webview prompt card before this fires.
   */
  async approve(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; challenge?: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      // The server requires the challenge too; checking the record's tier here
      // just saves the round trip when the prompt came back empty.
      if (a.tier === "T3" && (!msg.challenge || msg.challenge.trim() === "")) {
        return;
      }
      const decision = await this.host.client.actions.approve(a.actionId, msg.challenge);
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
    const current = this.host.sameIdentity();
    let acted = false;
    try {
      let a = this.host.cache.action(id, actionId);
      if (!a) {
        await this.host.refresh(id, panel);
        a = this.host.cache.action(id, actionId);
      }
      if (!a) {
        // After a change of identity the read was not kept: nothing to approve,
        // and nothing to tell the next person.
        if (current()) void vscode.window.showErrorMessage(`reckon: ${verb} failed — the action is not in this investigation's record`);
        return;
      }
      acted = true;
      await act(a);
    } catch (err) {
      void vscode.window.showErrorMessage(`reckon: ${verb} failed — ${errText(err)}`);
    } finally {
      // The record just read to find the action is current; only a decision
      // attempted over it changes what the panel should show.
      if (acted) {
        await this.host.refresh(id, panel);
      }
      void this.host.post(panel, { type: "action.settled", actionId });
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
  scheduleDispatchWatch(id: string, panel: vscode.WebviewPanel, actions: ActionRow[]): void {
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
    if (ticks >= 45 || !this.host.isOpen(id, panel)) {
      this.dispatchWatch.delete(id);
      return;
    }
    const timer = setTimeout(() => {
      void this.host.refresh(id, panel); // re-arms (or clears) the watch from fresh state
    }, 4000);
    this.dispatchWatch.set(id, { timer, ticks });
  }

  /** Reject one action — human token only; reason from the webview prompt card. */
  async reject(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; reason: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      const decision = await this.host.client.actions.reject(a.actionId, msg.reason.trim() === "" ? "rejected from the workbench" : msg.reason.trim());
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} → ${decision.status}`);
    }, "reject");
  }

  /** Resolve an action whose outcome is unknown (adr/0216) — human token only. */
  async resolve(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { actionId: string; outcome: "SUCCEEDED" | "FAILED"; note: string; reference?: string; artifact?: ArtifactLocation },
  ): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      const decision = await this.host.client.actions.resolve(a.actionId, msg);
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} resolved → ${decision.status}`);
    }, "resolve");
  }

  /**
   * Re-request an expired action — a fresh request (same targets & evidence,
   * new window, retry_of lineage) the analyst then approves. Not a bypass of
   * expiry: the conscious re-affirmation it exists to elicit.
   */
  /** Edit a pending request before approving it (adr/0127): the same action, new parameters, on the record. */
  async edit(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; parameters: Record<string, unknown>; reason?: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      await this.host.client.actions.edit(a.actionId, msg.parameters, msg.reason);
      void vscode.window.showInformationMessage(`reckon: ${a.actionType} edited — review it and approve`);
    }, "edit");
  }

  async rerequest(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; rationale: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async (a) => {
      await this.host.client.actions.rerequest(a.actionId, msg.rationale);
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
  async reverse(id: string, panel: vscode.WebviewPanel, msg: { actionId: string; rationale: string }): Promise<void> {
    await this.decide(id, panel, msg.actionId, async () => {
      const current = this.host.sameIdentity();
      const [record, types] = await Promise.all([this.host.client.actions.record(msg.actionId), this.host.client.actions.types()]);
      const original = record.action;
      const inverse = types.find((t) => t.actionType === original.actionType)?.reversibleBy;
      if (!inverse) {
        void vscode.window.showWarningMessage(`reckon: ${original.actionType} declares no reversal in the action catalog`);
        return;
      }
      if (!current()) return;
      const res = await this.host.client.actions.request({
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

  /** Drop a closed panel's dispatch watch. */
  forget(id: string): void {
    const watch = this.dispatchWatch.get(id);
    if (watch) {
      clearTimeout(watch.timer);
      this.dispatchWatch.delete(id);
    }
  }
}
