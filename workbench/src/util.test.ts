import { describe, expect, it } from "vitest";
import { errText, escapeHtml, makeNonce, panelTitle } from "./util";

describe("util", () => {
  it("reads an error's message and stringifies anything else", () => {
    expect(errText(new Error("boom"))).toBe("boom");
    expect(errText("plain")).toBe("plain");
    expect(errText(42)).toBe("42");
  });

  it("mints a 32-character nonce legal in a CSP nonce source, fresh each time", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 100; i++) {
      const n = makeNonce();
      expect(n).toMatch(/^[A-Za-z0-9_-]{32}$/);
      seen.add(n);
    }
    expect(seen.size).toBe(100);
  });

  it("escapes the five HTML metacharacters and nothing else", () => {
    expect(escapeHtml(`<a href="x">Tom & Jerry's</a>`)).toBe("&lt;a href=&quot;x&quot;&gt;Tom &amp; Jerry&#39;s&lt;/a&gt;");
    expect(escapeHtml("plain text")).toBe("plain text");
    expect(escapeHtml(undefined)).toBe("");
    expect(escapeHtml(null)).toBe("");
  });

  it("formats a panel title", () => {
    expect(panelTitle("RDP from host-a")).toBe("⚖ RDP from host-a");
  });
});
