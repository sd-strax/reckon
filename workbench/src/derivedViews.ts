// The derived lenses over the thread's citations, computed host-side with
// cached evidence fetches (impl §3.1): the event-time strip (13 §4 Timeline,
// minimal at v0 — every ref that carries an EVENT time: OCSF `time` and
// observed-data `first_observed`) and the minimal evidence graph (13 §7 step 7
// — the two-layer join, acts collapsed into grounding edges). Pure over the
// fetcher; shared by the document host and the harness recorder.
import type { EvidenceDoc, ThreadEntry } from "./backend";
import type { DerivedViews, GraphView, StripItem } from "./protocol";

export async function buildDerivedViews(
  thread: ThreadEntry[],
  getDoc: (ref: string) => Promise<EvidenceDoc | null>,
): Promise<DerivedViews> {
  const refs: string[] = [];
  for (const e of thread) {
    for (const r of [...e.inputRefs, ...e.outputRefs]) {
      if (r && !refs.includes(r)) refs.push(r);
    }
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
  return { strip: items, graph: { refs: graphRefs, edges: [...edgeByPair.values()] } };
}
