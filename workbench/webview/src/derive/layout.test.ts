import { describe, expect, it } from "vitest";
import type { ActionRow, CommsThread, Hypothesis, ThreadEntry } from "../../../src/backend";
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
    const close = stageOf({ status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" })], comms: noComms, openOwnerItems: 0 });
    expect(close.current).toBe("close");
    expect(stageOf({ status: "CONCLUDED", verdict, actions: [], comms: noComms, openOwnerItems: 0 }).current).toBeNull();
  });
  it("derives one primary action per stage", () => {
    const review = primaryAction({ status: "ACTIVE", verdict, actions: [A({ pending: true }), A({ actionId: "b", pending: true })], comms: noComms, openOwnerItems: 0 }, stageOf({ status: "ACTIVE", verdict, actions: [A({ pending: true })], comms: noComms, openOwnerItems: 0 }), false);
    expect(review).toEqual({ kind: "review", count: 2 });
    const i = { status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" })], comms: [{ status: "awaiting_reply" } as CommsThread], openOwnerItems: 0 };
    expect(primaryAction(i, stageOf(i), false)).toEqual({ kind: "notify" });
    const j = { status: "ACTIVE", verdict, actions: [A({ status: "SUCCEEDED" })], comms: noComms, openOwnerItems: 0 };
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

describe("attackPath", () => {
  it("orders the cited entities by first event time, keeps the relationships among them, and is absent without a SUPPORTED hypothesis", async () => {
    const { attackPath, aroundEvent, bundleGroups } = await import("./attackPath");
    const ev = (id: string, time: string, od: string[], ents: string[], rels: { type: string; sourceRef: string; targetRef: string }[] = []) => ({
      id, classUid: 3002, className: "Authentication", time, recordedAt: time, sourceTool: "fixture", callId: "c1", verb: "v", coverage: "COMPLETE",
      observedDataRefs: od, entityRefs: ents, relationships: rels,
    });
    const events = [
      ev("e2", "2026-01-01T10:05:00Z", ["observed-data--2"], ["x-host--b", "user-account--u"], [{ type: "x-authenticated-to", sourceRef: "user-account--u", targetRef: "x-host--b" }]),
      ev("e1", "2026-01-01T10:00:00Z", ["observed-data--1"], ["x-host--a", "user-account--u"]),
      ev("e3", "2026-01-01T10:09:00Z", ["observed-data--3"], ["x-host--a"]),
    ];
    const hs = [{ id: "h1", statement: "s", status: "SUPPORTED", labels: [], predictions: [
      { id: "p1", statement: "p", title: "p", status: "CONFIRMED", discriminates: [], testResultRefs: ["observed-data--1", "observed-data--2"] },
      { id: "p2", statement: "q", title: "q", status: "DISCONFIRMED", discriminates: [], testResultRefs: ["observed-data--3"] },
    ] }];
    const path = attackPath(hs as never, events as never)!;
    expect(path.nodes.map((n) => n.ref)).toEqual(["x-host--a", "user-account--u", "x-host--b"]);
    expect(path.edges).toEqual([{ type: "x-authenticated-to", from: "user-account--u", to: "x-host--b" }]);
    expect(path.spanMs).toBe(5 * 60000);
    expect(path.citedBy).toEqual([{ predictionId: "p1", hypothesisId: "h1" }]);
    expect(attackPath([{ ...hs[0], status: "OPEN" }] as never, events as never)).toBeNull();
    // Around it: the same host, in time order, the anchor excluded.
    const around = aroundEvent(events.slice().sort((a, b) => a.time.localeCompare(b.time)) as never, "e3")!;
    expect(around.key).toBe("x-host--a");
    expect(around.before.map((e) => e.id)).toEqual(["e1"]);
    expect(around.after).toEqual([]);
    // The bundle: identical sentences collapse per source.
    const groups = bundleGroups(events as never);
    expect(groups.length).toBe(1);
    expect(groups[0].rows.length).toBe(1); // no view: all three read as "Authentication"
    expect(groups[0].rows[0].ids.length).toBe(3);
  });
});

describe("collapsePatterns", () => {
  it("collapses a key repeated on three days into one row at its first event and leaves the rest as events", async () => {
    const { collapsePatterns, patternSentence } = await import("./baseline");
    const ev = (id: string, time: string, pattern?: Record<string, string>) => ({
      id, classUid: 3002, className: "Authentication", time, recordedAt: time, sourceTool: "f", callId: "c", verb: "v", coverage: "COMPLETE",
      pattern, observedDataRefs: [], entityRefs: [], relationships: [],
    });
    const night = { logon_type: "network", source: "10.0.9.10", destination: "h" };
    const items = collapsePatterns([
      ev("x", "2026-01-04T14:00:00Z", { logon_type: "interactive", source: "10.0.4.20", destination: "h" }),
      ev("n3", "2026-01-03T02:00:00Z", night), ev("n1", "2026-01-01T02:00:00Z", night), ev("n2", "2026-01-02T03:00:00Z", night),
      ev("k", "2026-01-01T01:00:00Z"),
    ] as never);
    expect(items.map((i) => i.kind)).toEqual(["event", "pattern", "event"]);
    const p = items[1] as { kind: "pattern"; count: number; days: number; typicalHour: number; time: string; events: { id: string }[] };
    expect([p.count, p.days, p.typicalHour, p.time]).toEqual([3, 3, 2, "2026-01-01T02:00:00Z"]);
    expect(p.events.map((e) => e.id)).toEqual(["n1", "n2", "n3"]);
    expect(patternSentence(p)).toBe("3 events over 3 days, typically around 02:00 UTC");
    // Two days are not a pattern.
    expect(collapsePatterns([ev("a", "2026-01-01T02:00:00Z", night), ev("b", "2026-01-02T02:00:00Z", night), ev("c", "2026-01-02T05:00:00Z", night)] as never).every((i) => i.kind === "event")).toBe(true);
  });
});

