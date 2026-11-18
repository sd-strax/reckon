package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// targetEntityStore is what the request-time planner reads a target's stored
// entity from (adr/0406); *evidence.Store satisfies it.
type targetEntityStore interface {
	Object(ctx context.Context, ref string) (evidence.Object, error)
	HostDeviceIDs(ctx context.Context, ref string) ([]evidence.SourceID, error)
}

// targetStore is the store target entities are read from, or nil when there
// is none.
func (b *Backend) targetStore() targetEntityStore {
	if b.targetEntities != nil {
		return b.targetEntities
	}
	if b.cfg.Evidence == nil {
		return nil
	}
	return b.cfg.Evidence
}

// planAction picks the binding for a built request and derives its targets'
// identifiers (adr/0406), filling cmd. It answers a non-zero HTTP status and
// a message when the request is refused; nothing is recorded then. A type
// with target types reads each target's stored entity; a type with none
// keeps the requester's destination, and only its binding is chosen.
func (b *Backend) planAction(ctx context.Context, cmd *aggregate.RequestAction, scope string) (int, string) {
	resolver := b.actionResolver()
	if resolver == nil {
		return http.StatusServiceUnavailable, "action layer not configured: no write bindings, so no binding can be chosen for the request"
	}
	d, ok := b.cfg.ActionCatalog.Descriptor(cmd.ActionType)
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf("unknown action_type %q (not in catalog)", cmd.ActionType)
	}
	in := action.PlanInput{
		ActionType:  cmd.ActionType,
		Targets:     cmd.Targets,
		Parameters:  paramsMap(cmd.Parameters),
		SourceScope: scope,
	}
	if len(d.TargetTypes) > 0 {
		store := b.targetStore()
		if store == nil {
			return http.StatusServiceUnavailable, "evidence store not configured: a target's identifier cannot be derived from its entity"
		}
		// A host's device ids are read only when a binding that could be
		// chosen reads vendor_ids for a host (adr/0406), so a hostname rule
		// never waits on, or fails on, that read.
		hostVendorIDs := resolver.ReadsVendorIDs(cmd.ActionType, scope, identity.TypeHost)
		entities, status, msg := loadTargetEntities(ctx, store, cmd.ActionType, cmd.Targets, hostVendorIDs)
		if status != 0 {
			return status, msg
		}
		in.Entities = entities
	}
	res, err := resolver.Plan(in)
	if err != nil {
		if action.IsPlanRefusal(err) {
			return http.StatusUnprocessableEntity, err.Error()
		}
		return http.StatusInternalServerError, "plan action: " + err.Error()
	}
	cmd.Targets = res.Targets
	cmd.Binding = res.Binding
	return 0, ""
}

// loadTargetEntities reads each target's stored entity (adr/0406): its
// properties, flattened as the normalizer wrote them, and vendor_ids beside
// them. A host's vendor ids are the device ids sources reported for it across
// every record naming it or its alias set (evidence.Store.HostDeviceIDs: each
// device object's uid, not its uid_alt or agent ids), because a stored object
// keeps only its first source's properties. Any other entity's are its stored
// source_ids. hostVendorIDs says whether a host's device ids are read at
// all; when false a host has no vendor_ids. A target whose entity the
// evidence does not hold is refused with 422.
func loadTargetEntities(ctx context.Context, store targetEntityStore, actionType string, targets []aggregate.TargetSpec, hostVendorIDs bool) ([]action.TargetEntity, int, string) {
	out := make([]action.TargetEntity, len(targets))
	for i, t := range targets {
		typ, _, _ := strings.Cut(t.EntityRef, "--")
		obj, err := store.Object(ctx, t.EntityRef)
		// The store reads by uuid: an object of another type under it is no
		// entity of this ref's type.
		if errors.Is(err, evidence.ErrNotFound) || errors.Is(err, evidence.ErrMalformedRef) || (err == nil && obj.Type != typ) {
			return nil, http.StatusUnprocessableEntity, fmt.Sprintf(
				"action_type %q: target %d entity_ref %q is not an entity in the evidence; a target must be an entity reckon has observed, so its identifier can be derived", actionType, i, t.EntityRef)
		}
		if err != nil {
			return nil, http.StatusInternalServerError, "read target entity: " + err.Error()
		}
		props, err := entityProperties(obj)
		if err != nil {
			return nil, http.StatusInternalServerError, "read target entity: " + err.Error()
		}
		var ids []action.SourceID
		switch {
		case typ == identity.TypeHost && !hostVendorIDs:
			// No binding that could be chosen reads them.
		case typ == identity.TypeHost:
			devices, err := store.HostDeviceIDs(ctx, t.EntityRef)
			if err != nil {
				return nil, http.StatusInternalServerError, "read target host device ids: " + err.Error()
			}
			for _, d := range devices {
				ids = append(ids, action.SourceID{Label: d.Tool, ID: d.UID})
			}
		default:
			ids = storedSourceIDs(props)
		}
		vendorIDs, conflicts := action.VendorIDs(ids)
		props[action.VendorIDsKey] = vendorIDs
		out[i] = action.TargetEntity{Properties: props, VendorConflicts: conflicts}
	}
	return out, 0, ""
}

// entityProperties flattens a stored STIX object back into the properties the
// normalizer gave it (stix.PropertiesFrom).
func entityProperties(obj evidence.Object) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(obj.Payload, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", obj.Ref, err)
	}
	return stix.PropertiesFrom(m, obj.Type), nil
}

// storedSourceIDs reads the stored source_ids of an entity: the source's id
// for it, keyed by the source label.
func storedSourceIDs(props map[string]any) []action.SourceID {
	m, _ := props["source_ids"].(map[string]any)
	out := make([]action.SourceID, 0, len(m))
	for label, v := range m {
		if id, ok := v.(string); ok {
			out = append(out, action.SourceID{Label: label, ID: id})
		}
	}
	return out
}
