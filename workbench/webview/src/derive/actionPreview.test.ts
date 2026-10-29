import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { actionPreview, undoWords } from "./actionPreview";

const row = (over: Partial<ActionRow>) => ({ actionType: "ticket.comment", targets: ["INC0010042"], ...over }) as ActionRow;

describe("what an action will do (#124)", () => {
  it("shows the comment a ticket.comment will write before it is approved", () => {
    const pv = actionPreview(row({ name: "Comment on a ticket", parameters: { body: "Investigation opened in reckon; this case will be updated." } }));
    expect(pv.sentence).toBe("Comment on a ticket — INC0010042");
    expect(pv.fields).toEqual([{ label: "Comment", value: "Investigation opened in reckon; this case will be updated.", long: false }]);
  });
  it("labels every parameter in words and hides plumbing", () => {
    const pv = actionPreview(row({ actionType: "notify.slack", name: "Post to a Slack channel", targets: ["#itops"], parameters: { message: "reimage WIN-FILE01", follow_up_hours: 48, thread_ref: "t1" } }));
    expect(pv.fields.map((f) => f.label)).toEqual(["Message", "Follow up after"]);
    expect(pv.fields[1].value).toBe("48 hours if there is no reply");
    expect(pv.followUp).toBe(true);
    expect(actionPreview(row({ actionType: "host.isolate", name: "Isolate host", targets: ["WIN-FILE01"] })).sentence).toBe("Isolate host — WIN-FILE01");
  });
  it("says whether it can be undone in words", () => {
    expect([undoWords("REVERSIBLE"), undoWords("BEST_EFFORT"), undoWords("IRREVERSIBLE"), undoWords(undefined)]).toEqual(["yes", "partly — treat as permanent", "no", "not stated"]);
  });
});
