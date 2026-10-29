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
      { action_id: "a1", action_type: "account.disable", status: "SUCCEEDED", reversibility: "reversible" },
      { action_id: "a2", action_type: "ioc.block", status: "SUCCEEDED", reversibility: "best_effort" },
      { action_id: "a3", action_type: "ticket.comment", status: "SUCCEEDED", reversibility: "irreversible" },
      { action_id: "a4", action_type: "host.isolate", status: "REQUESTED" },
    ] });
    const rows = await client.actions("inv-1");
    expect(rows.map((r) => r.reversibility)).toEqual(["REVERSIBLE", "BEST_EFFORT", "IRREVERSIBLE", undefined]);
  });
});

describe("BackendClient: the action catalog", () => {
  it("reads each type's inverse from its descriptor", async () => {
    const client = clientAnswering({ action_types: [
      { descriptor: { action_type: "host.isolate", default_tier: "T2", reversibility: "reversible", reversible_by: "host.unisolate" }, status: "available", bindings: 1 },
      { descriptor: { action_type: "ticket.comment", default_tier: "T1", reversibility: "irreversible" }, status: "available", bindings: 1 },
    ] });
    expect(await client.actionTypes()).toEqual([
      { actionType: "host.isolate", reversibleBy: "host.unisolate", tier: "T2", name: undefined, category: undefined, available: true, inputs: [] },
      { actionType: "ticket.comment", reversibleBy: undefined, tier: "T1", name: undefined, category: undefined, available: true, inputs: [] },
    ]);
  });
});
