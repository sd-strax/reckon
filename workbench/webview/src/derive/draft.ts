// The unseeded (draft) state's live interpretation (design/ui/02 §2.7). Two
// rules, mirrored from the server so the hint matches what will be persisted:
// whitespace or a trailing "?" is a question; anything else is an entity. The
// entity sub-label is cosmetic — the server mints the id.

export type DraftKind = "entity" | "question" | "";

export function classifyDraft(v: string): DraftKind {
  const s = v.trim();
  if (!s) return "";
  if (/\s/.test(s) || s.endsWith("?")) return "question";
  return "entity";
}

export function entitySubLabel(v: string): string {
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(v) || v.includes(":")) return "IP";
  if (/^[0-9a-fA-F]+$/.test(v) && (v.length === 32 || v.length === 40 || v.length === 64)) return "file";
  return "host";
}

export function effectiveDraftKind(v: string, override: DraftKind | null): DraftKind {
  const inferred = classifyDraft(v);
  if (!inferred) return "";
  return override || inferred;
}
