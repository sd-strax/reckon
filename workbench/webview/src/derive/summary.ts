// The Summary lens's facts (08 §8.4, impl §9): key entities with role and the
// derivable status words, still-to-do, procedures followed, the handoff note.
// Status words come only from the verdict and the actions — never invented.
import type { ActionRow, Hypothesis, PinRow, ThreadEntry, Seed, CommsThread, Verdict } from "../../../src/viewTypes";
import { isEntityRef } from "../refs";
import { terminal, isNotify } from "./stage";
import { hypName } from "./hyps";

export interface KeyEntity {
  ref: string;
  role: string;
  /** Derivable status words: "isolation requested", "isolated", "disabled", "blocked", "". */
  status: string;
  citations: number;
}

const ACTION_WORDS: Record<string, [string, string]> = {
  "host.isolate": ["isolation requested", "isolated"],
  "host.unisolate": ["un-isolation requested", "isolation lifted"],
  "account.disable": ["disable requested", "disabled"],
  "account.enable": ["enable requested", "enabled"],
  "account.reset_credentials": ["reset requested", "credentials reset"],
  "ioc.block": ["block requested", "blocked"],
  "process.kill": ["kill requested", "killed"],
};

export function keyEntities(seed: Seed | undefined, thread: ThreadEntry[], pins: PinRow[], actions: ActionRow[], labels: Record<string, string>, observed: { ref: string; citations: number }[] = []): KeyEntity[] {
  const counts = new Map<string, number>();
  const bump = (r: string, n = 1) => { if (isEntityRef(r)) counts.set(r, (counts.get(r) || 0) + n); };
  for (const e of thread) for (const r of [...(e.inputRefs || []), ...(e.outputRefs || [])]) bump(r);
  for (const p of pins) if (!p.superseded) for (const r of p.inputRefs || []) bump(r);
  // The hosts, accounts and addresses the cited evidence observed (#141): the
  // agent cites observations, rarely the entities themselves.
  for (const o of observed) bump(o.ref, o.citations);
  if (seed?.entityRef) counts.set(seed.entityRef, (counts.get(seed.entityRef) || 0) + 1000);
  const out: KeyEntity[] = [];
  for (const [ref, n] of counts) {
    const label = labels[ref];
    const onIt = actions.filter((a) => (a.targets || []).includes(label || "\u0000") || (a.targets || []).includes(ref));
    let status = "";
    for (const a of onIt) {
      const words = ACTION_WORDS[a.actionType];
      if (!words) continue;
      if (a.status === "SUCCEEDED") status = words[1];
      else if (!terminal(a) && !status) status = words[0];
    }
    const role = seed?.entityRef === ref ? "seed subject" : onIt.length ? "action target" : "cited";
    out.push({ ref, role, status, citations: n >= 1000 ? n - 1000 : n });
  }
  out.sort((a, b) => (a.role === "seed subject" ? -1 : b.role === "seed subject" ? 1 : b.citations - a.citations));
  return out.slice(0, 8);
}

export interface TodoItem {
  text: string;
  kind: "approve" | "notify" | "prediction" | "comms";
  expiresAt?: string;
  ref?: string;
}

export function stillToDo(actions: ActionRow[], comms: CommsThread[], hs: Hypothesis[], labels: Record<string, string>): TodoItem[] {
  const out: TodoItem[] = [];
  for (const a of actions) {
    if (a.pending && !a.expired) out.push({ text: `Approve ${a.actionType} on ${a.targets.join(", ")}`, kind: "approve", expiresAt: a.expiresAt });
  }
  const notifyOpen = actions.filter((a) => isNotify(a) && !terminal(a));
  for (const t of comms) {
    if (t.status === "awaiting_reply" || t.status === "followed_up") out.push({ text: `Awaiting a reply from ${t.target}`, kind: "comms" });
  }
  if (!actions.some(isNotify) && !notifyOpen.length && actions.length) out.push({ text: "Send the stakeholder notification", kind: "notify" });
  for (const h of hs) {
    if (h.status === "ABANDONED") continue;
    for (const p of h.predictions) {
      if (p.status === "UNTESTED" || p.status === "INCONCLUSIVE") out.push({ text: `${p.status === "UNTESTED" ? "Test" : "Resolve"} P${p.ordinal ?? "?"} under ${hypName(h, hs)}`, kind: "prediction", ref: p.id });
    }
  }
  void labels;
  return out;
}

export function proceduresFollowed(thread: ThreadEntry[]): { sopId: string; title: string }[] {
  const seen = new Map<string, string>();
  for (const e of thread) for (const s of e.consultedSops || []) if (s.used && !seen.has(s.sopId)) seen.set(s.sopId, s.title || s.sopId);
  return [...seen].map(([sopId, title]) => ({ sopId, title }));
}

/** The handoff note (08 §8.4 Summary): the facts, as text, for the clipboard. */
export function handoffNote(title: string, verdict: Verdict | null | undefined, actions: ActionRow[], todo: TodoItem[], hs: Hypothesis[], followed: { title: string }[]): string {
  const lines: string[] = [title, ""];
  lines.push(verdict ? `Verdict: ${verdict.disposition}${verdict.rationale ? " — " + verdict.rationale : ""}` : "Verdict: none yet");
  const live = hs.filter((h) => h.status !== "ABANDONED");
  if (live.length) lines.push("Hypotheses: " + live.map((h) => `${hypName(h, hs)} ${h.status}`).join("; "));
  if (actions.length) {
    lines.push("Actions:");
    for (const a of actions) lines.push(`- ${a.actionType} → ${a.targets.join(", ")}: ${a.pending ? a.pendingLabel : a.status}${a.resultRef ? " (" + a.resultRef + ")" : ""}`);
  }
  if (followed.length) lines.push("Procedures followed: " + followed.map((f) => f.title).join(", "));
  if (todo.length) {
    lines.push("Still to do:");
    for (const t of todo) lines.push(`- ${t.text}`);
  }
  return lines.join("\n");
}
