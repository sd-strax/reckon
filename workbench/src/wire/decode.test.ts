import { describe, expect, it } from "vitest";
import { anything, bool, decode, DecodeError, lazy, list, namedButAbsent, nullable, num, object, optional, record, str, type Decoder } from "./decode";
import * as backend from "./backendShapes";

const shape = object({ title: str, n: optional(num), tags: list(str), nested: optional(object({ ok: bool })) });

describe("decode", () => {
  it("names the source and the path of what was wrong", () => {
    expect(() => decode("x", shape, { title: "t", tags: ["a", 1] })).toThrow("unexpected response from x: tags[1] is not a string");
    expect(() => decode("x", shape, { title: "t", tags: [], nested: {} })).toThrow("nested.ok missing");
    expect(() => decode("x", shape, { title: "t", tags: [] , n: "1" })).toThrow("n is not a number");
  });

  it("says when the whole response is not the object expected", () => {
    expect(() => decode("x", shape, "nope")).toThrow("the response is not an object");
    expect(() => decode("x", shape, undefined)).toThrow(DecodeError);
    expect(() => decode("x", shape, null)).toThrow("the response is not an object");
    expect(() => decode("x", shape, [])).toThrow("the response is not an object");
  });

  it("drops what it does not name, and leaves out what is absent", () => {
    expect(decode("x", shape, { title: "t", tags: null, extra: 1 })).toEqual({ title: "t", tags: [] });
    expect(Object.keys(decode("x", shape, { title: "t", tags: [] }))).toEqual(["title", "tags"]);
  });

  it("reads a nullable field as null or its value, and a missing one as an error", () => {
    const answer = object({ identity: nullable(object({ ok: bool })) });
    expect(decode("x", answer, { identity: null })).toEqual({ identity: null });
    expect(decode("x", answer, { identity: { ok: true } })).toEqual({ identity: { ok: true } });
    expect(() => decode("x", answer, {})).toThrow("identity missing");
    expect(() => decode("x", backend.identityAct, {})).toThrow("identity missing");
    expect(decode("x", backend.identityAct, { identity: null })).toEqual({ identity: null });
  });

  it("reads an act's answer with the account's or the person's view beside the host's, which it always requires", () => {
    expect(decode("x", backend.identityAct, { identity: null })).toEqual({ identity: null });
    const account = { aliases: null, distinct: null, seen_as: { logins: null, ids: null, principal_names: null }, owners: [] };
    expect(decode("x", backend.identityAct, { identity: null, account_identity: account })).toEqual({
      identity: null, account_identity: { aliases: [], distinct: [], seen_as: { logins: [], ids: [], principal_names: [] }, owners: [] },
    });
    expect(() => decode("x", backend.identityAct, { account_identity: account })).toThrow("identity missing");
    expect(() => decode("x", backend.identityAct, { identity: null, account_identity: { ...account, owners: [{ person_ref: "identity--1" }] } }))
      .toThrow("account_identity.owners[0].account_ref missing");
    expect(() => decode("x", backend.identityAct, { identity: null, person_identity: { aliases: [], distinct: [], accounts: [{ account_ref: "a", person_ref: "p", account_aliases: ["a"], edge_id: "e", kind: "observed", at: "t", withdrawn: false }] } }))
      .toThrow("person_identity.accounts[0].cut missing");
  });

  it("tells a null list from a missing one", () => {
    expect(() => decode("x", shape, { title: "t" })).toThrow("tags missing");
    expect(decode("x", optional(list(str)), undefined)).toBeUndefined();
  });

  it("reads a record's values, and a null as empty", () => {
    expect(decode("x", record(num), { a: 1 })).toEqual({ a: 1 });
    expect(decode("x", record(num), null)).toEqual({});
    expect(() => decode("x", object({ m: record(num) }), { m: { a: "1" } })).toThrow("m.a is not a number");
  });

  it("passes opaque data through, null included, and refuses it missing", () => {
    expect(decode("x", object({ input: anything }), { input: { a: 1 } })).toEqual({ input: { a: 1 } });
    expect(decode("x", object({ input: anything }), { input: null })).toEqual({ input: null });
    expect(() => decode("x", object({ input: anything }), {})).toThrow("input missing");
    expect(decode("x", object({ input: optional(anything) }), {})).toEqual({});
  });

  it("reads a recursive shape", () => {
    interface Tree { kids: Tree[] }
    const tree: Decoder<Tree> = object({ kids: list(lazy(() => tree)) });
    expect(decode("x", tree, { kids: [{ kids: null }] })).toEqual({ kids: [{ kids: [] }] });
  });
});

describe("namedButAbsent", () => {
  it("lists the keys named and not sent, at any depth, required or not", () => {
    expect(namedButAbsent(shape, { title: "t", tags: [], nested: { ok: true } })).toEqual(["n"]);
    expect(namedButAbsent(object({ a: optional(object({ b: optional(str) })) }), { a: {} })).toEqual(["a.b"]);
  });

  it("does not listen outside a call", () => {
    decode("x", object({ a: optional(str) }), {});
    expect(namedButAbsent(object({ a: optional(str) }), { a: "x" })).toEqual([]);
  });
});
