// The investigation document's inbound router: one handler per message the
// webview can post (protocol.ts InvestigationInboundMessage). The table's type
// requires a handler for every message type, so a new message cannot be added
// to the protocol and silently dropped by the host. The Knowledge view's own
// intents have no handler here (adr/0344): one arriving is logged as unknown.
import type { InboundMessage, InvestigationInboundMessage } from "./protocol";

export type InboundHandlers = {
  [K in InvestigationInboundMessage["type"]]: (msg: Extract<InvestigationInboundMessage, { type: K }>) => void;
};

/** Webview payloads are not validated beyond `type`: same build as the host (adr/0244). */
export function routeInbound(handlers: InboundHandlers, msg: InboundMessage, onUnknown: (type: unknown) => void): void {
  if (!Object.hasOwn(handlers, msg?.type)) {
    onUnknown(msg?.type);
    return;
  }
  (handlers as unknown as Record<string, (m: InboundMessage) => void>)[msg.type]!(msg);
}
