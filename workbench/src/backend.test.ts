import { afterEach, describe, expect, it, vi } from "vitest";
import { BackendClient } from "./backend";

afterEach(() => vi.unstubAllGlobals());

const compatible = { overall: "ok", components: {}, api_version: 1 };

/** `fetch` with /status answering as a compatible backend, unless the test says otherwise. */
function stubFetch(answer: (url: string, init: { headers: Record<string, string> }) => Response | undefined, status: () => unknown = () => compatible) {
  vi.stubGlobal("fetch", async (url: string, init: { headers: Record<string, string> }) =>
    url.endsWith("/status") ? Response.json(status()) : answer(url, init));
}

/** A client whose every authenticated GET answers with `body`. */
function clientAnswering(body: unknown): BackendClient {
  stubFetch(() => Response.json(body));
  return new BackendClient(() => "http://reckon.test", async () => "token");
}

describe("BackendClient: action rows", () => {
  it("carries the reversibility the engine sends in its documented upper-case form", async () => {
    const client = clientAnswering({ actions: [
      { action_id: "a1", action_type: "account.disable", tier: "T2", status: "SUCCEEDED", reversibility: "reversible" },
      { action_id: "a2", action_type: "ioc.block", tier: "T2", status: "SUCCEEDED", reversibility: "best_effort" },
      { action_id: "a3", action_type: "ticket.comment", tier: "T1", status: "SUCCEEDED", reversibility: "irreversible" },
      { action_id: "a4", action_type: "host.isolate", tier: "T2", status: "REQUESTED" },
    ] });
    const rows = await client.actions.list("inv-1");
    expect(rows.map((r) => r.reversibility)).toEqual(["REVERSIBLE", "BEST_EFFORT", "IRREVERSIBLE", undefined]);
  });
});

describe("BackendClient: the action catalog", () => {
  it("reads each type's inverse from its descriptor", async () => {
    const client = clientAnswering({ action_types: [
      { descriptor: { action_type: "host.isolate", default_tier: "T2", name: "", category: "", inputs: [], reversibility: "reversible", reversible_by: "host.unisolate" }, status: "available", bindings: 1 },
      { descriptor: { action_type: "ticket.comment", default_tier: "T1", name: "", category: "", inputs: null, reversibility: "irreversible" }, status: "available", bindings: 1 },
    ] });
    expect(await client.actions.types()).toEqual([
      { actionType: "host.isolate", reversibleBy: "host.unisolate", tier: "T2", name: undefined, category: undefined, available: true, inputs: [] },
      { actionType: "ticket.comment", reversibleBy: undefined, tier: "T1", name: undefined, category: undefined, available: true, inputs: [] },
    ]);
  });
});

describe("BackendClient: a 401", () => {
  const ok = () => Response.json({ investigations: [] });

  function answering(...responses: Response[]) {
    const bearers: string[] = [];
    stubFetch((_url, init) => {
      bearers.push(init.headers.Authorization!);
      return responses.shift();
    });
    return bearers;
  }

  it("retries once with a renewed token, sending the refused token to be replaced", async () => {
    const bearers = answering(new Response("", { status: 401 }), ok());
    const renew = vi.fn(async (_rejected: string) => "fresh");

    await new BackendClient(() => "http://reckon.test", async () => "stale", renew).investigations.list();

    expect(renew.mock.calls).toEqual([["stale"]]);
    expect(bearers).toEqual(["Bearer stale", "Bearer fresh"]);
  });

  it("gives up after one retry", async () => {
    const bearers = answering(new Response("", { status: 401 }), new Response("", { status: 401 }));
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => "fresh");

    await expect(client.investigations.list()).rejects.toMatchObject({ status: 401 });

    expect(bearers).toHaveLength(2);
  });

  it("does not resend a write it could not renew for the same person", async () => {
    const bearers = answering(new Response("", { status: 401 }), ok());
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => { throw new Error("the session changed"); });

    await expect(client.actions.approve("a-1")).rejects.toThrow("the session changed");

    expect(bearers).toHaveLength(1);
  });

  it("retries the markdown export too", async () => {
    const bearers = answering(new Response("", { status: 401 }), new Response("# report"));
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => "fresh");

    expect(await client.investigations.exportMarkdown("inv-1")).toBe("# report");

    expect(bearers).toEqual(["Bearer stale", "Bearer fresh"]);
  });

  it("leaves other refusals alone", async () => {
    const bearers = answering(new Response("", { status: 403 }));
    const renew = vi.fn(async () => "fresh");

    await expect(new BackendClient(() => "http://reckon.test", async () => "t", renew).investigations.list()).rejects.toMatchObject({ status: 403 });

    expect(renew).not.toHaveBeenCalled();
    expect(bearers).toHaveLength(1);
  });
});

describe("BackendClient: reading a response", () => {
  it("names the field a response is missing", async () => {
    const client = clientAnswering({ investigations: [{ aggregate_id: "i", status: "ACTIVE", last_event_sequence: 1 }] });
    await expect(client.investigations.list()).rejects.toThrow("unexpected response from GET /api/investigations: investigations[0].title missing");
  });

  it("refuses a renamed list rather than showing it empty", async () => {
    const client = clientAnswering({ investigation_rows: [] });
    await expect(client.investigations.list()).rejects.toThrow("investigations missing");
  });

  it("shows a list the backend sent as null as empty", async () => {
    expect(await clientAnswering({ investigations: null }).investigations.list()).toEqual([]);
  });

  it("names a field of the wrong type", async () => {
    const client = clientAnswering({ subject: "s", preferred_username: 7, tenant_id: "t", roles: [] });
    await expect(client.me()).rejects.toThrow("preferred_username is not a string");
  });

  it("ignores a field it does not know", async () => {
    const client = clientAnswering({ subject: "s", preferred_username: "p", tenant_id: "t", roles: [], added_later: true });
    expect((await client.me()).username).toBe("p");
  });

  it("names a success that is not JSON", async () => {
    stubFetch(() => new Response("<html>", { status: 200 }));
    const client = new BackendClient(() => "http://reckon.test", async () => "token");
    await expect(client.me()).rejects.toThrow("unexpected response from GET /api/me: not JSON");
  });

  it("does not read the answer to a write that ignores it", async () => {
    stubFetch(() => new Response("", { status: 200 }));
    await expect(new BackendClient(() => "http://reckon.test", async () => "token").actions.commsAct("t-1", "ack")).resolves.toBeUndefined();
  });

});

describe("BackendClient: /status", () => {
  const answering = (body: Response) => {
    vi.stubGlobal("fetch", async () => body);
    return new BackendClient(() => "http://reckon.test");
  };

  it("takes a body with no api_version as from before the handshake", async () => {
    const client = answering(Response.json({ overall: "ok" }));
    const status = await client.status();
    expect(status).toMatchObject({ reachable: true, apiVersion: null, compatible: false });
    expect(client.incompatibilityReason(status)).toContain("predates the version handshake");
  });

  it("says a page that is not JSON is unreadable, not unreachable", async () => {
    const client = answering(new Response("<html>bad gateway</html>", { status: 502 }));
    const status = await client.status();
    expect(status.reachable).toBe(true);
    expect(client.incompatibilityReason(status)).toBe("unexpected response from /status: not JSON — align versions");
  });

  it("names the field that is wrong, and does not blame the handshake", async () => {
    const client = answering(Response.json({ overall: "ok", components: {}, api_version: "1" }));
    const status = await client.status();
    expect(status.reachable).toBe(true);
    expect(client.incompatibilityReason(status)).toBe("unexpected response from /status: api_version is not a number — align versions");
  });

  it("is unreachable only when nothing answered", async () => {
    vi.stubGlobal("fetch", async () => { throw new TypeError("fetch failed"); });
    const client = new BackendClient(() => "http://reckon.test");
    const status = await client.status();
    expect(status.reachable).toBe(false);
    expect(client.incompatibilityReason(status)).toContain("not reachable");
  });
});

describe("BackendClient: an incompatible backend", () => {
  function counting(status: () => unknown) {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async (url: string) => {
      calls.push(new URL(url).pathname);
      return Response.json(url.endsWith("/status") ? status() : { queue: "", cases: [], title: "t", outcome: "created" });
    });
    return { calls, client: new BackendClient(() => "http://reckon.test", async () => "token") };
  }
  const old = { overall: "ok", components: {}, api_version: 9 };
  afterEach(() => vi.restoreAllMocks());

  it("is not written to, whichever surface asks", async () => {
    const { client, calls } = counting(() => old);

    await expect(client.knowledge.importMarkdown("a.md", "# a")).rejects.toThrow("backend API v9 unsupported");

    expect(calls).toEqual(["/status"]);
  });

  it("is not read from either", async () => {
    const { client, calls } = counting(() => old);

    await expect(client.cases.queue()).rejects.toThrow("backend API v9 unsupported");

    expect(calls).toEqual(["/status"]);
  });

  it("gates the markdown export too", async () => {
    const { client, calls } = counting(() => old);

    await expect(client.investigations.exportMarkdown("inv-1")).rejects.toThrow("backend API v9 unsupported");

    expect(calls).toEqual(["/status"]);
  });

  it("asks once for parallel calls, and holds a good answer for a while", async () => {
    const { client, calls } = counting(() => compatible);

    await Promise.all([client.cases.queue(), client.cases.queue(), client.cases.queue()]);
    await client.cases.queue();

    expect(calls.filter((c) => c === "/status")).toHaveLength(1);
  });

  it("asks again once a good answer has aged, and catches an upgrade", async () => {
    let status: unknown = compatible;
    const { client } = counting(() => status);
    const now = vi.spyOn(Date, "now");
    now.mockReturnValue(1_000);
    await client.cases.queue();

    status = old;
    now.mockReturnValue(1_000 + 31_000);

    await expect(client.cases.queue()).rejects.toThrow("backend API v9 unsupported");
  });

  it("keeps refusing: a bad answer never lets the next call through", async () => {
    const { client, calls } = counting(() => old);

    await expect(client.cases.queue()).rejects.toThrow("unsupported");
    await expect(client.cases.queue()).rejects.toThrow("unsupported");

    expect(calls).toEqual(["/status", "/status"]);
  });

  it("does not take a panel's bad handshake as a good one", async () => {
    const { client, calls } = counting(() => old);
    await client.status();

    await expect(client.cases.queue()).rejects.toThrow("unsupported");

    expect(calls).not.toContain("/api/cases");
  });

  it("does not hold a bad answer: a backend put right is used at once", async () => {
    let status: unknown = old;
    const { client } = counting(() => status);
    await expect(client.cases.queue()).rejects.toThrow("unsupported");

    status = compatible;

    await expect(client.cases.queue()).resolves.toEqual({ queue: "", cases: [] });
  });

  describe("when the backend URL changes", () => {
    /** Two backends behind one fetch, by host; `url` is what the setting says. */
    function twoBackends(b: unknown) {
      const calls: string[] = [];
      vi.stubGlobal("fetch", async (raw: string) => {
        const url = new URL(raw);
        calls.push(url.host + url.pathname);
        if (url.pathname !== "/status") return Response.json({ title: "t", outcome: "created", queue: "", cases: [] });
        return Response.json(url.host === "a.test" ? compatible : b);
      });
      const setting = { url: "http://a.test" };
      return { calls, setting, client: new BackendClient(() => setting.url, async () => "token") };
    }

    it("checks the new backend, and writes nothing to an incompatible one", async () => {
      const { client, calls, setting } = twoBackends(old);
      await client.knowledge.importMarkdown("a.md", "# a");

      setting.url = "http://b.test";

      await expect(client.knowledge.importMarkdown("b.md", "# b")).rejects.toThrow("backend API v9 unsupported");
      expect(calls).not.toContain("b.test/api/sops/import_markdown");
    });

    it("trusts the new backend only after checking it", async () => {
      const { client, calls, setting } = twoBackends(compatible);
      await client.knowledge.importMarkdown("a.md", "# a");

      setting.url = "http://b.test";
      await client.knowledge.importMarkdown("b.md", "# b");

      expect(calls.slice(-2)).toEqual(["b.test/status", "b.test/api/sops/import_markdown"]);
    });

    it("sends the call to the backend it checked, if the setting moves during the check", async () => {
      let checked!: () => void;
      const calls: string[] = [];
      vi.stubGlobal("fetch", async (raw: string) => {
        const url = new URL(raw);
        calls.push(url.host + url.pathname);
        if (url.pathname === "/status") {
          await new Promise<void>((r) => { checked = r; });
          return Response.json(compatible);
        }
        return Response.json({ title: "t", outcome: "created" });
      });
      const setting = { url: "http://a.test" };
      const client = new BackendClient(() => setting.url, async () => "token");
      const write = client.knowledge.importMarkdown("a.md", "# a");
      await vi.waitFor(() => expect(checked).toBeDefined());

      setting.url = "http://b.test";
      checked();
      await write;

      expect(calls).toEqual(["a.test/status", "a.test/api/sops/import_markdown"]);
    });

    it("retries a 401 at the backend it checked", async () => {
      const calls: string[] = [];
      const setting = { url: "http://a.test" };
      vi.stubGlobal("fetch", async (raw: string) => {
        const url = new URL(raw);
        calls.push(url.host + url.pathname);
        if (url.pathname === "/status") return Response.json(compatible);
        return calls.filter((c) => c.endsWith("/import_markdown")).length === 1
          ? new Response("", { status: 401 })
          : Response.json({ title: "t", outcome: "created" });
      });
      const renew = async () => { setting.url = "http://b.test"; return "fresh"; };

      await new BackendClient(() => setting.url, async () => "token", renew).knowledge.importMarkdown("a.md", "# a");

      expect(calls.filter((c) => c.endsWith("/import_markdown"))).toEqual(["a.test/api/sops/import_markdown", "a.test/api/sops/import_markdown"]);
    });

    it("keeps backend A's good answer through a bad answer from B", async () => {
      const { client, calls, setting } = twoBackends(old);
      await client.cases.queue();
      setting.url = "http://b.test";
      await expect(client.cases.queue()).rejects.toThrow("unsupported");

      setting.url = "http://a.test";
      calls.length = 0;
      await client.cases.queue();

      expect(calls).toEqual(["a.test/api/cases"]);
    });
  });

  it("does not gate the public auth config", async () => {
    const { calls } = counting(() => old);
    vi.stubGlobal("fetch", async (url: string) => { calls.push(new URL(url).pathname); return Response.json({ issuer: "i", client_id: "c" }); });

    await new BackendClient(() => "http://reckon.test").authConfig();

    expect(calls).toEqual(["/api/auth-config"]);
  });
});
