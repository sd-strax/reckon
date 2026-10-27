// Citations (02 §2.8): how a ref reads before and after the host resolves it,
// and which chip colour it takes (01 §Entity-type palette). Pure.

/** Human type labels by STIX type prefix (an OCSF event id has no prefix). */
export const REF_TYPE_LABELS: Record<string, string> = {
  "observed-data": "observed data", "x-host": "host", "ipv4-addr": "IP",
  "ipv6-addr": "IP", "user-account": "account", "process": "process",
  "file": "file", "domain-name": "domain", "email-addr": "email",
  "url": "URL", "windows-registry-key": "registry", "indicator": "indicator",
  "x-hypothesis": "hypothesis", "x-prediction": "prediction",
  "identity": "vendor", "sighting": "sighting",
  "note": "attestation", // a human statement recorded as evidence (adr/0054)
  "x-null-observation": "nothing observed", // a complete, empty answer recorded as evidence (adr/0061)
};

/** Entity-type palette classes: one fixed colour per type, everywhere. */
export const REF_CHIP_CLASS: Record<string, string> = {
  "x-host": "chip-host", "ipv4-addr": "chip-ip", "ipv6-addr": "chip-ip",
  "user-account": "chip-account", "file": "chip-hash",
  "domain-name": "chip-domain", "url": "chip-domain", "email-addr": "chip-email",
  "indicator": "chip-alert", "sighting": "chip-alert",
  "x-hypothesis": "chip-brand", "x-prediction": "chip-brand",
};

/** Entity types get the popover (ui/02 §2.4); everything else opens its record. */
export const ENTITY_POPOVER_TYPES = new Set([
  "x-host", "ipv4-addr", "ipv6-addr", "user-account", "process", "file",
  "domain-name", "email-addr", "url", "windows-registry-key", "indicator",
]);

/** The STIX type prefix of a ref, "" for an OCSF event id. */
export function refType(ref: string): string {
  return ref.includes("--") ? ref.slice(0, ref.indexOf("--")) : "";
}

export function refTypeLabel(ref: string): string {
  const t = refType(ref) || "event";
  return REF_TYPE_LABELS[t] || t;
}

/** The chip text: type alone until resolved, then "type · value". */
export function chipText(ref: string, resolved: string | undefined): string {
  const type = refTypeLabel(ref);
  return resolved ? `${type} · ${resolved}` : type;
}

export function chipClass(ref: string): string {
  const c = REF_CHIP_CLASS[refType(ref)];
  return "refchip" + (c ? " " + c : "");
}

export function isEntityRef(ref: string): boolean {
  return ENTITY_POPOVER_TYPES.has(refType(ref));
}

/** The seed line's type word for an entity seed. */
export function seedTypeLabel(ref: string | undefined): string {
  const t = String(ref || "").split("--")[0];
  if (t === "x-host") return "host";
  if (t === "ipv4-addr" || t === "ipv6-addr") return "IP";
  if (t === "file") return "file";
  if (t === "user-account") return "account";
  if (t === "domain-name") return "domain";
  return "entity";
}
