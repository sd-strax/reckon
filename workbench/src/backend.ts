// The client for the reckon backend's public HTTP surface: the connection
// (where it is, whether this build can speak to it, who is signed in) and the
// domains behind it, each its own client over the shared transport
// (client/http.ts): investigations, actions, evidence, knowledge, cases,
// capabilities. A consumer declares the slice it needs (the `CacheClient`
// pattern) and this composition satisfies it.
//
// `status` mirrors server.StatusResponse (server/backend.go), including the
// API contract version that drives the design/13 §2 handshake: the extension
// pins a set of backend API versions it supports and fails closed with a
// diagnostic on mismatch, rather than dispatching against an incompatible
// surface.

import { ActionsClient } from "./client/actions";
import { CapabilitiesClient } from "./client/capabilities";
import { CasesClient } from "./client/cases";
import { EvidenceClient } from "./client/evidence";
import { BackendHttp } from "./client/http";
import { InvestigationsClient } from "./client/investigations";
import { KnowledgeClient } from "./client/knowledge";
import type { AuthConfig, BackendStatus, ComponentStatus, Me } from "./viewTypes";

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

export class BackendClient {
  private readonly http: BackendHttp;
  readonly investigations: InvestigationsClient;
  readonly actions: ActionsClient;
  readonly evidence: EvidenceClient;
  readonly knowledge: KnowledgeClient;
  readonly cases: CasesClient;
  readonly capabilities: CapabilitiesClient;

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
    this.http = new BackendHttp(baseUrl, token, renew);
    this.investigations = new InvestigationsClient(this.http);
    this.actions = new ActionsClient(this.http);
    this.evidence = new EvidenceClient(this.http);
    this.knowledge = new KnowledgeClient(this.http);
    this.cases = new CasesClient(this.http);
    this.capabilities = new CapabilitiesClient(this.http);
  }

  async status(): Promise<BackendStatus> {
    try {
      const res = await fetch(`${this.baseUrl()}/status`, {
        signal: AbortSignal.timeout(3_000),
      });
      const body = (await res.json()) as {
        overall?: string;
        components?: Record<string, ComponentStatus>;
        api_version?: number;
      };
      const apiVersion = typeof body.api_version === "number" ? body.api_version : null;
      return {
        reachable: true,
        overall: body.overall ?? "unknown",
        components: body.components ?? {},
        apiVersion,
        compatible: apiVersion !== null && SUPPORTED_API_VERSIONS.includes(apiVersion),
      };
    } catch {
      return {
        reachable: false,
        overall: "unreachable",
        components: {},
        apiVersion: null,
        compatible: false,
      };
    }
  }

  /** GET /api/auth-config — public; the login prerequisites. Throws on 503/unreachable. */
  async authConfig(): Promise<AuthConfig> {
    const res = await fetch(`${this.baseUrl()}/api/auth-config`, {
      signal: AbortSignal.timeout(5_000),
    });
    if (!res.ok) {
      throw new Error(`auth-config ${res.status} (interactive login may be unconfigured)`);
    }
    const body = (await res.json()) as { issuer?: string; client_id?: string; agent_client_id?: string };
    if (!body.issuer || !body.client_id) {
      throw new Error("auth-config missing issuer/client_id");
    }
    return { issuer: body.issuer, clientId: body.client_id, agentClientId: body.agent_client_id };
  }

  /** GET /api/me — the signed-in identity. Authenticated. */
  async me(): Promise<Me> {
    const body = await this.http.get<{
      subject?: string;
      preferred_username?: string;
      tenant_id?: string;
      roles?: string[];
    }>("/api/me");
    return {
      subject: body.subject ?? "",
      username: body.preferred_username ?? "",
      tenantId: body.tenant_id ?? "",
      roles: body.roles ?? [],
    };
  }

  /** A one-line diagnostic for an incompatible/unreachable backend, or null when compatible. */
  incompatibilityReason(status: BackendStatus): string | null {
    if (!status.reachable) {
      return "backend not reachable — run `reckon start`";
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
