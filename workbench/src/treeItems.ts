// The rows the sidebar's trees share (adr/0342, adr/0344): an info row, an
// action row that runs a command, and the gate a tree passes before it reads:
// the backend row while the backend cannot be spoken to, and nothing at all
// while nobody is signed in. Signed out, the container shows one view and one
// Sign In on its header, never a sign-in row per view. Written once, so every
// view words them the same.
import * as vscode from "vscode";
import type { BackendClient } from "./backend";
import type { Session } from "./auth";

export function infoItem(label: string, description: string, icon: string): vscode.TreeItem {
  const item = new vscode.TreeItem(label);
  item.description = description;
  item.tooltip = description || label;
  item.iconPath = new vscode.ThemeIcon(icon);
  return item;
}

export function actionItem(label: string, description: string, icon: string, command: string): vscode.TreeItem {
  const item = infoItem(label, description, icon);
  item.command = { command, title: label };
  return item;
}

/**
 * The handshake and the session, before a tree reads anything: the rows to
 * show in place of the content (the backend row when it is incompatible or
 * not connected; none when signed out, since the container's header carries
 * Sign In), or null when the tree may read.
 */
export async function gateItem(client: BackendClient, session: Session): Promise<vscode.TreeItem[] | null> {
  const status = await client.status();
  const reason = client.incompatibilityReason(status);
  if (reason) {
    return [actionItem(
      status.reachable ? "Backend incompatible" : "Backend not connected",
      reason,
      status.reachable ? "warning" : "debug-disconnect",
      "reckon.checkBackend",
    )];
  }
  return session.signedIn ? null : [];
}
