// @vitest-environment node
import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { EnablementAdapter } from "./backend";
import { applyEnablement, confirmEnablement } from "./enablementFlow";
import { schemaFields } from "./enablement";
import { resetVscodeFake } from "./test/vscode";

const adapter: EnablementAdapter = {
  name: "dns-adapter",
  class: "fixture",
  enabled: false,
  scenario: "lateral",
  configFields: {
    scenario: { type: "string", description: "which recorded scenario to serve" },
    endpoint: { type: "string", description: "where the resolver listens" },
    token: { type: "string", description: "the API token", secret: true },
  },
  requiredFields: ["endpoint"],
  supportable: true,
};

type InputOptions = { title?: string; prompt?: string; password?: boolean; value?: string; validateInput?: (v: string) => string | undefined };
const inputs = () => vi.mocked(vscode.window.showInputBox).mock.calls.map((c) => c[0] as InputOptions);

beforeEach(() => {
  resetVscodeFake();
  vi.clearAllMocks();
});

describe("the native enablement flow (adr/0342)", () => {
  it("asks for every schema field after the confirm: required ones marked and enforced, a secret as a reference through a password input", async () => {
    vi.mocked(vscode.window.showWarningMessage).mockResolvedValueOnce("Enable" as never);
    vi.mocked(vscode.window.showInputBox)
      .mockResolvedValueOnce("lateral")
      .mockResolvedValueOnce(" http://dns:53 ")
      .mockResolvedValueOnce("keychain://reckon/dns");
    const config = await confirmEnablement({ adapter: "dns-adapter", enabled: true, config: {}, ask: schemaFields(adapter) }, () => true);

    expect(config).toEqual({ scenario: "lateral", endpoint: "http://dns:53", token: "keychain://reckon/dns" });
    expect(vi.mocked(vscode.window.showWarningMessage).mock.calls[0]![1]).toEqual({ modal: true });
    const [scenario, endpoint, token] = inputs();
    expect(scenario!.value).toBe("lateral");
    expect(scenario!.prompt).toBe("which recorded scenario to serve");
    expect(endpoint!.title).toBe("reckon — dns-adapter.endpoint · required");
    expect(endpoint!.validateInput!(" ")).toBe("endpoint is required");
    expect(scenario!.validateInput).toBeUndefined();
    expect(token!.password).toBe(true);
    expect(token!.title).toBe("reckon — dns-adapter.token (secret reference)");
    expect(token!.prompt).toMatch(/secret REFERENCE/);
  });

  it("applies nothing when declined, when an input is cancelled, or when the person changed", async () => {
    const change = { adapter: "dns-adapter", enabled: true, config: { a: "1" }, ask: [{ name: "token", secret: true }] };
    expect(await confirmEnablement(change, () => true)).toBeUndefined(); // the modal answered nothing
    expect(vscode.window.showInputBox).not.toHaveBeenCalled();

    vi.mocked(vscode.window.showWarningMessage).mockResolvedValueOnce("Enable" as never);
    vi.mocked(vscode.window.showInputBox).mockResolvedValueOnce(undefined);
    expect(await confirmEnablement(change, () => true)).toBeUndefined();

    vi.mocked(vscode.window.showWarningMessage).mockResolvedValueOnce("Enable" as never);
    vi.mocked(vscode.window.showInputBox).mockResolvedValueOnce("env://TOKEN");
    let same = true;
    vi.mocked(vscode.window.showInputBox).mockImplementationOnce(async () => { same = false; return "env://TOKEN"; });
    expect(await confirmEnablement({ ...change, ask: [{ name: "token", secret: true }, { name: "other", secret: true }] }, () => same)).toBeUndefined();
  });

  it("keeps the document form's values and adds the secrets it asks for", async () => {
    vi.mocked(vscode.window.showWarningMessage).mockResolvedValueOnce("Enable" as never);
    vi.mocked(vscode.window.showInputBox).mockResolvedValueOnce("");
    const config = await confirmEnablement({ adapter: "dns-adapter", enabled: true, config: { endpoint: "x" }, ask: [{ name: "token", secret: true }] }, () => true);
    expect(config).toEqual({ endpoint: "x" }); // a blank reference leaves the field out
  });

  it("refuses a required field left blank, even when the input returns one", async () => {
    vi.mocked(vscode.window.showWarningMessage).mockResolvedValueOnce("Enable" as never);
    vi.mocked(vscode.window.showInputBox).mockResolvedValueOnce(" ");
    const config = await confirmEnablement({ adapter: "dns-adapter", enabled: true, config: {}, ask: [{ name: "token", secret: true, required: true }] }, () => true);
    expect(config).toBeUndefined();
  });

  it("applies on the client and says so", async () => {
    const apply = vi.fn(async () => undefined);
    await applyEnablement({ capabilities: { applyEnablement: apply } }, { adapter: "dns-adapter", enabled: true }, { endpoint: "x" });
    expect(apply).toHaveBeenCalledWith("dns-adapter", true, { endpoint: "x" });
    expect(vscode.window.showInformationMessage).toHaveBeenCalledWith("reckon: dns-adapter enabled — capability surface rebuilt");
  });
});
