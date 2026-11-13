import { describe, expect, it } from "vitest";
import { buildDerivedViews } from "./derivedViews";
import type { EvidenceDoc, Hypothesis, ThreadEntry } from "./viewTypes";

// The road-test shape (#141): the thread lists only the records each act made;
// the evidence sits on a prediction's test results and on the seed.
const thread = [{ sequenceNo: 1, interpretationId: "i1", inputRefs: [], outputRefs: ["x-prediction--p"], actor: { kind: "AI_DELEGATED" } }] as unknown as ThreadEntry[];
const hypotheses = [{ id: "x-hypothesis--h", predictions: [{ id: "x-prediction--p", testResultRefs: ["observed-data--logon"] }] }] as unknown as Hypothesis[];
const docs: Record<string, EvidenceDoc> = {
  "observed-data--logon": { ref: "observed-data--logon", kind: "stix", type: "observed-data", payload: { first_observed: "2026-04-20T14:32:47Z", object_refs: ["x-host--win", "user-account--svc"] } } as unknown as EvidenceDoc,
  "observed-data--case": { ref: "observed-data--case", kind: "stix", type: "observed-data", payload: { first_observed: "2026-04-20T14:40:00Z", object_refs: ["x-host--win"] } } as unknown as EvidenceDoc,
};
const getDoc = async (ref: string) => docs[ref] ?? null;

describe("derived views over every citing record (#141)", () => {
  it("the timeline, entities and graph read evidence cited outside the thread", async () => {
    const v = await buildDerivedViews(thread, getDoc, { hypotheses, seed: { type: "case", caseRef: "observed-data--case" } });
    expect(v.strip.map((s) => s.ref)).toEqual(["observed-data--logon", "observed-data--case"]);
    expect(v.entities).toEqual([{ ref: "x-host--win", citations: 2 }, { ref: "user-account--svc", citations: 1 }]);
    expect(v.graph.edges).toContainEqual({ from: "x-prediction--p", to: "observed-data--logon", ai: false, join: undefined });
  });
  it("with the thread alone it stays as before", async () => {
    const v = await buildDerivedViews(thread, getDoc);
    expect(v.strip).toEqual([]);
  });
});

describe("pinned accounts the record joins by an alias (adr/0352)", () => {
  const sid = "user-account--sid", login = "user-account--login", other = "user-account--other";
  const acct: Record<string, EvidenceDoc> = {
    [sid]: { ref: sid, kind: "stix", type: "user-account", payload: { account_login: "CONTOSO\\svc_backup", user_id: "S-1-5-21-1004" } } as unknown as EvidenceDoc,
    [login]: { ref: login, kind: "stix", type: "user-account", payload: { account_login: "CONTOSO\\svc_backup" } } as unknown as EvidenceDoc,
    [other]: { ref: other, kind: "stix", type: "user-account", payload: { account_login: "CONTOSO\\svc_backup2" } } as unknown as EvidenceDoc,
  };
  const get = async (ref: string) => acct[ref] ?? null;
  const pins = [{ interpretationId: "pin", finding: "", inputRefs: [sid, login, other], actor: "HUMAN", pinnedAt: "", superseded: false }];
  it("joins a live aliases edge, the login's account first; a withdrawn edge or a name joins nothing", async () => {
    const reader = async (ref: string) => ref === login ? [sid] : ref === other ? [] : null;
    const v = await buildDerivedViews([], get, { pins }, reader);
    expect(v.accountAliases).toEqual([{ ref: login, also: [sid] }]);
    const none = await buildDerivedViews([], get, { pins }, async () => []);
    expect(none.accountAliases).toBeUndefined();
  });
  it("reads the edges from the account's identity view by default, live ones only", async () => {
    const withView = (ref: string, others: { otherRef: string; withdrawn: boolean }[]) => ({ ...acct[ref]!, accountIdentity: { aliases: others, distinct: [], seenAs: { logins: [], ids: [], principalNames: [] }, owners: [] } }) as unknown as EvidenceDoc;
    const docsOf: Record<string, EvidenceDoc> = { [sid]: withView(sid, [{ otherRef: login, withdrawn: false }]), [login]: withView(login, []), [other]: withView(other, [{ otherRef: sid, withdrawn: true }]) };
    const v = await buildDerivedViews([], async (r) => docsOf[r] ?? null, { pins });
    expect(v.accountAliases).toEqual([{ ref: login, also: [sid] }]);
  });
});
