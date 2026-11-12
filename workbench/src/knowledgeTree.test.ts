// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import type { Session } from "./auth";
import type { BackendClient, BackendStatus } from "./backend";
import { KnowledgeGroup, KnowledgeItem, KnowledgeProvider, OPEN_ITEM_COMMAND, pastCaseCount } from "./knowledgeTree";
import type { LibraryItem } from "./protocol";
import { resetVscodeFake } from "./test/vscode";

const ok: BackendStatus = { reachable: true, overall: "ready", apiVersion: 1, components: {} } as unknown as BackendStatus;

const sop: LibraryItem = { kind: "sop", ref: "sop-1", title: "Containment", detail: "rdp, lateral", citedIn: 1 };
const pastCase: LibraryItem = { kind: "case", ref: "grouping--prior-1", title: "Prior case A", detail: "malicious · concluded 2026-10-01", citedIn: 0 };

function client(over: { status?: BackendStatus; reason?: string; procedures?: () => Promise<LibraryItem[]>; caseList?: () => Promise<{ items: LibraryItem[]; total: number }> } = {}): BackendClient {
  return {
    status: async () => over.status ?? ok,
    incompatibilityReason: () => over.reason,
    knowledge: {
      procedures: over.procedures ?? (async () => [sop]),
      caseList: over.caseList ?? (async () => ({ items: [pastCase], total: 1 })),
    },
  } as unknown as BackendClient;
}

const tree = (c: BackendClient, signedIn = true) => new KnowledgeProvider(c, { signedIn } as unknown as Session);

beforeEach(() => resetVscodeFake());

describe("KnowledgeProvider (adr/0344)", () => {
  it("shows the same action rows as the other views when unreachable or signed out, and reads no library", async () => {
    let reads = 0;
    const counting = { procedures: async () => { reads++; return []; }, caseList: async () => { reads++; return { items: [], total: 0 }; } };
    const [d] = await tree(client({ status: { ...ok, reachable: false }, reason: "backend not reachable", ...counting })).getChildren();
    expect(d!.label).toBe("Backend not connected");
    expect(d!.command?.command).toBe("reckon.checkBackend");
    const [s] = await tree(client(counting), false).getChildren();
    expect(s!.label).toBe("Sign in to reckon");
    expect(s!.command?.command).toBe("reckon.signIn");
    expect(reads).toBe(0);
  });

  it("lists Procedures and Past cases with their counts; a procedure by its tags, a past case by its disposition and day, cited-in in the tooltip", async () => {
    const p = tree(client());
    const groups = await p.getChildren();
    expect(groups.map((g) => g.label)).toEqual(["Procedures (1)", "Past cases (1)"]);
    const [proc] = await p.getChildren(groups[0]) as KnowledgeItem[];
    expect(proc!.label).toBe("Containment");
    expect(proc!.description).toBe("rdp, lateral");
    expect(proc!.tooltip).toBe("Containment\nrdp, lateral\ncited in 1 investigation");
    expect(proc!.contextValue).toBe("reckon.knowledgeItem");
    expect(proc!.command).toMatchObject({ command: OPEN_ITEM_COMMAND, arguments: [proc] });
    const [prior] = await p.getChildren(groups[1]) as KnowledgeItem[];
    expect(prior!.description).toBe("malicious · concluded 2026-10-01");
    expect(prior!.tooltip).toBe("Prior case A\nmalicious · concluded 2026-10-01\ncited in 0 investigations");
    expect(prior!.item).toEqual(pastCase);
  });

  it("reads N of M when the corpus holds more past cases than the page", async () => {
    expect(pastCaseCount(50, 50)).toBe("50");
    expect(pastCaseCount(50, 150)).toBe("50 of 150");
    const groups = await tree(client({ caseList: async () => ({ items: [pastCase], total: 3 }) })).getChildren();
    expect(groups[1]!.label).toBe("Past cases (1 of 3)");
  });

  it("says a group could not be read, rather than calling it empty, and keeps the other", async () => {
    const groups = await tree(client({ procedures: async () => { throw new Error("HTTP 500"); } })).getChildren();
    expect(groups[0]).not.toBeInstanceOf(KnowledgeGroup);
    expect(groups[0]!.label).toBe("Procedures");
    expect(groups[0]!.description).toBe("could not be read");
    expect(groups[0]!.tooltip).toBe("HTTP 500");
    expect(groups[0]!.command?.command).toBe("reckon.refreshKnowledge");
    expect(groups[1]!.label).toBe("Past cases (1)");
  });

  it("offers import on an empty procedures group", async () => {
    const p = tree(client({ procedures: async () => [], caseList: async () => ({ items: [], total: 0 }) }));
    const groups = await p.getChildren();
    expect(groups.map((g) => g.label)).toEqual(["Procedures (0)", "Past cases (0)"]);
    const [empty] = await p.getChildren(groups[0]);
    expect(empty!.command?.command).toBe("reckon.importSOP");
    expect((await p.getChildren(groups[1]))[0]!.label).toBe("No concluded investigations yet");
  });

  it("answers an older read that lands last with the newer read's groups", async () => {
    let answerOld!: (rows: LibraryItem[]) => void;
    let first = true;
    const p = tree(client({
      procedures: () => {
        if (first) { first = false; return new Promise<LibraryItem[]>((r) => { answerOld = r; }); }
        return Promise.resolve([sop, { ...sop, ref: "sop-2", title: "Triage" }]);
      },
    }));
    const older = p.getChildren();
    await new Promise((r) => setTimeout(r, 0));
    const newer = await p.getChildren();
    expect(newer[0]!.label).toBe("Procedures (2)");
    answerOld([]);
    expect((await older)[0]!.label).toBe("Procedures (2)");
    expect(vscode.TreeItemCollapsibleState.Expanded).toBe((newer[0] as KnowledgeGroup).collapsibleState);
  });

  it("gives each group a stable id, so its collapse state survives a count change", async () => {
    let rows = [sop];
    const p = tree(client({ procedures: async () => rows }));
    const first = await p.getChildren();
    rows = [sop, { ...sop, ref: "sop-2", title: "Triage" }];
    const second = await p.getChildren();
    expect(second[0]!.label).not.toBe(first[0]!.label);
    expect(first.map((g) => g.id)).toEqual(["knowledge.procedures", "knowledge.cases"]);
    expect(second.map((g) => g.id)).toEqual(["knowledge.procedures", "knowledge.cases"]);
    const failed = await tree(client({ procedures: async () => { throw new Error("down"); } })).getChildren();
    expect(failed[0]!.id).toBe("knowledge.procedures");
  });
});
