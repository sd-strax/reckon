// Text the investigation document builds, with no editor or network: the
// panel nonce, error wording, and the candidate-SOP markdown the import path
// parses. Kept free of vscode so it is unit-testable.
import type { SOPCandidate } from "./agentTransport";

export function makeNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}

export function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Render a candidate SOP as markdown + YAML frontmatter — the exact shape the
 * server's markdown import parses (design/06 §2.4). The source URL keys the SOP
 * to its originating investigation, so re-accepting a re-drafted candidate
 * revises the same SOP rather than creating a duplicate.
 */
export function candidateSOPMarkdown(c: SOPCandidate, investigationId: string): string {
  const tags = (c.tags ?? []).map((t) => t.trim()).filter(Boolean);
  const lines = [
    "---",
    `title: ${yamlScalar(c.title.trim())}`,
    `tags: [${tags.map(yamlScalar).join(", ")}]`,
  ];
  if (c.recommendation?.trim()) {
    lines.push(`recommendation: ${yamlScalar(c.recommendation.trim())}`);
  }
  lines.push(
    "source:",
    "  system: reckon",
    `  url: reckon://investigation/${investigationId}`,
    "---",
    "",
    c.body.trim(),
    "",
  );
  return lines.join("\n");
}

/** Minimal YAML scalar quoting — quote when the value could be misread. */
export function yamlScalar(s: string): string {
  return /^[\w .\-/]+$/.test(s) ? s : JSON.stringify(s);
}

/** A filesystem-safe slug from a SOP title, for the import filename. */
export function slugifyTitle(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return slug || "candidate-sop";
}
