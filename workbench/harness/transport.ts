// The harness transport (impl §5.4): answers the webview from a recorded
// snapshot set and echoes every intent, so a click's effect is visible and
// component tests can assert on what the page asked the host for.
import type { ActionRecord, Appearance, CapabilityCall, CaseSummaryView, EventRepeats, EventRow, EvidenceDoc, KnowledgeCitations, ProcedureReads, SOPDoc } from "../src/viewTypes";
import type { InboundMessage, InspectorTarget, InvestigationSnapshot, RenderMessage } from "../src/protocol";
import type { WebviewTransport } from "../webview/src/transport";

/** One recorded stage of the reference scenario (harness/record.ts). */
export interface HarnessSnapshot {
  name: string;
  snapshot: InvestigationSnapshot;
  /** Resolved labels for the refs the snapshot cites. */
  labels: Record<string, string>;
  /** Cross-investigation appearances per entity ref (the inspector). */
  appearances?: Record<string, Appearance[]>;
  /** The inspector's records, keyed as the host answers them (impl §4.1). */
  docs?: Record<string, EvidenceDoc>;
  transcripts?: Record<string, string>;
  actionRecords?: Record<string, ActionRecord>;
  sops?: Record<string, SOPDoc>;
  caseSummaries?: Record<string, CaseSummaryView | null>;
  /** Where a procedure (by sop id) or a past case (by ref) was consulted elsewhere (adr/0109). */
  citations?: Record<string, KnowledgeCitations>;
  /** What the agent read of a procedure (by sop id) here, as it was (adr/0097). */
  procedureReads?: Record<string, ProcedureReads>;
  /** Capability calls by id, and the retrieved events around one event (adr/0092). */
  calls?: Record<string, CapabilityCall>;
  around?: Record<string, EventRow[]>;
  /** An entity's retrieved events folded by the default key (adr/0095). */
  repeats?: Record<string, EventRepeats>;
}

export class HarnessTransport implements WebviewTransport {
  private handlers: ((m: RenderMessage) => void)[] = [];
  private state: unknown = undefined;
  readonly intents: InboundMessage[] = [];

  constructor(
    private data: HarnessSnapshot | null,
    private readonly onIntent?: (m: InboundMessage) => void,
  ) {}

  /** Swap the recorded stage (the harness stage picker). */
  load(data: HarnessSnapshot | null): void {
    this.data = data;
    this.postInitial();
  }

  send(intent: InboundMessage): void {
    this.intents.push(intent);
    this.onIntent?.(intent);
    switch (intent.type) {
      case "ready":
      case "refresh":
        this.postInitial();
        break;
      case "evidence.resolve": {
        const label = this.data?.labels[intent.ref];
        if (label) this.emit({ type: "evidence.label", ref: intent.ref, label });
        break;
      }
      case "inspector.open":
        this.answerInspector(intent.target);
        break;
      case "rename":
        this.emit({ type: "renamed", title: intent.title });
        break;
      case "seed.caseSearch":
        this.emit({ type: "draft.cases", cases: [{ number: "CASE-1001", title: "A case from the queue", status: "New" }] });
        break;
      case "seed.submit":
      case "seed.alert":
      case "seed.case":
        // The real host re-keys the panel and loads the created investigation;
        // the harness only leaves the draft (it has nothing to load).
        this.emit({ type: "seeded" });
        break;
      case "events.fold": {
        // The stage holds one folded record (adr/0095); the flat page is the
        // same rows without the groups, and a key change re-posts the fold as
        // recorded, since nothing here folds.
        const s = this.data?.snapshot;
        if (!s) break;
        const flat = intent.keys === null;
        this.emit({ type: "state.patch", seq: s.seq, patch: { events: s.events ?? [], eventsTotal: s.eventsTotal ?? 0, eventsError: null, eventsCursor: null, eventGroups: flat ? null : s.eventGroups ?? [], eventKeys: flat ? [] : s.eventKeys ?? [], eventsFoldedOver: flat ? 0 : s.eventsFoldedOver ?? 0 } });
        break;
      }
      // Dependencies (adr/0101): the real host forwards the act to the engine
      // and re-reads the slice; here the recorded slice moves the same way, so
      // a click's effect is visible.
      case "dependency.create":
      case "dependency.attach":
      case "dependency.resolve": {
        const s = this.data?.snapshot;
        if (!s) break;
        const now = new Date().toISOString();
        const deps = [...(s.dependencies ?? [])];
        if (intent.type === "dependency.create") {
          deps.push({ dependencyId: "dep-" + (deps.length + 1), title: intent.title, owner: intent.owner, detail: intent.detail, externalRef: intent.externalRef, notifyActionId: intent.notifyActionId, status: "OPEN", createdBy: "you", createdAt: now, history: [{ at: now, by: "you", event: "created" }] });
        } else {
          const i = deps.findIndex((d) => d.dependencyId === intent.dependencyId);
          const d = deps[i];
          if (!d) break;
          deps[i] = intent.type === "dependency.attach"
            ? { ...d, notifyActionId: intent.notifyActionId, history: [...d.history, { at: now, by: "you", event: "attached", ref: intent.notifyActionId }] }
            : { ...d, status: intent.verb === "confirm" ? "CONFIRMED" : "WAIVED", resolvedBy: "you", resolvedAt: now, resolutionNote: intent.note, externalRef: intent.externalRef || d.externalRef, history: [...d.history, { at: now, by: "you", event: intent.verb === "confirm" ? "confirmed" : "waived", note: intent.note, ref: intent.externalRef }] };
        }
        this.data = { ...this.data!, snapshot: { ...s, dependencies: deps } };
        this.emit({ type: "state.snapshot", seq: s.seq, snapshot: this.data.snapshot });
        break;
      }
      // Every other intent is an act the real host forwards to the engine;
      // here it is only recorded.
    }
  }

  /** The inspector's answers come from the recorded maps; a record the stage
   * did not capture is an honest error, as the host would post for a ref it
   * cannot open. */
  private answerInspector(target: InspectorTarget): void {
    const d = this.data;
    switch (target.kind) {
      case "ref": {
        const doc = d?.docs?.[target.ref];
        if (doc) this.emit({ type: "inspector.evidence", ref: target.ref, doc, appearances: d?.appearances?.[target.ref] ?? [], around: d?.around?.[target.ref] ?? [], repeats: d?.repeats?.[target.ref] });
        else this.emit({ type: "inspector.error", target: target.ref, message: "not recorded in this harness stage" });
        break;
      }
      case "sop": {
        const sop = d?.sops?.[target.sopId];
        if (sop) this.emit({ type: "inspector.procedure", sopId: target.sopId, sop, citations: d?.citations?.[target.sopId], reads: d?.procedureReads?.[target.sopId] });
        else this.emit({ type: "inspector.error", target: target.sopId, message: "procedure not recorded in this harness stage" });
        break;
      }
      case "turn": {
        const body = d?.transcripts?.[target.interpretationId] ?? d?.snapshot.turns.find((t) => d.snapshot.thread?.find((e) => e.interpretationId === target.interpretationId)?.sequenceNo === t.sequenceNo)?.body;
        if (body !== undefined) this.emit({ type: "inspector.turn", interpretationId: target.interpretationId, body });
        else this.emit({ type: "inspector.error", target: target.interpretationId, message: "transcript not recorded in this harness stage" });
        break;
      }
      case "action": {
        const record = d?.actionRecords?.[target.actionId];
        if (record) this.emit({ type: "inspector.action", actionId: target.actionId, record });
        else this.emit({ type: "inspector.error", target: target.actionId, message: "action record not recorded in this harness stage" });
        break;
      }
      case "case":
        this.emit({ type: "inspector.case", investigationRef: target.investigationRef, summary: d?.caseSummaries?.[target.investigationRef] ?? null, citations: d?.citations?.[target.investigationRef] });
        break;
      case "call": {
        const call = d?.calls?.[target.callId];
        if (call) this.emit({ type: "inspector.call", callId: target.callId, call });
        else this.emit({ type: "inspector.error", target: target.callId, message: "capability call not recorded in this harness stage" });
        break;
      }
      // thread, person: rendered from the snapshot alone
    }
  }

  /** Push a host message as if the extension had posted it (tests, the stage picker). */
  emit(m: RenderMessage): void {
    for (const h of this.handlers) h(m);
  }

  onMessage(handler: (m: RenderMessage) => void): () => void {
    this.handlers.push(handler);
    return () => { this.handlers = this.handlers.filter((h) => h !== handler); };
  }
  getState<T>(): T | undefined { return this.state as T | undefined; }
  setState(s: unknown): void { this.state = s; }

  private postInitial(): void {
    if (!this.data) {
      this.emit({ type: "draft", caseQueue: "soc-triage" });
      return;
    }
    this.emit({ type: "state.snapshot", seq: this.data.snapshot.seq, snapshot: this.data.snapshot });
    this.emit({ type: "me", name: "Analyst One", principal: "analyst-1" });
  }
}
