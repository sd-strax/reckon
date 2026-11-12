// The enablement rules, pure and written once (adr/0342), so the extension
// host and the webview bundle both compile them: which adapter a verb's Set up
// enables, and which fields an enablement asks for natively. Types only from
// viewTypes.
import type { Enablement, EnablementAdapter } from "./viewTypes";

/**
 * The adapter that could serve an unavailable verb, or null: the verb is not
 * enabled and the first adapter that would close it is installed, supportable
 * (the form enables it) and not enabled. `status` is the verb's status as the
 * caller read it — the catalog's, or a tool result's coverage. The sidebar's
 * Capabilities rows and the document's tool rows both offer Set up by this.
 */
export function setUpAdapter(verb: string, status: string | undefined, enablement: Enablement | null | undefined): string | null {
  if (status !== "unavailable" || !enablement) return null;
  const ev = (enablement.verbs || []).find((x) => x.verb === verb);
  const adapter = ev?.closableBy?.[0];
  if (!ev || ev.enabled || !adapter) return null;
  const a = (enablement.adapters || []).find((x) => x.name === adapter);
  return a && a.supportable && !a.enabled ? adapter : null;
}

/** One config field an enablement asks for natively, in order. */
export interface EnablementField {
  name: string;
  secret: boolean;
  /** Required by the adapter's schema: marked, and an empty answer is refused. */
  required?: boolean;
  description?: string;
  /** A value to start the input with (the adapter's current scenario). */
  value?: string;
}

/** Every field of an adapter's schema, with the schema's rules. */
export function schemaFields(a: EnablementAdapter): EnablementField[] {
  return Object.entries(a.configFields || {}).map(([name, f]) => ({
    name,
    secret: f.secret === true,
    required: (a.requiredFields || []).includes(name),
    description: f.description,
    value: name === "scenario" ? a.scenario || undefined : undefined,
  }));
}

/**
 * What the document's form leaves to the native flow: every secret field (a
 * reference, never typed into the webview) and any required field it left
 * empty, each with the schema's rules, so both paths refuse the same blanks.
 */
export function nativeFields(a: EnablementAdapter, collected: Record<string, string>): EnablementField[] {
  return schemaFields(a).filter((f) => f.secret || (f.required && !(collected[f.name] ?? "").trim()));
}
