// What the agent read of a procedure in this investigation, as it was
// (adr/0097). Every act that consulted a procedure recorded the revision, the
// cut and the section indexes it had in front of it (adr/0303); the library
// keeps every revision and can recut any of them under any cut it ever made.
// So the record's three numbers name text, and this reads that text back,
// beside what the current revision holds under the same heading, for the
// procedure inspector to show "exactly as retrieved" or "revised since".
import type { ProcedureReads, ProcedureSectionRead, ProcedureTurnRead, SOPOutlineView, SOPSectionView, ThreadEntry } from "./viewTypes";

/** The three revision reads of the knowledge client, for the composition and its tests. */
export interface ProcedureReader {
  revisions(id: string): Promise<{ current: number }>;
  revisionOutline(id: string, revision: number, cut?: string): Promise<SOPOutlineView>;
  revisionSection(id: string, revision: number, idx: number, cut?: string): Promise<SOPSectionView>;
}

type Consulted = ThreadEntry["consultedSops"][number];

/**
 * Whether a consulted entry names text: a revision, a cut and at least one
 * section. An index without its cut cannot be read back honestly (a re-cut
 * renumbers), so such an entry counts as not recorded, as one from before
 * sections existed does.
 */
export function recordedSections(s: Consulted): s is Consulted & { revision: number; cut: string; sections: number[] } {
  return !!s.revision && !!s.cut && !!s.sections?.length;
}

/**
 * Reads back every section the thread's acts recorded of one procedure.
 * Undefined when no act recorded sections: there is nothing to read back and
 * the inspector shows what the recall results carried. A section read once
 * is read once however many acts recorded it, and an outline once per
 * revision and cut. A counterpart is looked up by heading path, never by
 * index, since an edit or a re-cut renumbers; see counterpart for how parts
 * and repeated headings are matched. Any failed read fails the whole: the
 * pane then says the reads could not be made, rather than showing some.
 */
export async function procedureReads(reader: ProcedureReader, sopId: string, thread: ThreadEntry[]): Promise<ProcedureReads | undefined> {
  const recorded = thread.flatMap((e) => (e.consultedSops || [])
    .filter((s) => s.sopId === sopId && recordedSections(s))
    .map((s) => ({ interpretationId: e.interpretationId, revision: s.revision as number, cut: s.cut as string, sections: s.sections as number[] })));
  if (!recorded.length) return undefined;

  const { current } = await reader.revisions(sopId);
  const sections = new Map<string, Promise<SOPSectionView>>();
  const section = (revision: number, cut: string, idx: number) => {
    const key = `${revision}/${cut}/${idx}`;
    let p = sections.get(key);
    if (!p) {
      p = reader.revisionSection(sopId, revision, idx, cut || undefined);
      sections.set(key, p);
    }
    return p;
  };
  const outlines = new Map<string, Promise<SOPOutlineView>>();
  const outline = (revision: number, cut: string) => {
    const key = `${revision}/${cut}`;
    let p = outlines.get(key);
    if (!p) {
      p = reader.revisionOutline(sopId, revision, cut || undefined);
      outlines.set(key, p);
    }
    return p;
  };

  const turns: ProcedureTurnRead[] = [];
  for (const r of recorded) {
    const reads: ProcedureSectionRead[] = [];
    for (const idx of r.sections) {
      const then = await section(r.revision, r.cut, idx);
      const read: ProcedureSectionRead = { idx: then.idx, location: then.location, text: then.text };
      if (r.revision !== current) {
        const [thenOutline, now] = await Promise.all([outline(r.revision, r.cut), outline(current, "")]);
        const thenEntry = thenOutline.sections.find((o) => o.idx === then.idx) ?? { idx: then.idx, location: then.location, headingPath: then.headingPath, part: then.part, parts: then.parts };
        const match = counterpart(thenEntry, thenOutline, now);
        if (!match.length) {
          read.current = null;
        } else {
          const texts = await Promise.all(match.map((m) => section(current, now.cut, m.idx)));
          const first = texts[0] as SOPSectionView;
          read.current = {
            revision: current,
            idx: first.idx,
            location: texts.length === 1 ? first.location : first.headingPath.join(" > ") || first.location,
            text: texts.map((t) => t.text).join("\n\n"),
          };
        }
      }
      reads.push(read);
    }
    turns.push({ interpretationId: r.interpretationId, revision: r.revision, cut: r.cut, sections: reads });
  }
  return { currentRevision: current, turns };
}

type OutlineSection = SOPOutlineView["sections"][number];

/**
 * The sections of an outline that stand under one heading path, grouped
 * into occurrences: a heading the document repeats (two "Notes" under the
 * same parent) is several occurrences, and a heading cut into parts (a long
 * section under the budget) is one occurrence of several sections. Parts are
 * consecutive and numbered from 1, so a section that is not a part, or is
 * part 1, opens an occurrence.
 */
function occurrences(sections: OutlineSection[], path: string): OutlineSection[][] {
  const out: OutlineSection[][] = [];
  for (const s of sections) {
    if (s.headingPath.join("\u0000") !== path) continue;
    const last = out[out.length - 1];
    if (last && (s.parts ?? 0) > 1 && (s.part ?? 0) > 1 && last[0]?.parts === s.parts) last.push(s);
    else out.push([s]);
  }
  return out;
}

/**
 * The current revision's sections that stand where a retrieved section stood.
 * The retrieved section's heading path is matched, never its index; among
 * repeated headings the same occurrence by order is taken, and a missing
 * occurrence is gone, never its first namesake. Within the occurrence: the
 * same part when the retrieved section was a part and the current side is
 * cut the same way, otherwise every section of the occurrence together, so
 * the pane never says a heading is gone because its parts moved. Empty when
 * the heading, or that occurrence of it, is gone.
 */
function counterpart(then: OutlineSection, thenOutline: SOPOutlineView, now: SOPOutlineView): OutlineSection[] {
  const path = then.headingPath.join("\u0000");
  const ordinal = occurrences(thenOutline.sections, path).findIndex((o) => o.some((s) => s.idx === then.idx));
  const occurrence = occurrences(now.sections, path)[ordinal];
  if (ordinal < 0 || !occurrence) return [];
  if ((then.parts ?? 0) > 1) {
    const exact = occurrence.find((s) => s.part === then.part && s.parts === then.parts);
    if (exact) return [exact];
  }
  return occurrence;
}
