// Reading a stored STIX object (adr/0357). An object's STIX properties are at
// its top level. Its custom properties are in reckon's property-extensions,
// each keyed by its extension-definition id under `extensions`. This module is
// the one place the workbench names those ids. The engine's stix package
// defines them, and a Go test checks that the two lists agree.

/** The definition id of each of reckon's property-extensions. */
export const stixExtensionIds = {
  "reckon-provenance": "extension-definition--d9cb6ba5-7f21-41e1-88bc-3fb28093fb6e",
  "reckon-observation": "extension-definition--3f256422-b1e1-4eab-b517-8a5e2ef879fc",
  "reckon-attestation": "extension-definition--7e5f42fd-8a20-4f42-9fe1-8bb610acacd8",
  "reckon-account": "extension-definition--995fe152-3be8-4989-925d-66f84ff8c5cc",
  "reckon-person": "extension-definition--172a680d-cc8c-47e6-9562-fed23e4fa9ff",
  "reckon-file": "extension-definition--dd95cf33-8636-464c-b0f0-0a235b98052c",
  "reckon-process": "extension-definition--f6373d83-b6b9-49e4-b16c-e300d5187038",
  "reckon-email-message": "extension-definition--d406ebfb-83e0-48cf-adc4-299269d886fe",
  "reckon-assertion": "extension-definition--7fd9b083-aa06-4623-b280-873699a3a3c6",
} as const;

export type StixExtension = keyof typeof stixExtensionIds;

type Obj = Record<string, unknown>;

function asObj(v: unknown): Obj | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : null;
}

/** A property-extension's properties on a stored STIX object, or null when the object does not carry it. */
export function stixExtension(payload: unknown, name: StixExtension): Obj | null {
  const exts = asObj(asObj(payload)?.extensions);
  const ext = asObj(exts?.[stixExtensionIds[name]]);
  if (!ext) return null;
  const props: Obj = {};
  for (const [k, v] of Object.entries(ext)) if (k !== "extension_type") props[k] = v;
  return props;
}

/** The properties every STIX object has, which say what it is rather than what it holds. */
const stixCommon = new Set(["type", "spec_version", "id", "created", "modified", "extensions"]);

/**
 * The object's own properties, as one flat record: the top-level ones other
 * than the common ones, and the custom ones from its property-extensions.
 * Provenance is left out, because it says how the object came to be, not what
 * it is.
 */
export function stixProperties(payload: unknown): Obj {
  const p = asObj(payload);
  if (!p) return {};
  const out: Obj = {};
  for (const [k, v] of Object.entries(p)) if (!stixCommon.has(k)) out[k] = v;
  for (const name of Object.keys(stixExtensionIds) as StixExtension[]) {
    if (name === "reckon-provenance") continue;
    const ext = stixExtension(p, name);
    if (ext) for (const [k, v] of Object.entries(ext)) if (!(k in out)) out[k] = v;
  }
  return out;
}

/**
 * Whether a ref in an observation's object_refs names an entity the record is
 * about. A relationship is not one. Nor is the artifact that stands for a
 * stored record with nothing to extract: it is the record itself (adr/0357).
 */
export function isObservedEntityRef(ref: string): boolean {
  return ref !== "" && !ref.startsWith("relationship--") && !ref.startsWith("artifact--");
}

/**
 * The entities an observation is about: its object_refs that name an entity,
 * and the person a directory record describes in reckon-observation's
 * person_ref. A person is an SDO, which object_refs cannot list, so a
 * directory record with no accounts names its person only there
 * (adr/0324, adr/0357).
 */
export function observedEntityRefs(payload: unknown): string[] {
  const p = asObj(payload);
  const out: string[] = [];
  for (const r of Array.isArray(p?.object_refs) ? p.object_refs : []) {
    if (typeof r === "string" && isObservedEntityRef(r) && !out.includes(r)) out.push(r);
  }
  const person = stixExtension(p, "reckon-observation")?.person_ref;
  if (typeof person === "string" && person && !out.includes(person)) out.push(person);
  return out;
}

/** The ATT&CK technique ids an indicator names in its external_references (adr/0357). */
export function attackTechniques(payload: unknown): string[] {
  const refs = asObj(payload)?.external_references;
  const out: string[] = [];
  for (const r of Array.isArray(refs) ? refs : []) {
    const o = asObj(r);
    if (o?.source_name === "mitre-attack" && typeof o.external_id === "string" && o.external_id && !out.includes(o.external_id)) out.push(o.external_id);
  }
  return out;
}
