// What a new citation may name (adr/0364): a STIX object. A raw OCSF event is
// cited through the observation made from it, and the engine refuses a bare
// event id in a pin, a prediction outcome or an action request. Older records
// that cite raw events keep them, and every reader still renders them; this
// only shapes what the workbench sends. Pure.

/** A STIX identifier's shape: a type, two hyphens and a UUID. The engine
 * checks the UUID's version and variant; here only the shape matters, since
 * what is left out is a bare event id. */
const STIX_ID = /^[a-z][a-z0-9-]*--[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Whether a new citation may name the ref: a STIX id, never a raw event id. */
export function isCitable(ref: string): boolean {
  return STIX_ID.test(ref);
}

/** The refs a new citation may carry, in order: raw event ids left out. */
export function citableRefs(refs: readonly string[]): string[] {
  return refs.filter(isCitable);
}
