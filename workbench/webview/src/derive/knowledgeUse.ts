// What the agent was actually given of a procedure or past case in this
// investigation (#132): the recall results that carried it, read from the
// committed turns' transcripts. Nothing else counts as "read" — the library is
// not context (adr/0129). Pure.
import type { InvestigationSnapshot } from "../../../src/protocol";
import { unescapeT } from "../transcript";

export interface Received {
  interpretationId: string;
  summary: string;
  excerpt?: string;
  band?: string;
  score?: number;
  rationale?: string;
}

const RECALL = /^\[tool_result (recall_sops|recall_similar_investigations|prior_investigations) id=\S+ error=false\] (.*)$/;

/** Every turn whose recall results carried this item, with what it said. */
export function received(snap: InvestigationSnapshot | null, kind: "sop" | "case", ref: string): Received[] {
  if (!snap) return [];
  const bySeq = new Map((snap.thread || []).map((e) => [e.sequenceNo, e]));
  const out: Received[] = [];
  for (const t of snap.turns) {
    const entry = bySeq.get(t.sequenceNo);
    if (!entry) continue;
    for (const line of t.body.split("\n")) {
      const m = RECALL.exec(line);
      if (!m) continue;
      let body: { results?: Record<string, unknown>[]; investigations?: Record<string, unknown>[] };
      try { body = JSON.parse(unescapeT(m[2])); } catch { continue; }
      for (const r of [...(body.results || []), ...(body.investigations || [])]) {
        const id = kind === "sop" ? r.sop_id : r.investigation_ref;
        if (id !== ref) continue;
        out.push({
          interpretationId: entry.interpretationId, summary: entry.summary,
          excerpt: typeof r.excerpt === "string" ? r.excerpt : typeof r.body === "string" ? r.body : undefined,
          band: typeof r.band === "string" ? r.band : undefined,
          score: typeof r.score === "number" ? r.score : undefined,
          rationale: typeof r.match_rationale === "string" ? r.match_rationale : undefined,
        });
      }
    }
  }
  return out;
}
