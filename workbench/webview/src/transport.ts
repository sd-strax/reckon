// The transport seam (impl §6): the only place the webview touches its host.
// VsCodeTransport wraps acquireVsCodeApi(); the harness provides another; a
// future browser surface implements the same interface against the HTTP API.
import type { InboundMessage, RenderMessage } from "../../src/protocol";

export interface WebviewTransport {
  send(intent: InboundMessage): void;
  onMessage(handler: (m: RenderMessage) => void): () => void;
  getState<T>(): T | undefined;
  setState(s: unknown): void;
}

interface VsCodeApi {
  postMessage(msg: unknown): void;
  getState(): unknown;
  setState(s: unknown): void;
}

declare global {
  // eslint-disable-next-line no-var
  var acquireVsCodeApi: (() => VsCodeApi) | undefined;
}

export class VsCodeTransport implements WebviewTransport {
  private readonly api: VsCodeApi;
  constructor() {
    if (typeof acquireVsCodeApi !== "function") {
      throw new Error("not inside a VS Code webview");
    }
    this.api = acquireVsCodeApi();
  }
  send(intent: InboundMessage): void {
    this.api.postMessage(intent);
  }
  onMessage(handler: (m: RenderMessage) => void): () => void {
    const listener = (event: MessageEvent) => handler(event.data as RenderMessage);
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }
  getState<T>(): T | undefined {
    return this.api.getState() as T | undefined;
  }
  setState(s: unknown): void {
    this.api.setState(s);
  }
}
