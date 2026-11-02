// The host's small shared helpers: the one place error wording, the CSP nonce
// and HTML escaping are defined, so no module carries its own copy. Free of
// vscode so it is unit-testable.
import { randomBytes } from "crypto";

/** The message of an error, or the value as text when it is not an Error. */
export function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * A fresh CSP nonce for one render of a panel: 24 random bytes from the
 * platform's CSPRNG, as 32 base64url characters (every one legal in a
 * `nonce-` source).
 */
export function makeNonce(): string {
  return randomBytes(24).toString("base64url");
}

/** Escape text for interpolation into HTML the host builds. */
export function escapeHtml(s: unknown): string {
  return String(s ?? "").replace(/[&<>"']/g, (c) => HTML_ESCAPES[c] ?? c);
}

const HTML_ESCAPES: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

/** The tab title of an investigation panel. */
export function panelTitle(title: string): string {
  return `⚖ ${title}`;
}
