// The rows the sidebar's trees share (adr/0342, adr/0344): an info row, an
// action row that runs a command, and the rows a tree shows instead of its
// content while the backend cannot be spoken to or nobody is signed in.
// Written once, so every view words them the same.
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

/** The sign-in row a tree shows while nobody is signed in. */
export function signInItem(): vscode.TreeItem {
  return actionItem("Sign in to reckon", "opens Keycloak in your browser", "sign-in", "reckon.signIn");
}

/**
 * The handshake and the session, before a tree reads anything: the one row
 * to show in place of the content (backend incompatible or not connected,
 * or signed out), or null when the tree may read.
 */
export async function gateItem(client: BackendClient, session: Session): Promise<vscode.TreeItem | null> {
  const status = await client.status();
  const reason = client.incompatibilityReason(status);
  if (reason) {
    return actionItem(
      status.reachable ? "Backend incompatible" : "Backend not connected",
      reason,
      status.reachable ? "warning" : "debug-disconnect",
      "reckon.checkBackend",
    );
  }
  return session.signedIn ? null : signInItem();
}
