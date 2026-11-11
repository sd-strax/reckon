// @vitest-environment node
// A seed typed by the analyst (adr/0324): the entity kind they chose rides in
// seed_input, and an absent one is left to the server's default.
import { readFileSync } from "fs";
import * as path from "path";
import { describe, expect, it } from "vitest";
import type { BackendHttp } from "./http";
import { InvestigationsClient } from "./investigations";
import { decode, type Decoder } from "../wire/decode";

const created = JSON.parse(readFileSync(path.join(__dirname, "..", "wire", "fixtures", "backend", "created.filled.json"), "utf8")) as unknown;

describe("createFromInput", () => {
  it("sends the entity kind in seed_input, and leaves it out when none was chosen", async () => {
    const bodies: unknown[] = [];
    const http = {
      post: async <T>(p: string, b: unknown, shape: Decoder<T>) => { bodies.push(b); return decode(p, shape, created); },
    } as unknown as BackendHttp;
    const c = new InvestigationsClient(http);
    await c.createFromInput("CONTOSO\\jdoe", "entity", undefined, undefined, "person");
    await c.createFromInput("WIN-FILE01", "entity");
    await c.createFromInput("root", "entity", undefined, undefined, undefined, "user-account--7");
    expect(JSON.parse(JSON.stringify(bodies))).toEqual([
      { seed_input: { value: "CONTOSO\\jdoe", kind: "entity", entity_kind: "person" } },
      { seed_input: { value: "WIN-FILE01", kind: "entity" } },
      { seed_input: { value: "root", kind: "entity", account_ref: "user-account--7" } },
    ]);
  });
});
