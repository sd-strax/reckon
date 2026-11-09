// How much of a procedure's section actually reached the model (adr/0097).
// The record names the section (revision, cut, index); the committed tool
// results say what of it was delivered: a recall hit carries the section as
// an excerpt cut to a budget (`truncated` when it was), a read_sop result
// carries the section's text whole or, when the section is larger than one
// result page, as `text_parts` the model reads page by page through
// read_result, each page credited to the section. A recall result over the
// page size is paged the same way, its later pages keeping the recall shape
// (`results`), and the agent credits the hits on them too. This reads those
// results out of the committed turns, so the inspector can say "received
// whole" or how much was received, rather than presenting the stored
// section as what the agent saw. Pure.
import type { InvestigationSnapshot } from "../../../src/protocol";
import { unescapeT } from "../transcript";

export interface Delivery {
  /** The text that reached the model, in order; for parts, the parts received joined. */
  text: string;
  /** Every rune of the section reached the model. */
  whole: boolean;
  via: "recall" | "read_sop";
  /** For a paged read: how many of the section's parts the turn received. */
  parts?: { got: number; of: number };
}

const RESULT = /^\[tool_result (recall_sops|read_sop|read_result) id=\S+ error=false\] (.*)$/;

/** The key a delivery is kept under: the turn and the record's tuple. */
export function deliveryKey(interpretationId: string, revision: number, cut: string, idx: number): string {
  return `${interpretationId}/${revision}/${cut}/${idx}`;
}

interface RecallResult { results?: { sop_id?: unknown; revision?: unknown; cut?: unknown; excerpt?: unknown; truncated?: unknown; section?: { idx?: unknown } }[] }
interface ReadResult {
  sop_id?: unknown; revision?: unknown; cut?: unknown;
  section?: { idx?: unknown; text?: unknown };
  text_parts?: unknown[];
  result_bounded?: { result_id?: unknown; offset?: unknown; lists?: { text_parts?: { total?: unknown } } };
}

/** What each turn delivered of each recorded section of one procedure, by deliveryKey. */
export function delivered(snap: InvestigationSnapshot | null, sopId: string): Map<string, Delivery> {
  const out = new Map<string, Delivery>();
  if (!snap) return out;
  const bySeq = new Map((snap.thread || []).map((e) => [e.sequenceNo, e]));
  // A paged section: the parts received, by position, per turn and held result.
  const paged = new Map<string, { key: string; got: Map<number, string>; of: number }>();
  const better = (key: string, d: Delivery) => {
    const have = out.get(key);
    if (!have || (d.whole && !have.whole) || (d.whole === have.whole && d.text.length > have.text.length)) out.set(key, d);
  };
  for (const t of snap.turns) {
    const entry = bySeq.get(t.sequenceNo);
    if (!entry) continue;
    for (const line of t.body.split("\n")) {
      const m = RESULT.exec(line);
      if (!m) continue;
      let body: unknown;
      try { body = JSON.parse(unescapeT(m[2])); } catch { continue; }
      if (typeof body !== "object" || body === null) continue;
      // A recall result, first page or a later one through read_result: the
      // shape says which, since a read_result page keeps its source's shape.
      if (m[1] === "recall_sops" || Array.isArray((body as RecallResult).results)) {
        for (const r of (body as RecallResult).results || []) {
          if (r.sop_id !== sopId || typeof r.revision !== "number" || typeof r.cut !== "string" || typeof r.section?.idx !== "number") continue;
          better(deliveryKey(entry.interpretationId, r.revision, r.cut, r.section.idx), {
            text: typeof r.excerpt === "string" ? r.excerpt : "", whole: r.truncated !== true, via: "recall",
          });
        }
        continue;
      }
      const r = body as ReadResult;
      if (r.sop_id !== sopId || typeof r.revision !== "number" || typeof r.cut !== "string" || typeof r.section?.idx !== "number") continue;
      const key = deliveryKey(entry.interpretationId, r.revision, r.cut, r.section.idx);
      if (typeof r.section.text === "string") {
        better(key, { text: r.section.text, whole: true, via: "read_sop" });
        continue;
      }
      if (!Array.isArray(r.text_parts)) continue;
      const note = r.result_bounded;
      const offset = typeof note?.offset === "number" ? note.offset : 0;
      const total = typeof note?.lists?.text_parts?.total === "number" ? note.lists.text_parts.total : r.text_parts.length;
      const id = `${entry.interpretationId}/${typeof note?.result_id === "string" ? note.result_id : m[0]}`;
      let p = paged.get(id);
      if (!p) {
        p = { key, got: new Map(), of: total };
        paged.set(id, p);
      }
      r.text_parts.forEach((part, i) => { if (typeof part === "string") p.got.set(offset + i, part); });
    }
  }
  for (const p of paged.values()) {
    const positions = [...p.got.keys()].sort((a, b) => a - b);
    const text = positions.map((i) => p.got.get(i) ?? "").join("");
    better(p.key, { text, whole: p.got.size >= p.of, via: "read_sop", parts: { got: p.got.size, of: p.of } });
  }
  return out;
}
