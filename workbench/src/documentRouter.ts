// The investigation document's inbound router: one handler per message the
// webview can post (protocol.ts InboundMessage). The table's type requires a
// handler for every message type, so a new message cannot be added to the
// protocol and silently dropped by the host.
import type { InboundMessage } from "./protocol";

export type InboundHandlers = {
  [K in InboundMessage["type"]]: (msg: Extract<InboundMessage, { type: K }>) => void;
};

export function routeInbound(handlers: InboundHandlers, msg: InboundMessage): void {
  (handlers[msg.type] as (m: InboundMessage) => void)(msg);
}
