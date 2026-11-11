// @vitest-environment node
// A host's identity over HTTP (adr/0211): the evidence read decodes the view
// Go sends (the wire fixture), and the three acts send the bodies the routes
// take and decode their answer.
import { readFileSync } from "fs";
import * as path from "path";
import { describe, expect, it } from "vitest";
import type { BackendHttp } from "./http";
import { EvidenceClient, otherHostBody } from "./evidence";
import { decode, type Decoder } from "../wire/decode";

const fixture = (name: string): unknown => JSON.parse(readFileSync(path.join(__dirname, "..", "wire", "fixtures", "backend", name + ".json"), "utf8"));

/** An HTTP double that decodes its canned body through the route's shape, as the real one does. */
function client(body: unknown): { e: EvidenceClient; calls: { method: string; path: string; body?: unknown }[] } {
  const calls: { method: string; path: string; body?: unknown }[] = [];
  const http = {
    get: async <T>(p: string, shape: Decoder<T>) => { calls.push({ method: "GET", path: p }); return decode(p, shape, body); },
    post: async <T>(p: string, b: unknown, shape: Decoder<T>) => { calls.push({ method: "POST", path: p, body: b }); return decode(p, shape, body); },
  } as unknown as BackendHttp;
  return { e: new EvidenceClient(http), calls };
}

describe("a host's identity view (adr/0211)", () => {
  it("decodes from the evidence read Go sends: origins, the note, the withdrawn mark, the split and what it was seen as", async () => {
    const doc = await client(fixture("evidence.filled")).e.get("x-host--1");
    expect(doc.identity).toEqual({
      aliases: [
        { edgeId: "relationship--1", otherRef: "x-host--2", kind: "observed", source: "edr", at: "2026-01-02T03:04:05Z", withdrawn: false },
        { edgeId: "relationship--3", otherRef: "x-host--4", kind: "asserted", by: "u-1", byName: "Ana Analyst", at: "2026-01-02T03:04:05Z", note: "same serial in the asset register", withdrawn: false },
        { edgeId: "relationship--5", otherRef: "x-host--6", kind: "asserted", by: "u-1", byName: "Ana Analyst", at: "2026-01-02T03:04:05Z", withdrawn: true },
      ],
      distinct: [
        { edgeId: "relationship--7", otherRef: "x-host--8", kind: "asserted", by: "u-2", byName: "Ben Analyst", at: "2026-01-02T03:04:05Z", note: "two machines", withdrawn: false },
      ],
      seenAs: { domains: ["corp", "corp.example"], agents: [{ tool: "edr", uid: "a-7" }], ips: ["10.0.0.7"] },
    });
  });

  it("is absent on a ref that is not a host; an act whose view could not be read answers null, never an empty view", async () => {
    const zero = await client(fixture("evidence.zero")).e.get("ipv4-addr--1");
    expect(zero.identity).toBeUndefined();
    expect(await client(fixture("identityAct.zero")).e.assertIdentity("x-host--1", "aliases", "ws1")).toBeNull();
    expect(await client(fixture("identityAct.zero")).e.withdrawIdentity("relationship--5", "x-host--1")).toBeNull();
  });

  it("sends a join or a split to its route, the other host by name or by ref, and decodes the answer", async () => {
    const { e, calls } = client(fixture("identityAct.filled"));
    const id = await e.assertIdentity("x-host--a", "aliases", " WIN-FILE01.corp.example ", " checked ");
    await e.assertIdentity("x-host--a", "distinct", "x-host--b");
    expect(calls).toEqual([
      { method: "POST", path: "/api/entities/x-host--a/aliases", body: { other_hostname: "WIN-FILE01.corp.example", note: "checked" } },
      { method: "POST", path: "/api/entities/x-host--a/distinct", body: { other_ref: "x-host--b" } },
    ]);
    expect(id?.aliases.map((a) => a.edgeId)).toEqual(["x"]);
  });

  it("withdraws by the edge id, asking for the view of the host in view", async () => {
    const { e, calls } = client(fixture("identityAct.filled"));
    await e.withdrawIdentity("relationship--5", "x-host--a", "wrong link");
    await e.withdrawIdentity("relationship--5", "x-host--a");
    expect(calls).toEqual([
      { method: "POST", path: "/api/identity/relationship--5/withdraw", body: { ref: "x-host--a", note: "wrong link" } },
      { method: "POST", path: "/api/identity/relationship--5/withdraw", body: { ref: "x-host--a" } },
    ]);
    expect(otherHostBody("  ", "")).toEqual({ other_hostname: "" });
  });

  it("sends a host ref as other_ref and anything else, punycode included, as a hostname", () => {
    expect(otherHostBody("x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01")).toEqual({ other_ref: "x-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01" });
    expect(otherHostBody("xn--bcher-kva.corp.example")).toEqual({ other_hostname: "xn--bcher-kva.corp.example" });
    expect(otherHostBody("ws--01")).toEqual({ other_hostname: "ws--01" });
  });
});
