// The capability layer over HTTP (design/03, 11): the verb catalog with its
// availability, the operator's enablement view and the human-confirmed apply,
// and the read-tool dispatch target.
import type { BackendHttp } from "./http";
import type { Capability, CapabilityInput, CapabilityResult, ConfigField, Enablement } from "../viewTypes";

export class CapabilitiesClient {
  constructor(private readonly http: BackendHttp) {}

  /**
   * GET /api/capabilities — the verb catalog with per-tenant availability
   * (03 §2.8). The agent loop trims this to `available` verbs before turning
   * them into LLM tool definitions (03 §6.3). Authenticated.
   */
  async list(): Promise<Capability[]> {
    interface Raw {
      descriptor?: { verb?: string; intent?: string; inputs?: CapabilityInput[]; output?: string };
      status?: string;
    }
    const body = await this.http.get<{ capabilities?: Raw[] }>("/api/capabilities");
    return (body.capabilities ?? []).map((c) => ({
      verb: c.descriptor?.verb ?? "",
      intent: c.descriptor?.intent ?? "",
      inputs: c.descriptor?.inputs ?? [],
      output: c.descriptor?.output ?? "",
      status: c.status ?? "unavailable",
    }));
  }

  /** GET /api/capabilities — count of resolvable verbs. Authenticated. */
  async count(): Promise<number> {
    const body = await this.http.get<{ capabilities?: unknown[] } | unknown[]>("/api/capabilities");
    if (Array.isArray(body)) {
      return body.length;
    }
    return body.capabilities?.length ?? 0;
  }

  /**
   * GET /api/enablement — the operator view of installed adapters and the
   * gaps a disabled one could close (11 §5.1, §6.2). Analyst role.
   */
  async enablement(): Promise<Enablement> {
    interface RawAdapter {
      name?: string; class?: string; enabled?: boolean; scenario?: string;
      config_schema?: { properties?: Record<string, { type?: string; description?: string; "x-secret"?: boolean }>; required?: string[] };
      supportable?: boolean;
    }
    interface RawVerb {
      verb?: string; adapters?: string[]; enabled?: boolean; closable_by?: string[];
    }
    const body = await this.http.get<{ adapters?: RawAdapter[]; verbs?: RawVerb[] }>("/api/enablement");
    return {
      adapters: (body.adapters ?? []).map((a) => {
        const fields: Record<string, ConfigField> = {};
        const props = a.config_schema?.properties ?? {};
        for (const [k, p] of Object.entries(props)) {
          fields[k] = { type: p.type, description: p.description, secret: p["x-secret"] === true };
        }
        return {
          name: a.name ?? "",
          class: a.class ?? "",
          enabled: a.enabled ?? false,
          scenario: a.scenario,
          configFields: fields,
          requiredFields: a.config_schema?.required ?? [],
          supportable: a.supportable ?? false,
        };
      }),
      verbs: (body.verbs ?? []).map((v) => ({
        verb: v.verb ?? "",
        adapters: v.adapters ?? [],
        enabled: v.enabled ?? false,
        closableBy: v.closable_by ?? [],
      })),
    };
  }

  /**
   * POST /api/enablement/adapters/{name} — the human-confirmed apply
   * (11 §5.1). Always the HUMAN token: the backend 403s a delegate token, and
   * no sidecar tool reaches this path — the tool gap is the guarantee.
   */
  async applyEnablement(adapter: string, enabled: boolean, config: Record<string, string>): Promise<void> {
    await this.http.post(`/api/enablement/adapters/${encodeURIComponent(adapter)}`, { enabled, config });
  }

  /**
   * POST /api/capability/{verb} — the agent loop's read-tool dispatch target
   * (03 §3.4). The body carries the entity to resolve templates against, an
   * optional window, and extra template roots. Analyst role.
   */
  async invoke(
    verb: string,
    body: { entity?: unknown; window?: { from: string; to: string }; extra?: Record<string, unknown> },
  ): Promise<CapabilityResult> {
    interface Raw {
      verb?: string;
      coverage?: string;
      ocsf_event_refs?: string[];
      observed_data_refs?: string[];
      entity_refs?: string[];
      degradation_notes?: string[];
      events?: unknown[];
      normalized?: unknown[];
    }
    const r = await this.http.post<Raw>(`/api/capability/${encodeURIComponent(verb)}`, body);
    return {
      verb: r.verb ?? verb,
      coverage: r.coverage ?? "UNKNOWN",
      ocsfEventRefs: r.ocsf_event_refs ?? [],
      observedDataRefs: r.observed_data_refs ?? [],
      entityRefs: r.entity_refs ?? [],
      degradationNotes: r.degradation_notes ?? [],
      events: r.events ?? [],
      normalized: r.normalized ?? [],
    };
  }
}
