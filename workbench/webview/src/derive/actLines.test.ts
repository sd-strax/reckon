import { describe, expect, it } from "vitest";
import type { ActionRow, ThreadEntry } from "../../../src/viewTypes";
import { actionRequestText } from "./turns";

const req = (id: string, summary: string) => ({ interpretationId: id, interpretationType: "action-request", summary }) as ThreadEntry;
const row = (over: Partial<ActionRow>) => ({ actionId: "a", actionType: "notify.email", targets: ["ciso@contoso.com"], isReversal: false, ...over }) as ActionRow;

describe("an action request names what it asked for (#153)", () => {
  it("leads with the act, the type and the target, then the reason", () => {
    const actions = [
      row({ requestingInterpretationId: "i1" }),
      row({ actionId: "b", requestingInterpretationId: "i2", retryOf: "a" }),
      row({ actionId: "c", actionType: "host.unisolate", targets: ["WIN-FILE01"], isReversal: true, requestingInterpretationId: "i3" }),
    ];
    expect(actionRequestText(req("i1", "notify the CISO"), actions)).toBe("requested notify.email → ciso@contoso.com: notify the CISO");
    expect(actionRequestText(req("i2", "this is baad"), actions)).toBe("↻ re-requested notify.email → ciso@contoso.com: this is baad");
    expect(actionRequestText(req("i3", "reimaged, verified clean"), actions)).toBe("↺ requested the reversal host.unisolate → WIN-FILE01: reimaged, verified clean");
    expect(actionRequestText(req("i9", "orphan reason"), actions)).toBe("orphan reason");
  });
});
