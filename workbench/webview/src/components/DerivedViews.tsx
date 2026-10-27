// The derived lenses over the thread's citations: the event-time strip (13 §4
// Timeline, minimal at v0 — the OTHER clock, binding §7) and the minimal
// evidence graph (13 §7 step 7 — the two-layer join, acts as edges).
import { useApp } from "../app";
import { fmtSpan } from "../derive/actions";
import { isReason, layoutGraph, type GraphRef } from "../derive/graph";
import type { DerivedViews as Derived } from "../../../src/protocol";

export function EventStrip({ items }: { items: Derived["strip"] }) {
  const { send } = useApp();
  if (!items || items.length < 2) return null;
  const t0 = Date.parse(items[0].time);
  const t1 = Date.parse(items[items.length - 1].time);
  const span = Math.max(1, t1 - t0);
  return (
    <details id="stripWrap">
      <summary>Event time · {items.length} events over {fmtSpan(span)}</summary>
      <div id="strip">
        {items.map((it, i) => (
          <span
            key={it.ref + i}
            className={"evdot" + (it.label === "observed-data" ? " od" : "")}
            style={{ left: (((Date.parse(it.time) - t0) / span) * 100).toFixed(2) + "%" }}
            title={it.label + " · " + new Date(it.time).toLocaleString() + " — open record"}
            onClick={() => send({ type: "evidence.open", ref: it.ref })}
          />
        ))}
      </div>
      <div id="stripAxis"><span>{new Date(t0).toLocaleString()}</span><span>{new Date(t1).toLocaleString()}</span></div>
    </details>
  );
}

const ROW = 24, TOP = 22, NW = [252, 240], CX = [30, 360];

export function EvidenceGraph({ graph }: { graph: Derived["graph"] }) {
  const { state, send } = useApp();
  if (!graph || !graph.refs.length) return null;
  const { stix, ocsf, ungrounded } = layoutGraph(graph);
  const H = TOP + 10 + Math.max(stix.length, ocsf.length, 1) * ROW;
  const pos: Record<string, { x: number; y: number; w: number }> = {};
  const label = (n: GraphRef) => {
    const short = state.labels[n.id] || (n.type + (n.id.includes("--") ? " " + n.id.slice(n.id.indexOf("--") + 2, n.id.indexOf("--") + 8) : ""));
    return short.length > 32 ? short.slice(0, 31) + "…" : short;
  };
  const nodes = (list: GraphRef[], col: number, base: string) => list.map((n, i) => {
    const y = TOP + i * ROW;
    pos[n.id] = { x: CX[col], y: y + 8, w: NW[col] };
    let cls = "gnode " + (base === "stix" && isReason(n) ? "reason" : base);
    if (ungrounded.has(n.id)) cls += " ungrounded";
    return (
      <g key={n.id} className={cls} transform={`translate(${CX[col]},${y})`} onClick={() => send({ type: "evidence.open", ref: n.id })}>
        <rect width={NW[col]} height={17} />
        <text x={5} y={12}>{label(n)}</text>
      </g>
    );
  });
  const left = nodes(stix, 0, "stix");
  const right = nodes(ocsf, 1, "ocsf");
  const edges = graph.edges.map((e, i) => {
    const a = pos[e.from], b = pos[e.to];
    if (!a || !b) return null;
    let d: string;
    if (a.x === b.x) {
      // Intra-layer citation: route it as a lobe to the LEFT of the column so
      // it never crosses the STIX↔OCSF channel.
      const bx = a.x - 20;
      d = `M${a.x},${a.y} C${bx},${a.y} ${bx},${b.y} ${b.x},${b.y}`;
    } else {
      const x1 = a.x + a.w, mid = (x1 + b.x) / 2;
      d = `M${x1},${a.y} C${mid},${a.y} ${mid},${b.y} ${b.x},${b.y}`;
    }
    return <path key={i} d={d} className={"gedge" + (e.join ? " join" : (e.ai ? "" : " human"))} />;
  });
  return (
    <details id="graphWrap">
      <summary>Evidence graph · {stix.length} objects · {ocsf.length} events{ungrounded.size ? ` · ${ungrounded.size} ungrounded` : ""}</summary>
      <div id="graphBox">
        <svg viewBox={`0 0 620 ${H}`} width="100%">
          <g>{edges}</g>
          <text x={CX[0]} y={12} className="gcol">interpretation layer (stix)</text>
          <text x={CX[1]} y={12} className="gcol">telemetry (ocsf)</text>
          {left}
          {right}
        </svg>
      </div>
    </details>
  );
}
