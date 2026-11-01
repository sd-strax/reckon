// What every call to the backend shares: where it is, whose token goes on it,
// the one retry on a refused token (adr/0240), the compatibility gate every
// authenticated call passes (adr/0244), how a refusal is reported, and how an
// answer is read: through a wire shape, never cast. The domain clients
// (./investigations, ./actions, …) are built on this and hold nothing of it.
import { decode, DecodeError, type Decoder } from "../wire/decode";

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
   * @param gate    run before every authenticated call with the base it goes
   *                to; throws when that backend must not be spoken to.
   * @param token   supplies a bearer token for authenticated calls; omitted for
   *                the public /status and /auth-config probes.
   * @param renew   a fresh token to replace one the backend refused; omitted, a
   *                401 stands.
   */
  constructor(
    private readonly baseUrl: () => string,
    private readonly gate: (base: string) => Promise<void>,
    private readonly token?: () => Promise<string>,
    private readonly renew?: (rejected: string) => Promise<string>,
  ) {}

  async get<T>(path: string, shape: Decoder<T>): Promise<T> {
    return this.decoded(`GET ${path}`, shape, await this.authed("GET", path));
  }

  /** A POST whose answer is read through `shape`; without one, only its success counts. */
  async post(path: string, body: unknown): Promise<void>;
  async post<T>(path: string, body: unknown, shape: Decoder<T>): Promise<T>;
  async post<T>(path: string, body: unknown, shape?: Decoder<T>): Promise<T | void> {
    const res = await this.authed("POST", path, body);
    return shape ? this.decoded(`POST ${path}`, shape, res) : undefined;
  }

  private async decoded<T>(from: string, shape: Decoder<T>, res: Response): Promise<T> {
    const body: unknown = await res.json().catch(() => {
      throw new DecodeError(`unexpected response from ${from}: not JSON`);
    });
    return decode(from, shape, body);
  }

  /**
   * Send with the current token, and once more with a fresh one on a 401. Safe
   * for a write: a 401 comes from the auth middleware, before any handler ran.
   * The URL is read once: the backend checked is the backend called, whatever
   * the setting does meanwhile.
   */
  async withBearer(send: (base: string, bearer: string) => Promise<Response>): Promise<Response> {
    if (!this.token) {
      throw new Error("no token source configured");
    }
    const base = this.baseUrl();
    await this.gate(base);
    const bearer = await this.token();
    const res = await send(base, bearer);
    if (res.status !== 401 || !this.renew) {
      return res;
    }
    return send(base, await this.renew(bearer));
  }

  private async authed(method: "GET" | "POST", path: string, body?: unknown): Promise<Response> {
    const res = await this.withBearer((base, bearer) => fetch(`${base}${path}`, {
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
    return res;
  }
}
