// A new investigation's first step (#160): until anything has happened in it —
// no agent turn, hypothesis, pin, action or verdict — the analyst sees what it
// is rooted on and one way to start, not empty lenses that read as a failed
// create. Pure.
import type { InvestigationSnapshot } from "../../../src/protocol";

export function isFresh(snap: InvestigationSnapshot | null): boolean {
  if (!snap) return false;
  const state = String(snap.investigation.state).toUpperCase();
  if (state !== "DRAFT" && state !== "ACTIVE") return false;
  const acted = (snap.thread || []).some((e) => e.hasTranscript || (e.interpretationType !== "lifecycle" && e.interpretationType !== "note"));
  return !acted && !snap.hypotheses.length && !snap.pins.length && !(snap.actions || []).length && !snap.investigation.verdict;
}

/** What the investigation is rooted on, in words, and the request staged to start it. */
export function firstStep(snap: InvestigationSnapshot): { rootedOn: string; ask: string } {
  const seed = snap.investigation.seed;
  if (seed?.type === "case" && seed.caseId) {
    return { rootedOn: `case ${seed.caseId}`, ask: `Investigate case ${seed.caseId}: read the case, pull the telemetry it points at, and tell me what happened.` };
  }
  if (seed?.type === "alert" && seed.alertId) {
    return { rootedOn: `alert ${seed.alertId}`, ask: `Investigate alert ${seed.alertId}: what fired, on what, and what happened around it?` };
  }
  if (seed?.type === "question" && seed.hypothesisStatement) {
    return { rootedOn: `the question “${seed.hypothesisStatement}”`, ask: `Hunt for this: ${seed.hypothesisStatement}` };
  }
  const what = seed?.entityIdentifier || snap.investigation.title;
  return { rootedOn: what, ask: `Investigate ${what}: pull the logon, process and network telemetry and tell me what happened.` };
}
