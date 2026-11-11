// @vitest-environment node
// The cross-language check (adr/0244): Go writes a sample of each wire type
// (internal/wirefixture, make wire-fixtures) and these decoders must read them.
// The zero sample holds `required` honest, against what Go omits or sends as
// null; the filled one holds the names honest, for an optional field too.
import { readdirSync, readFileSync } from "fs";
import * as path from "path";
import { describe, expect, it } from "vitest";
import { PROTOCOL_VERSION } from "../agentTransport";
import { SUPPORTED_API_VERSIONS } from "../backend";
import * as backend from "./backendShapes";
import { decode, namedButAbsent, type Decoder } from "./decode";
import * as sidecar from "./sidecarShapes";

const fixtures = (dir: string): Map<string, unknown> => {
  const root = path.join(__dirname, "fixtures", dir);
  return new Map(readdirSync(root).map((f) => [f.replace(/\.json$/, ""), JSON.parse(readFileSync(path.join(root, f), "utf8"))]));
};

describe.each([
  ["backend", backend],
  ["sidecar", sidecar],
])("the %s fixtures", (dir, shapes) => {
  const samples = fixtures(dir);
  samples.delete("versions");
  const decoders = Object.entries(shapes).filter((e): e is [string, Decoder<unknown>] => typeof e[1] === "function");

  it("cover every decoder, and every sample has one", () => {
    const named = new Set(decoders.map(([name]) => name));
    const sampled = new Set([...samples.keys()].map((k) => k.replace(/\.(zero|filled)$/, "")));
    expect(sampled).toEqual(named);
  });

  it.each(decoders)("%s reads what Go sends when empty", (name, decoder) => {
    expect(() => decode(name, decoder, samples.get(`${name}.zero`))).not.toThrow();
  });

  it.each(decoders)("%s reads what Go sends when full", (name, decoder) => {
    expect(() => decode(name, decoder, samples.get(`${name}.filled`))).not.toThrow();
  });

  it.each(decoders)("%s names no key the wire does not send", (name, decoder) => {
    // A sample of a recursive type stops at a depth: its deepest evidence has no identity.
    // The evidence sample carries each identity view as its entity has it (an
    // observed edge has no asserter), so every key of the three views is held
    // by the identityAct sample instead.
    const views = ["identity.", "account_identity.", "person_identity."];
    const absent = namedButAbsent(decoder, samples.get(`${name}.filled`))
      .filter((key) => !key.endsWith(".related.identity"))
      .filter((key) => !(name === "evidence" && views.some((v) => key.startsWith(v))));
    expect(absent).toEqual([]);
  });
});

describe("the versions", () => {
  const versions = fixtures("sidecar").get("versions") as { protocol: number; api: number };

  it("match the Go constants", () => {
    expect(PROTOCOL_VERSION).toBe(versions.protocol);
    expect(SUPPORTED_API_VERSIONS).toContain(versions.api);
  });
});
