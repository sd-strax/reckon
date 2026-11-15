// A human label for an evidence ref — what it IS, not its id. Shared by the
// document host (chip resolution) and the harness recorder.
import type { EvidenceDoc } from "./backend";
import { observedEntityRefs, stixProperties } from "./stixExtensions";

/**
 * A short human label for an opened evidence ref — what it IS, not its id.
 * OCSF records label by class; STIX objects by the salient property of their
 * type (hostname, account, process+pid, address). Empty when nothing sensible
 * can be read, leaving the chip its type-only label.
 */
export function deriveEvidenceLabel(doc: EvidenceDoc): string {
  if (doc.kind === "ocsf") {
    return doc.type || "event";
  }
  const p = (doc.payload ?? {}) as Record<string, unknown>;
  if (doc.type === "artifact") {
    // The artifact that stands for a stored record with nothing to extract
    // (adr/0357). It is the record, not an entity, and its chip's type label
    // ("stored record") says so on its own.
    return "";
  }
  if (doc.type === "note") {
    // An attestation (adr/0054): "attested by analyst-1 by phone: J. Doe".
    return typeof p.abstract === "string" && p.abstract ? p.abstract : "attestation";
  }
  // A process's name and a file's path are in their property-extensions
  // (adr/0357); stixProperties reads them beside the top-level ones.
  const props = stixProperties(p);
  const first = (...keys: string[]): string => {
    for (const k of keys) {
      const v = props[k];
      if (typeof v === "string" && v !== "") return v;
      if (typeof v === "number") return String(v);
    }
    return "";
  };
  if (doc.type === "windows-registry-key") {
    // adr/0341: the key, then the value's name; "(default)" for the default value.
    const key = first("key");
    const value = Array.isArray(props.values) ? (props.values[0] as Record<string, unknown> | undefined) : undefined;
    if (!key || typeof value?.name !== "string") return key;
    return `${key} (${value.name || "default"})`;
  }
  const val = first("hostname", "value", "address", "name", "user_id", "account_login", "display_name", "path");
  if (val) {
    const pid = props.pid;
    return typeof pid === "number" ? `${val} (pid ${pid})` : val;
  }
  if (doc.type === "observed-data" && Array.isArray(p.object_refs)) {
    // Only the entities count, the person a directory record describes
    // among them. An observation of a record with nothing to extract lists
    // the record's artifact alone (adr/0357).
    const n = observedEntityRefs(doc.payload).length;
    if (n === 0) return "the stored record";
    return `${n} object${n === 1 ? "" : "s"}`;
  }
  return "";
}
