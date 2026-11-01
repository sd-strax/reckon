// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import type { Session } from "./auth";
import type { BackendClient, BackendStatus, InvestigationSummary } from "./backend";
import { GroupItem, InvestigationRowItem, InvestigationsProvider } from "./investigationsTree";
import { SeenTracker } from "./seenTracker";
import { resetVscodeFake } from "./test/vscode";

const ok: BackendStatus = { reachable: true, overall: "ready", apiVersion: 1, components: {} } as unknown as BackendStatus;

function row(over: Partial<InvestigationSummary>): InvestigationSummary {
  return {
    id: "inv-" + over.title, title: "t", state: "ACTIVE", pendingActions: 0, lastEventSequence: 1, updatedAt: "2024-01-01T00:00:00Z",
    ...over,
  } as InvestigationSummary;
}

function client(over: { status?: BackendStatus; reason?: string; investigations?: () => Promise<InvestigationSummary[]> } = {}): BackendClient {
  return {
    status: async () => over.status ?? ok,
    incompatibilityReason: () => over.reason,
    investigations: over.investigations ?? (async () => []),
  } as unknown as BackendClient;
}

function memento() {
  const data = new Map<string, unknown>();
  return {
    get: <T>(key: string, fallback?: T) => (data.has(key) ? data.get(key) as T : fallback),
    update: async (key: string, value: unknown) => { if (value === undefined) data.delete(key); else data.set(key, value); },
  } as unknown as vscode.Memento;
}

function tree(c: BackendClient, signedIn = true) {
  const seen = new SeenTracker(memento(), () => "alice");
  const provider = new InvestigationsProvider(c, { signedIn } as unknown as Session, seen);
  return { provider, seen };
}

const labels = (items: vscode.TreeItem[]) => items.map((i) => String(i.label));

beforeEach(() => resetVscodeFake());

describe("InvestigationsProvider (ui/06)", () => {
  it("fails closed on an unreachable or incompatible backend, before any read", async () => {
    const down = tree(client({ status: { ...ok, reachable: false }, reason: "backend not reachable" }));
    const [item] = await down.provider.getChildren();
    expect(item.label).toBe("Backend not connected");
    expect(item.command?.command).toBe("reckon.checkBackend");

    const old = tree(client({ reason: "API v9 is not supported" }));
    expect(labels(await old.provider.getChildren())).toEqual(["Backend incompatible"]);
  });

  it("offers sign-in when signed out, and says so when the list cannot be read", async () => {
    const out = tree(client(), false);
    const [item] = await out.provider.getChildren();
    expect(item.label).toBe("Sign in to reckon");
    expect(item.command?.command).toBe("reckon.signIn");

    const failing = tree(client({ investigations: async () => { throw new Error("401 token expired"); } }));
    const [err] = await failing.provider.getChildren();
    expect(err.label).toBe("Could not load investigations");
    expect(err.description).toBe("401 token expired");
    expect(err.command?.command).toBe("reckon.refreshInvestigations");
  });

  it("groups rows by status in display order, rolls up pending approvals, and keeps unknown states visible", async () => {
    const rows = [
      row({ title: "a", state: "CONCLUDED" }),
      row({ title: "b", state: "ACTIVE", pendingActions: 2 }),
      row({ title: "c", state: "ACTIVE", pendingActions: 1 }),
      row({ title: "d", state: "SOMETHING_NEW" }),
    ];
    const { provider } = tree(client({ investigations: async () => rows }));
    const items = await provider.getChildren();
    expect(labels(items)).toEqual(["Active (2)", "Concluded (1)", "d"]);
    expect(items[0]).toBeInstanceOf(GroupItem);
    expect(items[0].description).toBe("3 awaiting approval");
    expect(items[0].collapsibleState).toBe(vscode.TreeItemCollapsibleState.Expanded);
    expect(items[1].collapsibleState).toBe(vscode.TreeItemCollapsibleState.Collapsed);
    expect(items[2]).toBeInstanceOf(InvestigationRowItem);
  });

  it("orders a group needs-me-first: soonest expiry, then pending, then unseen, then recency", async () => {
    const soon = new Date(Date.now() + 5 * 60_000).toISOString();
    const later = new Date(Date.now() + 60 * 60_000).toISOString();
    const rows = [
      row({ id: "recent", title: "recent", updatedAt: "2024-06-01T00:00:00Z" }),
      row({ id: "unseen", title: "unseen", lastEventSequence: 9, updatedAt: "2024-01-01T00:00:00Z" }),
      row({ id: "pending", title: "pending", pendingActions: 1 }),
      row({ id: "later", title: "later", nearestExpiry: later }),
      row({ id: "soon", title: "soon", nearestExpiry: soon, pendingActions: 1 }),
    ];
    const { provider, seen } = tree(client({ investigations: async () => rows }));
    await seen.set("unseen", 3); // the thread grew past what this person saw
    await seen.set("recent", 1); // caught up

    const [group] = await provider.getChildren();
    const ordered = await provider.getChildren(group);
    expect(ordered.map((i) => (i as InvestigationRowItem).invId)).toEqual(["soon", "later", "pending", "unseen", "recent"]);
    expect(ordered[0].description).toMatch(/^⏳ \dm · 1 pending$/);
    expect(ordered[3].description).toBe("● new");
    expect((ordered[0].iconPath as vscode.ThemeIcon).id).toBe("bell-dot");
    expect((ordered[4].iconPath as vscode.ThemeIcon).id).toBe("circle-filled");
    expect(ordered[4].command).toEqual({ command: "reckon.openInvestigation", title: "Open Investigation", arguments: ["recent", "recent"] });
  });
});
