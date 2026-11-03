// The capability layer over HTTP (design/03, 11): the verb catalog with its
// availability, the operator's enablement view and the human-confirmed apply,
// and the read-tool dispatch target. Answers are decoded through the wire
// shapes (adr/0244).
import type { BackendHttp } from "./http";
import * as shapes from "../wire/backendShapes";
import type { Capability, CapabilityResult, ConfigField, Enablement } from "../viewTypes";

export class CapabilitiesClient {
  constructor(private readonly http: BackendHttp) {}

  /**
   * GET /api/capabilities — the verb catalog with per-tenant availability
   * (03 §2.8). The agent loop trims this to `available` verbs before turning
   * them into LLM tool definitions (03 §6.3). Authenticated.
   */
  async list(): Promise<Capability[]> {
    const body = await this.http.get("/api/capabilities", shapes.capabilities);
    return body.capabilities.map((c) => ({
      verb: c.descriptor.verb,
      intent: c.descriptor.intent,
      inputs: c.descriptor.inputs,
      output: c.descriptor.output,
      status: c.status,
    }));
  }

  /** GET /api/capabilities — count of resolvable verbs. Authenticated. */
  async count(): Promise<number> {
    const body = await this.http.get("/api/capabilities", shapes.capabilities);
    return body.capabilities.length;
  }

  /**
   * GET /api/enablement — the operator view of installed adapters and the
   * gaps a disabled one could close (11 §5.1, §6.2). Analyst role.
   */
  async enablement(): Promise<Enablement> {
    const body = await this.http.get("/api/enablement", shapes.enablement);
    return {
      adapters: body.adapters.map((a) => {
        const fields: Record<string, ConfigField> = {};
        const props = a.config_schema?.properties ?? {};
        for (const [k, p] of Object.entries(props)) {
          fields[k] = { type: p.type, description: p.description, secret: p["x-secret"] === true };
        }
        return {
          name: a.name,
          class: a.class,
          enabled: a.enabled,
          scenario: a.scenario,
          configFields: fields,
          requiredFields: a.config_schema?.required ?? [],
          supportable: a.supportable,
        };
      }),
      verbs: body.verbs.map((v) => ({
        verb: v.verb,
        adapters: v.adapters,
        enabled: v.enabled,
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
    body: { entity?: unknown; window?: { from: string; to: string }; extra?: Record<string, unknown>; investigation_ref?: string },
  ): Promise<CapabilityResult> {
    const r = await this.http.post(`/api/capability/${encodeURIComponent(verb)}`, body, shapes.capabilityInvoke);
    return {
      callId: r.call_id,
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
