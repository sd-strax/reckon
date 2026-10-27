// The minimal evidence graph's layout facts (13 §7 step 7): two layers, and
// which reasoning objects are grounded. Pure; the component draws the SVG.
import type { GraphView } from "../../../src/protocol";

export type GraphRef = GraphView["refs"][number];

export interface GraphLayout {
  stix: GraphRef[];
  ocsf: GraphRef[];
  ungrounded: Set<string>;
}

export const isReason = (r: GraphRef): boolean => r.type === "x-hypothesis" || r.type === "x-prediction";

export function layoutGraph(g: GraphView): GraphLayout {
  const refs = g.refs || [], edges = g.edges || [];
  // Unfetched refs classify by shape — a STIX id has the "type--uuid" separator.
  const stix = refs.filter((r) => r.kind === "stix" || (r.kind === "" && r.id.includes("--")));
  const ocsf = refs.filter((r) => !stix.includes(r));
  // A reasoning object is grounded if it reaches ANY concrete evidence by
  // following citations — even through other reasoning objects. One that
  // reaches nothing but its own kind is floating on assertion.
  const adj: Record<string, string[]> = {};
  for (const e of edges) (adj[e.from] = adj[e.from] || []).push(e.to);
  const reasonIds = new Set(stix.filter(isReason).map((r) => r.id));
  const grounded = (id: string): boolean => {
    const seen = new Set([id]), stack = [id];
    while (stack.length) {
      for (const nxt of adj[stack.pop()!] || []) {
        if (!reasonIds.has(nxt)) return true;
        if (!seen.has(nxt)) { seen.add(nxt); stack.push(nxt); }
      }
    }
    return false;
  };
  const ungrounded = new Set(stix.filter((r) => isReason(r) && !grounded(r.id)).map((r) => r.id));
  return { stix, ocsf, ungrounded };
}
