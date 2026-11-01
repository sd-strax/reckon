// A transport for a side panel's component test: the test plays the host,
// emitting what it would post and reading what the page asked for.
import type { WebviewTransport } from "../transport";

export class FakeTransport<In, Out> implements WebviewTransport<In, Out> {
  readonly intents: In[] = [];
  private handlers: ((m: Out) => void)[] = [];
  private state: unknown;
  send(intent: In): void {
    this.intents.push(intent);
  }
  onMessage(handler: (m: Out) => void): () => void {
    this.handlers.push(handler);
    return () => { this.handlers = this.handlers.filter((h) => h !== handler); };
  }
  emit(m: Out): void {
    for (const h of [...this.handlers]) h(m);
  }
  getState<T>(): T | undefined {
    return this.state as T | undefined;
  }
  setState(s: unknown): void {
    this.state = s;
  }
}
