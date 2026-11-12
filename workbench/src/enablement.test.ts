import { describe, expect, it } from "vitest";
import type { Enablement, EnablementAdapter } from "./viewTypes";
import { nativeFields, setUpAdapter } from "./enablement";

const adapter = (name: string, over: Partial<EnablementAdapter> = {}): EnablementAdapter =>
  ({ name, class: "fixture", enabled: false, configFields: {}, requiredFields: [], supportable: true, ...over });

const view = (adapters: EnablementAdapter[], verbs: Enablement["verbs"]): Enablement => ({ adapters, verbs });

describe("setUpAdapter: the one Set up rule (adr/0342)", () => {
  const verb = { verb: "query_dns", adapters: ["dns"], enabled: false, closableBy: ["dns"] };

  it("offers the first closing adapter of a disabled, unavailable verb when it is installed, supportable and disabled", () => {
    expect(setUpAdapter("query_dns", "unavailable", view([adapter("dns")], [verb]))).toBe("dns");
  });

  it("offers nothing otherwise", () => {
    const ok = view([adapter("dns")], [verb]);
    expect(setUpAdapter("query_dns", "available", ok)).toBeNull(); // the verb answers
    expect(setUpAdapter("query_dns", undefined, ok)).toBeNull();
    expect(setUpAdapter("query_dns", "unavailable", null)).toBeNull(); // no enablement view
    expect(setUpAdapter("other", "unavailable", ok)).toBeNull(); // no verb entry
    expect(setUpAdapter("query_dns", "unavailable", view([adapter("dns")], [{ ...verb, enabled: true }]))).toBeNull();
    expect(setUpAdapter("query_dns", "unavailable", view([adapter("dns")], [{ ...verb, closableBy: [] }]))).toBeNull();
    expect(setUpAdapter("query_dns", "unavailable", view([], [verb]))).toBeNull(); // not installed
    expect(setUpAdapter("query_dns", "unavailable", view([adapter("dns", { supportable: false })], [verb]))).toBeNull();
    expect(setUpAdapter("query_dns", "unavailable", view([adapter("dns", { enabled: true })], [verb]))).toBeNull();
  });
});

describe("nativeFields: what the document's form leaves to the native flow", () => {
  it("every secret and any required field left empty, with the schema's rules", () => {
    const a = adapter("dns", {
      configFields: { base_url: { description: "The API." }, region: {}, api_key: { secret: true, description: "key" } },
      requiredFields: ["base_url", "api_key"],
    });
    expect(nativeFields(a, { region: "eu" })).toEqual([
      { name: "base_url", secret: false, required: true, description: "The API.", value: undefined },
      { name: "api_key", secret: true, required: true, description: "key", value: undefined },
    ]);
    expect(nativeFields(a, { base_url: "x" }).map((f) => f.name)).toEqual(["api_key"]);
  });
});
