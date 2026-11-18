package action

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
)

// The request-time planner (adr/0406). When an action is requested, the
// engine picks the binding and derives each target's resolved_identifier from
// the stored entity, by the rule the binding declares. Both are frozen on the
// action, and dispatch uses them as they are.

// targetEntityRoot is the template root of a target's stored entity. It is
// valid only inside a binding's identifier rule.
const targetEntityRoot = "target.entity"

// VendorIDsKey is the property of a loaded target entity that maps each
// vendor to that vendor's own id for the entity: target.entity.vendor_ids.
const VendorIDsKey = "vendor_ids"

// TargetEntity is a target's stored entity as the planner reads it.
type TargetEntity struct {
	// Properties are the stored entity's properties, with vendor_ids beside
	// them (VendorIDs).
	Properties map[string]any
	// VendorConflicts holds, for each vendor whose ids for the entity
	// disagree, the ids it reported. Such a vendor is left out of
	// vendor_ids, and a refusal names the conflict.
	VendorConflicts map[string][]string
}

// SourceID is one id a source reported for an entity, under the source's
// label.
type SourceID struct {
	Label string
	ID    string
}

// Vendor returns the vendor a source label names (adr/0406). A label is
// <vendor>:<tool>, or a bare <vendor> for a source with one tool, so the
// vendor is the label up to the first colon. It is trimmed and lowercased,
// so a binding names it one way.
func Vendor(label string) string {
	v, _, _ := strings.Cut(label, ":")
	return strings.ToLower(strings.TrimSpace(v))
}

// VendorIDs groups a target's source ids by vendor (adr/0406). A vendor whose
// ids all agree maps to that id. A vendor whose ids disagree is left out and
// returned in conflicts with its distinct ids, sorted, because picking one
// would be a guess about which source is right. Empty labels and ids are
// skipped.
func VendorIDs(ids []SourceID) (vendorIDs map[string]any, conflicts map[string][]string) {
	byVendor := map[string][]string{}
	for _, s := range ids {
		v, id := Vendor(s.Label), strings.TrimSpace(s.ID)
		if v == "" || id == "" {
			continue
		}
		if !slices.Contains(byVendor[v], id) {
			byVendor[v] = append(byVendor[v], id)
		}
	}
	vendorIDs = map[string]any{}
	for v, list := range byVendor {
		if len(list) == 1 {
			vendorIDs[v] = list[0]
			continue
		}
		if conflicts == nil {
			conflicts = map[string][]string{}
		}
		sort.Strings(list)
		conflicts[v] = list
	}
	return vendorIDs, conflicts
}

// PlanInput is a request for the planner: what the action is and whom it
// acts on, with each target's stored entity.
type PlanInput struct {
	ActionType string
	Targets    []aggregate.TargetSpec
	// Entities holds each target's stored entity, index for index with
	// Targets, for an action type with target types. It is nil for a type
	// with none (ticket, notify), whose resolved_identifier is the
	// requester's destination and is kept.
	Entities    []TargetEntity
	Parameters  map[string]any
	SourceScope string
}

// PlanResult is what the planner froze: the chosen binding and the targets
// with their resolved identifiers.
type PlanResult struct {
	Binding aggregate.FrozenBinding
	Targets []aggregate.TargetSpec
}

// PlanRefusal is the planner's refusal: no usable binding could derive every
// target's identifier and render its params. Reasons names, per binding tried
// in priority order, why it did not apply. Nothing has been recorded or sent.
type PlanRefusal struct {
	ActionType string
	Reasons    []string
}

// Error implements error.
func (e *PlanRefusal) Error() string {
	if len(e.Reasons) == 0 {
		return fmt.Sprintf("action_type %q: no usable binding in this install can carry it out", e.ActionType)
	}
	return fmt.Sprintf("action_type %q: no binding can derive every target's identifier and render its params: %s",
		e.ActionType, strings.Join(e.Reasons, "; "))
}

// IsPlanRefusal reports whether err is a planner refusal.
func IsPlanRefusal(err error) bool {
	var r *PlanRefusal
	return errors.As(err, &r)
}

// Plan picks the binding for a request and derives its targets' identifiers
// (adr/0406). It walks the usable bindings by priority (candidates): adapter
// configured and enabled, scope matching. A binding applies when it has an identifier rule
// for every target's type, every rule renders from that target's entity, and
// its params render. The first that applies is chosen, and each target's
// resolved_identifier is set to its rendered rule; a value the requester sent
// is not used. A binding that does not apply gives way to the next, since
// nothing has been sent. When none applies, the request is refused. For an
// action type with no target types the requester's identifiers are kept and
// only the params decide.
func (r *ActionResolver) Plan(in PlanInput) (PlanResult, error) {
	typed := in.Entities != nil
	if typed && len(in.Entities) != len(in.Targets) {
		return PlanResult{}, fmt.Errorf("action_type %q: %d entities for %d targets", in.ActionType, len(in.Entities), len(in.Targets))
	}
	refusal := &PlanRefusal{ActionType: in.ActionType}
	for _, c := range r.candidates(in.ActionType, in.SourceScope) {
		b := c.binding
		targets, why := deriveTargets(b, in, typed)
		if why != "" {
			refusal.Reasons = append(refusal.Reasons, bindingName(b)+": "+why)
			continue
		}
		req := DispatchRequest{ActionType: in.ActionType, Targets: targets, Parameters: in.Parameters}
		tctx := dispatchContext(req)
		_, applicable, err := capability.RenderParams(b.Params, tctx)
		switch {
		case err != nil:
			refusal.Reasons = append(refusal.Reasons, bindingName(b)+": params do not render: "+err.Error())
			continue
		case !applicable:
			refusal.Reasons = append(refusal.Reasons, bindingName(b)+": params need "+strings.Join(missingRefs(b.Params, tctx), ", "))
			continue
		}
		return PlanResult{
			Binding: aggregate.FrozenBinding{Adapter: b.Adapter, Operation: b.Operation},
			Targets: targets,
		}, nil
	}
	return PlanResult{}, refusal
}

// ReadsVendorIDs reports whether any binding the planner could choose for a
// request of actionType in sourceScope (candidates) has an identifier rule
// for targetType that reads target.entity.vendor_ids. The entity loader reads
// a host's device ids only then, so a binding whose rule names a base field
// never pays for that read.
func (r *ActionResolver) ReadsVendorIDs(actionType, sourceScope, targetType string) bool {
	prefix := targetEntityRoot + "." + VendorIDsKey
	for _, c := range r.candidates(actionType, sourceScope) {
		rule, ok := c.binding.Identifier[targetType]
		if !ok {
			continue
		}
		for _, ref := range templateRefs(rule) {
			if ref.path == prefix || strings.HasPrefix(ref.path, prefix+".") {
				return true
			}
		}
	}
	return false
}

// deriveTargets renders b's identifier rule for every target. It returns the
// targets with their identifiers set, or why the rule does not apply. For an
// action type with no target types the targets are returned as they came.
func deriveTargets(b ActionBinding, in PlanInput, typed bool) ([]aggregate.TargetSpec, string) {
	targets := slices.Clone(in.Targets)
	if !typed {
		return targets, ""
	}
	for i, t := range targets {
		typ, _, _ := strings.Cut(t.EntityRef, "--")
		rule, ok := b.Identifier[typ]
		if !ok {
			return nil, fmt.Sprintf("target %d (%s): no identifier rule for %s", i, t.EntityRef, typ)
		}
		id, why := renderIdentifier(rule, in.Entities[i])
		if why != "" {
			return nil, fmt.Sprintf("target %d (%s): %s", i, t.EntityRef, why)
		}
		targets[i].ResolvedIdentifier = id
	}
	return targets, ""
}

// renderIdentifier renders one identifier rule against a target's entity. It
// returns the identifier, or why the rule does not render: the fields it
// needs that the entity lacks, with any vendor conflict behind a missing
// vendor id, or a field whose value is not one an identifier can be. A field
// the rule reads is used as text: a string as it is, a number in decimal
// (identifierText). A bool, a structure or any other kind is no identifier,
// so the rule does not render and the binding gives way.
func renderIdentifier(rule string, ent TargetEntity) (string, string) {
	tctx := map[string]any{"target": map[string]any{"entity": ent.Properties}}
	// Each field the rule reads is put beside the entity as its text, under
	// its full path: the template engine reads a flat key of the path before
	// walking it, so the rendering never formats a value itself.
	for _, ref := range templateRefs(rule) {
		v, ok := capability.LookupPath(tctx, ref.path)
		if !ok || v == nil {
			continue // missing: the render reports it
		}
		text, ok := identifierText(v)
		if !ok {
			return "", fmt.Sprintf("%s is a %s, not a value an identifier can be", strings.TrimPrefix(ref.path, targetEntityRoot+"."), kindOf(v))
		}
		tctx[ref.path] = text
	}
	out, applicable, err := capability.RenderParams(map[string]any{"identifier": rule}, tctx)
	if err != nil {
		return "", "identifier rule does not render: " + err.Error()
	}
	v, present := out["identifier"]
	if !applicable || !present {
		return "", "the entity has no " + strings.Join(describeMissing(missingRefs(map[string]any{"identifier": rule}, tctx), ent), ", ")
	}
	x, ok := v.(string)
	if !ok {
		return "", fmt.Sprintf("identifier rule renders a %s, not a value", kindOf(v))
	}
	if strings.TrimSpace(x) == "" {
		return "", "identifier rule renders empty"
	}
	return x, ""
}

// identifierText is a stored value as identifier text: a string as it is, a
// float in its shortest decimal form with no exponent (a JSON number decodes
// as float64, so 1234567 is "1234567"), an integer in base 10. ok is false for
// any other kind.
func identifierText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case int:
		return strconv.FormatInt(int64(x), 10), true
	case int8:
		return strconv.FormatInt(int64(x), 10), true
	case int16:
		return strconv.FormatInt(int64(x), 10), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint:
		return strconv.FormatUint(uint64(x), 10), true
	case uint8:
		return strconv.FormatUint(uint64(x), 10), true
	case uint16:
		return strconv.FormatUint(uint64(x), 10), true
	case uint32:
		return strconv.FormatUint(uint64(x), 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	case json.Number:
		return x.String(), true
	default:
		return "", false
	}
}

// kindOf names a value's kind in a refusal.
func kindOf(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case map[string]any:
		return "structure"
	case []any:
		return "list"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// describeMissing names each missing field, with the conflict when the field
// is a vendor id the vendor reported more than one value for.
func describeMissing(paths []string, ent TargetEntity) []string {
	out := make([]string, 0, len(paths))
	prefix := targetEntityRoot + "." + VendorIDsKey + "."
	for _, p := range paths {
		field := strings.TrimPrefix(p, targetEntityRoot+".")
		if v, ok := strings.CutPrefix(p, prefix); ok {
			if ids, conflict := ent.VendorConflicts[v]; conflict {
				out = append(out, fmt.Sprintf("%s: %s reported more than one id for it (%s)", field, v, strings.Join(ids, ", ")))
				continue
			}
		}
		out = append(out, field)
	}
	return out
}

// bindingName names a binding in a refusal.
func bindingName(b ActionBinding) string {
	return "binding " + b.Adapter + " " + b.Operation
}

// templateRef is one ${...} reference of a template: its path and whether a
// missing value rejects the template (not optional, no default).
type templateRef struct {
	path     string
	required bool
	// optional and defaulted say which marker made the reference not
	// required: path? or path ?? default.
	optional, defaulted bool
	transforms          []string
}

// templateRefs lists the references of one template string, in order.
func templateRefs(s string) []templateRef {
	var out []templateRef
	for _, m := range exprRe.FindAllStringSubmatch(s, -1) {
		segs := strings.Split(m[1], "|")
		ref := templateRef{required: true}
		for _, t := range segs[1:] {
			ref.transforms = append(ref.transforms, strings.TrimSpace(t))
		}
		head := strings.TrimSpace(segs[0])
		if i := strings.Index(head, "??"); i >= 0 {
			head, ref.required, ref.defaulted = strings.TrimSpace(head[:i]), false, true
		} else if strings.HasSuffix(head, "?") {
			head, ref.required, ref.optional = strings.TrimSpace(strings.TrimSuffix(head, "?")), false, true
		}
		ref.path = head
		out = append(out, ref)
	}
	return out
}

// identifierTransforms are the transforms an identifier rule may use: the
// built-in ones that take text and give text. A transform that gives another
// kind (int, bool, epoch_ms, epoch_s) yields no identifier, and one that reads
// its input as something else (iso8601 reads a time) would turn a field into
// a value it never held.
var identifierTransforms = map[string]bool{"upper": true, "lower": true, "trim": true}

// checkIdentifierRule refuses what would let an identifier rule render
// without every field it names: an optional marker, a default, or a
// transform that does not give text (identifierTransforms). The class rule:
// an identifier rule either renders every field it names, or the binding
// gives way. Every reference must also read target.entity.
func checkIdentifierRule(rule string) error {
	refs := templateRefs(rule)
	if len(refs) == 0 {
		return fmt.Errorf("names no field (%q); an identifier rule renders from the entity, so every target does not get the same literal", rule)
	}
	for _, ref := range refs {
		switch {
		case !underTargetEntity(ref.path):
			return fmt.Errorf("references %s; an identifier rule reads only %s", ref.path, targetEntityRoot)
		case ref.optional:
			return fmt.Errorf("marks %s optional; an identifier rule renders every field it names, or the binding gives way", ref.path)
		case ref.defaulted:
			return fmt.Errorf("gives %s a default; an identifier rule sends what the entity holds, or the binding gives way", ref.path)
		}
		for _, t := range ref.transforms {
			if !identifierTransforms[t] {
				return fmt.Errorf("applies transform %q to %s; an identifier rule takes only transforms that give text (lower, trim, upper)", t, ref.path)
			}
		}
	}
	return nil
}

// paramTemplates lists every template string in a params map, recursing
// nested maps and lists.
func paramTemplates(params map[string]any) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			for _, x := range t {
				walk(x)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	for _, v := range params {
		walk(v)
	}
	return out
}

// underTargetEntity reports whether a template path reads the stored target
// entity.
func underTargetEntity(path string) bool {
	return path == targetEntityRoot || strings.HasPrefix(path, targetEntityRoot+".")
}

// paramsReadTargetEntity returns the first path in params that reads the
// stored target entity.
func paramsReadTargetEntity(params map[string]any) (string, bool) {
	for _, s := range paramTemplates(params) {
		for _, ref := range templateRefs(s) {
			if underTargetEntity(ref.path) {
				return ref.path, true
			}
		}
	}
	return "", false
}

// missingRefs lists, sorted and distinct, the required references of params
// that have no value in tctx: why the params do not render.
func missingRefs(params map[string]any, tctx map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range paramTemplates(params) {
		for _, ref := range templateRefs(s) {
			if !ref.required || seen[ref.path] {
				continue
			}
			if v, ok := capability.LookupPath(tctx, ref.path); ok && v != nil && v != "" {
				continue
			}
			seen[ref.path] = true
			out = append(out, ref.path)
		}
	}
	sort.Strings(out)
	return out
}
