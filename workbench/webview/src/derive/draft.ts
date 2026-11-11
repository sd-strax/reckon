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

/** The kinds of entity a seed may name (adr/0324). */
export type SeedEntityKind = "host" | "account" | "person";

/**
 * Whether the analyst chooses the entity's kind: not for an address or a
 * hash, which are what they are.
 */
export function entityKindChoosable(v: string): boolean {
  const sub = entitySubLabel(v);
  return sub !== "IP" && sub !== "file";
}

/**
 * The kind a token names when the analyst did not choose one, as the server
 * defaults it: a backslash or an "@" is an account ("DOMAIN\sam",
 * "user@domain"); anything else a host.
 */
export function defaultEntityKind(v: string): SeedEntityKind {
  return /[\\@]/.test(v) ? "account" : "host";
}

/**
 * The entity kind a seed is sent with: the analyst's choice, else the
 * default; undefined for an address or a hash, which carry none.
 */
export function effectiveEntityKind(v: string, chosen: SeedEntityKind | null): SeedEntityKind | undefined {
  if (!entityKindChoosable(v)) return undefined;
  return chosen ?? defaultEntityKind(v);
}
