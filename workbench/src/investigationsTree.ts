// The Investigations view (ui/06): a triage queue, not a file listing. Rows
// are the backend's investigations grouped by status and ordered
// needs-me-first; the unseen cue compares each row against what this person
// last had open (SeenTracker). Read-only: a click opens the document. With
// Cases and Capabilities beside it, this is the work queue (adr/0342): an open
// row carries its stage step, the cue the document's column used to carry.
import * as vscode from "vscode";
import { BackendClient, InvestigationSummary } from "./backend";
import { Session } from "./auth";
import { SeenTracker } from "./seenTracker";
import { STEP_LABELS, rowStep } from "./stage";
import { actionItem, gateItem } from "./treeItems";
import { errText } from "./util";

/** Display order + collapsed defaults for the status groups (ui/06). */
const TREE_GROUPS: { status: string; label: string; collapsed: boolean }[] = [
  { status: "ACTIVE", label: "Active", collapsed: false },
  { status: "PAUSED", label: "Paused", collapsed: false },
  { status: "DRAFT", label: "Draft", collapsed: false },
  { status: "CONCLUDED", label: "Concluded", collapsed: true },
  { status: "ARCHIVED", label: "Archived", collapsed: true },
];

/** Status dot colors (ui/06): themed chart colors, not hardcoded hex. */
const STATUS_COLOR: Record<string, string> = {
  ACTIVE: "charts.blue",
  PAUSED: "charts.yellow",
  DRAFT: "descriptionForeground",
  CONCLUDED: "charts.purple",
  ARCHIVED: "disabledForeground",
};

/**
 * The Investigations view — a triage queue, not a file listing (ui/06). Three
 * top-level states, each a distinct honest surface: backend incompatible or
 * unreachable (fail closed with a diagnostic), signed out (a sign-in
 * affordance), or signed in — status groups whose rows are ordered
 * needs-me-first: soonest-expiring approval, then pending approvals, then
 * unseen changes, then recency.
 */
export class InvestigationsProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private readonly emitter = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.emitter.event;
  private rows: InvestigationSummary[] = [];

  constructor(
    private readonly client: BackendClient,
    private readonly session: Session,
    private readonly seen: SeenTracker,
  ) {}

  refresh(): void {
    this.emitter.fire();
  }

  getTreeItem(element: vscode.TreeItem): vscode.TreeItem {
    return element;
  }

  async getChildren(element?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
    if (element) {
      const group = (element as GroupItem).groupStatus;
      return group ? this.groupRows(group) : [];
    }

    const gate = await gateItem(this.client, this.session);
    if (gate) {
      return [gate];
    }

    try {
      this.rows = await this.client.investigations.list();
    } catch (err) {
      // A 401 here means the token went stale under us — surface it, don't hang.
      return [actionItem("Could not load investigations", errText(err), "warning", "reckon.refreshInvestigations")];
    }
    if (this.rows.length === 0) {
      const empty = new vscode.TreeItem("No investigations yet");
      empty.description = "seed one to begin";
      empty.iconPath = new vscode.ThemeIcon("search");
      return [empty];
    }

    const items: vscode.TreeItem[] = [];
    for (const g of TREE_GROUPS) {
      const inGroup = this.rows.filter((r) => r.state.toUpperCase() === g.status);
      if (!inGroup.length) {
        continue;
      }
      // Group-level rollups so a collapsed group still signals what's inside.
      const pending = inGroup.reduce((n, r) => n + r.pendingActions, 0);
      const item = new GroupItem(
        `${g.label} (${inGroup.length})`,
        g.status,
        g.collapsed ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.Expanded,
      );
      if (pending > 0) {
        item.description = `${pending} awaiting approval`;
      }
      items.push(item);
    }
    // Anything in a state outside the known groups still shows (honesty over taxonomy).
    const known = new Set(TREE_GROUPS.map((g) => g.status));
    for (const r of this.rows.filter((x) => !known.has(x.state.toUpperCase()))) {
      items.push(this.rowItem(r));
    }
    return items;
  }

  /** The rows of one status group, needs-me-first (ui/06). */
  private groupRows(status: string): vscode.TreeItem[] {
    const rows = this.rows.filter((r) => r.state.toUpperCase() === status);
    rows.sort((a, b) => {
      // 1. Soonest approval expiry first (an open countdown outranks all).
      const ax = a.nearestExpiry ? Date.parse(a.nearestExpiry) : Infinity;
      const bx = b.nearestExpiry ? Date.parse(b.nearestExpiry) : Infinity;
      if (ax !== bx) return ax - bx;
      // 2. More pending approvals first.
      if (a.pendingActions !== b.pendingActions) return b.pendingActions - a.pendingActions;
      // 3. Unseen changes before caught-up.
      const au = this.unseen(a) ? 0 : 1;
      const bu = this.unseen(b) ? 0 : 1;
      if (au !== bu) return au - bu;
      // 4. Recency.
      return (b.updatedAt ? Date.parse(b.updatedAt) : 0) - (a.updatedAt ? Date.parse(a.updatedAt) : 0);
    });
    return rows.map((r) => this.rowItem(r));
  }

  /** Unseen = the thread grew past what this analyst last had open here. */
  private unseen(r: InvestigationSummary): boolean {
    const last = this.seen.get(r.id);
    return last > 0 && r.lastEventSequence > last;
  }

  private rowItem(r: InvestigationSummary): vscode.TreeItem {
    const item = new InvestigationRowItem(r.title);
    item.invId = r.id;
    item.invTitle = r.title;
    item.contextValue = "reckon.investigation";
    const cues: string[] = [];
    // The stage step leads, named as the document's progress strip names it.
    const step = rowStep(r);
    if (step) {
      cues.push(STEP_LABELS[step]);
    }
    if (r.nearestExpiry) {
      const left = Date.parse(r.nearestExpiry) - Date.now();
      cues.push(left <= 0 ? "⏳ expired" : `⏳ ${fmtShort(left)}`);
    }
    if (r.pendingActions > 0) {
      cues.push(`${r.pendingActions} pending`);
    }
    if (this.unseen(r)) {
      cues.push("● new");
    }
    if (r.seedSummary) {
      cues.push(r.seedSummary);
    }
    item.description = cues.join(" · ");
    item.tooltip = `${r.title}\n${r.id}\n${r.state}${r.seedSummary ? "\n" + r.seedSummary : ""}`;
    const color = STATUS_COLOR[r.state.toUpperCase()] ?? "descriptionForeground";
    // An open approval window trumps the status dot — that row needs a human.
    item.iconPath = r.pendingActions > 0
      ? new vscode.ThemeIcon("bell-dot", new vscode.ThemeColor("charts.orange"))
      : new vscode.ThemeIcon("circle-filled", new vscode.ThemeColor(color));
    item.command = {
      command: "reckon.openInvestigation",
      title: "Open Investigation",
      arguments: [r.id, r.title],
    };
    return item;
  }
}

/** One investigation row; carries its id/title so the context menu (rename)
 *  can act on it without re-deriving from the label. */
export class InvestigationRowItem extends vscode.TreeItem {
  invId = "";
  invTitle = "";
}

/** A status group node; groupStatus keys getChildren back into the rows. */
export class GroupItem extends vscode.TreeItem {
  constructor(
    label: string,
    readonly groupStatus: string,
    state: vscode.TreeItemCollapsibleState,
  ) {
    super(label, state);
    // Stable across reads, so the group's collapse state survives a count change.
    this.id = `investigations.${groupStatus}`;
    this.contextValue = "reckon.group";
  }
}

/** Compact remaining-time label for the tree's ⏳ cue. */
function fmtShort(ms: number): string {
  const m = Math.floor(ms / 60_000);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return h < 48 ? `${h}h` : `${Math.floor(h / 24)}d`;
}
