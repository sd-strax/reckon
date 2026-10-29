import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { actionTitle, groupOf } from "../components/ActionCard";
import { isContainment } from "./stage";

const row = (over: Partial<ActionRow>) => ({ actionType: "x.y", isReversal: false, ...over }) as ActionRow;

describe("the catalog names and groups an action (#123)", () => {
  it("groups by the served category, never by guessing", () => {
    expect(groupOf(row({ actionType: "ticket.comment", category: "case_record" }))).toBe("case_record");
    expect(groupOf(row({ actionType: "vendor.quarantine_device", category: "containment" }))).toBe("containment");
    expect(groupOf(row({ actionType: "ticket.comment" }))).toBe("case_record"); // a row served without one
  });
  it("a case-record write is never containment; a reversal never is", () => {
    expect(isContainment(row({ actionType: "ticket.comment", category: "case_record" }))).toBe(false);
    expect(isContainment(row({ actionType: "email.purge", category: "eradication" }))).toBe(true);
    expect(isContainment(row({ actionType: "host.unisolate", category: "recovery", isReversal: true }))).toBe(false);
  });
  it("titles by the human name, the id when there is none", () => {
    expect(actionTitle(row({ actionType: "host.isolate", name: "Isolate host" }))).toBe("Isolate host");
    expect(actionTitle(row({ actionType: "host.isolate" }))).toBe("host.isolate");
  });
});
