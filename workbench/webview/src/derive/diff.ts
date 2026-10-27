// A line diff for the procedure inspector (adr/0097): what the model read
// against the procedure's current text at the same place, and two revisions
// of a procedure against each other. A plain LCS over lines; display only.

export interface DiffLine {
  kind: "same" | "added" | "removed";
  text: string;
}

export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split("\n"), b = after.split("\n");
  const n = a.length, m = b.length;
  // LCS table over line equality (bounded inputs: excerpts and procedure bodies).
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const out: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push({ kind: "same", text: a[i] }); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { out.push({ kind: "removed", text: a[i] }); i++; }
    else { out.push({ kind: "added", text: b[j] }); j++; }
  }
  while (i < n) out.push({ kind: "removed", text: a[i++] });
  while (j < m) out.push({ kind: "added", text: b[j++] });
  return out;
}

/** The span of `body` the excerpt stood for: from the excerpt's first line in
 * the body, as many lines as the excerpt has; the whole body when the first
 * line is gone. */
export function currentSpan(body: string, excerpt: string): string {
  const lines = excerpt.replace(/…$/, "").split("\n");
  const first = lines.find((l) => l.trim());
  if (!first) return body;
  const idx = body.indexOf(first.trim());
  if (idx < 0) return body;
  const start = body.lastIndexOf("\n", idx) + 1;
  return body.slice(start).split("\n").slice(0, lines.length).join("\n");
}
