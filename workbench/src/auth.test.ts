// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Session } from "./auth";
import * as http from "http";
import { env, FakeSecretStorage, fakeLog, resetVscodeFake, window } from "./test/vscode";

const ISSUER = "http://127.0.0.1:8180/realms/reckon";
const TOKEN_URL = `${ISSUER}/protocol/openid-connect/token`;
const config = async () => ({ issuer: ISSUER, clientId: "reckon", agentClientId: "reckon-agent" });

/** The IdP: discovery, and a token endpoint that mints `<client>-<n>` tokens unless told to refuse. */
function idp() {
  const grants: Record<string, string>[] = [];
  let refuse = false;
  let n = 0;
  let subject: string | undefined;
  let answer: ((form: Record<string, string>) => Response | Promise<Response>) | undefined;
  const fetch = vi.fn(async (url: string, init?: { body?: string }) => {
    if (url.endsWith("/.well-known/openid-configuration")) {
      return Response.json({ authorization_endpoint: `${ISSUER}/auth`, token_endpoint: TOKEN_URL });
    }
    const form = Object.fromEntries(new URLSearchParams(init?.body ?? ""));
    grants.push(form);
    if (answer) {
      return answer(form);
    }
    if (refuse) {
      return Response.json({ error: "invalid_grant", error_description: "Token is not active" }, { status: 400 });
    }
    n++;
    if (subject && form.client_id === "reckon") {
      const claims = Buffer.from(JSON.stringify({ sub: subject })).toString("base64url");
      return Response.json({ access_token: `h.${claims}.s`, refresh_token: `rt-${n}`, expires_in: 60 });
    }
    return Response.json({ access_token: `${form.client_id}-at-${n}`, refresh_token: `${form.client_id}-rt-${n}`, expires_in: 60 });
  });
  return {
    fetch,
    grants,
    refuse: () => { refuse = true; },
    as: (sub: string) => { subject = sub; },
    answer: (a: typeof answer) => { answer = a; },
  };
}

function session(secrets = new FakeSecretStorage()) {
  const s = new Session(secrets, config, fakeLog());
  const changes = vi.fn();
  s.onDidChange(changes);
  return { s, secrets, changes };
}

let open: Session | null = null;
let server: ReturnType<typeof idp>;

beforeEach(() => {
  resetVscodeFake();
  vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"] });
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  server = idp();
  vi.stubGlobal("fetch", server.fetch);
});

afterEach(() => {
  open?.dispose();
  open = null;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Session.restore", () => {
  it("signs both sides in from the stored refresh tokens and stores the rotated ones", async () => {
    const { s, secrets, changes } = session();
    open = s;
    await secrets.store("reckon.refreshToken", "human-rt");
    await secrets.store("reckon.agentRefreshToken", "agent-rt");

    await s.restore();

    expect(s.signedIn).toBe(true);
    expect(changes).toHaveBeenCalledOnce();
    expect(server.grants.map((g) => [g.grant_type, g.client_id, g.refresh_token])).toEqual([
      ["refresh_token", "reckon", "human-rt"],
      ["refresh_token", "reckon-agent", "agent-rt"],
    ]);
    expect(await s.token("human")).toBe("reckon-at-1");
    expect(await s.token("delegate")).toBe("reckon-agent-at-2");
    expect(secrets.data.get("reckon.refreshToken")).toBe("reckon-rt-1");
  });

  it("drops a refresh token the IdP refuses and stays signed out", async () => {
    const { s, secrets, changes } = session();
    open = s;
    await secrets.store("reckon.refreshToken", "revoked");
    server.refuse();

    await s.restore();

    expect(s.signedIn).toBe(false);
    expect(changes).not.toHaveBeenCalled();
    expect(secrets.data.has("reckon.refreshToken")).toBe(false);
    await expect(s.token()).rejects.toThrow("not signed in");
  });

  it("keeps the stored token when the IdP is down, for the next window to restore", async () => {
    const { s, secrets } = session();
    open = s;
    await secrets.store("reckon.refreshToken", "good");
    server.answer(() => new Response("bad gateway", { status: 502 }));

    await s.restore();

    expect(s.signedIn).toBe(false);
    expect(secrets.data.get("reckon.refreshToken")).toBe("good");
  });
});

describe("Session.token", () => {
  async function signedIn() {
    const ctx = session();
    open = ctx.s;
    await ctx.secrets.store("reckon.refreshToken", "human-rt");
    await ctx.s.restore();
    return ctx;
  }

  it("answers from the held token while it is outside the refresh window", async () => {
    const { s } = await signedIn();
    vi.setSystemTime(Date.now() + 29_000);
    expect(await s.token()).toBe("reckon-at-1");
    expect(server.grants).toHaveLength(1);
  });

  it("refreshes inside the 30s window before expiry", async () => {
    const { s } = await signedIn();
    vi.setSystemTime(Date.now() + 31_000);
    expect(await s.token()).toBe("reckon-at-2");
    expect(server.grants.at(-1)).toMatchObject({ grant_type: "refresh_token", refresh_token: "reckon-rt-1" });
  });

  it("mints a fresh token on force (the 401 backstop)", async () => {
    const { s } = await signedIn();
    expect(await s.token("human", true)).toBe("reckon-at-2");
  });

  it("refuses a delegate token when no agent session exists", async () => {
    const { s } = await signedIn();
    await expect(s.token("delegate")).rejects.toThrow("no agent session");
  });

  it("refreshes in the background before expiry", async () => {
    const { s } = await signedIn();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(server.grants).toHaveLength(2);
    expect(await s.token()).toBe("reckon-at-2");
  });
});

describe("Session.signOut", () => {
  it("forgets both sides, deletes the stored tokens and tells the UI", async () => {
    const { s, secrets, changes } = session();
    open = s;
    await secrets.store("reckon.refreshToken", "human-rt");
    await secrets.store("reckon.agentRefreshToken", "agent-rt");
    await s.restore();
    changes.mockClear();

    await s.signOut();

    expect(s.signedIn).toBe(false);
    expect(changes).toHaveBeenCalledOnce();
    expect([...secrets.data.keys()]).toEqual([]);
    await expect(s.token()).rejects.toThrow("not signed in");
  });
});

describe("Session.onDidChangeIdentity", () => {
  /** A session that notes identity changes and restores from a stored token. */
  async function restored(sub: string) {
    const made = session();
    open = made.s;
    const identity = vi.fn();
    made.s.onDidChangeIdentity(identity);
    server.as(sub);
    await made.secrets.store("reckon.refreshToken", "human-rt");
    await made.s.restore();
    return { ...made, identity };
  }

  /** The browser side of a login: Keycloak redirecting straight back. */
  function browserReturnsToLoopback(): void {
    vi.mocked(env.openExternal).mockImplementation(async (target) => {
      const auth = new URL(target.toString());
      const back = `${auth.searchParams.get("redirect_uri")}?code=c&state=${auth.searchParams.get("state")}`;
      http.get(back.replace("localhost", "127.0.0.1"), (res) => res.resume());
      return true;
    });
  }

  it("fires when a session is restored and when it signs out, before the UI is told", async () => {
    const { s, identity, changes } = await restored("alice");
    expect(identity).toHaveBeenCalledOnce();

    const order: string[] = [];
    identity.mockImplementation(() => order.push("identity"));
    changes.mockImplementation(() => order.push("changed"));
    await s.signOut();
    expect(order).toEqual(["identity", "changed"]);
  });

  it("does not fire on a refresh of the same person's token", async () => {
    const { s, identity } = await restored("alice");
    identity.mockClear();
    await s.token("human", true);
    expect(identity).not.toHaveBeenCalled();
  });

  it("fires when someone else signs in over the session, and not for the same person", async () => {
    const { s, identity } = await restored("alice");
    browserReturnsToLoopback();

    identity.mockClear();
    await s.signIn();
    expect(identity).not.toHaveBeenCalled();

    server.as("bob");
    await s.signIn();
    expect(identity).toHaveBeenCalledOnce();
  });

  it("treats a token with no readable subject as a change", async () => {
    const { s, secrets, changes } = session();
    open = s;
    const identity = vi.fn();
    s.onDidChangeIdentity(identity);
    await secrets.store("reckon.refreshToken", "human-rt");
    await s.restore();
    await s.restore();
    expect(changes).toHaveBeenCalledTimes(2);
    expect(identity).toHaveBeenCalledTimes(2);
  });
});

describe("Session: refreshing", () => {
  const near = () => vi.setSystemTime(Date.now() + 31_000);
  const fresh = (form: Record<string, string>) =>
    Response.json({ access_token: `${form.client_id}-new`, refresh_token: "rt-new", expires_in: 60 });
  const gate = () => {
    let open!: (r: Response) => void;
    const opened = new Promise<Response>((r) => { open = r; });
    return { opened, open };
  };
  const refreshes = () => server.grants.filter((g) => g.grant_type === "refresh_token").length;
  const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };

  async function signedIn(sub?: string) {
    const ctx = session();
    open = ctx.s;
    const identity = vi.fn();
    ctx.s.onDidChangeIdentity(identity);
    if (sub) server.as(sub);
    await ctx.secrets.store("reckon.refreshToken", "human-rt");
    await ctx.secrets.store("reckon.agentRefreshToken", "agent-rt");
    await ctx.s.restore();
    ctx.changes.mockClear();
    identity.mockClear();
    server.grants.length = 0;
    return { ...ctx, identity };
  }

  it("posts one refresh for parallel callers inside the window", async () => {
    const { s } = await signedIn();
    near();

    const got = await Promise.all([s.token(), s.token(), s.token(), s.token("human", true)]);

    expect(refreshes()).toBe(1);
    expect(new Set(got).size).toBe(1);
  });

  it("refreshes each side on its own", async () => {
    const { s } = await signedIn();
    near();

    await Promise.all([s.token("human"), s.token("delegate")]);

    expect(refreshes()).toBe(2);
  });

  it("joins the timer's refresh in flight", async () => {
    const { s } = await signedIn();
    const pending = gate();
    server.answer(() => pending.opened);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(refreshes()).toBeGreaterThan(0);
    const n = refreshes();

    const token = s.token("human", true);
    pending.open(Response.json({ access_token: "joined", refresh_token: "rt", expires_in: 60 }));
    server.answer(fresh);

    expect(await token).toBeTruthy();
    expect(refreshes()).toBe(n);
  });

  it("keeps the refresh token when the response omits it", async () => {
    const { s, secrets } = await signedIn();
    near();
    server.answer(() => Response.json({ access_token: "at", expires_in: 60 }));

    await s.token();
    near();
    await s.token();

    expect(server.grants.map((g) => g.refresh_token)).toEqual(["reckon-rt-1", "reckon-rt-1"]);
    expect(secrets.data.get("reckon.refreshToken")).toBe("reckon-rt-1");
  });

  it("signs out when the IdP refuses the refresh, with the identity change and a word to the analyst", async () => {
    const { s, secrets, changes, identity } = await signedIn("alice");
    near();
    server.refuse();

    await expect(s.token()).rejects.toThrow("token request 400");

    expect(s.signedIn).toBe(false);
    expect(s.subject).toBeUndefined();
    expect(identity).toHaveBeenCalledOnce();
    expect(changes).toHaveBeenCalledOnce();
    expect(window.showWarningMessage).toHaveBeenCalledOnce();
    expect(secrets.data.has("reckon.refreshToken")).toBe(false);
    expect(secrets.data.has("reckon.agentRefreshToken")).toBe(false);
  });

  it("ends only the delegate side when only it is refused", async () => {
    const { s, secrets, changes } = await signedIn();
    near();
    server.answer((form) => form.client_id === "reckon-agent"
      ? Response.json({ error: "invalid_grant" }, { status: 400 })
      : fresh(form));

    await expect(s.token("delegate")).rejects.toThrow("invalid_grant");

    expect(s.signedIn).toBe(true);
    expect(changes).not.toHaveBeenCalled();
    expect(secrets.data.has("reckon.agentRefreshToken")).toBe(false);
    await expect(s.token("delegate")).rejects.toThrow("no agent session");
  });

  it("keeps the session on a server failure, and says the status of a non-JSON page", async () => {
    const { s, changes } = await signedIn();
    near();
    server.answer(() => new Response("<html>bad gateway</html>", { status: 502 }));

    await expect(s.token()).rejects.toThrow(/^token request 502$/);

    expect(s.signedIn).toBe(true);
    expect(changes).not.toHaveBeenCalled();
  });

  it("keeps the session on a network failure", async () => {
    const { s } = await signedIn();
    near();
    server.answer(() => Promise.reject(new TypeError("fetch failed")));

    await expect(s.token()).rejects.toThrow("fetch failed");

    expect(s.signedIn).toBe(true);
  });

  it("retries a failed background refresh with backoff, and stops once it lands", async () => {
    const { s } = await signedIn();
    const human = () => server.grants.filter((g) => g.client_id === "reckon").length;
    server.answer(() => new Response("", { status: 503 }));

    await vi.advanceTimersByTimeAsync(30_000);
    await flush();
    expect(s.signedIn).toBe(true);
    expect(human()).toBe(1);
    await vi.advanceTimersByTimeAsync(5_000);
    await flush();
    expect(human()).toBe(2);
    await vi.advanceTimersByTimeAsync(9_000);
    await flush();
    expect(human()).toBe(2);
    await vi.advanceTimersByTimeAsync(1_000);
    await flush();
    expect(human()).toBe(3);

    server.answer(fresh);
    await vi.advanceTimersByTimeAsync(20_000);
    await flush();
    expect(human()).toBe(4);
    expect(await s.token()).toBe("reckon-new");
    await vi.advanceTimersByTimeAsync(20_000);
    await flush();
    expect(human()).toBe(4);
  });

  it("restores nothing from a refresh that lands after sign-out", async () => {
    const { s, secrets, changes } = await signedIn();
    near();
    const pending = gate();
    server.answer(() => pending.opened);
    const token = s.token();
    await flush();

    await s.signOut();
    changes.mockClear();
    pending.open(Response.json({ access_token: "late", refresh_token: "late-rt", expires_in: 60 }));

    await expect(token).rejects.toThrow("session changed");
    await flush();
    expect(s.signedIn).toBe(false);
    expect(secrets.data.has("reckon.refreshToken")).toBe(false);
    expect(changes).not.toHaveBeenCalled();
    server.grants.length = 0;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(refreshes()).toBe(0);
  });

  it("ignores a refusal that lands after sign-out", async () => {
    const { s } = await signedIn();
    near();
    const pending = gate();
    server.answer(() => pending.opened);
    const token = s.token();
    await flush();

    await s.signOut();
    window.showWarningMessage.mockClear();
    pending.open(Response.json({ error: "invalid_grant" }, { status: 400 }));

    await expect(token).rejects.toThrow("invalid_grant");
    expect(window.showWarningMessage).not.toHaveBeenCalled();
  });

  it("renews a rejected token for the person it belongs to", async () => {
    const { s } = await signedIn("alice");
    const rejected = await s.token();

    server.answer(fresh);

    expect(await s.renew(rejected)).toBe("reckon-new");
    expect(refreshes()).toBe(1);
  });

  it("will not renew a token of someone who is no longer signed in", async () => {
    const { s } = await signedIn("alice");
    const rejected = await s.token();
    await s.signOut();

    await expect(s.renew(rejected)).rejects.toThrow("session changed");
  });

  it("does not leave the last person's delegate standing when a sign-in's agent flow fails", async () => {
    const { s, secrets } = await signedIn("alice");
    vi.mocked(env.openExternal).mockImplementation(async (uri) => {
      const u = new URL((uri as unknown as { toString(): string }).toString());
      if (u.searchParams.get("client_id") === "reckon-agent") {
        return true; // the agent flow never completes; the test fails it by its timeout
      }
      const back = new URL(u.searchParams.get("redirect_uri")!);
      back.searchParams.set("code", "c");
      back.searchParams.set("state", u.searchParams.get("state")!);
      back.hostname = "127.0.0.1";
      http.get(back.toString());
      return true;
    });
    server.as("bob");

    const signing = s.signIn();
    await vi.waitFor(async () => expect(secrets.data.has("reckon.agentRefreshToken")).toBe(false));

    await expect(s.token("delegate")).rejects.toThrow("no agent session");
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    await signing;
  });
});
