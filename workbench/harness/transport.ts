// The harness transport (impl §5.4): answers the webview from a recorded
// snapshot set and echoes every intent, so a click's effect is visible and
// component tests can assert on what the page asked the host for.
import type { ActionRecord, Appearance, CaseRow, CaseSummaryView, EvidenceDoc, InvestigationSummary, SOPDoc } from "../src/viewTypes";
import type { InboundMessage, InspectorTarget, InvestigationSnapshot, LibraryItem, RenderMessage } from "../src/protocol";
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
  /** The knowledge library (adr/0129) as the host posts it. */
  library?: { sops: LibraryItem[]; cases: LibraryItem[] };
  /** The work queue (08 §8.3) as the host posts it. */
  queue?: { investigations: InvestigationSummary[]; cases: CaseRow[]; caseSource?: string };
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
      case "queue.get":
        this.postQueue();
        break;
      case "library.get":
        this.emit({ type: "library", sops: this.data?.library?.sops ?? [], cases: this.data?.library?.cases ?? [] });
        break;
      case "library.search": {
        // The host recalls over both corpora; the harness answers with the
        // library entries whose title mentions the query, rated in words.
        const q = intent.text.toLowerCase();
        const hit = (k: LibraryItem): LibraryItem => ({ ...k, relevance: "related" });
        this.emit({ type: "library.results", query: intent.text,
          sops: (this.data?.library?.sops ?? []).filter((k) => k.title.toLowerCase().includes(q)).map(hit),
          cases: (this.data?.library?.cases ?? []).filter((k) => k.title.toLowerCase().includes(q)).map(hit) });
        break;
      }
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
        if (doc) this.emit({ type: "inspector.evidence", ref: target.ref, doc, appearances: d?.appearances?.[target.ref] ?? [] });
        else this.emit({ type: "inspector.error", target: target.ref, message: "not recorded in this harness stage" });
        break;
      }
      case "sop": {
        const sop = d?.sops?.[target.sopId];
        if (sop) this.emit({ type: "inspector.procedure", sopId: target.sopId, sop });
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
        this.emit({ type: "inspector.case", investigationRef: target.investigationRef, summary: d?.caseSummaries?.[target.investigationRef] ?? null });
        break;
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
    this.postQueue();
    // The host posts the library with every load, like the queue.
    this.emit({ type: "library", sops: this.data.library?.sops ?? [], cases: this.data.library?.cases ?? [] });
    this.emit({ type: "me", name: "Analyst One", principal: "analyst-1" });
  }

  private postQueue(): void {
    const q = this.data?.queue;
    if (q) this.emit({ type: "queue", investigations: q.investigations, cases: q.cases, caseSource: q.caseSource });
  }
}
