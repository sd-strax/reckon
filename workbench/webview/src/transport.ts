// The transport seam (impl §6): the only place the webview touches its host.
// VsCodeTransport wraps acquireVsCodeApi(); the harness provides another; a
// future browser surface implements the same interface against the HTTP API.
// The message types are the surface's (protocol.ts): the investigation
// document's by default, a side panel's where it mounts.
import type { InboundMessage, RenderMessage } from "../../src/protocol";

export interface WebviewTransport<In = InboundMessage, Out = RenderMessage> {
  send(intent: In): void;
  onMessage(handler: (m: Out) => void): () => void;
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

export class VsCodeTransport<In = InboundMessage, Out = RenderMessage> implements WebviewTransport<In, Out> {
  private readonly api: VsCodeApi;
  constructor() {
    if (typeof acquireVsCodeApi !== "function") {
      throw new Error("not inside a VS Code webview");
    }
    this.api = acquireVsCodeApi();
  }
  send(intent: In): void {
    this.api.postMessage(intent);
  }
  onMessage(handler: (m: Out) => void): () => void {
    const listener = (event: MessageEvent) => handler(event.data as Out);
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
