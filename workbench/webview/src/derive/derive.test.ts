import { describe, expect, it } from "vitest";
import type { ActionRow, Hypothesis } from "../../../src/viewTypes";
import { actionStatusBadge, canRerequest, canReverse, caseNoteStatus, headerLabel, pendingQueue, successors } from "./actions";
import { classifyDraft, effectiveDraftKind, entitySubLabel } from "./draft";
import { layoutGraph } from "./graph";
import { blockersOf, hypName, orderHypotheses, outstanding, rivalsOf, tested } from "./hyps";
import { stillToDo } from "./summary";
import { verdictChecklist, verdictRefs, verdictResidual } from "./verdict";

const H = (id: string, status: string, preds: { id: string; status: string; discriminates?: string[] }[] = [], handle?: string): Hypothesis => ({
  id, statement: "claim " + id, status, handle, labels: [],
  predictions: preds.map((p) => ({ id: p.id, statement: "pred " + p.id, status: p.status, discriminates: p.discriminates || [], testResultRefs: [] })),
});
const A = (over: Partial<ActionRow>): ActionRow => ({
  actionId: "a", actionType: "host.isolate", tier: "T2", status: "REQUESTED", requiredMode: "MANUAL", targets: [],
  pending: false, pendingLabel: "PENDING_MANUAL", expired: false, isReversal: false, tierEscalated: false, evidenceRefs: [], ...over,
});

describe("hyps", () => {
  it("names by handle or position, orders live first", () => {
    const hs = [H("a", "REFUTED"), H("b", "OPEN", [], "H2 legit"), H("c", "PROPOSED")];
    expect(hypName(hs[0], hs)).toBe("H1");
    expect(hypName(hs[1], hs)).toBe("H2 legit");
    expect(orderHypotheses(hs).map((h) => h.id)).toEqual(["b", "c", "a"]);
  });
  it("briefs rivals with their untested discriminators and finds blockers", () => {
    const h1 = H("h1", "OPEN", [{ id: "p1", status: "CONFIRMED" }]);
    const h2 = H("h2", "OPEN", [{ id: "p2", status: "UNTESTED", discriminates: ["h1"] }, { id: "p3", status: "UNTESTED" }]);
    const h3 = H("h3", "ABANDONED", [{ id: "p4", status: "UNTESTED", discriminates: ["h1"] }]);
    expect(rivalsOf(h1, [h1, h2, h3])).toEqual([{ id: "h2", handle: undefined, statement: "claim h2", untestedDiscriminators: 1 }]);
    expect(blockersOf(h1, [h1, h2, h3]).map((b) => b.p.id)).toEqual(["p2"]); // the abandoned rival does not block
  });
});

describe("outstanding predictions (#140)", () => {
  it("one rule: untested or inconclusive under a live hypothesis; nothing under a decided one", () => {
    const refuted = H("h1", "REFUTED", [{ id: "p1", status: "DISCONFIRMED" }, { id: "p2", status: "DISCONFIRMED" }, { id: "p3", status: "INCONCLUSIVE" }]);
    const open = H("h2", "OPEN", [{ id: "p4", status: "INCONCLUSIVE" }, { id: "p5", status: "UNTESTED" }, { id: "p6", status: "CONFIRMED" }]);
    expect(refuted.predictions.filter(tested).length).toBe(3); // inconclusive was tested
    expect(refuted.predictions.some((p) => outstanding(p, refuted))).toBe(false);
    expect(open.predictions.filter((p) => outstanding(p, open)).map((p) => p.id)).toEqual(["p4", "p5"]);
    const todo = stillToDo([], [], [refuted, open], {}).map((t) => t.ref);
    expect(todo).toEqual(["p4", "p5"]); // the Summary agrees with the card
  });
});

describe("actions", () => {
  it("queues pending and expired-without-successor rows", () => {
    const rows = [A({ actionId: "1", pending: true }), A({ actionId: "2", status: "EXPIRED" }), A({ actionId: "3", status: "EXPIRED" }), A({ actionId: "4", status: "REQUESTED", pending: true, retryOf: "3" })];
    expect(pendingQueue(rows).map((a) => a.actionId)).toEqual(["1", "2", "4"]);
  });
  it("keeps each chain a line: one re-request, one live reversal (adr/0152)", () => {
    const expired = A({ actionId: "e", status: "EXPIRED", expired: true });
    const retry = A({ actionId: "r", status: "REJECTED", retryOf: "e" });
    expect(canRerequest(expired, [expired])).toBe(true);
    expect(canRerequest(expired, [expired, retry])).toBe(false); // answered: the chain moves on from the retry
    expect(successors(expired, [expired, retry]).retry?.actionId).toBe("r");

    const done = A({ actionId: "o", status: "SUCCEEDED", reversibility: "REVERSIBLE" });
    const pendingRev = A({ actionId: "v1", status: "REQUESTED", pending: true, isReversal: true, reversalOfRef: "o" });
    const failedRev = A({ actionId: "v1", status: "FAILED", isReversal: true, reversalOfRef: "o" });
    expect(canReverse(done, [done])).toBe(true);
    expect(canReverse(done, [done, pendingRev])).toBe(false);
    expect(canReverse(done, [done, failedRev])).toBe(true); // a failed reversal may be tried again
    expect(canReverse(A({ status: "SUCCEEDED", reversibility: "BEST_EFFORT" }), [])).toBe(false);
  });
  it("derives the presentation label", () => {
    expect(headerLabel("ACTIVE", [A({ pending: true })], null)).toBe("ACTIVE · REMEDIATING");
    expect(headerLabel("ACTIVE", [A({ status: "EXECUTING" })], null)).toBe("ACTIVE · REMEDIATING");
    expect(headerLabel("ACTIVE", [], { disposition: "MALICIOUS", verdictAt: "" })).toBe("ACTIVE · VERDICT REACHED");
    expect(headerLabel("PAUSED", [A({ pending: true })], null)).toBe("PAUSED");
  });
  it("maps statuses to badges and the case-note word", () => {
    expect(actionStatusBadge(A({ status: "EXECUTING" }))).toEqual({ label: "DISPATCHING", tone: "info" });
    expect(actionStatusBadge(A({ pending: true, pendingLabel: "PENDING_TWO_PARTY" })).label).toBe("PENDING_TWO_PARTY");
    const seed = { type: "case", caseId: "INC1" };
    expect(caseNoteStatus(seed, [A({ actionType: "ticket.comment", targets: ["INC1"], status: "SUCCEEDED" })])).toBe("posted");
    expect(caseNoteStatus(seed, [A({ actionType: "ticket.comment", targets: ["INC1"], pending: true })])).toBe("awaiting approval");
    expect(caseNoteStatus(seed, [])).toBeNull();
  });
});

describe("draft", () => {
  it("classifies entities and questions, honours the override", () => {
    expect(classifyDraft("WIN-FILE01")).toBe("entity");
    expect(classifyDraft("why is this host beaconing?")).toBe("question");
    expect(classifyDraft("two words")).toBe("question");
    expect(effectiveDraftKind("WIN-FILE01", "question")).toBe("question");
    expect(entitySubLabel("10.0.4.20")).toBe("IP");
    expect(entitySubLabel("d41d8cd98f00b204e9800998ecf8427e")).toBe("file");
    expect(entitySubLabel("WIN-FILE01")).toBe("host");
  });
});

describe("verdict", () => {
  it("builds the checklist, the residual and the cited refs", () => {
    const pins = [
      { interpretationId: "p1", finding: "f", inputRefs: ["r1", "r2"], actor: "HUMAN", pinnedAt: "", superseded: false },
      { interpretationId: "p2", finding: "g", inputRefs: ["r2", "r3"], actor: "HUMAN", pinnedAt: "", superseded: true },
    ];
    expect(verdictChecklist(pins, null)[0]).toEqual({ ok: true, text: "1 evidence item(s) pinned" });
    expect(verdictChecklist([], null)[0].ok).toBe(false);
    expect(verdictRefs(pins)).toEqual(["r1", "r2"]);
    const residual = verdictResidual([{ verb: "x", intent: "", inputs: [], output: "", status: "unavailable" }], [H("h", "PROPOSED", [{ id: "p", status: "UNTESTED", discriminates: ["o"] }])]);
    expect(residual[0]).toBe("verbs never checkable in this tenant: x");
    expect(residual[1]).toContain("undecided hypothesis H1 — never adopted, 1 untested discriminating test");
    expect(residual[2]).toBe("untested prediction: pred p");
    expect(verdictResidual(null, [])).toEqual(["no known gaps — all configured verbs available, no undecided hypotheses, no untested predictions"]);
  });
});

describe("graph", () => {
  it("splits the layers and flags reasoning that reaches no evidence", () => {
    const l = layoutGraph({
      refs: [{ id: "x-hypothesis--1", kind: "stix", type: "x-hypothesis" }, { id: "x-prediction--2", kind: "stix", type: "x-prediction" }, { id: "observed-data--3", kind: "stix", type: "observed-data" }, { id: "e1", kind: "ocsf", type: "Authentication" }],
      edges: [{ from: "x-hypothesis--1", to: "x-prediction--2", ai: true }, { from: "x-prediction--2", to: "x-hypothesis--1", ai: true }],
    });
    expect(l.stix.length).toBe(3);
    expect(l.ocsf.map((r) => r.id)).toEqual(["e1"]);
    expect([...l.ungrounded]).toEqual(["x-hypothesis--1", "x-prediction--2"]);
    const grounded = layoutGraph({ refs: l.stix.concat(l.ocsf), edges: [{ from: "x-hypothesis--1", to: "x-prediction--2", ai: true }, { from: "x-prediction--2", to: "observed-data--3", ai: false }] });
    expect(grounded.ungrounded.size).toBe(0);
  });
});
