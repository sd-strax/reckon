// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import type { Session } from "./auth";
import type { BackendClient, BackendStatus } from "./backend";
import { CapabilitiesProvider } from "./capabilitiesTree";
import { CasesProvider } from "./casesTree";
import { InvestigationsProvider } from "./investigationsTree";
import { KnowledgeProvider } from "./knowledgeTree";
import type { SeenTracker } from "./seenTracker";
import { resetVscodeFake } from "./test/vscode";
import { gateItem } from "./treeItems";

const ok = { reachable: true, overall: "ready", apiVersion: 1, components: {} } as unknown as BackendStatus;
const client = (status: BackendStatus, reason?: string) => ({ status: async () => status, incompatibilityReason: () => reason }) as unknown as BackendClient;
const session = (signedIn: boolean) => ({ signedIn }) as unknown as Session;
const memento = { get: () => undefined, update: async () => undefined } as never;

beforeEach(() => resetVscodeFake());

describe("the sidebar trees' shared rows (adr/0344)", () => {
  it("gates on the backend, then the session, and lets a signed-in tree read", async () => {
    expect((await gateItem(client({ ...ok, reachable: false }, "backend not reachable"), session(true)))![0]!.label).toBe("Backend not connected");
    expect((await gateItem(client(ok, "API v9 unsupported"), session(true)))![0]!.label).toBe("Backend incompatible");
    // Signed out, no tree carries a sign-in row: the Account view's one button does.
    expect(await gateItem(client(ok), session(false))).toEqual([]);
    expect(await gateItem(client(ok), session(true))).toBeNull();
  });

  it("no view shows a sign-in row, and every view words the backend row the same", async () => {
    const rows = async (c: BackendClient, s: Session) => Promise.all([
      new InvestigationsProvider(c, s, {} as SeenTracker).getChildren(),
      new CasesProvider(c, s, memento).getChildren(),
      new CapabilitiesProvider(c, s).getChildren(),
      new KnowledgeProvider(c, s).getChildren(),
    ]);
    const signedOut = await rows(client(ok), session(false));
    for (const r of signedOut) expect(r).toEqual([]);
    const down = await rows(client({ ...ok, reachable: false }, "backend not reachable"), session(true));
    const gated = [down[0]!, down[2]!, down[3]!]; // the Cases view reads only after sign-in
    for (const [row] of gated) expect(row).toEqual(gated[0]![0]);
    expect(gated[0]![0]!.tooltip).toBe("backend not reachable");
  });
});
