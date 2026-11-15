import { describe, expect, it } from "vitest";
import { observedEntityRefs, stixExtensionIds } from "./stixExtensions";

describe("the entities an observation is about (adr/0324, adr/0357)", () => {
  const obs = (refs: string[], person?: string) => ({
    type: "observed-data",
    object_refs: refs,
    extensions: person ? { [stixExtensionIds["reckon-observation"]]: { extension_type: "property-extension", person_ref: person } } : {},
  });

  it("reads the person a directory record describes, beside object_refs", () => {
    // An accountless directory record: the artifact alone in object_refs.
    expect(observedEntityRefs(obs(["artifact--1"], "identity--p"))).toEqual(["identity--p"]);
    // A record with an account: the account and the person.
    expect(observedEntityRefs(obs(["user-account--a", "relationship--r"], "identity--p"))).toEqual(["user-account--a", "identity--p"]);
  });

  it("lists object_refs' entities alone when there is no person_ref", () => {
    expect(observedEntityRefs(obs(["reckon-host--h", "artifact--1", "relationship--r"]))).toEqual(["reckon-host--h"]);
    expect(observedEntityRefs(null)).toEqual([]);
  });
});
