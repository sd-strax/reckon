// What every call to the backend shares: where it is, whose token goes on it,
// the one retry on a refused token (adr/0240), and how a refusal is reported.
// The domain clients (./investigations, ./actions, …) are built on this and
// hold nothing of it themselves.

/** A refused request, with its status and the server's JSON body when it sent one. */
export class BackendRequestError extends Error {
  constructor(message: string, readonly status: number, readonly body?: Record<string, unknown>) {
    super(message);
  }
}

/** The investigation a refused create points at — a case already investigated (#158) — or undefined. */
export function existingInvestigation(err: unknown): string | undefined {
  if (err instanceof BackendRequestError && err.status === 409 && typeof err.body?.investigation_id === "string") {
    return err.body.investigation_id;
  }
  return undefined;
}

export class BackendHttp {
  /**
   * @param baseUrl read per-call so a settings change needs no reload.
   * @param token   supplies a bearer token for authenticated calls; omitted for
   *                the public /status and /auth-config probes.
   * @param renew   a fresh token to replace one the backend refused; omitted, a
   *                401 stands.
   */
  constructor(
    private readonly baseUrl: () => string,
    private readonly token?: () => Promise<string>,
    private readonly renew?: (rejected: string) => Promise<string>,
  ) {}

  /** The absolute URL of a path on the backend. */
  url(path: string): string {
    return `${this.baseUrl()}${path}`;
  }

  async get<T>(path: string): Promise<T> {
    return this.authed<T>("GET", path);
  }

  async post<T>(path: string, body: unknown): Promise<T> {
    return this.authed<T>("POST", path, body);
  }

  /**
   * Send with the current token, and once more with a fresh one on a 401. Safe
   * for a write: a 401 comes from the auth middleware, before any handler ran.
   */
  async withBearer(send: (bearer: string) => Promise<Response>): Promise<Response> {
    if (!this.token) {
      throw new Error("no token source configured");
    }
    const bearer = await this.token();
    const res = await send(bearer);
    if (res.status !== 401 || !this.renew) {
      return res;
    }
    return send(await this.renew(bearer));
  }

  private async authed<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
    const res = await this.withBearer((bearer) => fetch(this.url(path), {
      method,
      headers: {
        Authorization: `Bearer ${bearer}`,
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(10_000),
    }));
    if (!res.ok) {
      // Surface the server's explanation when it sent one — a Gate 2 denial
      // or a guarded transition is information, not noise (approve/reject
      // rejections carry their reason in the error body).
      let detail = "";
      let parsed: Record<string, unknown> | undefined;
      try {
        parsed = (await res.json()) as Record<string, unknown>;
        detail = typeof parsed.error === "string" && parsed.error ? `: ${parsed.error}` : "";
      } catch {
        // non-JSON error body — the status alone will have to do
      }
      throw new BackendRequestError(`${method} ${path} → ${res.status}${detail}`, res.status, parsed);
    }
    return (await res.json()) as T;
  }
}
