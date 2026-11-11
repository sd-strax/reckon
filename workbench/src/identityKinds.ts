// The entities that carry an identity view (adr/0211, adr/0324): a host, an
// account and a person. Their view is mutable, read fresh and never cached;
// every rule that held for a host's view holds for the three, through the
// one predicate here. Pure; shared by the extension and the webview.
import type { EvidenceDoc, IdentityViews } from "./viewTypes";

/** The kinds of entity whose identity an analyst asserts on. */
export type IdentityKind = "host" | "account" | "person";

/**
 * The identity kind a ref names by its type, or null. An `identity--` ref is
 * a person or a vendor; only its doc tells which (isPersonDoc), so by ref
 * alone it counts as a person: read fresh until its doc says otherwise.
 */
export function identityKindOf(ref: string): IdentityKind | null {
  const r = ref.toLowerCase();
  if (r.startsWith("x-host--")) return "host";
  if (r.startsWith("user-account--")) return "account";
  if (r.startsWith("identity--")) return "person";
  return null;
}

/** Whether a ref is of a type that carries an identity view. */
export function bearsIdentity(ref: string): boolean {
  return identityKindOf(ref) !== null;
}

/** The word a message names the kind by. */
export function identityWord(ref: string): string {
  return identityKindOf(ref) ?? "entity";
}

/**
 * Whether an `identity--` doc is a person (identity_class individual) rather
 * than a vendor: it carries a person's view, or its payload says so. The
 * payload is immutable, so a cached doc answers it too.
 */
export function isPersonDoc(doc: EvidenceDoc | null | undefined): boolean {
  if (!doc) return false;
  if (doc.personIdentity) return true;
  const p = doc.payload && typeof doc.payload === "object" ? (doc.payload as Record<string, unknown>) : null;
  const props = p && typeof p.properties === "object" && p.properties ? (p.properties as Record<string, unknown>) : p;
  return !!props && props.identity_class === "individual";
}

/**
 * Whether a held doc carries an identity view that may move: a host, an
 * account, or an `identity--` that is a person (a vendor never does).
 */
export function docBearsIdentity(ref: string, doc: EvidenceDoc | null | undefined): boolean {
  const kind = identityKindOf(ref);
  return kind !== null && (kind !== "person" || isPersonDoc(doc));
}

/** The view fields, one per kind. */
export const identityViewKeys = ["identity", "accountIdentity", "personIdentity"] as const satisfies readonly (keyof IdentityViews)[];

/** The view field a kind carries its view in. */
export function viewKeyOf(kind: IdentityKind): keyof IdentityViews {
  return kind === "host" ? "identity" : kind === "account" ? "accountIdentity" : "personIdentity";
}
