// The harness transport (impl §5.4): answers the webview from a recorded
// snapshot set and echoes every intent, so a click's effect is visible and
// component tests can assert on what the page asked the host for.
import type { InboundMessage, InvestigationSnapshot, RenderMessage } from "../src/protocol";
import type { WebviewTransport } from "../webview/src/transport";

/** One recorded stage of the reference scenario (harness/record.ts). */
export interface HarnessSnapshot {
  name: string;
  snapshot: InvestigationSnapshot;
  /** Resolved labels for the refs the snapshot cites. */
  labels: Record<string, string>;
  /** Cross-investigation appearances per entity ref (the popover). */
  appearances?: Record<string, import("../src/backend").Appearance[]>;
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
      case "ui.resync":
        this.postInitial();
        break;
      case "evidence.resolve": {
        const label = this.data?.labels[intent.ref];
        if (label) this.emit({ type: "evidence.label", ref: intent.ref, label });
        break;
      }
      case "entity.info":
        this.emit({ type: "entity.info", ref: intent.ref, appearances: this.data?.appearances?.[intent.ref] ?? [] });
        break;
      case "rename":
        this.emit({ type: "renamed", title: intent.title });
        break;
      case "seed.caseSearch":
        this.emit({ type: "draft.cases", cases: [{ number: "INC0010042", title: "Suspicious interactive RDP logon", status: "New" }] });
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
  }
}
