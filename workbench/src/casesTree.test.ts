// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import type { Session } from "./auth";
import type { BackendClient } from "./backend";
import { CasesProvider } from "./casesTree";
import type { CaseQueue } from "./viewTypes";
import { resetVscodeFake } from "./test/vscode";

const memento = { get: () => undefined, update: async () => undefined } as unknown as vscode.Memento;

function tree(queue: CaseQueue): CasesProvider {
  const client = { cases: { queue: async () => queue } } as unknown as BackendClient;
  return new CasesProvider(client, { signedIn: true } as unknown as Session, memento);
}

beforeEach(() => resetVscodeFake());

describe("CasesProvider: records that could not be read (adr/0357)", () => {
  it("shows a one-line notice above the readable cases, with the reason", async () => {
    const reason = "normalize class 2005: the event has no time";
    const rows = await tree({ queue: "SOC", cases: [{ number: "INC2", title: "Second" }], unread: { count: 2, reasons: [reason] } }).getChildren();
    expect(rows[0]!.label).toBe("2 records couldn't be read");
    expect(rows[0]!.description).toBe(reason);
    expect(rows.map((r) => r.label)).toContain("Second");
  });

  it("does not read as an empty queue when no record could be read", async () => {
    const rows = await tree({ queue: "SOC", cases: [], unread: { count: 1, reasons: ["r"] } }).getChildren();
    expect(rows[0]!.label).toBe("1 record couldn't be read");
  });

  it("shows no notice when every record was read", async () => {
    const rows = await tree({ queue: "SOC", cases: [{ number: "INC2", title: "Second" }] }).getChildren();
    expect(rows.map((r) => r.label)).toEqual(["Second"]);
  });
});
