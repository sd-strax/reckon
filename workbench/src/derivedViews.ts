// The derived lenses over the thread's citations, computed host-side with
// cached evidence fetches (impl §3.1): the event-time strip (13 §4 Timeline,
// minimal at v0 — every ref that carries an EVENT time: OCSF `time` and
// observed-data `first_observed`) and the minimal evidence graph (13 §7 step 7
// — the two-layer join, acts collapsed into grounding edges). Pure over the
// fetcher; shared by the document host and the harness recorder.
import type { ActionRow, EvidenceDoc, Hypothesis, PinRow, Seed, ThreadEntry } from "./viewTypes";
import type { DerivedViews, GraphView, StripItem } from "./protocol";

/**
 * The records that cite evidence beside the thread (#141): a prediction's test
 * results, a pin's evidence, an action's grounds and the seed. The thread's own
 * entries list only the record each act created, so the evidence lives here.
 */
export interface CitingRecords {
  hypotheses?: Hypothesis[];
  pins?: PinRow[];
  actions?: ActionRow[];
  seed?: Seed;
}

export async function buildDerivedViews(
  thread: ThreadEntry[],
  getDoc: (ref: string) => Promise<EvidenceDoc | null>,
  citing: CitingRecords = {},
): Promise<DerivedViews> {
  const refs: string[] = [];
  const add = (r: string | undefined) => { if (r && !refs.includes(r)) refs.push(r); };
  // What cites what, beyond the thread: each citing record → the evidence it cites.
  const cites: { from: string; to: string[]; ai: boolean }[] = [];
  for (const h of citing.hypotheses ?? []) {
    for (const p of h.predictions ?? []) if (p.testResultRefs?.length) cites.push({ from: p.id, to: p.testResultRefs, ai: false });
  }
  for (const p of citing.pins ?? []) if (!p.superseded && p.inputRefs?.length) cites.push({ from: p.interpretationId, to: p.inputRefs, ai: p.actor === "AI_DELEGATED" });
  for (const a of citing.actions ?? []) if (a.evidenceRefs?.length) cites.push({ from: a.actionId, to: a.evidenceRefs, ai: false });
  const seed = citing.seed;
  for (const r of [seed?.caseRef, seed?.detectionFindingRef, seed?.entityRef]) add(r);
  for (const c of cites) for (const r of c.to) add(r);
  for (const e of thread) {
    for (const r of [...e.inputRefs, ...e.outputRefs]) add(r);
  }
  const bounded = refs.slice(0, 80);
  const docs = new Map<string, EvidenceDoc | null>();
  const items: StripItem[] = [];
  // Small batches: local backend, but no reason to fire 80 at once.
  for (let i = 0; i < bounded.length; i += 8) {
    const slice = bounded.slice(i, i + 8);
    const fetched = await Promise.all(slice.map((r) => getDoc(r)));
    fetched.forEach((doc, j) => docs.set(slice[j], doc));
    for (const doc of fetched) {
      if (!doc) continue;
      let time = doc.kind === "ocsf" ? doc.time : undefined;
      if (!time && doc.type === "observed-data") {
        const p = (doc.payload ?? {}) as Record<string, unknown>;
        if (typeof p.first_observed === "string") time = p.first_observed;
      }
      if (time && Number.isFinite(Date.parse(time))) {
        items.push({ ref: doc.ref, time, label: doc.type || doc.kind });
      }
    }
  }
  items.sort((a, b) => Date.parse(a.time) - Date.parse(b.time));

  // --- the entities the cited evidence observed (Summary key entities) ---
  const entityCount = new Map<string, number>();
  for (const doc of docs.values()) {
    if (doc?.kind !== "stix" || doc.type !== "observed-data") continue;
    const p = doc.payload as { object_refs?: unknown } | null;
    for (const or of Array.isArray(p?.object_refs) ? p.object_refs : []) {
      if (typeof or === "string" && or) entityCount.set(or, (entityCount.get(or) || 0) + 1);
    }
  }
  const entities = [...entityCount].map(([ref, citations]) => ({ ref, citations }))
    .sort((a, b) => b.citations - a.citations);

  // --- the graph: the two-layer evidence structure ----------------------
  // Each act collapses into grounding edges: every object it produced points
  // at every piece of evidence it consumed, tinted by whether a human or the
  // AI asserted the link. Edges dedup by (from,to); a human assertion
  // outranks an AI one for the tint.
  const edgeByPair = new Map<string, GraphView["edges"][number]>();
  const addEdge = (from: string, to: string, ai: boolean, join?: boolean) => {
    const key = from + "|" + to;
    const prior = edgeByPair.get(key);
    if (!prior) edgeByPair.set(key, { from, to, ai, join });
    else if (prior.ai && !ai) prior.ai = false;
  };
  const inGraph = new Set<string>();
  for (const e of thread.slice(-40)) {
    const ins = e.inputRefs.filter(Boolean);
    const outs = e.outputRefs.filter(Boolean);
    for (const r of [...ins, ...outs]) inGraph.add(r);
    const ai = e.actor.kind === "AI_DELEGATED";
    for (const o of outs) for (const i of ins) if (o !== i) addEdge(o, i, ai);
  }
  for (const c of cites) {
    inGraph.add(c.from);
    for (const r of c.to) { inGraph.add(r); addEdge(c.from, r, c.ai); }
  }
  // The explicit two-layer join: observed-data → the entities it observed.
  // Snapshot first — join targets are new nodes, not new observed-data.
  for (const r of [...inGraph]) {
    const doc = docs.get(r) ?? null;
    if (doc?.kind === "stix" && doc.type === "observed-data") {
      const p = doc.payload as { object_refs?: unknown } | null;
      const objRefs = Array.isArray(p?.object_refs) ? p.object_refs : [];
      for (const or of objRefs) {
        if (typeof or !== "string" || or === "") continue;
        inGraph.add(or);
        addEdge(r, or, false, true);
      }
    }
  }
  const graphRefs: GraphView["refs"] = [];
  for (const r of inGraph) {
    const doc = docs.get(r) ?? null;
    graphRefs.push({
      id: r,
      kind: doc?.kind ?? "",
      type: doc?.type ?? (r.includes("--") ? r.slice(0, r.indexOf("--")) : "event"),
    });
  }
  return { strip: items, graph: { refs: graphRefs, edges: [...edgeByPair.values()] }, entities };
}
