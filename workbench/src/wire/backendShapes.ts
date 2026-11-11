// The backend's responses, as the workbench reads them (adr/0244). Each mirrors
// the Go view that serves it: a field is required where the Go tag has no
// omitempty. Only the fields the workbench uses are named.
import { anything, bool, lazy, list, nullable, num, object, optional, record, str, type Decoded, type Decoder } from "./decode";

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

/** Who recorded a narrative revision (server.NarrativeAuthorView, adr/0103). */
const narrativeAuthor = object({ kind: str, name: optional(str), model: optional(str) });

/** GET /api/investigations/{id}/narrative, and `narrative` on the detail (adr/0103). */
export const narrative = object({
  investigation_ref: str,
  revision: num,
  body: str,
  sequence_no: num,
  occurred_at: str,
  author: narrativeAuthor,
  drafted_by: narrativeAuthor,
  revisions: list(object({ revision: num, sequence_no: num, occurred_at: str, author: narrativeAuthor, body: str })),
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
  narrative: optional(narrative),
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
  // What the issuer asserted in the token at the act, and the role set reckon
  // required of it (adr/0105); absent on acts recorded before either existed.
  roles: optional(list(str)),
  required_roles: optional(list(str)),
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
    consulted_sops: optional(list(object({
      sop_id: str,
      title: optional(str),
      cited: bool,
      // The revision, cut and section indexes the act had in front of it
      // (adr/0303); absent on acts recorded before sections existed.
      revision: optional(num),
      cut: optional(str),
      sections: optional(list(num)),
    }))),
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
  secondary_approver_pool: optional(list(str)),
  primary_approver_ref: optional(str),
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

export const dependency = object({
  dependency_id: str,
  title: str,
  owner: str,
  detail: optional(str),
  status: str,
  notify_action_id: optional(str),
  external_ref: optional(str),
  created_by: str,
  created_by_name: optional(str),
  created_at: str,
  resolved_by: optional(str),
  resolved_by_name: optional(str),
  resolved_at: optional(str),
  resolution_note: optional(str),
  history: optional(list(object({ at: str, by: str, by_name: optional(str), event: str, note: optional(str), ref: optional(str) }))),
});
export type RawDependency = Decoded<typeof dependency>;

export const dependencies = object({ dependencies: list(dependency) });

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
  // An encoded command line read back (adr/0300): present only when a known
  // encoding decoded cleanly; the field it came from is in `fields` as stored.
  decoded: optional(object({ field: str, encoding: str, text: str })),
});
export type RawEventView = Decoded<typeof eventView>;

// A host's identity view (adr/0211): the aliases and distinct-from edges that
// touch it, each with its origin, and what the record has seen under it. It
// rides on a host's evidence read and in the identity acts' answer.
const identityEdge = object({
  edge_id: str,
  other_ref: str,
  kind: str,
  source: optional(str),
  by: optional(str),
  by_name: optional(str),
  at: str,
  note: optional(str),
  withdrawn: bool,
});

const hostIdentity = object({
  aliases: list(identityEdge),
  distinct: list(identityEdge),
  seen_as: object({
    domains: list(str),
    agents: list(object({ tool: str, uid: str })),
    ips: list(str),
  }),
});
export type RawHostIdentity = Decoded<typeof hostIdentity>;

// An account's and a person's identity views (adr/0324): the same edges, and
// for an account what its alias set was seen as and the persons it belongs
// to, for a person the accounts attached to it, each with its origin and
// whether a live split cuts it.
const belongsTo = {
  edge_id: str,
  kind: str,
  source: optional(str),
  by: optional(str),
  by_name: optional(str),
  at: str,
  note: optional(str),
  withdrawn: bool,
  cut: bool,
  // The cutting distinct-from and its two ends, set when cut is.
  cut_edge_id: optional(str),
  cut_account_ref: optional(str),
  cut_person_ref: optional(str),
};

const accountIdentity = object({
  aliases: list(identityEdge),
  distinct: list(identityEdge),
  seen_as: object({
    logins: list(str),
    ids: list(object({ tool: str, uid: str })),
    principal_names: list(str),
  }),
  owners: list(object({ person_ref: str, account_ref: str, ...belongsTo })),
});
export type RawAccountIdentity = Decoded<typeof accountIdentity>;

const personIdentity = object({
  aliases: list(identityEdge),
  distinct: list(identityEdge),
  accounts: list(object({ account_ref: str, ...belongsTo })),
});
export type RawPersonIdentity = Decoded<typeof personIdentity>;

// An identity act's answer: a 200 means the act is recorded. The view of the
// entity acted on is the field of its kind; the host's is always sent, null
// when the entity is no host or its view could not be read, and an account's
// or a person's is absent then. Never an empty view.
export const identityAct = object({
  identity: nullable(hostIdentity),
  account_identity: optional(accountIdentity),
  person_identity: optional(personIdentity),
});

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
  identity?: RawHostIdentity;
  account_identity?: RawAccountIdentity;
  person_identity?: RawPersonIdentity;
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
  // On an x-host only (adr/0211).
  identity: optional(hostIdentity),
  // On a user-account, and on an identity that is a person (adr/0324).
  account_identity: optional(accountIdentity),
  person_identity: optional(personIdentity),
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

// A grouped read (adr/0095): the newest events folded by class and key, what
// each class present can key on, and how many of the total the fold covered.
const eventGroup = object({
  class_uid: num,
  key: list(object({ slot: str, label: str, text: str, ref: optional(str) })),
  count: num,
  first: str,
  last: str,
  days: num,
  event_ids: list(str),
});
const eventClassKeys = object({
  class_uid: num,
  class_name: str,
  slots: list(object({ name: str, label: str })),
  default: list(str),
  key: list(str),
});

export const investigationEvents = object({
  events: list(eventRow),
  total: num,
  next_cursor: optional(str),
  groups: optional(list(eventGroup)),
  keys: optional(list(eventClassKeys)),
  folded_over: optional(num),
});

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
  learned_procedures: list(object({ id: str, title: str })),
});

export const sops = object({ sops: list(object({ id: str, title: str, tags: list(str), cited_in: num })) });

/** GET /api/knowledge/case_summaries — the past-case listing, filtered (adr/0107). */
export const caseSummaries = object({
  case_summaries: list(object({
    investigation_ref: str,
    title: str,
    generator_model: optional(str),
    verdict_disposition: optional(str),
    conclusion_at: optional(str),
    techniques: list(str),
    primary_entities: list(str),
    actions_taken: list(object({ action_type: str, target_count: num, outcome: str })),
    duration_hours: optional(num),
    cited_in: num,
  })),
  total: num,
});

/** GET /api/knowledge/case_summaries/facets — the values present in the corpus (adr/0107). */
export const caseFacets = object({
  techniques: list(object({ value: str, count: num })),
  dispositions: list(object({ value: str, count: num })),
});

/** GET /api/sops/{id}/citations and /api/knowledge/case_summaries/{ref}/citations (adr/0109). */
export const citations = object({
  ref: str,
  cited_in: num,
  recalled_in: num,
  investigations: list(object({
    investigation_id: str,
    title: str,
    status: str,
    cited: bool,
    last_at: str,
    acts: num,
    revisions: list(num),
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
  revision: num,
});

/** One section of a procedure (knowledge.SOPSection): its place, and its text when read singly. */
const sopSectionItem = object({
  idx: num,
  of: num,
  heading_path: list(str),
  part: optional(num),
  parts: optional(num),
  location: str,
  runes: num,
  text: optional(str),
});

/** GET /api/sops/{id}/revisions/{r}/sections — one revision's outline (adr/0097). */
export const sopOutline = object({
  sop_id: str,
  title: str,
  revision: num,
  cut: str,
  status: str,
  sections: list(sopSectionItem),
});

/** GET /api/sops/{id}/revisions/{r}/sections/{n} — one section of one revision, whole. */
export const sopSection = object({
  sop_id: str,
  title: str,
  revision: num,
  cut: str,
  status: str,
  section: sopSectionItem,
  outline: list(str),
});

/** GET /api/sops/{id}/revisions — a procedure's revision history, newest first. */
export const sopRevisions = object({
  sop_id: str,
  current: num,
  revisions: list(object({
    revision: num,
    content_hash: str,
    created_at: str,
    superseded_at: optional(str),
    authored_by: optional(str),
    imported_by: optional(str),
    imported_at: optional(str),
    source: optional(object({ system: optional(str), url: str, version: optional(str) })),
    current: bool,
  })),
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
