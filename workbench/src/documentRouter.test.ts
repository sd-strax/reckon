import { describe, expect, it } from "vitest";
import { routeInbound, type InboundHandlers } from "./documentRouter";

describe("document router", () => {
  it("hands each message to the handler for its type", () => {
    const seen: string[] = [];
    const handlers = new Proxy({}, { get: (_t, k) => (m: { type: string }) => seen.push(String(k) + ":" + m.type) }) as InboundHandlers;
    routeInbound(handlers, { type: "pin.unpin", interpretationId: "i", reason: "r" });
    routeInbound(handlers, { type: "ready" });
    expect(seen).toEqual(["pin.unpin:pin.unpin", "ready:ready"]);
  });
});
