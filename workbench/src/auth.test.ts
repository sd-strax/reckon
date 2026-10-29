// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Session } from "./auth";
import { FakeSecretStorage, fakeLog, resetVscodeFake } from "./test/vscode";

const ISSUER = "http://127.0.0.1:8180/realms/reckon";
const TOKEN_URL = `${ISSUER}/protocol/openid-connect/token`;
const config = async () => ({ issuer: ISSUER, clientId: "reckon", agentClientId: "reckon-agent" });

/** The IdP: discovery, and a token endpoint that mints `<client>-<n>` tokens unless told to refuse. */
function idp() {
  const grants: Record<string, string>[] = [];
  let refuse = false;
  let n = 0;
  const fetch = vi.fn(async (url: string, init?: { body?: string }) => {
    if (url.endsWith("/.well-known/openid-configuration")) {
      return Response.json({ authorization_endpoint: `${ISSUER}/auth`, token_endpoint: TOKEN_URL });
    }
    const form = Object.fromEntries(new URLSearchParams(init?.body ?? ""));
    grants.push(form);
    if (refuse) {
      return Response.json({ error: "invalid_grant", error_description: "Token is not active" }, { status: 400 });
    }
    n++;
    return Response.json({ access_token: `${form.client_id}-at-${n}`, refresh_token: `${form.client_id}-rt-${n}`, expires_in: 60 });
  });
  return { fetch, grants, refuse: () => { refuse = true; } };
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
