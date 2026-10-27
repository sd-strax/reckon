// The ACH matrix as data (08 §8.4 Reasoning, impl §7.5): every prediction not
// WITHDRAWN under any hypothesis not ABANDONED, against the live columns. The
// owner cell follows the outcome; each rival in `discriminates` gets the
// opposite; other cells are blank. Pure.
import type { Hypothesis, Prediction } from "../../../src/backend";

export type Cell = "✓" | "✕" | "?" | "";

export interface MatrixRow {
  p: Prediction;
  owner: Hypothesis;
  cells: Cell[];
  /** The owner cell is undecided (tinted row). */
  open: boolean;
}

export interface Matrix {
  columns: Hypothesis[];
  rows: MatrixRow[];
  /** ✕ count per column. */
  inconsistent: number[];
}

function outcomeCell(status: string): Cell {
  return status === "CONFIRMED" ? "✓" : status === "DISCONFIRMED" ? "✕" : "?";
}
function opposite(c: Cell): Cell {
  return c === "✓" ? "✕" : c === "✕" ? "✓" : "?";
}

export function buildMatrix(hs: Hypothesis[]): Matrix {
  const columns = hs.filter((h) => h.status !== "ABANDONED");
  const rows: MatrixRow[] = [];
  for (const h of columns) {
    for (const p of h.predictions) {
      if (p.status === "WITHDRAWN") continue;
      const own = outcomeCell(p.status);
      const cells = columns.map((c) => c.id === h.id ? own : (p.discriminates || []).includes(c.id) ? opposite(own) : "");
      rows.push({ p, owner: h, cells, open: own === "?" });
    }
  }
  rows.sort((a, b) => (a.p.ordinal ?? 0) - (b.p.ordinal ?? 0));
  const inconsistent = columns.map((_, i) => rows.filter((r) => r.cells[i] === "✕").length);
  return { columns, rows, inconsistent };
}
