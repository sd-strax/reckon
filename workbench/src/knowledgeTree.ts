// The Knowledge view in the sidebar (adr/0344): the organization's procedures
// and past cases, third in the reckon container after Investigations and
// Cases. Two groups, each with its count; a row opens its item in the
// Knowledge view (adr/0107), and while an investigation document is the
// active editor a row offers "Ask reckon to use this", which stages the
// request in that investigation's composer. Search by meaning is the
// Knowledge view's query bar; this tree only gets the analyst there.
import * as vscode from "vscode";
import { BackendClient } from "./backend";
import { Session } from "./auth";
import type { KnowledgeCue, LibraryItem } from "./protocol";
import { citedInWords } from "./knowledgeWords";
import { actionItem, gateItem, infoItem } from "./treeItems";
import { errText } from "./util";

/** One procedure or past-case row; carries what the open and the ask act on. */
export class KnowledgeItem extends vscode.TreeItem {
  constructor(readonly item: LibraryItem) {
    super(item.title);
  }
}

/** A group node: its rows are the ones read with it, so a group never shows another read's rows. */
export class KnowledgeGroup extends vscode.TreeItem {
  constructor(id: string, label: string, readonly rows: vscode.TreeItem[]) {
    super(label, vscode.TreeItemCollapsibleState.Expanded);
    // Stable across reads, so the group's collapse state survives a count change.
    this.id = id;
    this.contextValue = "reckon.knowledgeGroup";
  }
}

/** The cue that opens the Knowledge view on a row's item. */
export function knowledgeShow(k: LibraryItem): KnowledgeCue {
  return { type: "knowledge.show", target: k.kind === "sop" ? { kind: "sop", sopId: k.ref } : { kind: "case", investigationRef: k.ref } };
}

export const PROCEDURES_ID = "knowledge.procedures";
export const CASES_ID = "knowledge.cases";

/** The command a row runs: the Knowledge view, opened on that item. */
export const OPEN_ITEM_COMMAND = "reckon.openKnowledgeItem";

export class KnowledgeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private readonly emitter = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.emitter.event;
  // Bumped by every read of the groups. A read that a newer one overtook
  // answers with the newer read's groups, so counts that were already
  // replaced never come back on screen.
  private generation = 0;
  private latest: Promise<vscode.TreeItem[]> = Promise.resolve([]);

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
      return element instanceof KnowledgeGroup ? element.rows : [];
    }
    let generation = ++this.generation;
    this.latest = this.readGroups();
    let items = await this.latest;
    while (generation !== this.generation) {
      generation = this.generation;
      items = await this.latest;
    }
    return items;
  }

  private async readGroups(): Promise<vscode.TreeItem[]> {
    const gate = await gateItem(this.client, this.session);
    if (gate) {
      return gate;
    }
    const [sops, cases] = await Promise.all([
      this.client.knowledge.procedures().then((rows) => ({ rows }), (err: unknown) => ({ error: errText(err) })),
      this.client.knowledge.caseList().then((list) => list, (err: unknown) => ({ error: errText(err) })),
    ]);
    return [
      "error" in sops
        ? unreadGroup(PROCEDURES_ID, "Procedures", sops.error)
        : new KnowledgeGroup(PROCEDURES_ID, `Procedures (${sops.rows.length})`, sops.rows.length
          ? sops.rows.map(procedureRow)
          : [actionItem("No procedures yet", "import some", "info", "reckon.importSOP")]),
      "error" in cases
        ? unreadGroup(CASES_ID, "Past cases", cases.error)
        : new KnowledgeGroup(CASES_ID, `Past cases (${pastCaseCount(cases.items.length, cases.total)})`, cases.items.length
          ? cases.items.map(pastCaseRow)
          : [infoItem("No concluded investigations yet", "", "info")]),
    ];
  }
}

/** "N", or "N of M" when the corpus holds more past cases than the page. */
export function pastCaseCount(shown: number, total: number): string {
  return total > shown ? `${shown} of ${total}` : String(shown);
}

function procedureRow(k: LibraryItem): vscode.TreeItem {
  return knowledgeRow(k, "book");
}

function pastCaseRow(k: LibraryItem): vscode.TreeItem {
  return knowledgeRow(k, "history");
}

/** A row: the title, the client's detail line (tags, or disposition and concluded day), the cited-in reading in the tooltip. */
function knowledgeRow(k: LibraryItem, icon: string): vscode.TreeItem {
  const item = new KnowledgeItem(k);
  item.description = k.detail;
  item.tooltip = [k.title, k.detail, k.citedIn !== undefined ? citedInWords(k.citedIn) : ""].filter(Boolean).join("\n");
  item.iconPath = new vscode.ThemeIcon(icon);
  item.contextValue = "reckon.knowledgeItem";
  item.command = { command: OPEN_ITEM_COMMAND, title: "Open in the Knowledge view", arguments: [item] };
  return item;
}

/** A group whose read failed: it says so, and a click reads again. */
function unreadGroup(id: string, label: string, error: string): vscode.TreeItem {
  const item = actionItem(label, "could not be read", "warning", "reckon.refreshKnowledge");
  item.id = id;
  item.tooltip = error;
  return item;
}
