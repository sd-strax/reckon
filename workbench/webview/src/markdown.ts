// Minimal markdown, escape-first. Model text is untrusted: every path escapes
// BEFORE formatting, and only these known-safe elements are ever produced.
// Covers what the agent actually emits (inline code, bold, bullets, numbered
// lists, fenced blocks, light headings, pipe tables); everything else renders
// as written. The <Markdown> component is the only consumer of the HTML string.

export function esc(s: unknown): string {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[c] as string);
}

function inlineMd(s: string): string {
  let out = esc(s);
  out = out.replace(/`([^`]+)`/g, "<code>$1</code>");
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  return out;
}

// Pipe tables (ui/02 §2.3 result-table treatment). Cells go through inlineMd,
// so they stay escape-first like every other path.
function mdRow(line: string): string[] {
  let s = line.trim();
  if (s.charAt(0) === "|") s = s.slice(1);
  if (s.charAt(s.length - 1) === "|") s = s.slice(0, -1);
  return s.split("|").map((c) => c.trim());
}

function mdTable(block: string[]): string {
  const rows = block.map(mdRow);
  let header: string[] | null = null;
  let body = rows;
  if (rows.length >= 2 && rows[1].length > 0 && rows[1].every((c) => /^:?-{2,}:?$/.test(c))) {
    header = rows[0];
    body = rows.slice(2);
  }
  let html = '<div class="tblwrap"><table class="mdtable">';
  if (header) {
    html += "<thead><tr>" + header.map((c) => "<th>" + inlineMd(c) + "</th>").join("") + "</tr></thead>";
  }
  html += "<tbody>"
    + body.map((r) => "<tr>" + r.map((c) => "<td>" + inlineMd(c) + "</td>").join("") + "</tr>").join("")
    + "</tbody></table></div>";
  return html;
}

/** Render markdown to the sanitized HTML subset. */
export function md(src: unknown): string {
  const lines = String(src ?? "").split("\n");
  const out: string[] = [];
  let para: string[] = [];
  let list: { tag: "ul" | "ol"; items: string[] } | null = null;
  const flushPara = () => {
    if (para.length) { out.push("<p>" + para.map(inlineMd).join("<br>") + "</p>"); para = []; }
  };
  const flushList = () => {
    if (list) {
      out.push("<" + list.tag + ">" + list.items.map((it) => "<li>" + inlineMd(it) + "</li>").join("") + "</" + list.tag + ">");
      list = null;
    }
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^\s*```/.test(line)) {
      flushPara(); flushList();
      const buf: string[] = [];
      for (i++; i < lines.length && !/^\s*```/.test(lines[i]); i++) buf.push(lines[i]);
      // An unclosed fence (mid-stream) still renders as a block — the
      // re-render on the next delta keeps it live.
      out.push('<pre class="codeblock"><code>' + esc(buf.join("\n")) + "</code></pre>");
      continue;
    }
    if (/^\s*\|/.test(line)) {
      flushPara(); flushList();
      const block = [line];
      for (i++; i < lines.length && /^\s*\|/.test(lines[i]); i++) block.push(lines[i]);
      i--;
      out.push(mdTable(block));
      continue;
    }
    const ul = line.match(/^\s*[-*•]\s+(.*)$/);
    const ol = line.match(/^\s*\d+[.)]\s+(.*)$/);
    const h = line.match(/^\s*#{1,4}\s+(.*)$/);
    if (ul || ol) {
      flushPara();
      const tag = ul ? "ul" : "ol";
      if (!list || list.tag !== tag) { flushList(); list = { tag, items: [] }; }
      list.items.push((ul || ol)![1]);
    } else if (h) {
      flushPara(); flushList();
      out.push('<div class="mdh">' + inlineMd(h[1]) + "</div>");
    } else if (line.trim() === "") {
      flushPara();
      // A blank line inside a list is inter-item spacing (a "loose" list):
      // keep the list open when the next content is another item of the
      // same kind, so an <ol> numbers continuously instead of restarting.
      if (list) {
        let j = i + 1;
        while (j < lines.length && lines[j].trim() === "") j++;
        const next = j < lines.length ? lines[j] : "";
        const sameKind = list.tag === "ul" ? /^\s*[-*•]\s+/.test(next) : /^\s*\d+[.)]\s+/.test(next);
        if (!sameKind) flushList();
      }
    } else if (list && /^\s+\S/.test(line)) {
      // An indented continuation line wraps into the current list item.
      list.items[list.items.length - 1] += " " + line.trim();
    } else {
      flushList();
      para.push(line);
    }
  }
  flushPara(); flushList();
  return out.join("");
}
