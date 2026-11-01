import { describe, expect, it, vi } from "vitest";
import { routeInbound, type InboundHandlers } from "./documentRouter";
import type { InboundMessage } from "./protocol";

describe("document router", () => {
  it("hands each message to the handler for its type", () => {
    const seen: string[] = [];
    const record = (m: { type: string }) => { seen.push(m.type); };
    const handlers = { "pin.unpin": record, ready: record } as unknown as InboundHandlers;
    const unknown = vi.fn();
    routeInbound(handlers, { type: "pin.unpin", interpretationId: "i", reason: "r" }, unknown);
    routeInbound(handlers, { type: "ready" }, unknown);
    expect(seen).toEqual(["pin.unpin", "ready"]);
    expect(unknown).not.toHaveBeenCalled();
  });

  it.each(["no.such.message", "constructor", "__proto__", undefined])("reports a message of unknown type %s and does not throw", (type) => {
    const unknown = vi.fn();
    routeInbound({} as InboundHandlers, { type } as unknown as InboundMessage, unknown);
    expect(unknown).toHaveBeenCalledWith(type);
  });
});
