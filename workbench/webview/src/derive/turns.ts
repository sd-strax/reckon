// Turn grouping for the companion column (08 §8.5, impl §7.3): walk the thread
// by sequence; a transcript-bearing entry is a turn; the AI-delegated entries
// without a transcript recorded since the previous turn are that turn's
// records, each rendered as one link line. Human acts are not records of a
// turn: they live in the Thread mode and on their objects. Pure.
import type { Hypothesis, ThreadEntry } from "../../../src/backend";
import type { ChronicleTurn } from "../../../src/protocol";
import { hypName } from "./hyps";

export interface TurnRecord {
  /** The line: "H1·P5 confirmed", "H2 refuted", "recorded P3, P4 under H1", "proposed H1, H2", "Proposed 3 actions", "pinned …". */
  text: string;
  /** Where the link goes: a ref opens the inspector; a lens scrolls there. */
  target: { kind: "ref"; ref: string } | { kind: "lens"; lens: "reasoning" | "response" } | null;
  entry: ThreadEntry;
  /** "ok" for confirmed/supported, "bad" for disconfirmed/refuted, "" otherwise. */
  tone: string;
}

export interface Turn {
  entry: ThreadEntry;
  transcript: ChronicleTurn | null;
  records: TurnRecord[];
  /** The ⚡ scoped turns render collapsed (02 §2.9). */
  aside: boolean;
  /** A context reset precedes this turn (the agent re-grounded from the record). */
  resetBefore: ThreadEntry | null;
}

const RECORD_TYPES = new Set(["hypothesis", "prediction", "support", "refutation", "inconclusive", "action-request", "evidence-pin", "note"]);

function predOf(hs: Hypothesis[], id: string): { h: Hypothesis; ordinal?: number; status: string } | null {
  for (const h of hs) {
    const p = h.predictions.find((x) => x.id === id);
    if (p) return { h, ordinal: p.ordinal, status: p.status };
  }
  return null;
}

/** One record line for an AI-delegated act without a transcript. */
export function recordLine(e: ThreadEntry, hs: Hypothesis[]): TurnRecord {
  const outs = e.outputRefs || [];
  const ins = e.inputRefs || [];
  switch (e.interpretationType) {
    case "hypothesis": {
      const names = outs.map((r) => { const h = hs.find((x) => x.id === r); return h ? hypName(h, hs) : ""; }).filter(Boolean);
      return { text: names.length ? "proposed " + names.join(", ") : "proposed a hypothesis", target: outs[0] ? { kind: "ref", ref: outs[0] } : { kind: "lens", lens: "reasoning" }, entry: e, tone: "" };
    }
    case "prediction": {
      const found = outs.map((r) => predOf(hs, r)).filter((x): x is NonNullable<typeof x> => !!x);
      const owner = found[0]?.h;
      const names = found.map((p) => p.ordinal ? "P" + p.ordinal : "a prediction");
      return { text: names.length ? `recorded ${names.join(", ")}${owner ? " under " + hypName(owner, hs) : ""}` : e.summary || "recorded a prediction", target: outs[0] ? { kind: "ref", ref: outs[0] } : { kind: "lens", lens: "reasoning" }, entry: e, tone: "" };
    }
    case "support": case "refutation": case "inconclusive": {
      // The hypothesis decided is the output; a prediction outcome cites the prediction as input.
      const h = hs.find((x) => outs.includes(x.id)) || hs.find((x) => ins.includes(x.id));
      const pred = [...outs, ...ins].map((r) => predOf(hs, r)).find((x) => x);
      const word = e.interpretationType === "support" ? "supported" : e.interpretationType === "refutation" ? "refuted" : "inconclusive";
      if (pred && pred.h && !outs.includes(pred.h.id)) {
        const pw = pred.status === "CONFIRMED" ? "confirmed" : pred.status === "DISCONFIRMED" ? "disconfirmed" : pred.status.toLowerCase();
        const attested = (e.inputRefs || []).some((r) => r.startsWith("note--"));
        return { text: `${hypName(pred.h, hs)}·P${pred.ordinal ?? "?"} ${pw}${attested ? " by attestation" : ""}`, target: { kind: "ref", ref: [...outs, ...ins].find((r) => r.startsWith("x-prediction--")) || outs[0] }, entry: e, tone: pred.status === "CONFIRMED" ? "ok" : pred.status === "DISCONFIRMED" ? "bad" : "" };
      }
      return { text: h ? `${hypName(h, hs)} ${word}` : e.summary || word, target: h ? { kind: "ref", ref: h.id } : { kind: "lens", lens: "reasoning" }, entry: e, tone: word === "supported" ? "ok" : word === "refuted" ? "bad" : "" };
    }
    case "action-request":
      return { text: e.summary || "proposed an action", target: { kind: "lens", lens: "response" }, entry: e, tone: "" };
    case "evidence-pin":
      return { text: "pinned: " + (e.summary || "evidence"), target: { kind: "lens", lens: "reasoning" }, entry: e, tone: "" };
    default:
      return { text: e.summary || e.interpretationType, target: outs[0] ? { kind: "ref", ref: outs[0] } : null, entry: e, tone: "" };
  }
}

export function groupTurns(thread: ThreadEntry[], turns: ChronicleTurn[], hs: Hypothesis[]): Turn[] {
  const bodies = new Map(turns.map((t) => [t.sequenceNo, t]));
  const entries = thread.slice().sort((a, b) => a.sequenceNo - b.sequenceNo);
  const out: Turn[] = [];
  let pendingRecords: TurnRecord[] = [];
  let pendingReset: ThreadEntry | null = null;
  for (const e of entries) {
    if (e.summary && e.summary.indexOf("context reset:") === 0) { pendingReset = e; continue; }
    if (e.hasTranscript) {
      const transcript = bodies.get(e.sequenceNo) || null;
      const aside = !!transcript && /^\[user\] ⚡ /.test(transcript.body);
      const turn: Turn = { entry: e, transcript, records: [], aside, resetBefore: pendingReset };
      pendingReset = null;
      out.push(turn);
      // Records before the first turn (none expected) attach to it.
      if (pendingRecords.length) { turn.records.push(...pendingRecords); pendingRecords = []; }
      continue;
    }
    if (e.actor.kind === "AI_DELEGATED" && RECORD_TYPES.has(e.interpretationType)) {
      const rec = recordLine(e, hs);
      const last = out[out.length - 1];
      if (last) last.records.push(rec); else pendingRecords.push(rec);
    }
  }
  // Collapse a turn's action-requests into one line: "Proposed N actions".
  for (const t of out) {
    const reqs = t.records.filter((r) => r.entry.interpretationType === "action-request");
    if (reqs.length > 1) {
      t.records = t.records.filter((r) => r.entry.interpretationType !== "action-request");
      t.records.push({ text: `Proposed ${reqs.length} actions`, target: { kind: "lens", lens: "response" }, entry: reqs[0].entry, tone: "" });
    }
  }
  return out;
}
