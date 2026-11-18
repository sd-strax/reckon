package catalog

import (
	"fmt"
	"regexp"
	"strings"
)

// The tool-name rules (adr/0382). The agent gives the model one tool per verb,
// one per built-in tool and one per action type. The backend checks a verb or
// an action descriptor against these rules when it is registered, so a name
// the agent cannot use never reaches a session.

// toolNamePattern is the strictest tool-name rule common to the main model
// APIs: letters, digits, "_" and "-", a first character that is a letter or
// underscore, and at most 64 characters.
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

// ValidToolName reports whether name is a tool name every main model API
// accepts.
func ValidToolName(name string) bool {
	return toolNamePattern.MatchString(name)
}

// ActionToolName is the tool name of an action type: "request_", then the
// type with each "." written as "_".
func ActionToolName(actionType string) string {
	return "request_" + strings.ReplaceAll(actionType, ".", "_")
}

// The fields every action request carries beside its type's inputs. An
// action tool declares them next to the type's inputs, so no input may take
// one of these names.
const (
	RequestFieldTargets      = "targets"
	RequestFieldRationale    = "rationale"
	RequestFieldEvidenceRefs = "evidence_refs"
	RequestFieldRetryOf      = "retry_of"
)

// ActionRequestFields returns the fields every action request carries.
func ActionRequestFields() []string {
	return []string{RequestFieldTargets, RequestFieldRationale, RequestFieldEvidenceRefs, RequestFieldRetryOf}
}

// The agent's built-in tools: the tools every session has, whatever the
// tenant's verbs and action types. A verb may not take one of these names.
const (
	ToolRecallSOPs              = "recall_sops"
	ToolRecallSimilar           = "recall_similar_investigations"
	ToolPriorInvestigations     = "prior_investigations"
	ToolProposeHypothesis       = "propose_hypothesis"
	ToolRecordPrediction        = "record_prediction"
	ToolEvaluateHypothesis      = "evaluate_hypothesis"
	ToolRecordPredictionOutcome = "record_prediction_outcome"
	ToolListActions             = "list_actions"
	ToolReadResult              = "read_result"
	ToolReadSOP                 = "read_sop"
)

// BuiltinToolNames returns the agent's built-in tool names.
func BuiltinToolNames() []string {
	return []string{
		ToolRecallSOPs, ToolRecallSimilar, ToolPriorInvestigations,
		ToolProposeHypothesis, ToolRecordPrediction, ToolEvaluateHypothesis,
		ToolRecordPredictionOutcome, ToolListActions, ToolReadResult, ToolReadSOP,
	}
}

// IsBuiltinToolName reports whether name is one of the agent's built-in tools.
func IsBuiltinToolName(name string) bool {
	for _, b := range BuiltinToolNames() {
		if name == b {
			return true
		}
	}
	return false
}

// ValidateActionDescriptor checks an action descriptor, given its type and the
// names of its inputs. It fails when the type is empty or gives an invalid
// tool name, or when an input takes the name of a field every request carries
// or is declared twice.
func ValidateActionDescriptor(actionType string, inputs []string) error {
	if actionType == "" {
		return fmt.Errorf("action type %q: an action type is required", actionType)
	}
	if name := ActionToolName(actionType); !ValidToolName(name) {
		return fmt.Errorf("action type %q: tool name %q is not a valid tool name (%s)", actionType, name, toolNamePattern)
	}
	shared := map[string]bool{}
	for _, f := range ActionRequestFields() {
		shared[f] = true
	}
	declared := map[string]bool{}
	for _, in := range inputs {
		if shared[in] {
			return fmt.Errorf("action type %q: input %q takes the name of a field every action request carries", actionType, in)
		}
		if declared[in] {
			return fmt.Errorf("action type %q: input %q is declared twice", actionType, in)
		}
		declared[in] = true
	}
	return nil
}

// ValidateVerb checks a verb's name. It fails when the name is not a valid
// tool name or is one of the agent's built-in tool names. Whether it clashes
// with an action type's tool name is checked where both catalogs are known.
func ValidateVerb(verb string) error {
	if !ValidToolName(verb) {
		return fmt.Errorf("verb %q is not a valid tool name (%s)", verb, toolNamePattern)
	}
	if IsBuiltinToolName(verb) {
		return fmt.Errorf("verb %q takes the name of one of the agent's built-in tools", verb)
	}
	return nil
}
