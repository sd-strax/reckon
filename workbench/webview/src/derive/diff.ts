// A line diff for the procedure inspector's "Changes" view (adr/0097): what a
// section said when the agent read it against what the current revision says
// under the same heading. Equal lines at both ends are matched first; the
// middle is a longest-common-subsequence over lines when its table fits a
// fixed budget, and a plain removed-then-added block when it does not, so a
// section of thousands of short lines can never stall the webview or take
// its memory. In-house rather than a dependency: a page of code against a
// shipped license. Pure.

export interface DiffLine {
  kind: "same" | "del" | "add";
  text: string;
}

/** The most cells the LCS table may take; past it the middle is one block. */
export const MAX_DIFF_CELLS = 1_000_000;

/** The lines of `before` and `after` merged: `del` lines are gone, `add` lines are new, `same` lines are in both. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = lines(before);
  const b = lines(after);
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const out: DiffLine[] = a.slice(0, head).map((text) => ({ kind: "same", text }));
  const midA = a.slice(head, a.length - tail);
  const midB = b.slice(head, b.length - tail);
  if ((midA.length + 1) * (midB.length + 1) <= MAX_DIFF_CELLS) {
    out.push(...lcsDiff(midA, midB));
  } else {
    out.push(...midA.map((text): DiffLine => ({ kind: "del", text })), ...midB.map((text): DiffLine => ({ kind: "add", text })));
  }
  out.push(...a.slice(a.length - tail).map((text): DiffLine => ({ kind: "same", text })));
  return out;
}

/** The LCS diff of two line lists whose table fits the budget. */
function lcsDiff(a: string[], b: string[]): DiffLine[] {
  const n = a.length;
  const m = b.length;
  // lcs(i, j) is the length of the longest common subsequence of a[i..] and
  // b[j..], in one flat table of (n+1) × (m+1).
  const table = new Uint32Array((n + 1) * (m + 1));
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
