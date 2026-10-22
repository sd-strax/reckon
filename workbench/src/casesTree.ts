// The Cases view (adr/0030-cases-pane.md): the team's case queue from the
// system of record, below Investigations in the reckon container. Read-only —
// a click opens the case document (caseDocument.ts); starting an investigation
// happens there. The queue is the tenant's configured one (14 §4.2); rows that
// already have an open investigation say so and lead to it.
//
// VS Code tree items have no columns: the label is the title, the description
// carries number · priority · status · age, the tooltip the rest.

import * as vscode from "vscode";
import { BackendClient, CaseRow } from "./backend";
import { Session } from "./auth";

export class CasesProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private readonly emitter = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.emitter.event;

  constructor(
    private readonly client: BackendClient,
    private readonly session: Session,
  ) {}

  refresh(): void {
    this.emitter.fire();
  }

  getTreeItem(element: vscode.TreeItem): vscode.TreeItem {
    return element;
  }

  async getChildren(element?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
    if (element) {
      return [];
    }
    if (!this.session.signedIn) {
      return [actionItem("Sign in to reckon", "opens Keycloak in your browser", "sign-in", "reckon.signIn")];
    }
    let queue: string;
    let rows: CaseRow[];
    try {
      const res = await this.client.cases();
      queue = res.queue;
      rows = res.cases;
    } catch (err) {
      const msg = errText(err);
      if (/\b503\b|not configured|no case system/i.test(msg)) {
        return [infoItem("No case system of record configured", "bind query_external_cases to enable", "plug")];
      }
      return [actionItem("Could not read the case queue", msg, "warning", "reckon.refreshCases")];
    }

    const items: vscode.TreeItem[] = [];
    if (!queue) {
      items.push(infoItem("No case queue configured", "set policies.case_queue in the tenant capability config — showing recent cases", "info"));
    }
    if (rows.length === 0) {
      items.push(infoItem(queue ? `Nothing in the ${queue} queue` : "No recent cases", "", "inbox"));
      return items;
    }
    for (const r of rows) {
      items.push(rowItem(r));
    }
    return items;
  }
}

function rowItem(r: CaseRow): vscode.TreeItem {
  const item = new vscode.TreeItem(r.title || r.number);
  const cues: string[] = [r.number];
  const p = shortPriority(r.priority);
  if (p) cues.push(p);
  if (r.status) cues.push(r.status);
  if (r.openedAt) cues.push(age(r.openedAt));
  if (r.investigation) {
    cues.unshift("investigating");
    item.contextValue = "reckon.case.investigating";
    item.iconPath = new vscode.ThemeIcon("beaker", new vscode.ThemeColor("charts.blue"));
  } else {
    item.contextValue = "reckon.case";
    item.iconPath = new vscode.ThemeIcon("circle-outline", new vscode.ThemeColor(priorityColor(r.priority)));
  }
  item.description = cues.join(" · ");
  const lines = [
    `${r.number}${r.title ? " — " + r.title : ""}`,
    [r.priority, r.status, r.assignmentGroup].filter(Boolean).join(" · "),
  ];
  if (r.openedAt) lines.push(`opened ${new Date(r.openedAt).toLocaleString()}`);
  if (r.investigation) lines.push(`under investigation: ${r.investigation.title}`);
  if (r.source) lines.push(`source: ${r.source}`);
  item.tooltip = lines.filter(Boolean).join("\n");
  item.command = { command: "reckon.openCase", title: "Open Case", arguments: [r.number] };
  return item;
}

/** "2 - High" → "P2"; anything without a leading digit passes through. */
function shortPriority(p?: string): string {
  if (!p) return "";
  const m = /^\s*(\d)/.exec(p);
  return m ? `P${m[1]}` : p;
}

function priorityColor(p?: string): string {
  const m = /^\s*(\d)/.exec(p ?? "");
  switch (m?.[1]) {
    case "1": return "charts.red";
    case "2": return "charts.orange";
    case "3": return "charts.yellow";
    default: return "descriptionForeground";
  }
}

function age(iso: string): string {
  const ms = Date.now() - Date.parse(iso);
  if (!Number.isFinite(ms) || ms < 0) return "";
  const m = Math.floor(ms / 60000);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

function actionItem(label: string, description: string, icon: string, command: string): vscode.TreeItem {
  const item = infoItem(label, description, icon);
  item.command = { command, title: label };
  return item;
}

function infoItem(label: string, description: string, icon: string): vscode.TreeItem {
  const item = new vscode.TreeItem(label);
  item.description = description;
  item.tooltip = description || label;
  item.iconPath = new vscode.ThemeIcon(icon);
  return item;
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
