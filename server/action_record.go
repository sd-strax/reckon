package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// GET /api/actions/{id} — the action's record (design/ui 08 §8.6.1 "Action":
// the card is the summary; the inspector is the record). Everything here is
// read from the aggregate's own action.* events plus the action_current
// projection: the state history with actor and time per transition, the
// frozen Gate 2 authorization and its policy evaluations, the per-target
// results (08 §6c: PARTIAL stays auditable), and the lineage links in both
// directions. Nothing is derived that the events do not state.

// ActionRecordView is the response of GET /api/actions/{id}.
type ActionRecordView struct {
	Action        ActionView           `json:"action"`
	History       []ActionHistoryEntry `json:"history"`
	Authorization ActionAuthorization  `json:"authorization"`
	Result        *ActionResultView    `json:"result,omitempty"`
	// Resolution is a person's resolution of an unknown outcome (adr/0216):
	// what they found, what they checked, and where the evidence is.
	Resolution *ActionResolutionView `json:"resolution,omitempty"`
	Links      ActionLinks           `json:"links"`
}

// ActionResolutionView is what a person recorded when they resolved an
// OUTCOME_UNKNOWN action after checking the tool.
type ActionResolutionView struct {
	Outcome   string                      `json:"outcome"`
	Note      string                      `json:"note"`
	Reference string                      `json:"reference,omitempty"`
	Artifact  *aggregate.ArtifactLocation `json:"artifact,omitempty"`
	By        ActionApproverView          `json:"by"`
}

// ActionHistoryEntry is one action.* event on the action, in sequence order.
type ActionHistoryEntry struct {
	SequenceNo int64           `json:"sequence_no"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Actor      ThreadActorView `json:"actor"`
	// Status is the lifecycle status the event moved the action to, when it
	// moved one ("" for policy_evaluated and reversal_attempted).
	Status string `json:"status,omitempty"`
	// Detail is the event's one-line fact: the adapter dispatched to, the
	// rejection reason, the final outcome with its target tally, the policy
	// decision.
	Detail string `json:"detail,omitempty"`
}

// ActionApproverView names one approval: the principal (id + display name as
// the identity provider presented it, adr/0077) and when.
type ActionApproverView struct {
	Principal string    `json:"principal"`
	Name      string    `json:"principal_name,omitempty"`
	At        time.Time `json:"at"`
}

// ActionAuthorization is the frozen Gate 2 requirement and how it was met.
type ActionAuthorization struct {
	RequiredMode          string                             `json:"required_mode,omitempty"`
	Mode                  string                             `json:"mode,omitempty"`
	Stage                 string                             `json:"stage,omitempty"`
	PrimaryApprover       *ActionApproverView                `json:"primary_approver,omitempty"`
	SecondaryApprover     *ActionApproverView                `json:"secondary_approver,omitempty"`
	SecondaryApproverPool []string                           `json:"secondary_approver_pool,omitempty"`
	PolicyRef             string                             `json:"policy_ref,omitempty"`
	PolicyVersion         string                             `json:"policy_version,omitempty"`
	ChallengeAnswered     bool                               `json:"challenge_answered,omitempty"`
	MatchedPolicyRef      string                             `json:"matched_policy_ref,omitempty"`
	Evaluations           []aggregate.PolicyEvaluationRecord `json:"evaluations,omitempty"`
}

// ActionTargetResult is one target's recorded outcome (08 §6c). UNKNOWN is a
// legitimate residual, never inferred to success.
type ActionTargetResult struct {
	Target  string `json:"target"`
	Outcome string `json:"outcome"`
}

// ActionResultView is the terminal outcome of a dispatched action.
type ActionResultView struct {
	FinalOutcome   string               `json:"final_outcome"`
	Attempts       int                  `json:"attempts,omitempty"`
	Adapter        string               `json:"adapter,omitempty"`
	ErrorDetail    string               `json:"error_detail,omitempty"`
	RawResponseRef string               `json:"raw_response_ref,omitempty"`
	PerTarget      []ActionTargetResult `json:"per_target,omitempty"`
}

// ActionLinks are the lineage links in both directions (00 §6.6).
type ActionLinks struct {
	RetryOf                string   `json:"retry_of,omitempty"`
	RetriedBy              []string `json:"retried_by,omitempty"`
	ReversalOfRef          string   `json:"reversal_of_ref,omitempty"`
	ReversedByRef          string   `json:"reversed_by_ref,omitempty"`
	ReversalAttemptedByRef string   `json:"reversal_attempted_by_ref,omitempty"`
	// RequestingInterpretationID is the turn (or act) that requested it.
	RequestingInterpretationID string `json:"requesting_interpretation_id,omitempty"`
}

// getActionRecord serves GET /api/actions/{id}. Any authenticated reader.
func (b *Backend) getActionRecord(w http.ResponseWriter, r *http.Request, actionID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	rec, err := b.loadActionRecord(r.Context(), actionID)
	if err != nil {
		if errors.Is(err, errActionNotFound) {
			writeJSONError(w, http.StatusNotFound, "action not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

var errActionNotFound = errors.New("action not found")

func (b *Backend) loadActionRecord(ctx context.Context, actionID uuid.UUID) (ActionRecordView, error) {
	db := b.cfg.Handler.DB()
	cur, err := aggregate.LoadActionCurrent(ctx, db, actionID)
	if err != nil {
		return ActionRecordView{}, errActionNotFound
	}
	out := ActionRecordView{
		Action: b.actionView(cur),
		Authorization: ActionAuthorization{
			RequiredMode:          cur.RequiredMode,
			Mode:                  cur.Mode,
			SecondaryApproverPool: cur.SecondaryApproverPool,
		},
		Links: ActionLinks{},
	}
	if cur.RetryOf != (uuid.UUID{}) {
		out.Links.RetryOf = cur.RetryOf.String()
	}
	if cur.ReversalOfRef != (uuid.UUID{}) {
		out.Links.ReversalOfRef = cur.ReversalOfRef.String()
	}
	if cur.ReversedByRef != (uuid.UUID{}) {
		out.Links.ReversedByRef = cur.ReversedByRef.String()
	}
	if cur.ReversalAttemptedByRef != (uuid.UUID{}) {
		out.Links.ReversalAttemptedByRef = cur.ReversalAttemptedByRef.String()
	}

	// Lineage forward: the actions in the same investigation that retried this one.
	siblings, err := aggregate.ListActionCurrents(ctx, db, cur.AggregateID)
	if err != nil {
		return ActionRecordView{}, fmt.Errorf("list actions: %w", err)
	}
	for _, s := range siblings {
		if s.RetryOf == actionID {
			out.Links.RetriedBy = append(out.Links.RetriedBy, s.ActionID.String())
		}
	}

	// The action's own events, in the aggregate's order. A reversal's events
	// name the original as original_action_id; they belong to the original's
	// history too (the reversed / reversal_attempted transitions).
	rows, err := db.QueryContext(ctx, `
		SELECT sequence_no, event_type, occurred_at, actor, payload, event_version
		FROM events
		WHERE aggregate_id = $1
		  AND event_type LIKE 'action.%'
		  AND (payload->>'action_id' = $2 OR payload->>'original_action_id' = $2)
		ORDER BY sequence_no`, cur.AggregateID, actionID.String())
	if err != nil {
		return ActionRecordView{}, fmt.Errorf("query action events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			seq        int64
			typ        string
			at         time.Time
			actorRaw   []byte
			payloadRaw []byte
			version    int
		)
		if err := rows.Scan(&seq, &typ, &at, &actorRaw, &payloadRaw, &version); err != nil {
			return ActionRecordView{}, fmt.Errorf("scan action event: %w", err)
		}
		if err := aggregate.CheckEventVersion(typ, version); err != nil {
			return ActionRecordView{}, err
		}
		var actor aggregate.Actor
		_ = json.Unmarshal(actorRaw, &actor)
		entry := ActionHistoryEntry{SequenceNo: seq, EventType: typ, OccurredAt: at, Actor: threadActor(actor)}
		switch typ {
		case aggregate.EventTypeActionRequested:
			var p aggregate.ActionRequested
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = aggregate.ActionStatusRequested
			entry.Detail = fmt.Sprintf("%s · %s · %d target(s)", p.ActionType, p.Tier, len(p.Targets))
			if p.RequestingInterpretationID != (uuid.UUID{}) {
				out.Links.RequestingInterpretationID = p.RequestingInterpretationID.String()
			}
		case aggregate.EventTypeActionPolicyEvaluated:
			var p aggregate.ActionPolicyEvaluated
			_ = json.Unmarshal(payloadRaw, &p)
			out.Authorization.Evaluations = p.Evaluations
			out.Authorization.MatchedPolicyRef = p.MatchedPolicyRef
			entry.Detail = policyDecisionLine(cur.RequiredMode, p)
		case aggregate.EventTypeActionApproved:
			var p aggregate.ActionApproved
			_ = json.Unmarshal(payloadRaw, &p)
			a := p.Authorization
			out.Authorization.Mode, out.Authorization.Stage = a.Mode, a.Stage
			out.Authorization.PolicyRef, out.Authorization.PolicyVersion = a.PolicyRef, a.PolicyVersion
			out.Authorization.ChallengeAnswered = out.Authorization.ChallengeAnswered || a.ChallengeResponse != ""
			approver := ActionApproverView{Principal: a.PrimaryApproverRef, At: a.PrimaryApprovedAt}
			switch {
			case a.Stage == aggregate.AuthStageSecondary:
				approver = ActionApproverView{Principal: a.SecondaryApproverRef}
				if a.SecondaryApprovedAt != nil {
					approver.At = *a.SecondaryApprovedAt
				}
				entry.Status = aggregate.ActionStatusApproved
			case a.Mode == aggregate.AuthModeTwoParty:
				entry.Status = aggregate.ActionStatusPendingSecondary
			default:
				entry.Status = aggregate.ActionStatusApproved
			}
			// The display name comes from the approving act's actor when the
			// actor IS the approver; a policy's accountable human (AUTO_POLICY)
			// is named by their recorded ref only.
			if actor.PrincipalID == approver.Principal {
				approver.Name = actor.PrincipalName
			}
			if a.Stage == aggregate.AuthStageSecondary {
				out.Authorization.SecondaryApprover = &approver
			} else {
				out.Authorization.PrimaryApprover = &approver
			}
			entry.Detail = approvalLine(a, approver)
		case aggregate.EventTypeActionEdited:
			var p aggregate.ActionEdited
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = aggregate.ActionStatusRequested
			entry.Detail = "edited before approval"
			if p.Reason != "" {
				entry.Detail += ": " + p.Reason
			}
			if changes := paramChanges(p.PriorParameters, p.Parameters); changes != "" {
				entry.Detail += " — " + changes
			}
		case aggregate.EventTypeActionRejected:
			var p aggregate.ActionRejected
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = aggregate.ActionStatusRejected
			entry.Detail = p.Reason
		case aggregate.EventTypeActionExpired:
			entry.Status = aggregate.ActionStatusExpired
			entry.Detail = "approval window elapsed"
		case aggregate.EventTypeActionDispatched:
			var p aggregate.ActionDispatched
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = aggregate.ActionStatusExecuting
			entry.Detail = "via " + p.Adapter
		case aggregate.EventTypeActionResulted:
			var p aggregate.ActionResulted
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = p.FinalOutcome
			res := &ActionResultView{
				FinalOutcome: p.FinalOutcome, Attempts: p.Attempts, Adapter: p.Adapter,
				ErrorDetail: p.ErrorDetail, RawResponseRef: p.RawResponseRef,
			}
			keys := make([]string, 0, len(p.PerTargetResults))
			for k := range p.PerTargetResults {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			ok := 0
			for _, k := range keys {
				res.PerTarget = append(res.PerTarget, ActionTargetResult{Target: k, Outcome: p.PerTargetResults[k]})
				if p.PerTargetResults[k] == aggregate.ActionStatusSucceeded {
					ok++
				}
			}
			out.Result = res
			entry.Detail = resultLine(p, ok, len(keys))
		case aggregate.EventTypeActionReversed:
			var p aggregate.ActionReversed
			_ = json.Unmarshal(payloadRaw, &p)
			if p.OriginalActionID == actionID {
				entry.Status = aggregate.ActionStatusReversed
				entry.Detail = "reversed by " + p.ReversingActionID.String()
			}
		case aggregate.EventTypeActionReversalAttempted:
			var p aggregate.ActionReversalAttempted
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Detail = "reversal attempted by " + p.ReversingActionID.String() + " — effect not verified; status unchanged"
		case aggregate.EventTypeActionOutcomeUnknown:
			var p aggregate.ActionOutcomeUnknown
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = aggregate.ActionStatusOutcomeUnknown
			entry.Detail = p.Detail
		case aggregate.EventTypeActionOutcomeResolved:
			var p aggregate.ActionOutcomeResolved
			_ = json.Unmarshal(payloadRaw, &p)
			entry.Status = p.Outcome
			by := ActionApproverView{Principal: actor.PrincipalID, Name: actor.PrincipalName, At: at}
			out.Resolution = &ActionResolutionView{
				Outcome: p.Outcome, Note: p.Note, Reference: p.Reference, Artifact: p.Artifact, By: by,
			}
			who := by.Name
			if who == "" {
				who = by.Principal
			}
			entry.Detail = "resolved " + p.Outcome + " by " + who + " after checking the tool: " + p.Note
		}
		out.History = append(out.History, entry)
	}
	if err := rows.Err(); err != nil {
		return ActionRecordView{}, fmt.Errorf("iterate action events: %w", err)
	}
	if out.History == nil {
		out.History = []ActionHistoryEntry{}
	}
	return out, nil
}

// policyDecisionLine states the Gate 2 decision as a fact: which effect drove
// it, or that no policy did and a human decides.
func policyDecisionLine(requiredMode string, p aggregate.ActionPolicyEvaluated) string {
	if p.MatchedPolicyRef != "" {
		for _, e := range p.Evaluations {
			if e.PolicyRef == p.MatchedPolicyRef && !e.Shadow {
				return fmt.Sprintf("policy %s: %s", e.PolicyRef, e.Effect)
			}
		}
		return "policy " + p.MatchedPolicyRef
	}
	switch requiredMode {
	case aggregate.AuthModeTwoParty:
		return "two-party approval required · no policy pre-authorizes this"
	default:
		return "manual approval required · no policy pre-authorizes this"
	}
}

func approvalLine(a aggregate.Authorization, approver ActionApproverView) string {
	who := approver.Name
	if who == "" {
		who = approver.Principal
	}
	switch {
	case a.Mode == aggregate.AuthModeAutoPolicy:
		return fmt.Sprintf("auto-approved by policy %s · accountable: %s", a.PolicyRef, who)
	case a.Stage == aggregate.AuthStagePrimary:
		return "primary approval by " + who + " · awaiting a second approver"
	case a.Stage == aggregate.AuthStageSecondary:
		return "secondary approval by " + who
	default:
		if a.ChallengeResponse != "" {
			return "approved by " + who + " with the typed challenge"
		}
		return "approved by " + who
	}
}

func resultLine(p aggregate.ActionResulted, ok, total int) string {
	line := p.FinalOutcome
	if total > 0 {
		line += fmt.Sprintf(" · %d of %d target(s) %s", ok, total, aggregate.ActionStatusSucceeded)
	}
	if p.ErrorDetail != "" {
		line += " · " + p.ErrorDetail
	}
	return line
}

// paramChanges renders what an edit changed (adr/0127) as `key: “before” →
// “after”` per changed parameter, each side clipped, so the record says what
// the approver's text replaced.
func paramChanges(before, after json.RawMessage) string {
	var b, a map[string]any
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	clip := func(v any) string {
		if v == nil {
			return "—"
		}
		s := fmt.Sprint(v)
		if r := []rune(s); len(r) > 60 {
			s = string(r[:59]) + "…"
		}
		return "“" + s + "”"
	}
	var parts []string
	for _, k := range names {
		if fmt.Sprint(b[k]) == fmt.Sprint(a[k]) {
			continue
		}
		parts = append(parts, k+": "+clip(b[k])+" → "+clip(a[k]))
	}
	return strings.Join(parts, "; ")
}
