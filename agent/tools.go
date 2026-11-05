package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Intrinsic tool names — the reasoning/action surface that exists regardless of
// tenant capability config. Capability verbs are appended per session from the
// live catalog (05 §3.4 step 2).
const (
	ToolRecallSOPs              = "recall_sops"
	ToolRecallSimilar           = "recall_similar_investigations"
	ToolPriorInvestigations     = "prior_investigations"
	ToolProposeHypothesis       = "propose_hypothesis"
	ToolRecordPrediction        = "record_prediction"
	ToolEvaluateHypothesis      = "evaluate_hypothesis"
	ToolRecordPredictionOutcome = "record_prediction_outcome"
	ToolRequestAction           = "request_action"
	ToolListActions             = "list_actions"
	ToolReadResult              = "read_result"
	ToolReadSOP                 = "read_sop"
)

// buildTools assembles the session's tool definitions: one tool per AVAILABLE
// capability verb (unavailable/degraded verbs are trimmed, 03 §6.3 — the model
// only sees what can currently resolve), plus the intrinsic tools. actionTypes
// is the write-side catalog (may be nil when the action layer is off); it shapes
// the request_action tool so the model picks a real action_type.
//
// What the model is shown and what dispatch can run come from this one step: the
// definitions are the list, and the map is the list by name. Two tools of one
// name would leave the model's list ambiguous, so a verb that takes an intrinsic
// tool's name fails here.
func buildTools(caps []Capability, actionTypes []ActionType) ([]ToolDef, map[string]tool, error) {
	var tools []tool
	for _, c := range caps {
		if c.Status != "available" {
			continue
		}
		if c.Descriptor.Verb == "list_capabilities" {
			continue // the loop already trims the tool set; re-listing is not a model concern in v0
		}
		tools = append(tools, verbTool(c))
	}
	// The list is the head of every request and the prompt cache matches it
	// byte for byte, so its order is the session's, not the backend's: verbs by
	// name, then the intrinsics in declared order.
	slices.SortStableFunc(tools, func(a, b tool) int { return strings.Compare(a.def.Name, b.def.Name) })
	tools = append(tools, intrinsicTools(actionTypes)...)

	defs := make([]ToolDef, 0, len(tools))
	byName := make(map[string]tool, len(tools))
	for _, t := range tools {
		if _, taken := byName[t.def.Name]; taken {
			return nil, nil, fmt.Errorf("agent: two tools are named %q", t.def.Name)
		}
		byName[t.def.Name] = t
		defs = append(defs, t.def)
	}
	return defs, byName, nil
}

// verbTool is the tool for one available capability verb.
func verbTool(c Capability) tool {
	verb := c.Descriptor.Verb
	return tool{
		def: verbToolDef(c),
		run: func(ctx context.Context, s *Session, input json.RawMessage) (toolResult, error) {
			res, err := contentOf(s.invokeVerb(ctx, verb, input))
			if err == nil {
				res.CallID = capabilityCallID(string(res.Content))
			}
			return res, err
		},
	}
}

// verbToolDef renders a capability descriptor as an LLM tool. Every verb tool
// shares the CallInput shape: an `entity` object the tenant's binding templates
// resolve against, an optional `window`, and the descriptor's scalar inputs as
// extra template roots.
func verbToolDef(c Capability) ToolDef {
	props := map[string]any{}
	var required []string
	for _, in := range c.Descriptor.Inputs {
		switch in.Type {
		case "entity":
			props["entity"] = map[string]any{
				"type": "object",
				"description": in.Desc + " — a typed entity object keyed by kind, " +
					`e.g. {"host":{"hostname":"<hostname>"}}, {"user":{"name":"<account>"}}, {"process":{"pid":1234}} (pid is a number), {"ip":"<address>"}`,
			}
			if in.Required {
				required = append(required, "entity")
			}
		case "time_window":
			props["window"] = map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from": map[string]any{"type": "string", "format": "date-time"},
					"to":   map[string]any{"type": "string", "format": "date-time"},
				},
				"description": in.Desc,
			}
		default:
			props[in.Name] = map[string]any{"type": "string", "description": in.Desc}
			if in.Required {
				required = append(required, in.Name)
			}
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return ToolDef{
		Name:        c.Descriptor.Verb,
		Description: c.Descriptor.Intent + " Returns: " + c.Descriptor.Output + ".",
		InputSchema: schema,
	}
}

// toolResult is what one tool call gives back: the content the model reads and
// the knowledge that content put in front of it. The session records the
// knowledge on the turn's provenance; a tool mutates nothing there (adr/0233).
type toolResult struct {
	Content   []byte
	Consulted []ConsultedSOP
	Similar   []ConsultedSimilarInvestigation
	// CallID is the engine-minted capability call behind a verb's envelope
	// (adr/0092); only a verb tool sets it, and only when it is an id.
	CallID string
}

// contentOf wraps a call that returns only content.
func contentOf(content []byte, err error) (toolResult, error) {
	return toolResult{Content: content}, err
}

// tool is one tool's single definition: what the model is shown and what runs.
type tool struct {
	def ToolDef
	// origin and revised are an intrinsic tool's ledger entry (adr/0232): what
	// introduced its definition and what reworded it since, as pointers in
	// promptRule's form. A verb tool has none: its words are the tenant's.
	origin  string
	revised []string
	run     func(ctx context.Context, s *Session, input json.RawMessage) (toolResult, error)
	// selfBounded marks a tool whose content already fits the result limit, so
	// dispatch does not bound it again.
	selfBounded bool
}

// toolSpec declares an intrinsic tool. Its In is the schema and the decoder.
type toolSpec[In any] struct {
	Name        string
	Origin      string
	Revised     []string
	Description string
	Overrides   map[string]any // see inputSchema
	SelfBounded bool
	Run         func(s *Session, ctx context.Context, in In) (toolResult, error)
}

func defineTool[In any](spec toolSpec[In]) tool {
	return tool{
		def: ToolDef{Name: spec.Name, Description: spec.Description, InputSchema: inputSchema[In](spec.Overrides)},
		run: func(ctx context.Context, s *Session, input json.RawMessage) (toolResult, error) {
			var in In
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, fmt.Errorf("bad %s input: %w", spec.Name, err)
			}
			return spec.Run(s, ctx, in)
		},
		selfBounded: spec.SelfBounded,
		origin:      spec.Origin,
		revised:     spec.Revised,
	}
}

type recallSOPsInput struct {
	Query string   `json:"query" required:"true" desc:"what guidance you are looking for"`
	Tags  []string `json:"tags" desc:"hard filter on SOP tags, optional"`
	Unit  string   `json:"unit" desc:"see the override"`
}

// recallUnitOverride is the unit's schema: a closed choice the description
// cannot express as a plain string.
var recallUnitOverride = map[string]any{
	"unit": map[string]any{
		"type": "string",
		"enum": []string{"procedure", "section"},
		"description": "procedure (default): which procedure applies — one hit per procedure, carrying the section that matched best. " +
			"section: what do our procedures say about this — one hit per section, so two hits may come from one procedure.",
	},
}

type readSOPInput struct {
	SOPID    string `json:"sop_id" required:"true" desc:"the sop_id a recall_sops hit returned"`
	Revision int    `json:"revision" desc:"the revision the hit or outline returned; omitted, the newest revision you were shown this session. A section is read against that revision's numbering; a procedure changed since is refused and must be recalled again"`
	Section  int    `json:"section" desc:"the section to read whole, by its idx from a hit's section or outline; omit for the outline alone. A section read needs a recall or outline of the procedure first"`
}

type recallSimilarInput struct {
	Query string   `json:"query" required:"true" desc:"the current situation to match against past cases (seed, entities, techniques, what you're seeing)"`
	Tags  []string `json:"tags" desc:"hard filter on summary tags, optional: a technique id, seed:<kind> (seed:host, seed:case, seed:alert), disposition:<verdict>, entity:<identifier>"`
}

type priorInvestigationsInput struct {
	Entity string `json:"entity" required:"true" desc:"the identifier as the data names it: host name, DOMAIN\\account, IP address or domain"`
}

type proposeHypothesisInput struct {
	Statement   string   `json:"statement" required:"true" desc:"the claim, one or two sentences, falsifiable, a single mechanism"`
	Handle      string   `json:"handle" required:"true" desc:"short mnemonic the analyst will refer to it by: a number in proposal order plus two or three words naming the mechanism, e.g. \"H1 <mechanism>\", \"H2 <rival mechanism>\""`
	Rationale   string   `json:"rationale" required:"true" desc:"why you believe this, citing evidence refs"`
	Labels      []string `json:"labels" desc:"MITRE ATT&CK technique ids where evident"`
	RootedAtRef string   `json:"rooted_at_ref" desc:"STIX id of the anchor entity, optional"`
	ParentRef   string   `json:"parent_ref" desc:"x-hypothesis id this refines, optional"`
	Confidence  string   `json:"confidence" desc:"HIGH | MEDIUM | LOW"`
}

type recordPredictionInput struct {
	HypothesisRef string   `json:"hypothesis_ref" required:"true" desc:"the x-hypothesis id that predicts this WILL be observed (the owner)"`
	Statement     string   `json:"statement" required:"true" desc:"what should be observable if the hypothesis holds"`
	Title         string   `json:"title" desc:"a short name for the prediction: a noun phrase of at most 64 characters, no trailing period, optional — the record derives one from the statement when absent"`
	Rationale     string   `json:"rationale" required:"true" desc:"why this test discriminates (or, if it only confirms, say so)"`
	Discriminates []string `json:"discriminates" desc:"x-hypothesis ids of RIVALS that predict the OPPOSITE outcome for this test — CONFIRMED counts against them, DISCONFIRMED supports them. Empty if the test is merely consistent with the owner"`
	TestKind      string   `json:"test_kind" desc:"tool (default) | manual — manual only when no tool can test the prediction and a person's answer can"`
	TestTool      string   `json:"test_tool" desc:"the tool that would test it, optional (for a manual test: the role of the person to ask)"`
	TestQuery     string   `json:"test_query" desc:"the query text, optional (for a manual test: the question to put to them)"`
}

type evaluateHypothesisInput struct {
	HypothesisRef  string   `json:"hypothesis_ref" required:"true" desc:"the x-hypothesis id"`
	Disposition    string   `json:"disposition" required:"true" desc:"support | refutation | inconclusive"`
	Rationale      string   `json:"rationale" required:"true" desc:"the evidential reasoning"`
	TestResultRefs []string `json:"test_result_refs" desc:"STIX/OCSF refs of the observed evidence; required for support and refutation"`
	Abandoned      bool     `json:"abandoned" desc:"on inconclusive: mark ABANDONED instead"`
	Confidence     string   `json:"confidence" desc:"HIGH | MEDIUM | LOW"`
}

type recordPredictionOutcomeInput struct {
	PredictionRef  string   `json:"prediction_ref" required:"true" desc:"the x-prediction id"`
	Status         string   `json:"status" required:"true" desc:"CONFIRMED | DISCONFIRMED | INCONCLUSIVE"`
	Rationale      string   `json:"rationale" required:"true" desc:"what was observed"`
	TestResultRefs []string `json:"test_result_refs" desc:"refs of the observed evidence"`
}

// targetInput is what the model may say about a target. ActionTarget has more
// (asset_criticality reaches Gate 2 policy), and none of it is the model's to
// declare, so the decoder never reads it.
type targetInput struct {
	EntityRef          string `json:"entity_ref" required:"true" desc:"STIX id of the target entity"`
	ResolvedIdentifier string `json:"resolved_identifier" required:"true" desc:"the concrete identifier the tool acts on (hostname, account id)"`
}

// requestActionInput: action_type and parameters take their schemas from the
// catalog (see the overrides in intrinsicTools).
type requestActionInput struct {
	ActionType   string          `json:"action_type" required:"true"`
	Targets      []targetInput   `json:"targets" required:"true" desc:"the distinct targets; blast radius drives the trust tier"`
	Rationale    string          `json:"rationale" required:"true" desc:"why this action, now"`
	EvidenceRefs []string        `json:"evidence_refs" required:"true" nonempty:"true" desc:"REQUIRED: the STIX/OCSF refs that ground this action — the observed_data_refs / entity_refs / x-hypothesis ids you observed. A containment or remediation with no cited evidence is invalid and the engine rejects it."`
	Parameters   json.RawMessage `json:"parameters"`
	RetryOf      string          `json:"retry_of" desc:"when re-requesting a FAILED or EXPIRED action: that action's id (records the retry lineage)"`
}

type readResultInput struct {
	ResultID string `json:"result_id" required:"true" desc:"the result_id from result_bounded"`
	Offset   int    `json:"offset" required:"true" desc:"the item offset to start from: next_offset from the previous page"`
}

// intrinsicTools declares the reasoning + knowledge + action tools. actionTypes
// (may be nil) shapes the request_action tool from the frozen write catalog.
func intrinsicTools(actionTypes []ActionType) []tool {
	return []tool{
		defineTool(toolSpec[recallSOPsInput]{
			Name:    ToolRecallSOPs,
			Origin:  "80420c3",
			Revised: []string{"#303"},
			Description: "Recall the tenant's standard operating procedures relevant to a question. " +
				"A hit carries the section of the procedure that matched (its text, under excerpt), where it sits (section) and the procedure's outline; " +
				"it is not the whole procedure — read_sop reads any other section. " +
				"EMPTY coverage is evidence of absence, not an error.",
			Overrides:   recallUnitOverride,
			SelfBounded: true,
			Run:         (*Session).recallSOPs,
		}),
		defineTool(toolSpec[readSOPInput]{
			Name:   ToolReadSOP,
			Origin: "#303",
			Description: "Read a recalled procedure by section: with section, that section whole and the outline around it; without, the outline alone (each section's place and length). " +
				"Read a section before you cite the procedure for what it says, when the hit did not carry it.",
			SelfBounded: true,
			Run:         (*Session).readSOP,
		}),
		defineTool(toolSpec[recallSimilarInput]{
			Name:        ToolRecallSimilar,
			Origin:      "b0f9ce1",
			Description: "Recall concluded investigations similar to this one — 'have we handled something like this before?'. Query with the current investigation's situation (seed, entities, techniques). Results carry a similarity band (NEAR_DUPLICATE/RELATED/DISTINCT); EMPTY coverage means no prior similar case, evidence of absence.",
			SelfBounded: true,
			Run:         (*Session).recallSimilar,
		}),
		defineTool(toolSpec[priorInvestigationsInput]{
			Name:        ToolPriorInvestigations,
			Origin:      "d1f9e88",
			Description: "Exact lookup: the concluded investigations that involved an entity (a host name, account, address or domain) as their seed or as an action target, most recent first. It answers 'have we investigated this before?' and works from the seed alone, before any telemetry; recall_similar_investigations answers 'have we seen this pattern before?' once there is behaviour to compare. EMPTY coverage means no concluded investigation recorded the entity.",
			SelfBounded: true,
			Run:         (*Session).priorInvestigations,
		}),
		defineTool(toolSpec[proposeHypothesisInput]{
			Name:        ToolProposeHypothesis,
			Origin:      "80420c3",
			Revised:     []string{"0efca01"},
			Description: "Propose one falsifiable hypothesis — ONE claim, not a compound story. Hypotheses compete: call this once per rival (the leader, then the concrete alternative). It is recorded PROPOSED until the analyst acknowledges it. Returns the hypothesis id for later predictions and evaluation.",
			Run:         (*Session).proposeHypothesis,
		}),
		defineTool(toolSpec[recordPredictionInput]{
			Name:        ToolRecordPrediction,
			Origin:      "80420c3",
			Revised:     []string{"0efca01", "cebed87", "633ebd0", "#90"},
			Description: "Record a testable prediction under a hypothesis: what you expect to observe if it is true, and what query would test it. A prediction that DISCRIMINATES — one a rival hypothesis predicts the opposite outcome for — is worth more than one that merely confirms; name those rivals in discriminates. When a test is manual (no tool available to you can answer it and a person can), set test_kind to manual and say in test_query whom to ask and what; the analyst records its result, never you.",
			Run:         (*Session).recordPrediction,
		}),
		defineTool(toolSpec[evaluateHypothesisInput]{
			Name:        ToolEvaluateHypothesis,
			Origin:      "80420c3",
			Description: "Record an evidential outcome for a hypothesis: support, refutation, or inconclusive. Decisive outcomes must cite the observed evidence refs.",
			Run:         (*Session).evaluateHypothesis,
		}),
		defineTool(toolSpec[recordPredictionOutcomeInput]{
			Name:        ToolRecordPredictionOutcome,
			Origin:      "80420c3",
			Revised:     []string{"cebed87"},
			Description: "Record a prediction's test outcome. CONFIRMED and DISCONFIRMED must cite test_result_refs. A manual prediction (a person tests it) is not yours to record: the engine refuses it; ask the analyst for the result.",
			Run:         (*Session).recordPredictionOutcome,
		}),
		defineTool(toolSpec[requestActionInput]{
			Name:        ToolRequestAction,
			Origin:      "80420c3",
			Revised:     []string{"b50bd4f", "adac4a6", "0ade6d2", "6b020a1"},
			Description: requestActionDescription(actionTypes),
			Overrides: map[string]any{
				"action_type": actionTypeSchema(actionTypes),
				"parameters":  parametersSchema(actionTypes),
			},
			Run: (*Session).requestAction,
		}),
		defineTool(toolSpec[struct{}]{
			Name:        ToolListActions,
			Origin:      "c881117",
			Revised:     []string{"64e3783", "3523b3e"},
			Description: "List this investigation's requested actions with their CURRENT engine status (REQUESTED = awaiting the analyst's approval in this surface; APPROVED/EXECUTING/SUCCEEDED/FAILED/REJECTED/EXPIRED/REVERSED/OUTCOME_UNKNOWN). Use this for ground truth about whether an action was approved or executed — never assume. OUTCOME_UNKNOWN means the action was dispatched but nobody knows whether the tool acted: say so, and leave it to the analyst, who checks the tool and resolves it; never request it again while it is unknown. Each row carries a computed `expired` flag (and the result carries `now`): expired=true means the approval window elapsed and the engine will refuse an approve even though status may still read REQUESTED — such an action is dead; a new request is the only way forward.",
			Run:         (*Session).listActions,
		}),
		defineTool(toolSpec[readResultInput]{
			Name:   ToolReadResult,
			Origin: "1a7fecf",
			Description: "Read another page of a tool result too large to return whole. Such a result carries " +
				"result_bounded with a result_id, the returned/total count of each list, next_offset, and on the " +
				"first page an exact digest of the whole result. Pass the result_id and next_offset; every page " +
				"comes from the same snapshot. Read only what the question needs, or narrow the original query instead.",
			SelfBounded: true, // already a page
			Run: func(s *Session, _ context.Context, in readResultInput) (toolResult, error) {
				page, err := s.readResult(in)
				if err != nil {
					return toolResult{}, err
				}
				// A later page of a recall is credited like the first: what the
				// model was shown on it is consulted (adr/0303).
				res := toolResult{Content: page}
				switch s.results.sourceOf(in.ResultID) {
				case ToolRecallSOPs, ToolReadSOP:
					res.Consulted = consultedSOPs(page)
					if in.ResultID != "" && len(res.Consulted) == 0 {
						res.Consulted = readSOPConsulted(page)
					}
				case ToolRecallSimilar, ToolPriorInvestigations:
					res.Similar = consultedCases(page)
				}
				return res, nil
			},
		}),
	}
}

// requestActionDescription renders the request_action tool description from the
// frozen write catalog: the fixed guidance plus, when the catalog is known, an
// enumerated list of the real action types with tier, reversibility, and current
// dispatchability. This is what stops the model guessing action_type strings
// (ip.block, firewall.block_ip, …) — it can read the actual vocabulary.
func requestActionDescription(actionTypes []ActionType) string {
	base := "Propose a state-changing action (containment, remediation). It goes through authorization policy — you can propose, never approve; most proposals await the analyst's explicit approval. Every request MUST cite evidence_refs — the STIX/OCSF refs that ground it; an ungrounded action is rejected."
	if len(actionTypes) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nValid action_type values (use one EXACTLY as written; never invent a name):")
	for _, a := range actionTypes {
		d := a.Descriptor
		fmt.Fprintf(&b, "\n- %s [%s, %s, %s]: %s",
			d.ActionType, d.DefaultTier, d.Reversibility, a.Status, d.Intent)
		// The declared parameter schema (08 §3): entity inputs ride `targets`;
		// the rest are the ONLY keys `parameters` accepts — the backend rejects
		// unknown keys and missing required ones at request time.
		if params := paramList(a); params != "" {
			fmt.Fprintf(&b, "\n  parameters: %s", params)
		}
	}
	b.WriteString("\n\nEach action's `parameters` object accepts exactly its listed parameter keys (targets carry the entities). If the action you need is not in this list, or is marked 'unavailable' (no tool is wired for it), do NOT request it — tell the analyst it must be performed manually.")
	return b.String()
}

// paramList renders an action type's non-entity inputs — the declared
// request_action.parameters vocabulary — as "name (required), name, …".
func paramList(a ActionType) string {
	var parts []string
	for _, in := range a.Descriptor.Inputs {
		if in.Type == "entity" {
			continue // satisfied via targets
		}
		p := in.Name
		if in.Required {
			p += " (required)"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// actionTypeSchema builds the action_type property. When the catalog is known it
// is a hard enum of the real types, so an invented name cannot even be emitted;
// otherwise it falls back to a free-text string (action layer off).
func actionTypeSchema(actionTypes []ActionType) map[string]any {
	if len(actionTypes) == 0 {
		return map[string]any{"type": "string", "description": "the action type, e.g. host.isolate, account.disable"}
	}
	// Only requestable types belong in the hard enum: an 'unavailable' type has
	// no tool wired (08 §3), so the description flags it as unrequestable — the
	// enum must not then permit it. This mirrors the read side, where buildTools
	// trims non-available verbs from the tool set entirely.
	enum := make([]string, 0, len(actionTypes))
	for _, a := range actionTypes {
		if a.Status == "unavailable" {
			continue
		}
		enum = append(enum, a.Descriptor.ActionType)
	}
	// If nothing is currently requestable, fall back to the full list rather than
	// emit an empty enum (which no value could satisfy) — the backend still
	// rejects an unwired dispatch honestly, and the description says as much.
	if len(enum) == 0 {
		for _, a := range actionTypes {
			enum = append(enum, a.Descriptor.ActionType)
		}
	}
	return map[string]any{
		"type":        "string",
		"enum":        enum,
		"description": "the action type — exactly one of the enumerated catalog values",
	}
}

// UnwrapStringifiedObject corrects a common model quirk: emitting a nested
// object field — request_action.parameters — as a STRINGIFIED JSON string
// (`"{\"summary\":...}"`) instead of a JSON object. When raw is a JSON string
// whose contents parse as a JSON object, it returns the unwrapped object bytes;
// otherwise it returns raw unchanged so the backend still validates the shape.
// This is the loop being liberal in what it accepts from the model (05 §3.4):
// a benign double-encoding must not cost a rejected action — or, against a real
// write adapter templating ${parameters.x}, a silently empty field. The eval
// harness applies the same normalization so H5 grades the request's substance
// (keys), not its encoding.
func UnwrapStringifiedObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || raw[0] != '"' {
		return raw
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw
	}
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "{") {
		return raw // a plain string, not a stringified object — leave it
	}
	// Decode the FIRST complete JSON object and tolerate trailing garbage: the
	// model sometimes appends a stray ']' or other junk after the object (a
	// real, repeated malformation that blocked every ticket create), which
	// json.Unmarshal rejects but a streaming decoder does not. Re-marshal the
	// clean object so the backend receives well-formed bytes.
	dec := json.NewDecoder(strings.NewReader(trimmed))
	var probe map[string]json.RawMessage
	if dec.Decode(&probe) != nil {
		return raw // not even a leading JSON object — let the backend reject honestly
	}
	clean, err := json.Marshal(probe)
	if err != nil {
		return raw
	}
	return json.RawMessage(clean)
}

// parametersSchema builds the request_action `parameters` property as a concrete
// object schema: the union of every action type's non-entity declared inputs
// (08 §3), each annotated with which action_type(s) use it. A bare
// {"type":"object"} with NO properties is precisely what leads a model to emit
// the field as a STRINGIFIED JSON blob (the H6 finding); giving it real
// properties makes the model fill a structured object instead. The union is
// deliberately permissive (the model picks action_type at call time, so a single
// schema cannot be per-type): the description tells it to include only its
// action's keys, and the backend enforces per-type required-ness + rejects
// unknown keys.
func parametersSchema(actionTypes []ActionType) map[string]any {
	props := map[string]any{}
	owners := map[string][]string{}
	for _, a := range actionTypes {
		for _, in := range a.Descriptor.Inputs {
			if in.Type == "entity" {
				continue // entity inputs ride `targets`, not `parameters`
			}
			if _, seen := props[in.Name]; !seen {
				props[in.Name] = map[string]any{"type": jsonSchemaType(in.Type), "description": in.Desc}
			}
			owners[in.Name] = appendUnique(owners[in.Name], a.Descriptor.ActionType)
		}
	}
	if len(props) == 0 {
		return map[string]any{"type": "object",
			"description": "Action-type-specific parameters as a JSON object (this catalog declares none)."}
	}
	for name := range props {
		pm := props[name].(map[string]any)
		usedBy := "used by: " + strings.Join(owners[name], ", ")
		if d := strings.TrimSpace(pm["description"].(string)); d != "" {
			pm["description"] = d + " (" + usedBy + ")"
		} else {
			pm["description"] = usedBy
		}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"description": "The action-type-specific parameters, as a JSON OBJECT — never a JSON string. " +
			"Include only the keys listed for your chosen action_type (see the tool description above); omit the others.",
	}
}

// jsonSchemaType maps an InputParam type to a JSON Schema scalar type, defaulting
// to string for anything non-scalar (entity inputs never reach here).
func jsonSchemaType(t string) string {
	switch t {
	case "string", "number", "integer", "boolean":
		return t
	default:
		return "string"
	}
}

// appendUnique appends v to s if absent, preserving order.
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// dispatchTool executes one model-issued tool call against the backend and
// returns the result to feed back. A name outside the session's tool set is
// refused here, without reaching the backend, and goes to the model as an error
// it can correct. A backend rejection (4xx/5xx) likewise becomes an isError
// result — the engine's explanation goes to the model, which can adjust; only
// transport-level failures abort the turn.
func (s *Session) dispatchTool(ctx context.Context, name string, input json.RawMessage) (toolResult, bool) {
	t, ok := s.toolsByName[name]
	if !ok {
		return toolResult{Content: fmt.Appendf(nil, "unknown tool %q: not one of the tools defined for this session", name)}, true
	}
	res, err := t.run(ctx, s, input)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return toolResult{Content: []byte(apiErr.Body)}, true
		}
		return toolResult{Content: []byte("tool dispatch failed: " + err.Error())}, true
	}
	if !t.selfBounded {
		res.Content = s.boundResult(res.Content)
	}
	return res, false
}

// recallBody is the request both recall tools send.
func recallBody(query string, tags []string) map[string]any {
	body := map[string]any{"query": query}
	if len(tags) > 0 {
		body["tags"] = tags
	}
	return body
}

// The recall tools bound their own content because what a turn consulted is
// what the model was shown: a hit left off the page was not put in front of it.

func (s *Session) recallSOPs(ctx context.Context, in recallSOPsInput) (toolResult, error) {
	body := recallBody(in.Query, in.Tags)
	if in.Unit != "" {
		body["unit"] = in.Unit
	}
	// The excerpts are asked to fit the result page: the server's budget bounds
	// a section, and this bound keeps recallLimit of them under the page, so a
	// hit is never clipped after the fact (adr/0303). English prose runs
	// about 1.5 bytes a rune in JSON, hence the ratio.
	body["excerpt_budget"] = s.maxResultBytes * 2 / (recallLimit * 3)
	raw, err := s.backend.RecallSOPs(ctx, body)
	if err != nil {
		return toolResult{}, err
	}
	shown := s.boundResultFrom(ToolRecallSOPs, raw)
	return toolResult{Content: shown, Consulted: consultedSOPs(shown)}, nil
}

// recallLimit is the hits one recall_sops call returns (the server's default),
// the divisor of the excerpt budget above.
const recallLimit = 5

func (s *Session) readSOP(ctx context.Context, in readSOPInput) (toolResult, error) {
	if in.Section < 0 {
		return toolResult{}, fmt.Errorf("read_sop: section must be a positive index, got %d", in.Section)
	}
	if in.Revision < 0 {
		return toolResult{}, fmt.Errorf("read_sop: revision must be positive, got %d", in.Revision)
	}
	var (
		raw json.RawMessage
		err error
	)
	if in.Section == 0 {
		// The outline establishes the pin: it answers the current revision
		// and cut, and the model's revision, if given, is honoured.
		raw, err = s.backend.SOPSections(ctx, in.SOPID, sectionPin{Revision: in.Revision})
	} else {
		// A section index is read only against the revision and cut it came
		// from: the pin is the session's own record of what was shown, not
		// a number the model may omit.
		pin, perr := s.sectionPinFor(in.SOPID, in.Revision)
		if perr != nil {
			return toolResult{}, perr
		}
		raw, err = s.backend.SOPSection(ctx, in.SOPID, in.Section, pin)
	}
	if err != nil {
		return toolResult{}, err
	}
	if len(raw) > s.maxResultBytes {
		// One section can exceed the result page (a large configured budget,
		// or a script whose runes are several bytes each). The page machinery
		// pages lists, not one large string, so the text is reshaped into
		// parts the model reads page by page; the section's place rides every
		// page.
		raw = sectionInParts(raw, s.maxResultBytes/8)
	}
	shown := s.boundResultFrom(ToolReadSOP, raw)
	return toolResult{Content: shown, Consulted: readSOPConsulted(shown)}, nil
}

// sectionInParts rewrites a section read whose text is too large for one page:
// section.text becomes text_parts, a list of at most partRunes runes each,
// in order. Anything that is not such a read is returned unchanged.
func sectionInParts(raw []byte, partRunes int) []byte {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil || doc["section"] == nil {
		return raw
	}
	var section map[string]json.RawMessage
	if json.Unmarshal(doc["section"], &section) != nil || section["text"] == nil {
		return raw
	}
	var text string
	if json.Unmarshal(section["text"], &text) != nil {
		return raw
	}
	delete(section, "text")
	runes := []rune(text)
	partRunes = max(partRunes, 1) // a page under eight bytes would otherwise never advance
	parts := make([]string, 0, len(runes)/partRunes+1)
	for at := 0; at < len(runes); at += partRunes {
		parts = append(parts, string(runes[at:min(at+partRunes, len(runes))]))
	}
	sec, err := json.Marshal(section)
	if err != nil {
		return raw
	}
	doc["section"] = sec
	tp, err := json.Marshal(parts)
	if err != nil {
		return raw
	}
	doc["text_parts"] = tp
	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return out
}

func (s *Session) recallSimilar(ctx context.Context, in recallSimilarInput) (toolResult, error) {
	raw, err := s.backend.RecallSimilar(ctx, recallBody(in.Query, in.Tags))
	if err != nil {
		return toolResult{}, err
	}
	shown := s.boundResultFrom(ToolRecallSimilar, raw)
	return toolResult{Content: shown, Similar: consultedCases(shown)}, nil
}

func (s *Session) priorInvestigations(ctx context.Context, in priorInvestigationsInput) (toolResult, error) {
	raw, err := s.backend.PriorInvestigations(ctx, map[string]any{"entity": in.Entity})
	if err != nil {
		return toolResult{}, err
	}
	shown := s.boundResultFrom(ToolPriorInvestigations, raw)
	return toolResult{Content: shown, Similar: consultedCases(shown)}, nil
}

func (s *Session) proposeHypothesis(ctx context.Context, in proposeHypothesisInput) (toolResult, error) {
	resp, err := s.backend.RecordInterpretation(ctx, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: "hypothesis",
		Rationale:          clipRunes(in.Rationale, maxRationaleRunes),
		Confidence:         in.Confidence,
		Hypothesis: &Hypothesis{
			Statement:   in.Statement,
			Handle:      clipRunes(in.Handle, 40),
			ParentRef:   in.ParentRef,
			RootedAtRef: in.RootedAtRef,
			Labels:      in.Labels,
		},
	})
	if err != nil {
		return toolResult{}, err
	}
	return contentOf(json.Marshal(map[string]any{"hypothesis_ref": resp.NodeID, "status": "PROPOSED"}))
}

func (s *Session) recordPrediction(ctx context.Context, in recordPredictionInput) (toolResult, error) {
	pred := &Prediction{HypothesisRef: in.HypothesisRef, Statement: in.Statement, Title: in.Title, Discriminates: in.Discriminates, TestKind: in.TestKind}
	if in.TestTool != "" || in.TestQuery != "" {
		pred.TestQuery = &QuerySpec{Tool: in.TestTool, QueryText: in.TestQuery}
	}
	resp, err := s.backend.RecordInterpretation(ctx, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: "prediction",
		Rationale:          clipRunes(in.Rationale, maxRationaleRunes),
		Prediction:         pred,
	})
	if err != nil {
		return toolResult{}, err
	}
	return contentOf(json.Marshal(map[string]any{"prediction_ref": resp.NodeID, "status": "UNTESTED"}))
}

func (s *Session) evaluateHypothesis(ctx context.Context, in evaluateHypothesisInput) (toolResult, error) {
	// The disposition becomes the interpretation type, so only the three
	// outcomes pass: anything else would let the model record another
	// type, the context-reset marker among them (adr/0235).
	switch in.Disposition {
	case "support", "refutation", "inconclusive":
	default:
		return toolResult{}, fmt.Errorf("evaluate_hypothesis: disposition must be support, refutation or inconclusive, not %q", in.Disposition)
	}
	resp, err := s.backend.RecordInterpretation(ctx, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: in.Disposition,
		Rationale:          clipRunes(in.Rationale, maxRationaleRunes),
		Confidence:         in.Confidence,
		HypothesisRef:      in.HypothesisRef,
		Abandoned:          in.Abandoned,
		TestResultRefs:     in.TestResultRefs,
	})
	if err != nil {
		return toolResult{}, err
	}
	return contentOf(json.Marshal(map[string]any{"interpretation_id": resp.InterpretationID}))
}

func (s *Session) recordPredictionOutcome(ctx context.Context, in recordPredictionOutcomeInput) (toolResult, error) {
	resp, err := s.backend.RecordInterpretation(ctx, InterpretationRequest{
		InvestigationRef:   s.investigationID,
		InterpretationType: "prediction",
		Rationale:          clipRunes(in.Rationale, maxRationaleRunes),
		PredictionRef:      in.PredictionRef,
		PredictionStatus:   in.Status,
		TestResultRefs:     in.TestResultRefs,
	})
	if err != nil {
		return toolResult{}, err
	}
	return contentOf(json.Marshal(map[string]any{"interpretation_id": resp.InterpretationID}))
}

func (s *Session) requestAction(ctx context.Context, in requestActionInput) (toolResult, error) {
	targets := make([]ActionTarget, 0, len(in.Targets))
	for _, t := range in.Targets {
		targets = append(targets, ActionTarget{EntityRef: t.EntityRef, ResolvedIdentifier: t.ResolvedIdentifier})
	}
	resp, err := s.backend.RequestAction(ctx, ActionRequest{
		ActionType:       in.ActionType,
		Targets:          targets,
		Parameters:       UnwrapStringifiedObject(in.Parameters),
		EvidenceRefs:     in.EvidenceRefs,
		Rationale:        in.Rationale,
		InvestigationRef: s.investigationID,
		RetryOf:          in.RetryOf,
	})
	if err != nil {
		return toolResult{}, err
	}
	s.pendingActions = append(s.pendingActions, resp)
	return contentOf(json.Marshal(resp))
}

func (s *Session) listActions(ctx context.Context, _ struct{}) (toolResult, error) {
	acts, err := s.backend.ListActions(ctx, s.investigationID)
	if err != nil {
		return toolResult{}, err
	}
	// The model has no clock: it cannot compare expires_at to "now", so a
	// lazily-expired action (status still REQUESTED, 04) would read as
	// live. The engine answers the question instead — a computed expired
	// flag per action, plus now for grounding.
	now := s.now().UTC()
	type actionView struct {
		ActionStatus
		Expired bool `json:"expired"`
	}
	views := make([]actionView, 0, len(acts))
	for _, a := range acts {
		views = append(views, actionView{ActionStatus: a, Expired: a.Expired(now)})
	}
	return contentOf(json.Marshal(map[string]any{
		"now":     now.Format(time.RFC3339),
		"actions": views,
	}))
}

// invokeVerb runs a capability verb: every verb shares the CallInput shape
// (verbToolDef), so one decoder serves them all.
func (s *Session) invokeVerb(ctx context.Context, name string, input json.RawMessage) ([]byte, error) {
	var in struct {
		Entity map[string]any `json:"entity"`
		Window *struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"window"`
	}
	// Decode twice: once for the typed roots, once for the extras.
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("bad %s input: %w", name, err)
	}
	var all map[string]json.RawMessage
	_ = json.Unmarshal(input, &all)
	extra := map[string]any{}
	for k, v := range all {
		if k == "entity" || k == "window" {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err == nil {
			extra[k] = val
		}
	}
	call := InvokeInput{Entity: in.Entity, InvestigationRef: s.investigationID}
	if len(extra) > 0 {
		call.Extra = extra
	}
	if in.Window != nil {
		w := &Window{}
		if t, err := time.Parse(time.RFC3339, in.Window.From); err == nil {
			w.From = t
		}
		if t, err := time.Parse(time.RFC3339, in.Window.To); err == nil {
			w.To = t
		}
		call.Window = w
	}
	return s.backend.InvokeCapability(ctx, name, call)
}

// capabilityCallID reads the engine-minted call id out of a capability
// envelope (adr/0092). Only an id is returned: the aggregate accepts nothing
// else on the record, and a stray call_id would fail the whole commit. Only
// the verb tools call it, so another tool's payload never reaches the record.
func capabilityCallID(content string) string {
	var env struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return ""
	}
	if _, err := uuid.Parse(env.CallID); err != nil {
		return ""
	}
	return env.CallID
}
