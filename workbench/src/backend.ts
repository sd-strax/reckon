// The client for the reckon backend's public HTTP surface: the connection
// (where it is, whether this build can speak to it, who is signed in) and the
// domains behind it, each its own client over the shared transport
// (client/http.ts): investigations, actions, evidence, knowledge, cases,
// capabilities. A consumer declares the slice it needs (the `CacheClient`
// pattern) and this composition satisfies it.
//
// `status` mirrors server.StatusResponse (server/status.go), including the
// API contract version that drives the design/13 §2 handshake: the extension
// pins a set of backend API versions it supports and fails closed with a
// diagnostic on mismatch, rather than dispatching against an incompatible
// surface. Every authenticated call passes that gate (adr/0244).

import { ActionsClient } from "./client/actions";
import { CapabilitiesClient } from "./client/capabilities";
import { CasesClient } from "./client/cases";
import { EvidenceClient } from "./client/evidence";
import { BackendHttp } from "./client/http";
import { InvestigationsClient } from "./client/investigations";
import { KnowledgeClient } from "./client/knowledge";
import { SingleFlight } from "./singleFlight";
import type { AuthConfig, BackendStatus, Me } from "./viewTypes";
import * as shapes from "./wire/backendShapes";
import { decode, DecodeError } from "./wire/decode";

// The view types live in viewTypes.ts (types only); re-exported so host modules keep one import.
export type * from "./viewTypes";
export { BackendRequestError, existingInvestigation } from "./client/http";
export type { ActionsClient, ActionTypeRow } from "./client/actions";
export type { CapabilitiesClient } from "./client/capabilities";
export type { CasesClient } from "./client/cases";
export type { EvidenceClient } from "./client/evidence";
export type { InvestigationsClient } from "./client/investigations";
export type { KnowledgeClient } from "./client/knowledge";

/**
 * SUPPORTED_API_VERSIONS is the set of backend API contract versions
 * (server.APIVersion) this build of the workbench speaks. Add a version here
 * when the extension is updated to handle a new backend contract; a backend
 * outside this set is a hard, diagnosable incompatibility — never a silent
 * best-effort call.
 */
export const SUPPORTED_API_VERSIONS: readonly number[] = [1];

/** How long a compatible /status is taken as read before it is asked again. */
const COMPATIBLE_FOR_MS = 30_000;

export class BackendClient {
  private readonly http: BackendHttp;
  readonly investigations: InvestigationsClient;
  readonly actions: ActionsClient;
  readonly evidence: EvidenceClient;
  readonly knowledge: KnowledgeClient;
  readonly cases: CasesClient;
  readonly capabilities: CapabilitiesClient;
  // The last good handshake: which backend, and until when it is taken as read.
  private compatible: { base: string; until: number } | null = null;
  private readonly handshake = new SingleFlight<void>();

  /**
   * @param baseUrl read per-call so a settings change needs no reload.
   * @param token   supplies a bearer token for authenticated calls; omitted for
   *                the public /status and /auth-config probes.
   * @param renew   a fresh token to replace one the backend refused; omitted, a
   *                401 stands.
   */
  constructor(
    private readonly baseUrl: () => string,
    token?: () => Promise<string>,
    renew?: (rejected: string) => Promise<string>,
  ) {
    this.http = new BackendHttp(baseUrl, (base) => this.ensureCompatible(base), token, renew);
    this.investigations = new InvestigationsClient(this.http);
    this.actions = new ActionsClient(this.http);
    this.evidence = new EvidenceClient(this.http);
    this.knowledge = new KnowledgeClient(this.http);
    this.cases = new CasesClient(this.http);
    this.capabilities = new CapabilitiesClient(this.http);
  }

  /** The handshake: reads /status, and refreshes what ensureCompatible relies on. */
  status(): Promise<BackendStatus> {
    return this.statusOf(this.baseUrl());
  }

  private async statusOf(base: string): Promise<BackendStatus> {
    const status = await this.readStatus(base);
    if (status.compatible) {
      this.compatible = { base, until: Date.now() + COMPATIBLE_FOR_MS };
    } else if (this.compatible?.base === base) {
      this.compatible = null;
    }
    return status;
  }

  private async readStatus(base: string): Promise<BackendStatus> {
    const unreachable = { reachable: false, overall: "unreachable", components: {}, apiVersion: null, compatible: false };
    // It answered, so whatever is wrong with the answer, it is not unreachable.
    const answered = { ...unreachable, reachable: true, overall: "unknown" };
    let res: Response;
    let raw: unknown;
    try {
      res = await fetch(`${base}/status`, { signal: AbortSignal.timeout(3_000) });
      raw = await res.json();
    } catch (err) {
      return err instanceof SyntaxError ? { ...answered, unreadable: "unexpected response from /status: not JSON" } : unreachable;
    }
    if (typeof raw === "object" && raw !== null && !("api_version" in raw)) {
      return answered; // from before the handshake
    }
    try {
      const body = decode("/status", shapes.status, raw);
      return {
        reachable: true,
        overall: body.overall,
        components: body.components,
        apiVersion: body.api_version,
        compatible: SUPPORTED_API_VERSIONS.includes(body.api_version),
      };
    } catch (err) {
      if (err instanceof DecodeError) {
        return { ...answered, unreadable: err.message };
      }
      throw err;
    }
  }

  /**
   * The gate every authenticated call goes through (adr/0244): nothing is read
   * from or written to a backend the workbench does not speak to, whichever
   * surface asked. A good answer holds for COMPATIBLE_FOR_MS, for that backend
   * only; a bad one does not hold at all, so a backend put right is used at once.
   */
  private async ensureCompatible(base: string): Promise<void> {
    if (this.compatible?.base === base && Date.now() < this.compatible.until) {
      return;
    }
    await this.handshake.run(base, async () => {
      const reason = this.incompatibilityReason(await this.statusOf(base));
      if (reason) {
        throw new Error(reason);
      }
    });
  }

  /** GET /api/auth-config — public; the login prerequisites. Throws on 503/unreachable. */
  async authConfig(): Promise<AuthConfig> {
    const res = await fetch(`${this.baseUrl()}/api/auth-config`, {
      signal: AbortSignal.timeout(5_000),
    });
    if (!res.ok) {
      throw new Error(`auth-config ${res.status} (interactive login may be unconfigured)`);
    }
    const body = decode("/api/auth-config", shapes.authConfig, await res.json());
    if (!body.issuer || !body.client_id) {
      throw new Error("auth-config missing issuer/client_id");
    }
    return { issuer: body.issuer, clientId: body.client_id, agentClientId: body.agent_client_id };
  }

  /** GET /api/me — the signed-in identity. Authenticated. */
  async me(): Promise<Me> {
    const body = await this.http.get("/api/me", shapes.me);
    return {
      subject: body.subject,
      username: body.preferred_username,
      tenantId: body.tenant_id,
      roles: body.roles,
    };
  }

  /** A one-line diagnostic for an incompatible/unreachable backend, or null when compatible. */
  incompatibilityReason(status: BackendStatus): string | null {
    if (!status.reachable) {
      return "backend not reachable — run `reckon start`";
    }
    if (status.unreadable) {
      return `${status.unreadable} — align versions`;
    }
    if (status.apiVersion === null) {
      return "backend predates the version handshake — upgrade the backend";
    }
    if (!status.compatible) {
      return `backend API v${status.apiVersion} unsupported (this workbench speaks v${SUPPORTED_API_VERSIONS.join(", v")}) — align versions`;
    }
    return null;
  }
}
