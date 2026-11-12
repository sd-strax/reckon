// The enablement change (11 §5.1), written once (adr/0342): the investigation
// document's form (a tool row's Set up, where an agent's gap is met) and the
// sidebar's Capabilities Set up… both run it. The flow is NATIVE: a modal
// confirm bound to the human's identity, then a native input for each field
// still to ask — secret fields through a password input, out-of-band of the
// webview and the conversation, so a secret value never touches chat content
// or the transcript store — then the apply on the human token. What is
// captured for an x-secret field is a secret REFERENCE (keychain:// env://
// vault://), per 11 §4.3 — the engine refuses literals at config load.
import * as vscode from "vscode";
import type { CapabilitiesClient } from "./backend";
import type { EnablementField } from "./enablement";

export interface EnablementChange {
  adapter: string;
  enabled: boolean;
  /** Values already collected (the document's form collects its non-secret fields). */
  config: Record<string, string>;
  /** The fields still to ask for natively, with the schema's rules (enablement.ts). */
  ask: EnablementField[];
}

/**
 * Confirm the change and capture what is still to ask. Returns the config to
 * apply, or undefined when the analyst declined, cancelled an input, or the
 * identity changed while the flow waited on them (`held` is taken before the
 * confirm, so it binds the change to the person who confirmed it).
 */
export async function confirmEnablement(change: EnablementChange, held: () => boolean): Promise<Record<string, string> | undefined> {
  const verb = change.enabled ? "Enable" : "Disable";
  const pick = await vscode.window.showWarningMessage(
    `reckon: ${verb} adapter "${change.adapter}"? This edits the tenant config file (the source of truth) and records an attributed config-plane audit entry.`,
    { modal: true },
    verb,
  );
  if (pick !== verb || !held()) {
    return undefined;
  }
  const config = { ...change.config };
  for (const field of change.ask) {
    const mark = field.required ? " · required" : "";
    const v = await vscode.window.showInputBox(field.secret
      ? {
        title: `reckon — ${change.adapter}.${field.name} (secret reference)${mark}`,
        prompt: "A secret REFERENCE (keychain://…, env://NAME, vault://…) — never the literal value; the engine refuses literals at config load.",
        password: true,
        ignoreFocusOut: true,
        validateInput: requiredCheck(field),
      }
      : {
        title: `reckon — ${change.adapter}.${field.name}${mark}`,
        prompt: field.description,
        value: field.value,
        ignoreFocusOut: true,
        validateInput: requiredCheck(field),
      });
    if (v === undefined) {
      return undefined; // cancelled — nothing applied
    }
    if (field.required && v.trim() === "") {
      return undefined; // a required field left blank is refused, whichever path asked
    }
    if (v.trim() !== "") {
      config[field.name] = v.trim();
    }
  }
  return held() ? config : undefined;
}

function requiredCheck(field: EnablementField): ((v: string) => string | undefined) | undefined {
  return field.required ? (v) => (v.trim() === "" ? `${field.name} is required` : undefined) : undefined;
}

/**
 * Apply a confirmed change on the human token, and say so. Throws on a refused
 * apply. Callers reach it through InvestigationDocuments.applyEnablementChange,
 * which re-reads the capability layer for every open investigation after it.
 */
export async function applyEnablement(
  client: { capabilities: Pick<CapabilitiesClient, "applyEnablement"> },
  change: Pick<EnablementChange, "adapter" | "enabled">,
  config: Record<string, string>,
): Promise<void> {
  await client.capabilities.applyEnablement(change.adapter, change.enabled, config);
  void vscode.window.showInformationMessage(
    `reckon: ${change.adapter} ${change.enabled ? "enabled" : "disabled"} — capability surface rebuilt`,
  );
}
