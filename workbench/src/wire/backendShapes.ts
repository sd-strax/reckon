// The backend's responses, as the workbench reads them (adr/0244). Each mirrors
// the Go view that serves it: a field is required where the Go tag has no
// omitempty. Only the fields the workbench uses are named.
import { anything, bool, lazy, list, num, object, optional, record, str, type Decoded, type Decoder } from "./decode";

const componentStatus = object({ ready: bool, message: str });

export const status = object({
  overall: str,
  components: record(componentStatus),
  api_version: num,
});

export const authConfig = object({
  issuer: str,
  client_id: str,
  agent_client_id: optional(str),
});

export const me = object({
  subject: str,
  preferred_username: str,
  tenant_id: str,
  roles: list(str),
});

export const capabilities = object({
  capabilities: list(object({
    descriptor: object({
      verb: str,
      intent: str,
      inputs: list(object({ name: str, type: str, required: bool, desc: optional(str) })),
      output: str,
    }),
    status: str,
  })),
  case_queue: optional(str),
});

export const enablement = object({
  adapters: list(object({
    name: str,
    class: str,
    enabled: bool,
    scenario: optional(str),
    config_schema: optional(object({
      properties: optional(record(object({
        type: optional(str),
        description: optional(str),
        "x-secret": optional(bool),
      }))),
      required: optional(list(str)),
    })),
    supportable: bool,
  })),
  verbs: list(object({
    verb: str,
    adapters: list(str),
    enabled: bool,
    closable_by: optional(list(str)),
  })),
});

export const capabilityInvoke = object({
  call_id: optional(str),
  verb: str,
  coverage: str,
  ocsf_event_refs: optional(list(str)),
  observed_data_refs: optional(list(str)),
  entity_refs: optional(list(str)),
  degradation_notes: optional(list(str)),
  events: optional(list(anything)),
  normalized: optional(list(anything)),
});

export const interpretation = object({ interpretation_id: str });

const seed = object({
  type: str,
  alert_id: optional(str),
  source: optional(str),
  detection_finding_ref: optional(str),
  entity_ref: optional(str),
  entity_identifier: optional(str),
  hypothesis_statement: optional(str),
  case_id: optional(str),
  case_ref: optional(str),
});

export const investigation = object({
  aggregate_id: str,
  title: str,
  status: str,
  last_event_sequence: num,
  updated_at: optional(str),
  pending_actions: optional(num),
  nearest_expiry: optional(str),
  has_verdict: optional(bool),
  open_actions: optional(num),
  open_notifications: optional(num),
  created_at: optional(str),
  opened_by_name: optional(str),
  verdict: optional(object({ disposition: str, rationale: optional(str), verdict_at: str })),
  seed: optional(seed),
  seed_summary: optional(str),
  case_note_on_seed: optional(str),
  case_note: optional(object({ case_id: str, body: str, system_name: optional(str) })),
});
export type RawInvestigation = Decoded<typeof investigation>;

export const created = investigation;

export const investigations = object({ investigations: list(investigation) });

const artifact = object({
  system: optional(str),
  location: str,
  sha256: optional(str),
  access: optional(str),
});

const prediction = object({
  id: str,
  statement: str,
  title: str,
  title_derived: optional(bool),
  status: str,
  ordinal: optional(num),
  test_query: optional(object({ tool: str, query_text: str })),
  discriminates: optional(list(str)),
  test_result_refs: optional(list(str)),
  test_kind: optional(str),
  attestation: optional(object({
    note_ref: str,
    author: str,
    author_name: optional(str),
    content: str,
    source: optional(str),
    channel: optional(str),
    observed_at: optional(str),
    reference: optional(str),
    artifact: optional(artifact),
  })),
});

export const hypotheses = object({
  hypotheses: list(object({
    id: str,
    statement: str,
    handle: optional(str),
    status: str,
    parent_ref: optional(str),
    rooted_at_ref: optional(str),
    labels: optional(list(str)),
    predictions: optional(list(prediction)),
  })),
});

const actor = object({
  principal: str,
  principal_name: optional(str),
  kind: str,
  model: optional(str),
});

export const thread = object({
  thread: list(object({
    sequence_no: num,
    occurred_at: str,
    actor,
    interpretation_id: str,
    interpretation_type: str,
    summary: str,
    confidence: optional(str),
    input_refs: optional(list(str)),
    output_refs: optional(list(str)),
    tool_calls: optional(num),
    has_transcript: optional(bool),
    superseded: optional(bool),
    disposition: optional(str),
    consulted_sops: optional(list(object({ sop_id: str, title: optional(str), cited: bool }))),
    consulted_similar_investigations: optional(list(object({
      investigation_ref: str,
      title: optional(str),
      band: optional(str),
      cited: bool,
    }))),
  })),
});

const action = object({
  action_id: str,
  action_type: str,
  tier: str,
  status: str,
  required_mode: optional(str),
  expires_at: optional(str),
  targets: optional(list(object({ entity_ref: str, resolved_identifier: str }))),
  is_reversal: optional(bool),
  reversibility: optional(str),
  tier_escalated: optional(bool),
  evidence_refs: optional(list(str)),
  edited_at: optional(str),
  inputs: optional(list(object({ name: str, required: optional(bool) }))),
  name: optional(str),
  category: optional(str),
  retry_of: optional(str),
  reversal_of_ref: optional(str),
  requesting_interpretation_id: optional(str),
  adapter: optional(str),
  planned_adapter: optional(str),
  error_detail: optional(str),
  result_ref: optional(str),
  parameters: optional(record(anything)),
});
export type RawAction = Decoded<typeof action>;

export const actions = object({ actions: list(action) });

export const comms = object({
  threads: list(object({
    thread_id: str,
    action_id: str,
    action_type: str,
    target: str,
    subject: optional(str),
    status: str,
    follow_up_hours: optional(num),
    follow_ups: optional(num),
    unacked_reply: optional(bool),
    sent_at: str,
    next_followup_at: optional(str),
    trail: optional(list(object({ direction: str, author: str, at: str, body: str }))),
    follow_up_due: optional(bool),
    escalation_triggered: optional(bool),
    escalation_policy: optional(str),
  })),
});

export const actionRequested = object({ action_id: str, status: str });

export const actionDecision = object({ action_id: str, status: str, stage: optional(str) });

export const actionTypes = object({
  action_types: list(object({
    descriptor: object({
      action_type: str,
      reversible_by: optional(str),
      default_tier: str,
      name: str,
      category: str,
      inputs: list(object({ name: str, type: str, required: bool })),
    }),
    status: str,
  })),
});

export const pins = object({
  pins: list(object({
    interpretation_id: str,
    finding: str,
    input_refs: list(str),
    actor: str,
    pinned_at: str,
    superseded: optional(bool),
  })),
});

export const transcript = object({ turn_id: str, body: str });

export const appearances = object({
  appearances: list(object({
    investigation_id: str,
    title: str,
    status: str,
    seed_summary: optional(str),
    first_seen: optional(str),
    last_seen: optional(str),
    mentions: num,
  })),
});

// An event's rendering (adr/0090): the sentence as tokens (an entity token
// carries the ref the chip opens), the curated fields and the category word.
const eventView = object({
  sentence: optional(list(object({ t: str, ref: optional(str), text: str }))),
  fields: list(object({ label: str, value: str, ref: optional(str), mono: optional(bool), tone: optional(str) })),
  category: str,
});
export type RawEventView = Decoded<typeof eventView>;

export interface RawEvidence {
  ref: string;
  kind: string;
  type: string;
  payload?: unknown;
  class_uid?: number;
  time?: string;
  recorded_at?: string;
  source_tool?: string;
  view?: RawEventView;
  related?: { sightings: RawEvidence[]; identity?: RawEvidence; evidence: RawEvidence[] };
}

export const evidence: Decoder<RawEvidence> = object({
  ref: str,
  kind: str,
  type: str,
  payload: anything,
  class_uid: optional(num),
  time: optional(str),
  recorded_at: optional(str),
  source_tool: optional(str),
  view: optional(eventView),
  related: optional(object({
    sightings: list(lazy(() => evidence)),
    identity: optional(lazy(() => evidence)),
    evidence: list(lazy(() => evidence)),
  })),
});

// The investigation's retrieved record (adr/0092): events by call, and the calls.
const eventRow = object({
  id: str,
  class_uid: num,
  class_name: str,
  time: str,
  recorded_at: str,
  source_tool: str,
  call_id: str,
  verb: str,
  coverage: str,
  observed_data_refs: list(str),
  entity_refs: list(str),
  view: eventView,
});

export const investigationEvents = object({ events: list(eventRow), total: num, next_cursor: optional(str) });

const capabilityCallRow = {
  call_id: str,
  investigation_id: optional(str),
  verb: str,
  adapter: optional(str),
  operation: optional(str),
  priority: num,
  params_sent: optional(anything),
  coverage: str,
  bindings_answered: num,
  bindings_total: num,
  null_observation_ref: optional(str),
  degradation_notes: list(str),
  started_at: str,
  duration_ms: num,
  outcomes: list(object({ adapter: str, operation: str, priority: num, params: optional(anything), answered: bool, skipped: optional(str) })),
};

export const investigationCalls = object({ calls: list(object(capabilityCallRow)) });

export const capabilityCall = object({
  ...capabilityCallRow,
  produced: optional(object({ event_ids: list(str), observed_data_refs: list(str), entity_refs: list(str) })),
  bindings: optional(list(object({ adapter: str, operation: str, priority: num, enabled: bool }))),
});

const approver = object({ principal: str, principal_name: optional(str), at: str });

export const actionRecord = object({
  action,
  history: list(object({
    sequence_no: num,
    event_type: str,
    occurred_at: str,
    actor,
    status: optional(str),
    detail: optional(str),
  })),
  authorization: object({
    required_mode: optional(str),
    mode: optional(str),
    stage: optional(str),
    primary_approver: optional(approver),
    secondary_approver: optional(approver),
    secondary_approver_pool: optional(list(str)),
    policy_ref: optional(str),
    policy_version: optional(str),
    challenge_answered: optional(bool),
    matched_policy_ref: optional(str),
    evaluations: optional(list(object({
      policy_ref: str,
      policy_version: str,
      would_have_fired: bool,
      effect: str,
      shadow: bool,
    }))),
  }),
  result: optional(object({
    final_outcome: str,
    attempts: optional(num),
    adapter: optional(str),
    error_detail: optional(str),
    raw_response_ref: optional(str),
    per_target: optional(list(object({ target: str, outcome: str }))),
  })),
  links: object({
    retry_of: optional(str),
    retried_by: optional(list(str)),
    reversal_of_ref: optional(str),
    reversed_by_ref: optional(str),
    reversal_attempted_by_ref: optional(str),
    requesting_interpretation_id: optional(str),
  }),
});

export const caseSummary = object({
  investigation_ref: str,
  title: str,
  body: str,
  generator_model: optional(str),
  conclusion_at: optional(str),
  seed_kind: optional(str),
  seed_value: optional(str),
  techniques: list(str),
  primary_entities: list(str),
  actions_taken: list(object({ action_type: str, target_count: num, outcome: str })),
  conclusion_outcome: optional(str),
  duration_hours: optional(num),
  verdict_disposition: optional(str),
  verdict_rationale: optional(str),
  tags: list(str),
  revision: num,
  content_hash: optional(str),
});

export const sops = object({ sops: list(object({ id: str, title: str, tags: list(str) })) });

export const caseSummaries = object({
  case_summaries: list(object({
    investigation_ref: str,
    title: str,
    verdict_disposition: optional(str),
    conclusion_at: optional(str),
  })),
});

export const recallSOPs = object({ results: list(object({ sop_id: str, title: str, score: num })) });

export const recallSimilar = object({ results: list(object({ investigation_ref: str, title: str, band: str })) });

export const sop = object({
  id: str,
  title: str,
  body: str,
  tags: list(str),
  status: str,
  recommendation: optional(str),
  author_id: optional(str),
  source: optional(object({ system: optional(str), url: str, version: optional(str) })),
  imported_by: optional(str),
  imported_at: optional(str),
  created_at: str,
  updated_at: str,
});

export const lifecycle = object({ status: str, export_workflow_id: optional(str) });

export const renamed = object({ title: str });

export const sopImported = object({ title: str, outcome: str });

const caseView = object({
  number: str,
  title: optional(str),
  status: optional(str),
  priority: optional(str),
  assignment_group: optional(str),
  summary: optional(str),
  link: optional(str),
  source: optional(str),
  opened_at: optional(str),
  investigation: optional(object({ id: str, title: str, status: str })),
});
export type RawCase = Decoded<typeof caseView>;

export { caseView as case };

export const cases = object({ queue: optional(str), cases: list(caseView) });
