// A committed turn's transcript is line-framed ([user]/[assistant]/[tool_use]/
// [tool_result]/[loop] records, one per line, content newlines escaped) and the
// results carry the FULL payloads — so a cold restore renders real coverage,
// refs and pinnable chips, not a summary. Pure parsing; the components render.

export interface TranscriptUser { kind: "user"; text: string }
export interface TranscriptAssistant { kind: "assistant"; text: string }
export interface TranscriptTool {
  kind: "tool";
  name: string;
  /** The raw args line (JSON when the model produced JSON). */
  argsRaw: string;
  /** Pretty-printed args for the disclosure body. */
  argsPretty: string;
  result?: TranscriptToolResult;
}
export interface TranscriptToolResult {
  isError: boolean;
  /** "COMPLETE · 3 event(s)", "error", "done". */
  detail: string;
  /** Cited refs distilled from a capability envelope. */
  refs: string[];
  /** The result body to show behind the disclosure, "" when the envelope was
   * fully distilled into detail + refs. Clipped to 4000 chars. */
  body: string;
}
export interface TranscriptLoop { kind: "loop"; text: string }
export type TranscriptItem = TranscriptUser | TranscriptAssistant | TranscriptTool | TranscriptLoop;

export interface ParsedTranscript {
  items: TranscriptItem[];
  /** The turn was a scoped ⚡ aside: reconstructs collapsed. */
  aside: boolean;
  asideLabel: string;
}

export function unescapeT(s: unknown): string {
  return String(s ?? "").replace(/\\n/g, "\n").replace(/\\r/g, "\r");
}

/** Distill a tool_result body exactly as the sidecar does live. */
export function distillResult(isError: boolean, content: string): TranscriptToolResult {
  let detail = isError ? "error" : "done";
  const refs: string[] = [];
  let body = "";
  let env: unknown = undefined;
  let parsed = false;
  try {
    env = JSON.parse(content);
    parsed = true;
  } catch {
    body = content;
  }
  if (parsed) {
    const e = env as { coverage?: unknown; events?: unknown } & Record<string, unknown>;
    if (e && typeof e === "object" && e.coverage) {
      detail = String(e.coverage) + " · " + (Array.isArray(e.events) ? e.events.length : 0) + " event(s)";
      for (const k of ["observed_data_refs", "entity_refs", "ocsf_event_refs"]) {
        const arr = e[k];
        if (Array.isArray(arr)) {
          for (const r of arr) if (typeof r === "string" && !refs.includes(r)) refs.push(r);
        }
      }
      // A capability envelope is fully distilled into the summary + chips; the
      // raw JSON would be noise.
    } else {
      body = JSON.stringify(env, null, 2);
    }
  }
  if (body.length > 4000) body = body.slice(0, 4000) + "…";
  return { isError, detail, refs, body };
}

export function parseTranscript(bodyText: string): ParsedTranscript {
  const items: TranscriptItem[] = [];
  const rows = new Map<string, TranscriptTool>();
  let aside = false;
  let asideLabel = "⚡ aside";
  for (const line of String(bodyText).split("\n")) {
    let m: RegExpMatchArray | null;
    if ((m = line.match(/^\[user\] ([^]*)$/))) {
      const text = unescapeT(m[1]);
      if (text.indexOf("⚡ ") === 0) {
        aside = true;
        const colon = text.indexOf(":");
        asideLabel = (colon > 0 && colon < 40 ? text.slice(0, colon) : text.slice(0, 24)).toLowerCase();
      }
      items.push({ kind: "user", text });
    } else if ((m = line.match(/^\[assistant\] ([^]*)$/))) {
      items.push({ kind: "assistant", text: unescapeT(m[1]) });
    } else if ((m = line.match(/^\[tool_use ([^ \]]+) id=([^\]]+)\] ([^]*)$/))) {
      let argsPretty: string;
      try {
        argsPretty = JSON.stringify(JSON.parse(unescapeT(m[3])), null, 2);
      } catch {
        argsPretty = unescapeT(m[3]);
      }
      const row: TranscriptTool = { kind: "tool", name: m[1], argsRaw: m[3], argsPretty };
      rows.set(m[2], row);
      items.push(row);
    } else if ((m = line.match(/^\[tool_result ([^ \]]+) id=([^ \]]+) error=(true|false)\] ([^]*)$/))) {
      const row = rows.get(m[2]);
      if (row) row.result = distillResult(m[3] === "true", unescapeT(m[4]));
    } else if ((m = line.match(/^\[loop\] ([^]*)$/))) {
      items.push({ kind: "loop", text: m[1] });
    }
  }
  return { items, aside, asideLabel };
}
