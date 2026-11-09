// A line diff for the procedure inspector's "Changes" view (adr/0097): what a
// section said when the agent read it against what the current revision says
// under the same heading. A longest-common-subsequence over lines; equal runs
// kept, the rest marked removed or added. Both texts are one section each,
// bounded by the store's section budget, so the quadratic table is small.
// In-house rather than a dependency: sixty lines against a shipped license.
// Pure.

export interface DiffLine {
  kind: "same" | "del" | "add";
  text: string;
}

/** The lines of `before` and `after` merged: `del` lines are gone, `add` lines are new, `same` lines are in both. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = lines(before);
  const b = lines(after);
  const n = a.length;
  const m = b.length;
  // lcs(i, j) is the length of the longest common subsequence of a[i..] and
  // b[j..], in one flat table of (n+1) × (m+1).
  const table = new Array<number>((n + 1) * (m + 1)).fill(0);
  const lcs = (i: number, j: number): number => table[i * (m + 1) + j] ?? 0;
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * (m + 1) + j] = a[i] === b[j] ? lcs(i + 1, j + 1) + 1 : Math.max(lcs(i + 1, j), lcs(i, j + 1));
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    const ai = a[i] ?? "";
    const bj = b[j] ?? "";
    if (ai === bj) {
      out.push({ kind: "same", text: ai });
      i++;
      j++;
    } else if (lcs(i + 1, j) >= lcs(i, j + 1)) {
      out.push({ kind: "del", text: ai });
      i++;
    } else {
      out.push({ kind: "add", text: bj });
      j++;
    }
  }
  for (; i < n; i++) out.push({ kind: "del", text: a[i] ?? "" });
  for (; j < m; j++) out.push({ kind: "add", text: b[j] ?? "" });
  return out;
}

function lines(text: string): string[] {
  return text === "" ? [] : text.split("\n");
}
