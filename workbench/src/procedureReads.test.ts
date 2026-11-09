import { describe, expect, it } from "vitest";
import { procedureReads, type ProcedureReader } from "./procedureReads";
import type { SOPOutlineView, SOPSectionView, ThreadEntry } from "./viewTypes";

const entry = (interpretationId: string, consulted: ThreadEntry["consultedSops"]): ThreadEntry => ({
  sequenceNo: 1, occurredAt: "2026-01-01T00:00:00Z", actor: { principal: "agent", kind: "AI_DELEGATED" }, interpretationId,
  interpretationType: "observation", summary: "", inputRefs: [], outputRefs: [], toolCalls: 0, hasTranscript: true, superseded: false,
  consultedSops: consulted, consultedSimilar: [],
});

/** A library at revision 2: revision 1 had Triage and Containment; revision 2 renamed Containment to Response. */
function library(current = 2): { reader: ProcedureReader; calls: string[] } {
  const calls: string[] = [];
  const texts: Record<string, string> = {
    "1/1/16000/1": "# RDP\n\nScope.", "1/1/16000/2": "## Triage\nConfirm the session.", "1/1/16000/3": "## Containment\nIsolate.",
    "2/1/16000/1": "# RDP\n\nScope.", "2/1/16000/2": "## Triage\nConfirm the session with the owner.", "2/1/16000/3": "## Response\nIsolate, then collect.",
  };
  const locations: Record<number, string[]> = { 1: ["RDP", "RDP > Triage", "RDP > Containment"], 2: ["RDP", "RDP > Triage", "RDP > Response"] };
  const at = (revision: number, idx: number): string => locations[revision]?.[idx - 1] ?? "";
  const reader: ProcedureReader = {
    async revisions(id) { calls.push(`revisions ${id}`); return { current }; },
    async revisionOutline(_id, revision, cut): Promise<SOPOutlineView> {
      calls.push(`outline ${revision} ${cut ?? "-"}`);
      return { revision, cut: "1/16000", sections: (locations[revision] ?? []).map((location, i) => ({ idx: i + 1, location, headingPath: location.split(" > ") })) };
    },
    async revisionSection(_id, revision, idx, cut): Promise<SOPSectionView> {
      calls.push(`section ${revision} ${idx} ${cut ?? "-"}`);
      const text = texts[`${revision}/${cut ?? "1/16000"}/${idx}`];
      if (!text) throw new Error("404");
      return { revision, cut: cut ?? "1/16000", idx, of: 3, location: at(revision, idx), headingPath: at(revision, idx).split(" > "), text, outline: locations[revision] ?? [] };
    },
  };
  return { reader, calls };
}

describe("what the agent read of a procedure, as it was (adr/0097)", () => {
  it("reads each recorded section under its revision and cut, once, with the current counterpart by heading", async () => {
    const { reader, calls } = library();
    const thread = [
      entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, cut: "1/16000", sections: [2, 3] }]),
      entry("t2", [{ sopId: "sop-1", cited: false, revision: 1, cut: "1/16000", sections: [2] }, { sopId: "sop-9", cited: false, revision: 1, cut: "1/16000", sections: [1] }]),
    ];
    const got = await procedureReads(reader, "sop-1", thread);
    expect(got?.currentRevision).toBe(2);
    expect(got?.turns.map((t) => t.interpretationId)).toEqual(["t1", "t2"]);
    const t1 = got?.turns[0];
    const t2 = got?.turns[1];
    expect(t1?.revision).toBe(1);
    expect(t1?.sections[0]).toEqual({ idx: 2, location: "RDP > Triage", text: "## Triage\nConfirm the session.", current: { revision: 2, idx: 2, location: "RDP > Triage", text: "## Triage\nConfirm the session with the owner." } });
    expect(t1?.sections[1]).toEqual({ idx: 3, location: "RDP > Containment", text: "## Containment\nIsolate.", current: null }); // the heading is gone from revision 2
    expect(t2?.sections[0]?.text).toBe("## Triage\nConfirm the session.");
    // Section 2 of revision 1 was recorded twice and read once; the current outline once.
    expect(calls.filter((c) => c === "section 1 2 1/16000").length).toBe(1);
    expect(calls.filter((c) => c.startsWith("outline")).length).toBe(1);
    expect(calls.some((c) => c.includes("sop-9"))).toBe(false);
  });

  it("does not read a counterpart when the act saw the current revision", async () => {
    const { reader, calls } = library(1);
    const got = await procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, cut: "1/16000", sections: [2] }])]);
    expect(got?.turns[0]?.sections[0]).toEqual({ idx: 2, location: "RDP > Triage", text: "## Triage\nConfirm the session." });
    expect(got?.turns[0]?.sections[0]?.current).toBeUndefined();
    expect(calls.some((c) => c.startsWith("outline"))).toBe(false);
  });

  it("is undefined when no act recorded sections, without touching the library", async () => {
    const { reader, calls } = library();
    expect(await procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true }])])).toBeUndefined();
    expect(await procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, cut: "1/16000", sections: [] }])])).toBeUndefined();
    expect(calls).toEqual([]);
  });

  it("treats a record without its cut as not recorded, and fails whole when one read fails", async () => {
    const { reader, calls } = library(1);
    // An index without the cut it was numbered under cannot be read back honestly.
    expect(await procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, sections: [1] }])])).toBeUndefined();
    expect(calls).toEqual([]);
    await expect(procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, cut: "1/16000", sections: [1, 9] }])])).rejects.toThrow("404");
  });

  it("matches a counterpart by heading path: a heading cut into parts is one heading, and a repeated heading is told apart by order", async () => {
    // Revision 1: "Steps" was one section; a repeated "Notes" heading sits under A and under B with the same path text.
    // Revision 2: "Steps" is cut into two parts; the Notes headings stay.
    const rows: Record<number, { location: string; headingPath: string[]; part?: number; parts?: number; text: string }[]> = {
      1: [
        { location: "P > Steps", headingPath: ["P", "Steps"], text: "## Steps\nOne." },
        { location: "P > Notes", headingPath: ["P", "Notes"], text: "## Notes\nFirst notes." },
        { location: "P > Notes", headingPath: ["P", "Notes"], text: "## Notes\nSecond notes." },
      ],
      2: [
        { location: "P > Steps (part 1/2)", headingPath: ["P", "Steps"], part: 1, parts: 2, text: "## Steps\nOne." },
        { location: "P > Steps (part 2/2)", headingPath: ["P", "Steps"], part: 2, parts: 2, text: "Two." },
        { location: "P > Notes", headingPath: ["P", "Notes"], text: "## Notes\nFirst notes." },
        { location: "P > Notes", headingPath: ["P", "Notes"], text: "## Notes\nSecond notes, revised." },
      ],
    };
    const reader: ProcedureReader = {
      async revisions() { return { current: 2 }; },
      async revisionOutline(_id, revision) { return { revision, cut: "1/16000", sections: (rows[revision] ?? []).map((r, i) => ({ idx: i + 1, ...r, text: undefined })) }; },
      async revisionSection(_id, revision, idx) {
        const r = (rows[revision] ?? [])[idx - 1];
        if (!r) throw new Error("404");
        return { revision, cut: "1/16000", idx, of: (rows[revision] ?? []).length, ...r, outline: (rows[revision] ?? []).map((x) => x.location) };
      },
    };
    const got = await procedureReads(reader, "sop-1", [entry("t1", [{ sopId: "sop-1", cited: true, revision: 1, cut: "1/16000", sections: [1, 2, 3] }])]);
    const [steps, notesA, notesB] = got?.turns[0]?.sections ?? [];
    // The heading is not gone: its parts stand together as the current text.
    expect(steps?.current).toEqual({ revision: 2, idx: 1, location: "P > Steps", text: "## Steps\nOne.\n\nTwo." });
    // The second "Notes" is matched to the second "Notes", not the first.
    expect(notesA?.current?.text).toBe("## Notes\nFirst notes.");
    expect(notesB?.current?.text).toBe("## Notes\nSecond notes, revised.");
  });
});
