import { describe, expect, it } from "vitest";
import type { ActionRow, CommsThread, Hypothesis, ThreadEntry } from "../../../src/viewTypes";
import type { InvestigationSnapshot } from "../../../src/protocol";
import synthetic from "../../../harness/snapshots/synthetic.json";
import { defaultLens } from "./lens";
import { buildMatrix } from "./matrix";
import { concludePreflight, concludeAllowed, preflightClean } from "./preflight";
import { knowledgeNudge } from "./knowledgeNudge";
import { nearestExpiry, primaryAction, rowStep, stageOf } from "./stage";
import { handoffNote, keyEntities, proceduresFollowed, stillToDo } from "./summary";
import { groupTurns, recordLine } from "./turns";

const snap = (synthetic as unknown as { snapshot: InvestigationSnapshot }).snapshot;
const A = (over: Partial<ActionRow>): ActionRow => ({
  actionId: "a", actionType: "host.isolate", tier: "T2", status: "REQUESTED", requiredMode: "MANUAL", targets: ["host-a"],
  pending: false, pendingLabel: "PENDING_MANUAL", expired: false, isReversal: false, tierEscalated: false, evidenceRefs: [], ...over,
});
const verdict = { disposition: "MALICIOUS", verdictAt: "" };
const noComms: CommsThread[] = [];

describe("stage", () => {
  it("walks Investigate → Contain → Notify → Close from the record", () => {
    expect(stageOf({ status: "ACTIVE", verdict: null, actions: [], comms: noComms, openOwnerItems: 0 }).current).toBe("investigate");
    const contain = stageOf({ status: "ACTIVE", verdict, actions: [A({ pending: true })], comms: noComms, openOwnerItems: 0 });
    expect(contain.current).toBe("contain");
    expect(contain.done.investigate && contain.done.verdict).toBe(true);
    const notify = stageOf({ status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" }), A({ actionId: "n", actionType: "notify.email", status: "REQUESTED", pending: true })], comms: noComms, openOwnerItems: 0 });
    expect(notify.current).toBe("notify");
    expect(notify.done.contain).toBe(true);
    const awaiting = stageOf({ status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" })], comms: [{ status: "awaiting_reply" } as CommsThread], openOwnerItems: 0 });
    expect(awaiting.current).toBe("notify");
    const sent = A({ actionId: "n", actionType: "notify.slack", status: "SUCCEEDED" });
    const close = stageOf({ status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" }), sent], comms: noComms, openOwnerItems: 0 });
    expect(close.current).toBe("close");
    expect(stageOf({ status: "CONCLUDED", verdict, actions: [], comms: noComms, openOwnerItems: 0 }).current).toBeNull();
  });
  it("marks a step done only when something was done for it (#145)", () => {
    // The road test: a MALICIOUS verdict, nothing contained, only the case-note comment.
    const note = A({ actionId: "c", actionType: "ticket.comment", status: "SUCCEEDED" });
    const nothing = stageOf({ status: "ACTIVE", verdict, actions: [note], comms: noComms, openOwnerItems: 0 });
    expect(nothing.done.contain).toBe(false);
    expect(nothing.done.notify).toBe(false);
    expect(nothing.current).toBe("contain");
    // A reversal is not containment; a failed isolation contained nothing.
    const failed = stageOf({ status: "ACTIVE", verdict, actions: [A({ status: "FAILED" }), A({ actionId: "r", actionType: "host.unisolate", isReversal: true, status: "SUCCEEDED" })], comms: noComms, openOwnerItems: 0 });
    expect(failed.done.contain).toBe(false);
    // A benign verdict with nothing done reads as not needed, not as done.
    const benign = stageOf({ status: "ACTIVE", verdict: { disposition: "BENIGN", verdictAt: "" }, actions: [note], comms: noComms, openOwnerItems: 0 });
    expect(benign.done.contain || benign.done.notify).toBe(false);
    expect(benign.notNeeded.contain && benign.notNeeded.notify).toBe(true);
    expect(benign.current).toBe("close");
  });
  it("derives one primary action per stage", () => {
    const review = primaryAction({ status: "ACTIVE", verdict, actions: [A({ pending: true }), A({ actionId: "b", pending: true })], comms: noComms, openOwnerItems: 0 }, stageOf({ status: "ACTIVE", verdict, actions: [A({ pending: true })], comms: noComms, openOwnerItems: 0 }), false);
    expect(review).toEqual({ kind: "review", count: 2 });
    const i = { status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" })], comms: [{ status: "awaiting_reply" } as CommsThread], openOwnerItems: 0 };
    expect(primaryAction(i, stageOf(i), false)).toEqual({ kind: "notify" });
    const j = { status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" }), A({ actionId: "n", actionType: "notify.slack", status: "SUCCEEDED" })], comms: noComms, openOwnerItems: 0 };
    expect(primaryAction(j, stageOf(j), true)).toEqual({ kind: "conclude" });
    expect(primaryAction(j, stageOf(j), false)).toBeNull();
  });
  it("maps a list row to its step and finds the nearest expiry", () => {
    const row = { id: "", title: "", state: "active", lastEventSequence: 0, pendingActions: 0, hasVerdict: true, openActions: 1, openNotifications: 0 };
    expect(rowStep(row)).toBe("contain");
    expect(rowStep({ ...row, openActions: 0, openNotifications: 1 })).toBe("notify");
    expect(rowStep({ ...row, hasVerdict: false })).toBe("investigate");
    expect(rowStep({ ...row, state: "concluded" })).toBeNull();
    expect(nearestExpiry([A({ pending: true, expiresAt: "2030-01-01T00:10:00Z" }), A({ pending: true, expiresAt: "2030-01-01T00:05:00Z" }), A({ pending: true, expired: true, expiresAt: "2020-01-01T00:00:00Z" })])).toBe("2030-01-01T00:05:00Z");
  });
});

describe("lens", () => {
  it("follows the stage", () => {
    expect(defaultLens("active", false)).toBe("reasoning");
    expect(defaultLens("ACTIVE", true)).toBe("response");
    expect(defaultLens("concluded", true)).toBe("summary");
  });
});

describe("matrix", () => {
  it("scores every prediction against the live set with opposites for discriminators", () => {
    const m = buildMatrix(snap.hypotheses);
    expect(m.columns.map((h) => h.handle || h.id.slice(-1))).toEqual(["H1 not-owner", "2"]);
    expect(m.rows.map((r) => r.cells)).toEqual([["✓", "✕"], ["✓", "✕"], ["?", "?"], ["?", "?"]]);
    expect(m.inconsistent).toEqual([0, 2]);
    expect(m.rows.filter((r) => r.open).length).toBe(2);
    const withdrawn: Hypothesis = { ...snap.hypotheses[1], predictions: [{ ...snap.hypotheses[1].predictions[0], status: "WITHDRAWN" }] };
    expect(buildMatrix([snap.hypotheses[0], withdrawn]).rows.length).toBe(3);
  });
});

describe("preflight", () => {
  it("names each unmet gate and where to satisfy it", () => {
    const items = concludePreflight({ verdict: null, pins: [], actions: [A({ pending: true })], comms: [{ status: "awaiting_reply" } as CommsThread], openOwnerItems: 1 });
    expect(items.map((x) => x.ok)).toEqual([false, false, false, false, false]);
    expect(items[0].lens).toBe("reasoning");
    expect(items[2].text).toBe("1 action(s) not yet terminal");
    expect(concludeAllowed(items)).toBe(false);
    const clean = concludePreflight({ verdict, pins: [{ interpretationId: "p", finding: "", inputRefs: [], actor: "HUMAN", pinnedAt: "", superseded: false }], actions: [A({ status: "SUCCEEDED" })], comms: noComms, openOwnerItems: 0 });
    expect(preflightClean(clean)).toBe(true);
    expect(concludeAllowed(concludePreflight({ verdict, pins: [], actions: [], comms: noComms, openOwnerItems: 0 }))).toBe(true); // the engine gates only the verdict
  });
});

describe("knowledge nudge", () => {
  it("counts what auto would include under opt_in and stays silent otherwise", () => {
    expect(knowledgeNudge(snap.knowledge, "opt_in")?.text).toBe("1 procedure and 1 past case the auto posture would include are not included");
    expect(knowledgeNudge(snap.knowledge.map((k) => ({ ...k, included: true })), "opt_in")).toBeNull();
    expect(knowledgeNudge(snap.knowledge, "auto")).toBeNull();
    expect(knowledgeNudge([{ ...snap.knowledge[1], band: "DISTINCT" }], "opt_in")).toBeNull();
  });
});

describe("turns", () => {
  it("groups the thread into turns with their records as link lines", () => {
    const turns = groupTurns(snap.thread!, snap.turns, snap.hypotheses);
    expect(turns.length).toBe(3);
    expect(turns[0].aside).toBe(false);
    expect(turns[1].aside).toBe(true);
    expect(turns[0].records.map((r) => r.text)).toEqual([]); // the aside holds its own records
    expect(turns[2].resetBefore?.summary).toContain("context reset");
    expect(turns[2].transcript).toBeNull(); // the stub: no body loaded
  });
  it("renders record lines from the hypothesis set", () => {
    const hs = snap.hypotheses;
    const h1 = hs[0];
    const e = (over: Partial<ThreadEntry>): ThreadEntry => ({ sequenceNo: 1, occurredAt: "", actor: { principal: "a", kind: "AI_DELEGATED" }, interpretationId: "i", interpretationType: "support", summary: "", inputRefs: [], outputRefs: [], toolCalls: 0, hasTranscript: false, superseded: false, consultedSops: [], consultedSimilar: [], ...over });
    expect(recordLine(e({ interpretationType: "support", outputRefs: [h1.id] }), hs)).toMatchObject({ text: "H1 not-owner supported", tone: "ok" });
    expect(recordLine(e({ interpretationType: "refutation", outputRefs: [hs[1].id] }), hs)).toMatchObject({ text: "H2 refuted", tone: "bad" });
    expect(recordLine(e({ interpretationType: "support", inputRefs: [h1.predictions[1].id, h1.predictions[1].testResultRefs[0]] }), hs)).toMatchObject({ text: "H1 not-owner·P2 confirmed by attestation", tone: "ok" });
    expect(recordLine(e({ interpretationType: "prediction", outputRefs: [h1.predictions[0].id, h1.predictions[2].id] }), hs).text).toBe("recorded P1, P3 under H1 not-owner");
    expect(recordLine(e({ interpretationType: "hypothesis", outputRefs: [h1.id, hs[1].id] }), hs).text).toBe("proposed H1 not-owner, H2");
  });
});

describe("summary", () => {
  it("lists key entities with derivable status words, still-to-do, procedures followed, and the handoff note", () => {
    const labels = (synthetic as unknown as { labels: Record<string, string> }).labels;
    const ents = keyEntities(snap.investigation.seed, snap.thread!, snap.pins, snap.actions!, labels);
    expect(ents[0]).toMatchObject({ role: "seed subject", status: "isolated" });
    const todo = stillToDo(snap.actions!, snap.comms!, snap.hypotheses, labels);
    expect(todo.map((t) => t.kind)).toEqual(["approve", "approve", "comms", "prediction", "prediction"]);
    expect(proceduresFollowed(snap.thread!)).toEqual([{ sopId: "sop-1", title: "Procedure one" }]);
    const note = handoffNote("T", snap.investigation.verdict, snap.actions!, todo, snap.hypotheses, proceduresFollowed(snap.thread!));
    expect(note).toContain("Verdict: none yet");
    expect(note).toContain("- host.isolate → host-a: SUCCEEDED (ISO-1)");
    expect(note).toContain("Still to do:");
  });
});
