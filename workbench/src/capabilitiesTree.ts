// The Capabilities view (adr/0342): the capability layer's health, third in
// the reckon container after Investigations and Cases. One row per verb —
// available, then degraded, then unavailable, each sorted by verb — and the
// view's description counts what is available. An unavailable verb that an
// installed, supportable, disabled adapter could serve offers Set up…, which
// runs the native enablement flow (enablementFlow.ts); nothing else here
// writes.
import * as vscode from "vscode";
import { BackendClient, BackendRequestError, Capability, Enablement } from "./backend";
import { Session } from "./auth";
import { setUpAdapter } from "./enablement";
import { actionItem, gateItem, infoItem } from "./treeItems";
import { errText } from "./util";

const STATUS_ORDER: Record<string, number> = { available: 0, degraded: 1, unavailable: 2 };

/** A status icon per verb: the word is in the description too, so colour is never the only cue. */
const STATUS_ICON: Record<string, [string, string]> = {
  available: ["pass-filled", "charts.green"],
  degraded: ["warning", "charts.yellow"],
  unavailable: ["circle-slash", "charts.red"],
};

/** The verb rows in display order. */
export function orderCapabilities(caps: Capability[]): Capability[] {
  return caps.slice().sort((a, b) => (STATUS_ORDER[a.status] ?? 3) - (STATUS_ORDER[b.status] ?? 3) || a.verb.localeCompare(b.verb));
}

/** One verb row; carries the adapter its Set up… enables. */
export class CapabilityItem extends vscode.TreeItem {
  verb = "";
  adapter = "";
}

export class CapabilitiesProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private readonly emitter = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.emitter.event;
  // The view whose description counts the available verbs (attached once it exists).
  private view: { description?: string } | undefined;
  // Bumped by every read of the rows: only the newest read sets the
  // description, so the count always matches the rows on screen.
  private generation = 0;

  constructor(
    private readonly client: BackendClient,
    private readonly session: Session,
  ) {}

  /** The view's description is "N/M available", or nothing when there are no verb rows. */
  describeIn(view: { description?: string }): void {
    this.view = view;
  }

  private describe(generation: number, description: string | undefined): void {
    if (this.view && generation === this.generation) this.view.description = description;
  }

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
    const generation = ++this.generation;
    this.describe(generation, undefined);
    const gate = await gateItem(this.client, this.session);
    if (gate) {
      return gate;
    }
    let caps: Capability[];
    let enablement: Enablement | null;
    try {
      [caps, enablement] = await Promise.all([
        this.client.capabilities.list(),
        // Absence is honest: without the enablement view no row offers Set up….
        this.client.capabilities.enablement().catch(() => null),
      ]);
    } catch (err) {
      // A 503 is the capability layer switched off, as the document reads it.
      if (err instanceof BackendRequestError && err.status === 503) {
        return [infoItem("Capability layer off", "the tenant runs without the capability layer", "circle-slash")];
      }
      return [actionItem("Could not read capabilities", errText(err), "warning", "reckon.refreshCapabilities")];
    }
    if (caps.length === 0) {
      return [infoItem("No capabilities configured", "no verb is bound in the tenant capability config", "info")];
    }
    const available = caps.filter((c) => c.status === "available").length;
    this.describe(generation, `${available}/${caps.length} available`);
    return orderCapabilities(caps).map((c) => verbItem(c, setUpAdapter(c.verb, c.status, enablement)));
  }
}

function verbItem(c: Capability, adapter: string | null): vscode.TreeItem {
  const item = new CapabilityItem(c.verb);
  item.verb = c.verb;
  const [icon, color] = STATUS_ICON[c.status] ?? ["circle-outline", "descriptionForeground"];
  item.iconPath = new vscode.ThemeIcon(icon, new vscode.ThemeColor(color));
  item.description = c.status;
  const lines = [`${c.verb} — ${c.status}`];
  if (c.intent) lines.push(c.intent);
  if (adapter) {
    item.adapter = adapter;
    item.contextValue = "reckon.capability.setup";
    lines.push(`${adapter} is installed but not enabled — set it up to serve ${c.verb}`);
  } else {
    item.contextValue = "reckon.capability";
  }
  item.tooltip = lines.join("\n");
  return item;
}
