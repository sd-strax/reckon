// Interactive login for the workbench (design/13 §7 step 1, 05 §3.4) — now
// dual-client per implementation/agent-sidecar.md §5.
//
// OIDC authorization-code + PKCE against the bundled Keycloak, twice: the
// interactive flow on the HUMAN client (`reckon`), then a second flow on the
// AGENT client (`reckon-agent`, the one whose mapper stamps delegate_kind
// issuer-side). The second flow completes silently on the realm SSO session
// cookie the first one set — no re-prompt, no password, public clients only.
// The extension never sees a password: it opens the browser to Keycloak,
// catches the redirect on a loopback listener, and exchanges the code (+ PKCE
// verifier) for tokens.
//
// The extension is the SOLE auth owner: both refresh tokens live in
// vscode.SecretStorage — never in settings, never on disk in the clear, and
// never handed to the sidecar (which receives short-lived access tokens over
// the getToken RPC callback, per call). A 401 downstream forces a refresh.
//
// The issuer + both clients are discovered from the backend's
// /api/auth-config, so nothing here hardcodes Keycloak's port, realm, or
// client ids.

import * as crypto from "crypto";
import * as http from "http";
import { AddressInfo } from "net";
import * as vscode from "vscode";
import { SingleFlight } from "./singleFlight";
import { errText } from "./util";

/** Which realm client a token comes from — the actor-attribution axis. */
export type TokenKind = "human" | "delegate";

/** Where each persisted refresh token lives in SecretStorage. */
const REFRESH_TOKEN_KEYS: Record<TokenKind, string> = {
  human: "reckon.refreshToken",
  delegate: "reckon.agentRefreshToken",
};

/** Renew this many ms before the access token's expiry (matches agent/auth.go). */
const REFRESH_SKEW_MS = 30_000;

/** A failed background refresh retries after this, doubling up to the cap. */
const REFRESH_RETRY_MS = 5_000;
const REFRESH_RETRY_MAX_MS = 5 * 60_000;

interface AuthConfig {
  issuer: string;
  clientId: string;
  agentClientId?: string;
}

interface OidcEndpoints {
  authorization: string;
  token: string;
}

interface TokenSet {
  accessToken: string;
  refreshToken: string;
  /** Epoch ms at which the access token expires. */
  expiresAt: number;
}

/** Public claims the UI shows; parsed from /api/me, not the raw token. */
export interface Identity {
  subject: string;
  username: string;
  tenantId: string;
  roles: string[];
}

/**
 * Session owns both clients' tokens and their lifecycle. `token(kind)` always
 * returns a currently-valid access token for that kind (refreshing silently
 * when near expiry; `force` mints fresh regardless — the 401 backstop),
 * mirroring agent.TokenSource on the Go side. `onDidChange` fires whenever
 * signed-in state flips so the UI can re-render. Signed in means a token can
 * still be had: the IdP refusing the refresh token ends the session, and a
 * network or server failure does not (adr/0240).
 */
export class Session {
  private readonly tokens = new Map<TokenKind, TokenSet>();
  private endpoints: OidcEndpoints | null = null;
  private config: AuthConfig | null = null;
  private readonly refreshTimers = new Map<TokenKind, ReturnType<typeof setTimeout>>();
  // Failed background refreshes in a row, per side: the retry's backoff.
  private readonly refreshFailures = new Map<TokenKind, number>();
  private readonly refreshes = new SingleFlight<void>();
  // Bumped per side when its session ends or is replaced: a grant begun before
  // that is discarded when it lands. Per side, because the two sign in one
  // after the other and the second must not discard the first's refresh.
  private readonly generations = new Map<TokenKind, number>();

  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChange = this.changed.event;
  private readonly identityChanged = new vscode.EventEmitter<void>();
  /**
   * Fires when the person behind the session changes, signing out included:
   * whatever was held for the previous person must go (adr/0237). Fires before
   * onDidChange, so it is gone before anything re-renders.
   */
  readonly onDidChangeIdentity = this.identityChanged.event;
  private knownSubject: string | undefined;

  constructor(
    private readonly secrets: vscode.SecretStorage,
    private readonly loadAuthConfig: () => Promise<AuthConfig>,
    private readonly log: vscode.LogOutputChannel,
  ) {}

  /** Whose session this is: the human token's `sub`. Undefined when signed out. */
  get subject(): string | undefined {
    return this.knownSubject;
  }

  /** Signed in means the HUMAN session is live; the delegate side may lag (see token). */
  get signedIn(): boolean {
    return this.tokens.has("human");
  }

  /**
   * Attempt a silent restore from the persisted refresh tokens. Safe to call
   * on activation — a failure (expired/revoked refresh token, backend down)
   * just leaves that side signed out. The human side is the signed-in
   * criterion; a delegate-side failure surfaces later, at first agent turn.
   */
  async restore(): Promise<void> {
    for (const kind of ["human", "delegate"] as TokenKind[]) {
      const generation = this.generation(kind);
      const refreshToken = await this.secrets.get(REFRESH_TOKEN_KEYS[kind]);
      if (!refreshToken) {
        continue;
      }
      try {
        await this.ensureDiscovery();
        const clientId = this.clientIdFor(kind);
        if (!clientId) {
          continue;
        }
        await this.grant(kind, new URLSearchParams({
          grant_type: "refresh_token",
          client_id: clientId,
          refresh_token: refreshToken,
        }), generation);
        this.log.info(`${kind} session restored from stored refresh token`);
      } catch (err) {
        this.log.info(`${kind} session restore failed (${errText(err)}); signed out on that side`);
        // Only a refusal ends it: with the backend down at start-up, the stored
        // token still restores the next time the window opens.
        if (err instanceof TokenRejected && generation === this.generation(kind)) {
          await this.endSide(kind);
        }
      }
    }
    if (this.signedIn) {
      this.noteIdentity();
      this.changed.fire();
    }
  }

  /**
   * Run the interactive PKCE login on the human client, then the silent
   * second flow on the agent client (rides the realm SSO session — the
   * browser bounces straight back without a form). Resolves signed-in, throws
   * on failure/cancel of the HUMAN flow; an agent-flow failure logs and
   * defers (turns will surface it) rather than blocking sign-in.
   */
  async signIn(): Promise<void> {
    await this.ensureDiscovery();
    await this.pkceFlow("human", this.config!.clientId);
    this.log.info("signed in (human client)");
    // The delegate tokens were the last session's, and may be someone else's:
    // if the agent flow below fails they must not stand in for the new person.
    await this.endSide("delegate");
    // Now, not after the agent flow: the human tokens are already the new person's.
    this.noteIdentity();

    const agentClient = this.config!.agentClientId;
    if (agentClient) {
      try {
        await this.pkceFlow("delegate", agentClient);
        this.log.info("agent session established (silent second flow)");
      } catch (err) {
        this.log.warn(`agent-client flow failed (${errText(err)}); agent turns will prompt to sign in again`);
      }
    } else {
      this.log.info("backend advertises no agent client — delegate tokens unavailable");
    }
    this.changed.fire();
  }

  async signOut(): Promise<void> {
    await Promise.all((["human", "delegate"] as TokenKind[]).map((kind) => this.endSide(kind)));
    this.log.info("signed out");
    this.noteIdentity();
    this.changed.fire();
  }

  /**
   * A currently-valid access token of the given kind, refreshing if within
   * the skew window (or unconditionally when force — the 401 backstop).
   * Throws if that side is not signed in or the refresh fails — callers
   * surface that as a signed-out state, never a silent unauthenticated
   * request.
   */
  async token(kind: TokenKind = "human", force = false): Promise<string> {
    const set = this.tokens.get(kind);
    if (!set) {
      throw notSignedIn(kind);
    }
    if (force || Date.now() >= set.expiresAt - REFRESH_SKEW_MS) {
      const generation = this.generation(kind);
      await this.refresh(kind);
      // Whoever holds the session now, it is not the one this call was for.
      if (generation !== this.generation(kind)) {
        throw new Error("the session changed during the token refresh");
      }
    }
    return this.tokens.get(kind)!.accessToken;
  }

  /**
   * The 401 backstop for the backend client: a fresh token to replace
   * `rejected`, which throws unless the session still belongs to the person
   * that token was for. A request is never retried as someone else.
   */
  async renew(rejected: string): Promise<string> {
    const subject = tokenSubject(rejected);
    if (subject === undefined || subject !== this.knownSubject) {
      throw new Error("the session changed; not retrying the request");
    }
    return this.token("human", true);
  }

  dispose(): void {
    for (const timer of this.refreshTimers.values()) {
      clearTimeout(timer);
    }
    this.changed.dispose();
    this.identityChanged.dispose();
  }

  // --- internals ------------------------------------------------------------

  private generation(kind: TokenKind): number {
    return this.generations.get(kind) ?? 0;
  }

  /** Discard any grant in flight for the side. */
  private invalidate(kind: TokenKind): void {
    this.generations.set(kind, this.generation(kind) + 1);
  }

  /** Forget one side: its tokens, its timer, its stored refresh token. */
  private async endSide(kind: TokenKind): Promise<void> {
    this.invalidate(kind);
    clearTimeout(this.refreshTimers.get(kind));
    this.refreshTimers.delete(kind);
    this.refreshFailures.delete(kind);
    this.tokens.delete(kind);
    await this.secrets.delete(REFRESH_TOKEN_KEYS[kind]);
  }

  /**
   * Renew one side, joining a refresh already in flight: under rotation a
   * second POST of the same refresh token fails, and strict reuse detection
   * revokes the session. A refusal ends the session; the human side signing out
   * goes through signOut, so everything held for the person goes with it.
   */
  private refresh(kind: TokenKind): Promise<void> {
    const generation = this.generation(kind);
    return this.refreshes.run(`${kind}:${generation}`, async () => {
      const set = this.tokens.get(kind);
      if (!set) {
        throw notSignedIn(kind);
      }
      const clientId = this.clientIdFor(kind);
      if (!clientId) {
        throw new Error(`no ${kind} client configured`);
      }
      try {
        await this.grant(kind, new URLSearchParams({
          grant_type: "refresh_token",
          client_id: clientId,
          refresh_token: set.refreshToken,
        }), generation);
      } catch (err) {
        if (err instanceof TokenRejected && generation === this.generation(kind)) {
          this.log.warn(`${kind} session refused by the identity provider (${errText(err)}); signing out that side`);
          if (kind === "human") {
            await this.signOut();
            void vscode.window.showWarningMessage("reckon: your session has ended — sign in again");
          } else {
            await this.endSide(kind);
          }
        }
        throw err;
      }
    });
  }

  /** Fire onDidChangeIdentity if the human tokens now belong to someone else. */
  private noteIdentity(): void {
    const set = this.tokens.get("human");
    const subject = set ? tokenSubject(set.accessToken) : undefined;
    // A token with no readable subject cannot be shown to be the same person.
    const same = subject === this.knownSubject && (subject !== undefined || !set);
    if (!same) {
      this.knownSubject = subject;
      this.identityChanged.fire();
    }
  }

  private clientIdFor(kind: TokenKind): string | undefined {
    return kind === "human" ? this.config?.clientId : this.config?.agentClientId;
  }

  private async ensureDiscovery(): Promise<void> {
    if (this.endpoints && this.config) {
      return;
    }
    this.config = await this.loadAuthConfig();
    const wellKnown = `${trimSlash(this.config.issuer)}/.well-known/openid-configuration`;
    const res = await fetch(wellKnown, { signal: AbortSignal.timeout(5_000) });
    if (!res.ok) {
      throw new Error(`OIDC discovery ${res.status} at ${wellKnown}`);
    }
    const doc = (await res.json()) as {
      authorization_endpoint?: string;
      token_endpoint?: string;
    };
    if (!doc.authorization_endpoint || !doc.token_endpoint) {
      throw new Error("OIDC discovery missing authorization/token endpoint");
    }
    this.endpoints = { authorization: doc.authorization_endpoint, token: doc.token_endpoint };
  }

  /** One full PKCE authorization-code flow against the given client. */
  private async pkceFlow(kind: TokenKind, clientId: string): Promise<void> {
    const verifier = base64url(crypto.randomBytes(32));
    const challenge = base64url(crypto.createHash("sha256").update(verifier).digest());
    const state = base64url(crypto.randomBytes(16));

    const { code, redirectUri } = await this.captureAuthCode(clientId, challenge, state);
    // No generation: this grant replaces whatever the side holds.
    await this.grant(kind, new URLSearchParams({
      grant_type: "authorization_code",
      client_id: clientId,
      code,
      redirect_uri: redirectUri,
      code_verifier: verifier,
    }));
  }

  /**
   * Open the browser to Keycloak and capture the redirect on a loopback
   * listener. The realm allows http://localhost:* redirect URIs
   * (keycloak_realm.json), so a random loopback port is a valid target. On an
   * existing SSO session (the second, agent-client flow) Keycloak redirects
   * straight back without showing a form.
   */
  private captureAuthCode(
    clientId: string,
    challenge: string,
    state: string,
  ): Promise<{ code: string; redirectUri: string }> {
    return new Promise((resolve, reject) => {
      const server = http.createServer((req, res) => {
        const url = new URL(req.url ?? "/", "http://127.0.0.1");
        if (url.pathname !== "/callback") {
          res.writeHead(404).end();
          return;
        }
        const returnedState = url.searchParams.get("state");
        const code = url.searchParams.get("code");
        const err = url.searchParams.get("error");
        res.writeHead(200, { "Content-Type": "text/html" });
        res.end(
          `<html><body style="font-family:sans-serif;padding:2rem">` +
          `<h2>reckon</h2><p>${err ? "Sign-in failed." : "Signed in. You can close this tab."}</p>` +
          `</body></html>`,
        );
        server.close();
        if (err) {
          reject(new Error(`authorization error: ${err}`));
        } else if (returnedState !== state) {
          reject(new Error("state mismatch (possible CSRF) — sign-in aborted"));
        } else if (!code) {
          reject(new Error("no authorization code in redirect"));
        } else {
          resolve({ code, redirectUri });
        }
      });

      let redirectUri = "";
      server.on("error", reject);
      // Bind the listener to the IPv4 loopback (deterministic), but advertise
      // the redirect as `localhost` — Keycloak matches redirect URIs by exact
      // host string, and the realm allows `http://localhost:*`, not 127.0.0.1
      // (keycloak_realm.json). The browser resolves localhost → 127.0.0.1
      // (directly, or via Happy-Eyeballs fallback), so it still hits this listener.
      server.listen(0, "127.0.0.1", () => {
        const port = (server.address() as AddressInfo).port;
        redirectUri = `http://localhost:${port}/callback`;
        const authUrl = new URL(this.endpoints!.authorization);
        authUrl.searchParams.set("client_id", clientId);
        authUrl.searchParams.set("response_type", "code");
        authUrl.searchParams.set("redirect_uri", redirectUri);
        authUrl.searchParams.set("scope", "openid");
        authUrl.searchParams.set("state", state);
        authUrl.searchParams.set("code_challenge", challenge);
        authUrl.searchParams.set("code_challenge_method", "S256");
        void vscode.env.openExternal(vscode.Uri.parse(authUrl.toString()));
      });

      // Don't leave a dangling listener if the user never completes the flow.
      setTimeout(() => {
        if (server.listening) {
          server.close();
          reject(new Error("sign-in timed out (no redirect within 5 minutes)"));
        }
      }, 5 * 60_000);
    });
  }

  /**
   * POST the token endpoint, store the result under kind, arm its refresh
   * timer. A grant begun under `generation` is discarded if the side has moved
   * on since; without one the grant replaces the side and discards any other.
   */
  private async grant(kind: TokenKind, form: URLSearchParams, generation?: number): Promise<void> {
    await this.ensureDiscovery();
    const res = await fetch(this.endpoints!.token, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: form.toString(),
      signal: AbortSignal.timeout(10_000),
    });
    // A gateway's error page is not JSON: the status must survive it.
    const body = (await res.json().catch(() => ({}))) as {
      access_token?: string;
      refresh_token?: string;
      expires_in?: number;
      error?: string;
      error_description?: string;
    };
    if (!res.ok) {
      const detail = body.error_description ?? body.error;
      const message = `token request ${res.status}${detail ? `: ${detail}` : ""}`;
      throw res.status === 400 || res.status === 401 ? new TokenRejected(message) : new Error(message);
    }
    if (!body.access_token) {
      throw new Error(`token request ${res.status}: no access_token`);
    }
    if (generation === undefined) {
      this.invalidate(kind);
    } else if (generation !== this.generation(kind)) {
      return;
    }
    const ttlMs = (body.expires_in && body.expires_in > 0 ? body.expires_in : 60) * 1000;
    // A response may omit the refresh token: the one just used stays good.
    const refreshToken = body.refresh_token ?? form.get("refresh_token") ?? "";
    this.tokens.set(kind, { accessToken: body.access_token, refreshToken, expiresAt: Date.now() + ttlMs });
    this.refreshFailures.delete(kind);
    // Before the await: a sign-out from here on finds the timer and clears it.
    this.armRefresh(kind, Math.max(ttlMs - REFRESH_SKEW_MS, 5_000));
    if (refreshToken) {
      await this.secrets.store(REFRESH_TOKEN_KEYS[kind], refreshToken);
    } else {
      await this.secrets.delete(REFRESH_TOKEN_KEYS[kind]);
    }
  }

  private armRefresh(kind: TokenKind, delay: number): void {
    clearTimeout(this.refreshTimers.get(kind));
    this.refreshTimers.set(kind, setTimeout(() => {
      this.refresh(kind).catch((err) => {
        // A refusal ended the session; anything else keeps it, so keep trying.
        if (!this.tokens.has(kind)) {
          return;
        }
        const failures = (this.refreshFailures.get(kind) ?? 0) + 1;
        this.refreshFailures.set(kind, failures);
        const retry = Math.min(REFRESH_RETRY_MS * 2 ** (failures - 1), REFRESH_RETRY_MAX_MS);
        this.log.info(`background ${kind} token refresh failed (${errText(err)}); retrying in ${retry / 1000}s`);
        this.armRefresh(kind, retry);
      });
    }, delay));
  }
}

/** The identity provider refused the grant: the session is over, not unreachable. */
class TokenRejected extends Error {}

function notSignedIn(kind: TokenKind): Error {
  return new Error(kind === "delegate"
    ? "no agent session — sign in again to establish it"
    : "not signed in");
}

/** The `sub` claim of a JWT; undefined if the token has none to read. For keying, not trust: the backend verifies the token. */
function tokenSubject(token: string): string | undefined {
  try {
    const payload = JSON.parse(Buffer.from(token.split(".")[1] ?? "", "base64url").toString()) as { sub?: unknown };
    return typeof payload.sub === "string" ? payload.sub : undefined;
  } catch {
    return undefined;
  }
}

function base64url(b: Buffer): string {
  return b.toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function trimSlash(s: string): string {
  return s.replace(/\/+$/, "");
}
