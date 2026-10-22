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

/**
 * How the queue is ordered. "priority": the SoR's priority, then oldest first
 * within it — how a queue is worked. "age": oldest first regardless — what has
 * been sitting longest. One toggle, remembered; never an order reckon invents
 * (a relevance score would annotate rows, not reorder the queue).
 */
export type CasesSort = "priority" | "age";

const SORT_KEY = "reckon.casesSort";

export class CasesProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private readonly emitter = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.emitter.event;
  private sortMode: CasesSort;

  constructor(
    private readonly client: BackendClient,
    private readonly session: Session,
    private readonly state: vscode.Memento,
  ) {
    this.sortMode = state.get<CasesSort>(SORT_KEY) === "age" ? "age" : "priority";
    void vscode.commands.executeCommand("setContext", SORT_KEY, this.sortMode);
  }

  refresh(): void {
    this.emitter.fire();
  }

  get sort(): CasesSort {
    return this.sortMode;
  }

  /** Switch the order; the view-title button reflects it via the context key. */
  setSort(mode: CasesSort): void {
    this.sortMode = mode;
    void this.state.update(SORT_KEY, mode);
    void vscode.commands.executeCommand("setContext", SORT_KEY, mode);
    this.refresh();
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
    for (const r of sortRows(rows, this.sortMode)) {
      items.push(rowItem(r));
    }
    return items;
  }
}

/** Stable sort by the chosen order; a row missing the key sorts last. */
function sortRows(rows: CaseRow[], mode: CasesSort): CaseRow[] {
  const rank = (r: CaseRow): number => {
    const m = /^\s*(\d)/.exec(r.priority ?? "");
    return m ? Number(m[1]) : 9;
  };
  const opened = (r: CaseRow): number => {
    const t = r.openedAt ? Date.parse(r.openedAt) : NaN;
    return Number.isFinite(t) ? t : Number.POSITIVE_INFINITY;
  };
  return rows
    .map((r, i) => ({ r, i }))
    .sort((a, b) => {
      const byPriority = rank(a.r) - rank(b.r);
      const byAge = opened(a.r) - opened(b.r);
      const primary = mode === "priority" ? byPriority : byAge;
      const secondary = mode === "priority" ? byAge : byPriority;
      return primary || secondary || a.i - b.i;
    })
    .map((x) => x.r);
}

function rowItem(r: CaseRow): vscode.TreeItem {
  const item = new vscode.TreeItem(r.title || r.number);
  // Priority first: it is the sort key and the ring's meaning, spelled out so
  // the colour is never the only cue.
  const cues: string[] = [];
  const p = shortPriority(r.priority);
  if (p) cues.push(p);
  cues.push(r.number);
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
