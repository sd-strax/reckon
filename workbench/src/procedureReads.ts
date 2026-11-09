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
 * is read once however many acts recorded it; the current revision's
 * outline is read once. A counterpart is looked up by heading path, never by
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
  let outline: Promise<SOPOutlineView> | undefined;
  const currentOutline = () => (outline ??= reader.revisionOutline(sopId, current));

  const turns: ProcedureTurnRead[] = [];
  for (const r of recorded) {
    const reads: ProcedureSectionRead[] = [];
    for (const idx of r.sections) {
      const then = await section(r.revision, r.cut, idx);
      const read: ProcedureSectionRead = { idx: then.idx, location: then.location, text: then.text };
      if (r.revision !== current) {
        const now = await currentOutline();
        const match = counterpart(then, now);
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
 * The current revision's sections that stand where a retrieved section stood:
 * those under the same heading path. A heading cut into parts (a long section
 * under the budget) is one heading: when the retrieved section was part k of
 * n and the current side has the same part k of n it is that one, otherwise
 * every current part under the heading together, so the pane never says a
 * heading is gone because its parts moved. A heading the document repeats
 * (two "Notes" under the same parent) is told apart by its order among its
 * namesakes in each revision's outline. Empty when the heading is gone.
 */
function counterpart(then: SOPSectionView, now: SOPOutlineView): OutlineSection[] {
  const path = then.headingPath.join("\u0000");
  const group = now.sections.filter((s) => s.headingPath.join("\u0000") === path);
  if (!group.length) return [];
  const cut = (s: { parts?: number }) => (s.parts ?? 0) > 1;
  if (!cut(then) && !group.some(cut)) {
    // Whole headings on both sides: the same ordinal among namesakes, as
    // locations repeat only for repeated headings here.
    const ordinal = then.outline.slice(0, then.idx - 1).filter((l) => l === then.location).length;
    const same = group[ordinal] ?? group[0];
    return same ? [same] : [];
  }
  if (cut(then)) {
    const exact = group.find((s) => s.part === then.part && s.parts === then.parts);
    if (exact) return [exact];
  }
  return group;
}
