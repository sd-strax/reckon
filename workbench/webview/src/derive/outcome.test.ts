// Readers that ask what happened read the recorded outcome; readers that gate
// a lifecycle step read the status (adr/0361). The list serves a partial
// result as SUCCEEDED with outcome PARTIAL, a timeout as FAILED with TIMEOUT.
import { describe, expect, it } from "vitest";
import type { ActionRow } from "../../../src/viewTypes";
import { caseNoteStatus } from "./actions";
import { rideStopped } from "./dependencies";
import { stageOf } from "./stage";
import { handoffNote, notificationWord } from "./summary";

const A = (over: Partial<ActionRow>): ActionRow => ({
  actionId: "a", actionType: "host.isolate", tier: "T2", status: "SUCCEEDED", requiredMode: "MANUAL", targets: ["WIN-A"],
  pending: false, pendingLabel: "PENDING_MANUAL", expired: false, isReversal: false, tierEscalated: false, evidenceRefs: [], ...over,
});
const partial = { status: "SUCCEEDED", outcome: "PARTIAL" };
const timeout = { status: "FAILED", outcome: "TIMEOUT" };
const verdict = { disposition: "MALICIOUS", verdictAt: "" };

describe("what happened, as the list serves it (adr/0361)", () => {
  it("prints the outcome word on the handoff note", () => {
    const note = handoffNote("t", null, [A({ actionId: "p", ...partial }), A({ actionId: "t", ...timeout, targets: ["WIN-B"] }), A({ actionId: "s", outcome: "SUCCEEDED", targets: ["WIN-C"] })], [], [], []);
    expect(note).toContain("- host.isolate → WIN-A: PARTIAL");
    expect(note).toContain("- host.isolate → WIN-B: TIMEOUT");
    expect(note).toContain("- host.isolate → WIN-C: SUCCEEDED");
  });

  it("says a partial notify partly notified and a timed-out one unconfirmed on the tile", () => {
    const notify = (over: Partial<ActionRow>) => A({ actionType: "notify.email", category: "notification", ...over });
    expect(notificationWord([notify(partial)])).toBe("partly notified");
    expect(notificationWord([notify(timeout)])).toBe("notification unconfirmed");
    expect(notificationWord([notify({ outcome: "SUCCEEDED" })])).toBe("notified");
    expect(notificationWord([notify({ status: "FAILED", outcome: "FAILED" })])).toBe("not yet notified");
    expect(notificationWord([notify({ status: "REQUESTED", pending: true })])).toBe("awaiting approval");
    expect(notificationWord([])).toBe("not yet notified");
    // The strongest outcome wins.
    expect(notificationWord([notify(timeout), notify(partial), notify({ outcome: "SUCCEEDED" })])).toBe("notified");
    expect(notificationWord([notify(timeout), notify(partial)])).toBe("partly notified");
    // A request whose window ran out awaits nothing.
    expect(notificationWord([notify({ actionId: "p", ...partial }), notify({ actionId: "l", status: "REQUESTED", pending: true, expired: true })])).toBe("partly notified");
    // A pending retry of a timed-out send is what the analyst acts on next.
    expect(notificationWord([notify({ actionId: "t", ...timeout }), notify({ actionId: "r", status: "REQUESTED", pending: true, retryOf: "t" })])).toBe("awaiting approval");
  });

  // The stage strip follows the status, as the server does (adr/0361): a
  // partial cannot be re-requested, so the strip moves on and the Response
  // lens flags the partial instead.
  it("completes Notify on a partial send and Contain on a partial containment", () => {
    const stage = (a: ActionRow) => stageOf({ status: "ACTIVE", verdict, actions: [a], comms: [], openDependencies: 0 }).done;
    const notify = (over: Partial<ActionRow>) => A({ actionType: "notify.email", category: "notification", ...over });
    expect(stage(notify(partial)).notify).toBe(true);
    expect(stage(notify(timeout)).notify).toBe(false);
    expect(stage(notify({ outcome: "SUCCEEDED" })).notify).toBe(true);
    expect(stage(A(partial)).contain).toBe(true);
    expect(stage(A(timeout)).contain).toBe(false);
    expect(stage(A({ status: "REVERSED", outcome: "PARTIAL" })).contain).toBe(true);
  });

  it("does not call a partial case comment posted", () => {
    const seed = { type: "case", caseId: "INC1" };
    const comment = (over: Partial<ActionRow>) => A({ actionType: "ticket.comment", targets: ["INC1"], ...over });
    expect(caseNoteStatus(seed, [comment(partial)])).toBe("partial");
    expect(caseNoteStatus(seed, [comment(timeout)])).toBe("timeout");
    expect(caseNoteStatus(seed, [comment({ outcome: "SUCCEEDED" })])).toBe("posted");
  });

  it("keeps a partial notify's ride, as the server marks its dependencies sent, and stops a timed-out one", () => {
    expect(rideStopped({ ...partial, expired: false })).toBe(false);
    expect(rideStopped({ ...timeout, expired: false })).toBe(true);
  });
});
