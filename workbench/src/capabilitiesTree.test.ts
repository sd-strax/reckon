// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import type { Session } from "./auth";
import { BackendRequestError, type BackendClient, type BackendStatus, type Capability, type Enablement } from "./backend";
import { CapabilitiesProvider, CapabilityItem } from "./capabilitiesTree";
import { resetVscodeFake } from "./test/vscode";

const ok: BackendStatus = { reachable: true, overall: "ready", apiVersion: 1, components: {} } as unknown as BackendStatus;

const cap = (verb: string, status: string): Capability => ({ verb, status, intent: verb + " intent", inputs: [], output: "" });

const enablement: Enablement = {
  adapters: [
    { name: "dns-adapter", class: "fixture", enabled: false, configFields: {}, requiredFields: [], supportable: true },
    { name: "plugin-adapter", class: "plugin", enabled: false, configFields: {}, requiredFields: [], supportable: false },
  ],
  verbs: [
    { verb: "query_dns", adapters: ["dns-adapter"], enabled: false, closableBy: ["dns-adapter"] },
    { verb: "query_plugin", adapters: ["plugin-adapter"], enabled: false, closableBy: ["plugin-adapter"] },
    { verb: "query_none", adapters: [], enabled: false, closableBy: [] },
  ],
};

function client(over: { status?: BackendStatus; reason?: string; list?: () => Promise<Capability[]>; enablement?: () => Promise<Enablement> } = {}): BackendClient {
  return {
    status: async () => over.status ?? ok,
    incompatibilityReason: () => over.reason,
    capabilities: {
      list: over.list ?? (async () => []),
      enablement: over.enablement ?? (async () => enablement),
    },
  } as unknown as BackendClient;
}

function tree(c: BackendClient, signedIn = true) {
  const view: { description?: string } = { description: "stale" };
  const provider = new CapabilitiesProvider(c, { signedIn } as unknown as Session);
  provider.describeIn(view);
  return { provider, view };
}

beforeEach(() => resetVscodeFake());

describe("CapabilitiesProvider (adr/0342)", () => {
  it("shows the same action rows as the other views when unreachable or signed out", async () => {
    const down = tree(client({ status: { ...ok, reachable: false }, reason: "backend not reachable" }));
    const [d] = await down.provider.getChildren();
    expect(d!.label).toBe("Backend not connected");
    expect(d!.command?.command).toBe("reckon.checkBackend");

    const out = tree(client(), false);
    const [s] = await out.provider.getChildren();
    expect(s!.label).toBe("Sign in to reckon");
    expect(s!.command?.command).toBe("reckon.signIn");
  });

  it("says so in one row when the layer is off, when nothing is configured, and when the read fails", async () => {
    const off = tree(client({ list: async () => { throw new BackendRequestError("GET /api/capabilities → 503", 503); } }));
    expect((await off.provider.getChildren()).map((i) => i.label)).toEqual(["Capability layer off"]);

    const none = tree(client());
    expect((await none.provider.getChildren()).map((i) => i.label)).toEqual(["No capabilities configured"]);
    expect(none.view.description).toBeUndefined();

    const failing = tree(client({ list: async () => { throw new Error("500 boom"); } }));
    const [f] = await failing.provider.getChildren();
    expect(f!.label).toBe("Could not read capabilities");
    expect(f!.command?.command).toBe("reckon.refreshCapabilities");
  });

  it("lists one row per verb, available then degraded then unavailable, each by verb, and counts what is available", async () => {
    const caps = [cap("query_dns", "unavailable"), cap("zeta", "available"), cap("beta", "degraded"), cap("alpha", "available")];
    const { provider, view } = tree(client({ list: async () => caps }));
    const items = await provider.getChildren();
    expect(items.map((i) => i.label)).toEqual(["alpha", "zeta", "beta", "query_dns"]);
    expect(items.map((i) => (i.iconPath as vscode.ThemeIcon).id)).toEqual(["pass-filled", "pass-filled", "warning", "circle-slash"]);
    expect(items[2]!.description).toBe("degraded");
    expect(view.description).toBe("2/4 available");
  });

  it("offers Set up… only on an unavailable verb a supportable, disabled adapter could serve", async () => {
    const caps = [cap("query_dns", "unavailable"), cap("query_plugin", "unavailable"), cap("query_none", "unavailable"), cap("alpha", "available")];
    const { provider } = tree(client({ list: async () => caps }));
    const items = await provider.getChildren() as CapabilityItem[];
    const byVerb = Object.fromEntries(items.map((i) => [i.verb, i]));
    expect(byVerb.query_dns!.contextValue).toBe("reckon.capability.setup");
    expect(byVerb.query_dns!.adapter).toBe("dns-adapter");
    expect(byVerb.query_plugin!.contextValue).toBe("reckon.capability");
    expect(byVerb.query_none!.contextValue).toBe("reckon.capability");
    expect(byVerb.alpha!.contextValue).toBe("reckon.capability");
  });

  it("keeps the newer count when an older read of the rows lands last", async () => {
    let answerOld!: (c: Capability[]) => void;
    let first = true;
    const { provider, view } = tree(client({
      list: () => {
        if (first) { first = false; return new Promise<Capability[]>((r) => { answerOld = r; }); }
        return Promise.resolve([cap("alpha", "available"), cap("beta", "available")]);
      },
    }));
    const older = provider.getChildren();
    await new Promise((r) => setTimeout(r, 0));
    await provider.getChildren();
    expect(view.description).toBe("2/2 available");
    answerOld([cap("alpha", "unavailable")]);
    await older;
    expect(view.description).toBe("2/2 available");
  });

  it("offers no Set up… when the enablement view cannot be read", async () => {
    const { provider } = tree(client({ list: async () => [cap("query_dns", "unavailable")], enablement: async () => { throw new Error("403"); } }));
    const [row] = await provider.getChildren();
    expect(row!.contextValue).toBe("reckon.capability");
  });
});
