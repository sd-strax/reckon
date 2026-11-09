import { describe, expect, it, vi } from "vitest";
import type { DocumentHost } from "./documentHost";
import { NarrativeFlow } from "./narrativeFlow";

function host(over: Partial<{ draft: () => Promise<unknown>; same: boolean }> = {}) {
  const posted: unknown[] = [];
  const recordNarrative = vi.fn(async () => ({}));
  const refresh = vi.fn(async () => undefined);
  const releaseSession = vi.fn();
  const h = {
    post: (_p: unknown, m: unknown) => { posted.push(m); return Promise.resolve(true); },
    sameIdentity: () => () => over.same ?? true,
    releaseSession,
    refresh,
    mutate: async (_id: string, _p: unknown, _verb: string, fn: () => Promise<void>) => { await fn(); await refresh(); },
    transport: { draftNarrative: over.draft ?? (async () => ({ revision: 1, chars: 5 })) },
    client: { investigations: { recordNarrative } },
  } as unknown as DocumentHost;
  return { h, posted, recordNarrative, refresh, releaseSession };
}

describe("the narrative flow (adr/0103)", () => {
  it("posts drafting, drafts through the sidecar, releases the session, refreshes, then says written", async () => {
    const { h, posted, refresh, releaseSession } = host();
    await new NarrativeFlow(h).draft("inv-1", {} as never);
    expect(posted).toEqual([{ type: "narrative.status", state: "drafting" }, { type: "narrative.status", state: "written" }]);
    expect(refresh).toHaveBeenCalledOnce();
    expect(releaseSession).toHaveBeenCalledWith("inv-1");
  });

  it("reports a failed draft and refreshes nothing", async () => {
    const { h, posted, refresh } = host({ draft: async () => { throw new Error("no key"); } });
    await new NarrativeFlow(h).draft("inv-1", {} as never);
    expect(posted[1]).toEqual({ type: "narrative.status", state: "failed", message: "no key" });
    expect(refresh).not.toHaveBeenCalled();
  });

  it("still says written when the re-read after a draft fails", async () => {
    const { h, posted } = host();
    (h as unknown as { refresh: () => Promise<void> }).refresh = async () => { throw new Error("read failed"); };
    await expect(new NarrativeFlow(h).draft("inv-1", {} as never)).rejects.toThrow("read failed");
    expect(posted[1]).toEqual({ type: "narrative.status", state: "written" });
  });

  it("posts nothing after the person changed", async () => {
    const { h, posted } = host({ same: false });
    await new NarrativeFlow(h).draft("inv-1", {} as never);
    expect(posted).toEqual([{ type: "narrative.status", state: "drafting" }]);
  });

  it("records an edit on the human token, says saving then saved after the re-read, and ignores a blank one", async () => {
    const { h, posted, recordNarrative, refresh } = host();
    const flow = new NarrativeFlow(h);
    await flow.edit("inv-1", {} as never, "  My account.  ");
    expect(recordNarrative).toHaveBeenCalledWith("inv-1", "My account.");
    expect(refresh).toHaveBeenCalledOnce();
    expect(posted).toEqual([{ type: "narrative.status", state: "saving" }, { type: "narrative.status", state: "saved" }]);
    await flow.edit("inv-1", {} as never, "   ");
    expect(recordNarrative).toHaveBeenCalledOnce();
    expect(posted.length).toBe(2);
  });

  it("reports a refused edit with the reason and re-reads nothing, so the editor keeps the text", async () => {
    const { h, posted, recordNarrative, refresh } = host();
    recordNarrative.mockRejectedValueOnce(new Error("422 Body is 20001 runes; at most 20000"));
    await new NarrativeFlow(h).edit("inv-1", {} as never, "x".repeat(20001));
    expect(posted).toEqual([
      { type: "narrative.status", state: "saving" },
      { type: "narrative.status", state: "save-failed", message: "422 Body is 20001 runes; at most 20000" },
    ]);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("still says saved when the re-read after an edit fails", async () => {
    const { h, posted } = host();
    (h as unknown as { refresh: () => Promise<void> }).refresh = async () => { throw new Error("read failed"); };
    await expect(new NarrativeFlow(h).edit("inv-1", {} as never, "Mine.")).rejects.toThrow("read failed");
    expect(posted[1]).toEqual({ type: "narrative.status", state: "saved" });
  });
});
