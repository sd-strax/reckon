// @vitest-environment node
import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { panelHtml, panelOptions } from "./panelHtml";

const ext = vscode.Uri.file("/ext");
const webview = () => vscode.window.createWebviewPanel("t", "T", vscode.ViewColumn.Active, {}).webview;

describe("one page and one policy for every reckon panel (adr/0248)", () => {
  it("names the surface on the root and loads only the bundle, under a nonce-only CSP", () => {
    for (const surface of ["investigation", "case", "import"] as const) {
      const html = panelHtml(webview(), ext, surface, "T");
      const csp = /Content-Security-Policy" content="([^"]+)"/.exec(html)?.[1] ?? "";
      expect(csp).toContain("default-src 'none'");
      expect(csp).toMatch(/script-src 'nonce-[A-Za-z0-9_-]{32}'/);
      expect(csp).not.toContain("unsafe-inline");
      expect(csp).not.toContain("connect-src");
      expect(html).toContain(`<div id="root" data-surface="${surface}">`);
      expect(html).toContain("investigation.js");
      expect(html).toContain("investigation.css");
      expect(html).not.toContain("<style");
    }
  });

  it("confines local resources to the bundle and the media directory", () => {
    const opts = panelOptions(ext);
    expect(opts.enableScripts).toBe(true);
    expect(opts.retainContextWhenHidden).toBe(true);
    expect(opts.localResourceRoots?.map((u) => u.path)).toEqual(["/ext/out/webview", "/ext/media"]);
  });
});
