import { afterEach, describe, expect, it, vi } from "vitest";
import { BackendClient } from "./backend";

afterEach(() => vi.unstubAllGlobals());

/** A client whose every authenticated GET answers with `body`. */
function clientAnswering(body: unknown): BackendClient {
  vi.stubGlobal("fetch", async () => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }));
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
    const rows = await client.actions("inv-1");
    expect(rows.map((r) => r.reversibility)).toEqual(["REVERSIBLE", "BEST_EFFORT", "IRREVERSIBLE", undefined]);
  });
});

describe("BackendClient: the action catalog", () => {
  it("reads each type's inverse from its descriptor", async () => {
    const client = clientAnswering({ action_types: [
      { descriptor: { action_type: "host.isolate", default_tier: "T2", name: "", category: "", inputs: [], reversibility: "reversible", reversible_by: "host.unisolate" }, status: "available", bindings: 1 },
      { descriptor: { action_type: "ticket.comment", default_tier: "T1", name: "", category: "", inputs: null, reversibility: "irreversible" }, status: "available", bindings: 1 },
    ] });
    expect(await client.actionTypes()).toEqual([
      { actionType: "host.isolate", reversibleBy: "host.unisolate", tier: "T2", name: undefined, category: undefined, available: true, inputs: [] },
      { actionType: "ticket.comment", reversibleBy: undefined, tier: "T1", name: undefined, category: undefined, available: true, inputs: [] },
    ]);
  });
});

describe("BackendClient: a 401", () => {
  const ok = () => Response.json({ investigations: [] });

  function answering(...responses: Response[]) {
    const bearers: string[] = [];
    vi.stubGlobal("fetch", async (_url: string, init: { headers: Record<string, string> }) => {
      bearers.push(init.headers.Authorization);
      return responses.shift();
    });
    return bearers;
  }

  it("retries once with a renewed token, sending the refused token to be replaced", async () => {
    const bearers = answering(new Response("", { status: 401 }), ok());
    const renew = vi.fn(async (_rejected: string) => "fresh");

    await new BackendClient(() => "http://reckon.test", async () => "stale", renew).investigations();

    expect(renew.mock.calls).toEqual([["stale"]]);
    expect(bearers).toEqual(["Bearer stale", "Bearer fresh"]);
  });

  it("gives up after one retry", async () => {
    const bearers = answering(new Response("", { status: 401 }), new Response("", { status: 401 }));
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => "fresh");

    await expect(client.investigations()).rejects.toMatchObject({ status: 401 });

    expect(bearers).toHaveLength(2);
  });

  it("does not resend a write it could not renew for the same person", async () => {
    const bearers = answering(new Response("", { status: 401 }), ok());
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => { throw new Error("the session changed"); });

    await expect(client.approveAction("a-1")).rejects.toThrow("the session changed");

    expect(bearers).toHaveLength(1);
  });

  it("retries the markdown export too", async () => {
    const bearers = answering(new Response("", { status: 401 }), new Response("# report"));
    const client = new BackendClient(() => "http://reckon.test", async () => "stale", async () => "fresh");

    expect(await client.exportMarkdown("inv-1")).toBe("# report");

    expect(bearers).toEqual(["Bearer stale", "Bearer fresh"]);
  });

  it("leaves other refusals alone", async () => {
    const bearers = answering(new Response("", { status: 403 }));
    const renew = vi.fn(async () => "fresh");

    await expect(new BackendClient(() => "http://reckon.test", async () => "t", renew).investigations()).rejects.toMatchObject({ status: 403 });

    expect(renew).not.toHaveBeenCalled();
    expect(bearers).toHaveLength(1);
  });
});

describe("BackendClient: reading a response", () => {
  it("names the field a response is missing", async () => {
    const client = clientAnswering({ investigations: [{ aggregate_id: "i", status: "ACTIVE", last_event_sequence: 1 }] });
    await expect(client.investigations()).rejects.toThrow("unexpected response from GET /api/investigations: investigations[0].title missing");
  });

  it("refuses a renamed list rather than showing it empty", async () => {
    const client = clientAnswering({ investigation_rows: [] });
    await expect(client.investigations()).rejects.toThrow("investigations missing");
  });

  it("shows a list the backend sent as null as empty", async () => {
    expect(await clientAnswering({ investigations: null }).investigations()).toEqual([]);
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
    vi.stubGlobal("fetch", async () => new Response("<html>", { status: 200 }));
    const client = new BackendClient(() => "http://reckon.test", async () => "token");
    await expect(client.me()).rejects.toThrow("unexpected response from GET /api/me: not JSON");
  });

  it("does not read the answer to a write that ignores it", async () => {
    vi.stubGlobal("fetch", async () => new Response("", { status: 200 }));
    await expect(new BackendClient(() => "http://reckon.test", async () => "token").commsAct("t-1", "ack")).resolves.toBeUndefined();
  });

  it("reports a /status it cannot read as incompatible, not unreachable", async () => {
    const client = clientAnswering({ overall: "ok" });
    const status = await client.status();
    expect(status).toMatchObject({ reachable: true, apiVersion: null, compatible: false });
    expect(client.incompatibilityReason(status)).toContain("predates the version handshake");
  });
});
