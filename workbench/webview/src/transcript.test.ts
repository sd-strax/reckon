import { describe, expect, it } from "vitest";
import { distillResult, parseTranscript } from "./transcript";

const ENV = JSON.stringify({ coverage: "COMPLETE", events: [{}, {}], observed_data_refs: ["observed-data--1"], entity_refs: ["x-host--2"], ocsf_event_refs: ["e1"] });

describe("parseTranscript", () => {
  it("reconstructs user / assistant / tool rows and settles results", () => {
    const body = [
      "[user] look at WIN-FILE01",
      "[assistant] Checking.\\nSecond line",
      `[tool_use enumerate_logons id=c1] {"host":"WIN-FILE01"}`,
      `[tool_result enumerate_logons id=c1 error=false] ${ENV.replace(/\n/g, "\\n")}`,
      "[loop] 1 round",
    ].join("\n");
    const t = parseTranscript(body);
    expect(t.aside).toBe(false);
    expect(t.items.map((i) => i.kind)).toEqual(["user", "assistant", "tool", "loop"]);
    const asst = t.items[1] as { text: string };
    expect(asst.text).toBe("Checking.\nSecond line");
    const tool = t.items[2] as { name: string; argsPretty: string; result?: { detail: string; refs: string[]; body: string } };
    expect(tool.name).toBe("enumerate_logons");
    expect(tool.argsPretty).toContain('"host": "WIN-FILE01"');
    expect(tool.result?.detail).toBe("COMPLETE · 2 event(s)");
    expect(tool.result?.refs).toEqual(["observed-data--1", "x-host--2", "e1"]);
    expect(tool.result?.body).toBe(""); // an envelope is fully distilled
  });
  it("marks a ⚡ aside and derives its label", () => {
    const t = parseTranscript("[user] ⚡ Propose tests: for the hypothesis …");
    expect(t.aside).toBe(true);
    expect(t.asideLabel).toBe("⚡ propose tests");
  });
  it("keeps a non-envelope result body, clipped", () => {
    const r = distillResult(false, JSON.stringify({ ok: true }));
    expect(r.detail).toBe("done");
    expect(r.body).toContain('"ok": true');
    expect(distillResult(true, "boom").detail).toBe("error");
    expect(distillResult(false, "x".repeat(5000)).body.length).toBe(4001);
  });
});

describe("distillResult", () => {
  it("names the capability call the envelope came from (adr/0092)", async () => {
    const { distillResult } = await import("./transcript");
    const r = distillResult(false, JSON.stringify({ coverage: "COMPLETE", call_id: "c-1", events: [{}], ocsf_event_refs: ["e1"] }));
    expect(r.callId).toBe("c-1");
    expect(r.detail).toBe("COMPLETE · 1 event(s)");
    expect(distillResult(false, "plain text").callId).toBeUndefined();
  });
});

