// The investigation document's inbound router: one handler per message the
// webview can post (protocol.ts InboundMessage). The table's type requires a
// handler for every message type, so a new message cannot be added to the
// protocol and silently dropped by the host.
import type { InboundMessage } from "./protocol";

export type InboundHandlers = {
  [K in InboundMessage["type"]]: (msg: Extract<InboundMessage, { type: K }>) => void;
};

/** Webview payloads are not validated beyond `type`: same build as the host (adr/0244). */
export function routeInbound(handlers: InboundHandlers, msg: InboundMessage, onUnknown: (type: unknown) => void): void {
  if (!Object.hasOwn(handlers, msg?.type)) {
    onUnknown(msg?.type);
    return;
  }
  (handlers[msg.type] as (m: InboundMessage) => void)(msg);
}
