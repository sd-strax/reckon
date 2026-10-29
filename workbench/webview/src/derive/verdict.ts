// The verdict dialog's preflight + residual (02 §2.10): the engine's gates
// rendered BEFORE the attempt, and what the analyst is signing over. Pure.
import type { Capability, Hypothesis, PinRow, ThreadEntry, Verdict } from "../../../src/viewTypes";
import type { ChronicleTurn } from "../../../src/protocol";
import { parseTranscript } from "../transcript";
import { hypName, isLive, outstanding } from "./hyps";

export interface ChecklistItem { ok: boolean; text: string }

export function verdictChecklist(pins: PinRow[], verdict: Verdict | null | undefined): ChecklistItem[] {
  const active = pins.filter((p) => !p.superseded);
  // Only what is true (#143): the citation line appears once there is something to cite.
  const items: ChecklistItem[] = active.length > 0
    ? [{ ok: true, text: `${active.length} pinned evidence item${active.length === 1 ? "" : "s"} — the verdict cites ${active.length === 1 ? "it" : "them"}` }]
    : [{ ok: false, text: "no pinned evidence — pin the key findings first (the engine will refuse)" }];
  if (verdict) items.push({ ok: true, text: "revises the current verdict (" + verdict.disposition + ") — history is preserved" });
  return items;
}

/** Verbs that describe the tenant rather than investigate the case. */
const NOT_A_CHECK = new Set(["list_capabilities"]);

/** The checks this investigation asked for that no integration in the tenant
 * could answer, read from its committed turns (a tool result whose coverage is
 * UNAVAILABLE_*). Verbs nobody tried do not bear on this case. */
export function uncheckable(caps: Capability[] | null, turns: ChronicleTurn[]): { verb: string; what: string }[] {
  const tried = new Set<string>();
  for (const t of turns) {
    for (const it of parseTranscript(t.body).items) {
      if (it.kind === "tool" && it.result?.detail.startsWith("UNAVAILABLE") && !NOT_A_CHECK.has(it.name)) tried.add(it.name);
    }
  }
  return [...tried].map((verb) => {
    const intent = (caps || []).find((c) => c.verb === verb)?.intent || "";
    const first = intent.split(/(?<=\.)\s/)[0].replace(/\.$/, "");
    return { verb, what: first || verb };
  });
}

/**
 * What the analyst signs over (#143), for this case and in plain words: the
 * hypotheses still undecided, the predictions still to test under them, and
 * the checks the investigation asked for that the tenant could not answer.
 */
export function verdictResidual(caps: Capability[] | null, hyps: Hypothesis[], turns: ChronicleTurn[] = []): string[] {
  const lines: string[] = [];
  const untested: string[] = [];
  for (const h of hyps) {
    if (!isLive(h)) continue;
    const disc = (h.predictions || []).filter((p) => p.status === "UNTESTED" && (p.discriminates || []).length).length;
    lines.push(`${hypName(h, hyps)} — "${h.statement}" is ${h.status === "PROPOSED" ? "proposed and never adopted" : "still open"}; this verdict leaves it undecided`
      + (disc ? ` (${disc} untested test${disc === 1 ? "" : "s"} would tell it apart from a rival)` : "")
      + ". Abandon it on its card if you are not pursuing it.");
    for (const p of h.predictions || []) if (outstanding(p, h)) untested.push(p.statement);
  }
  for (const s of untested.slice(0, 5)) lines.push("Not tested: " + s);
  if (untested.length > 5) lines.push(`… and ${untested.length - 5} more untested predictions`);
  for (const u of uncheckable(caps, turns)) lines.push(`Could not check: ${u.what} — no integration in this tenant answers it.`);
  if (!lines.length) lines.push("Nothing known left open: every hypothesis is decided and every check this investigation asked for was answered.");
  return lines;
}

/** The refs a verdict cites: every active pin's refs, deduplicated. */
export function verdictRefs(pins: PinRow[]): string[] {
  const refs: string[] = [];
  for (const p of pins.filter((x) => !x.superseded)) {
    for (const r of p.inputRefs || []) if (!refs.includes(r)) refs.push(r);
  }
  return refs;
}

/** How verdicts in the thread relate (#155): each replaced verdict and the one
 * that replaced it (the next verdict in order), in both directions. */
export interface VerdictLink { replacedBy?: ThreadEntry; replaces?: ThreadEntry }

export function verdictLinks(thread: ThreadEntry[]): Map<string, VerdictLink> {
  const verdicts = thread.filter((e) => e.interpretationType === "verdict").sort((a, b) => a.sequenceNo - b.sequenceNo);
  const out = new Map<string, VerdictLink>();
  verdicts.forEach((v, i) => {
    const next = verdicts[i + 1];
    if (!v.superseded || !next) return;
    out.set(v.interpretationId, { ...out.get(v.interpretationId), replacedBy: next });
    out.set(next.interpretationId, { ...out.get(next.interpretationId), replaces: v });
  });
  return out;
}

/** The thread line's id for an act, so a link can scroll to it. */
export const actLineId = (interpretationId: string): string => "act-" + interpretationId;
