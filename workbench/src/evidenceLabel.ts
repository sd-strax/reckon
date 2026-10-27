// A human label for an evidence ref — what it IS, not its id. Shared by the
// document host (chip resolution) and the harness recorder.
import type { EvidenceDoc } from "./backend";

/**
 * A short human label for an opened evidence ref — what it IS, not its id.
 * OCSF records label by class; STIX objects by the salient property of their
 * type (hostname, account, process+pid, address). Empty when nothing sensible
 * can be read, leaving the chip its type-only label.
 */
export function deriveEvidenceLabel(doc: EvidenceDoc): string {
  if (doc.kind === "ocsf") {
    // The per-class sentence (adr/0090) names the event; the class name
    // stands in for a class with no renderer.
    const sentence = (doc.view?.sentence ?? []).map((s) => s.text).join("");
    if (sentence) return sentence.length > 96 ? sentence.slice(0, 95) + "…" : sentence;
    return doc.type || "event";
  }
  const p = (doc.payload ?? {}) as Record<string, unknown>;
  if (doc.type === "note") {
    // An attestation (adr/0054): "attested by analyst-1 by phone: J. Doe".
    return typeof p.abstract === "string" && p.abstract ? p.abstract : "attestation";
  }
  const props = (typeof p.properties === "object" && p.properties ? p.properties : p) as Record<string, unknown>;
  const first = (...keys: string[]): string => {
    for (const k of keys) {
      const v = props[k];
      if (typeof v === "string" && v !== "") return v;
      if (typeof v === "number") return String(v);
    }
    return "";
  };
  const val = first("hostname", "value", "address", "name", "user_id", "account_login", "display_name", "path");
  if (val) {
    const pid = props.pid;
    return typeof pid === "number" ? `${val} (pid ${pid})` : val;
  }
  if (doc.type === "observed-data" && Array.isArray(p.object_refs)) {
    const n = p.object_refs.length;
    return `${n} object${n === 1 ? "" : "s"}`;
  }
  return "";
}
