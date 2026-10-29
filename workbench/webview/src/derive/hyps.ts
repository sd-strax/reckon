// The hypothesis tracker's derivations (02 §2.9, adr/0047, adr/0051). Pure
// functions over the server views; the components only render their output.
import type { Hypothesis, Prediction } from "../../../src/viewTypes";
import type { RivalBrief } from "../../../src/protocol";

export const isLive = (h: Hypothesis): boolean => h.status === "PROPOSED" || h.status === "OPEN";
/** WITHDRAWN predictions are out of the count: a discarded test, not a gap. */
export const counted = (p: Prediction): boolean => p.status !== "WITHDRAWN";
export const tested = (p: Prediction): boolean => counted(p) && p.status !== "UNTESTED";

/** A hypothesis is referred to by its handle (01 §x-hypothesis); a handle-less
 * one by its position (H1, H2 …) so every hypothesis has a stable short name. */
export function hypName(h: Hypothesis, all: Hypothesis[]): string {
  if (h.handle) return h.handle;
  const i = all.findIndex((x) => x.id === h.id);
  return "H" + (i >= 0 ? i + 1 : "?");
}

export function nameOf(id: string, all: Hypothesis[]): string {
  const r = all.find((x) => x.id === id);
  return r ? hypName(r, all) : id;
}

/** A prediction's short name (adr/0047): its ordinal from the server view. */
export function predName(p: Prediction): string {
  return p.ordinal ? "P" + p.ordinal : "this prediction";
}

/** The rivals brief a scoped ⚡ turn carries: every OTHER live hypothesis, with
 * how many of its untested predictions discriminate against the one in view. */
export function rivalsOf(h: Hypothesis, all: Hypothesis[]): RivalBrief[] {
  return all.filter((r) => r.id !== h.id && isLive(r)).map((r) => ({
    id: r.id, handle: r.handle, statement: r.statement,
    untestedDiscriminators: (r.predictions || []).filter((p) =>
      p.status === "UNTESTED" && (p.discriminates || []).includes(h.id)).length,
  }));
}

/** The untested discriminators standing between h and a decision: predictions a
 * LIVE RIVAL holds that come out the opposite way under h (01 §x-hypothesis). */
export function blockersOf(h: Hypothesis, all: Hypothesis[]): { p: Prediction; owner: Hypothesis }[] {
  const out: { p: Prediction; owner: Hypothesis }[] = [];
  for (const o of all) {
    if (o.id === h.id || !isLive(o)) continue;
    for (const p of o.predictions || []) {
      if (p.status === "UNTESTED" && (p.discriminates || []).includes(h.id)) out.push({ p, owner: o });
    }
  }
  return out;
}

/** Live hypotheses first; otherwise record order. */
export function orderHypotheses(hs: Hypothesis[]): Hypothesis[] {
  return hs.slice().sort((a, b) => (isLive(a) ? 0 : 1) - (isLive(b) ? 0 : 1));
}

/** The tone a status word takes (01 §Semantic): confirmed things read green,
 * refuted red, everything undecided stays neutral. */
const BADGE_TONE: Record<string, string> = {
  CONFIRMED: "ok", SUPPORTED: "ok", TESTED: "ok", SUCCEEDED: "ok",
  REFUTED: "bad", FAILED: "bad", EXPIRED: "warn",
  DISCONFIRMED: "bad", WITHDRAWN: "muted", ABANDONED: "muted",
  // comms thread states (ui/04 §4.2)
  AWAITING_REPLY: "warn", REPLIED: "ok", FOLLOWED_UP: "warn",
};
export function badgeTone(status: string | undefined): string {
  return BADGE_TONE[String(status || "").toUpperCase()] || "";
}
