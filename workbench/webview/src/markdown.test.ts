import { describe, expect, it } from "vitest";
import { esc, md } from "./markdown";

describe("md", () => {
  it("escapes before formatting", () => {
    expect(md("<script>alert(1)</script> **x**")).toBe("<p>&lt;script&gt;alert(1)&lt;/script&gt; <strong>x</strong></p>");
    expect(esc(`"a" & 'b'`)).toBe("&quot;a&quot; &amp; &#39;b&#39;");
  });
  it("renders inline code, lists, headings and fences", () => {
    expect(md("`x`")).toBe("<p><code>x</code></p>");
    expect(md("- a\n- b")).toBe("<ul><li>a</li><li>b</li></ul>");
    expect(md("1. a\n\n2. b")).toBe("<ol><li>a</li><li>b</li></ol>"); // a loose list numbers continuously
    expect(md("## Head")).toBe('<div class="mdh">Head</div>');
    expect(md("```\n<x>\n```")).toBe('<pre class="codeblock"><code>&lt;x&gt;</code></pre>');
    expect(md("```\nopen")).toBe('<pre class="codeblock"><code>open</code></pre>'); // unclosed mid-stream
  });
  it("renders pipe tables with a header row", () => {
    const html = md("| a | b |\n|---|---|\n| 1 | 2 |");
    expect(html).toContain('<table class="mdtable"><thead><tr><th>a</th><th>b</th></tr></thead>');
    expect(html).toContain("<tbody><tr><td>1</td><td>2</td></tr></tbody>");
  });
  it("wraps indented continuation lines into the list item", () => {
    expect(md("- a\n  more")).toBe("<ul><li>a more</li></ul>");
  });
});
