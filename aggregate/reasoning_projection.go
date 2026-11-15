package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/stix"
)

// ReasoningNodeProjector materializes the reasoning nodes carried on
// interpretation.recorded events into two places, in the same transaction as
// the event append:
//
//   - stix_objects — the canonical STIX-shaped node (reckon-hypothesis /
//     reckon-prediction JSON payload, 01-domain-model.md). This is the first writer
//     of the interpretation-layer object store: reasoning nodes are "STIX
//     nodes inside the Grouping," and here they land as exactly that. (The
//     Grouping object itself and membership/reckon-supports/reckon-refutes edges are
//     not written; capability-layer SCOs arrive through evidence.Store.)
//   - hypothesis_current / prediction_current — investigation-scoped read
//     models (the panels' query surface), same pattern as action_current.
//
// Because the nodes are projections of interpretation events, Reset+Replay
// rebuilds them from the event log alone. Reset deletes ONLY the reasoning
// types from stix_objects — the store also holds capability-layer entities
// that are NOT event-sourced, and replay must never destroy those.
type ReasoningNodeProjector struct{}

// Name returns "reasoning_nodes".
func (ReasoningNodeProjector) Name() string { return "reasoning_nodes" }

// Apply materializes one event's node content. Events without reasoning-node
// content are a no-op.
func (ReasoningNodeProjector) Apply(ctx context.Context, tx *sql.Tx, evt Event) error {
	if evt.Type != EventTypeInterpretationRecorded {
		return nil
	}
	var rec InterpretationRecorded
	if err := json.Unmarshal(evt.Payload, &rec); err != nil {
		return fmt.Errorf("unmarshal InterpretationRecorded: %w", err)
	}

	if rec.Hypothesis != nil {
		if err := insertHypothesis(ctx, tx, evt, rec, *rec.Hypothesis); err != nil {
			return err
		}
	}
	if rec.HypothesisTransition != nil {
		if err := transitionNode(ctx, tx, evt, stixTypeHypothesis, rec.HypothesisTransition.Ref, rec.HypothesisTransition.To, nil); err != nil {
			return err
		}
		// Acknowledge-with-restatement / revise (01 §reckon-hypothesis): the node's
		// fields move with the act; the prior wording stays on the earlier event.
		if tr := rec.HypothesisTransition; tr.Statement != "" || tr.Handle != "" {
			if err := reviseHypothesisFields(ctx, tx, evt, tr.Ref, tr.Statement, tr.Handle); err != nil {
				return err
			}
		}
	}
	if rec.Prediction != nil {
		if err := insertPrediction(ctx, tx, evt, rec, *rec.Prediction); err != nil {
			return err
		}
	}
	if rec.Attestation != nil {
		// The note lands before the transition that cites it (adr/0054).
		if err := insertAttestationNote(ctx, tx, evt, *rec.Attestation); err != nil {
			return err
		}
	}
	if rec.PredictionTransition != nil {
		if err := transitionNode(ctx, tx, evt, stixTypePrediction, rec.PredictionTransition.Ref, rec.PredictionTransition.To, rec.PredictionTransition.TestResultRefs); err != nil {
			return err
		}
	}
	return nil
}

// stixExternalRef is a STIX 2.1 external reference: a URL, or an id in a
// named source (a ticket number, a message id, or its hash).
type stixExternalRef struct {
	SourceName  string            `json:"source_name"`
	Description string            `json:"description,omitempty"`
	URL         string            `json:"url,omitempty"`
	ExternalID  string            `json:"external_id,omitempty"`
	Hashes      map[string]string `json:"hashes,omitempty"`
}

// attestationRefs renders an attestation's reference as an external reference:
// a URL as url, a "sha256:<hex>" as a hash, anything else as an external id.
func attestationRefs(ref string) []stixExternalRef {
	switch {
	case ref == "":
		return nil
	case strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://"):
		return []stixExternalRef{{SourceName: "attestation", URL: ref}}
	case strings.HasPrefix(ref, "sha256:"):
		return []stixExternalRef{{SourceName: "attestation", Hashes: map[string]string{"SHA-256": strings.TrimPrefix(ref, "sha256:")}}}
	default:
		return []stixExternalRef{{SourceName: "attestation", ExternalID: ref}}
	}
}

// artifactRefs renders where an attestation's artifact is kept (adr/0139) as
// a STIX external reference: the holding system as source_name, a location
// with a scheme as url and anything else as external_id, the content digest
// as hashes, and how to get access as description.
func artifactRefs(a *ArtifactLocation) []stixExternalRef {
	if a == nil || a.Location == "" {
		return nil
	}
	ref := stixExternalRef{SourceName: a.System, Description: a.Access}
	if ref.SourceName == "" {
		ref.SourceName = "artifact"
	}
	if strings.Contains(a.Location, "://") {
		ref.URL = a.Location
	} else {
		ref.ExternalID = a.Location
	}
	if a.SHA256 != "" {
		ref.Hashes = map[string]string{"SHA-256": a.SHA256}
	}
	return []stixExternalRef{ref}
}

// attestedBy names the attesting principal for display: the name the identity
// provider gave at the time (adr/0077), falling back to the id for records
// that predate it.
func attestedBy(a AttestationRecorded) string {
	if a.AuthorName != "" {
		return a.AuthorName
	}
	return a.Author
}

// attestationAbstract is the one-line reading of an attestation: who attested,
// how, and who was asked — "attested by analyst-1 by phone: Jane Doe".
func attestationAbstract(a AttestationRecorded) string {
	s := "attested by " + attestedBy(a)
	if a.Channel != "" {
		s += " by " + a.Channel
	}
	if a.Source != "" {
		s += ": " + a.Source
	}
	return s
}

// attestationNotePayload builds the STIX JSON of an attestation's note.
func attestationNotePayload(evt Event, a AttestationRecorded) ([]byte, error) {
	refs := []string{a.PredictionRef}
	if a.HypothesisRef != "" {
		refs = append(refs, a.HypothesisRef)
	}
	// The note is a STIX note with the attestation's structure in the
	// reckon-attestation property-extension (adr/0054, adr/0357), so a reader
	// can tell who was asked and how without parsing the abstract.
	at := stix.Truncate(evt.OccurredAt)
	var observedAt any
	if a.ObservedAt != nil {
		observedAt = *a.ObservedAt
	}
	return stix.Marshal(stix.Object{
		Type:    stixTypeNote,
		ID:      a.ID,
		Created: at,
		Properties: map[string]any{
			"abstract":            attestationAbstract(a),
			"content":             a.Content,
			"authors":             []string{a.Author},
			"object_refs":         refs,
			"external_references": append(attestationRefs(a.Reference), artifactRefs(a.Artifact)...),
		},
		Extensions: map[string]map[string]any{stix.ExtAttestation: {
			"prediction_ref": a.PredictionRef,
			"source":         a.Source,
			"channel":        a.Channel,
			"observed_at":    observedAt,
		}},
	})
}

func insertAttestationNote(ctx context.Context, tx *sql.Tx, evt Event, a AttestationRecorded) error {
	noteUUID, err := stixIDUUID(a.ID, stixTypeNote)
	if err != nil {
		return fmt.Errorf("project attestation: %w", err)
	}
	at := stix.Truncate(evt.OccurredAt)
	payload, err := attestationNotePayload(evt, a)
	if err != nil {
		return fmt.Errorf("marshal note: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stix_objects (id, tenant_id, type, spec_version, payload, created, modified)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (id) DO NOTHING
	`, noteUUID, evt.TenantID, stixTypeNote, stix.SpecVersion, payload, at); err != nil {
		return fmt.Errorf("insert stix note: %w", err)
	}
	// The prediction row carries the attestation too, so the view needs no
	// second lookup to say "attested by … : …" on the tracker.
	att, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal attestation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE prediction_current SET attestation = $2 WHERE id = $1
	`, a.PredictionRef, att); err != nil {
		return fmt.Errorf("update prediction_current attestation: %w", err)
	}
	return nil
}

// hypothesisPayload builds the node's STIX JSON (01 §reckon-hypothesis).
// created_by_ref is left out: there are no identity objects for people yet,
// and the producing interpretation event records the actor and delegate.
func hypothesisPayload(evt Event, rec InterpretationRecorded, h HypothesisRecorded) ([]byte, error) {
	at := stix.Truncate(evt.OccurredAt)
	return hypothesisNode{HypothesisRecorded: h, Rationale: rec.Summary}.payload(at, at)
}

// hypothesisNode is a stored reckon-hypothesis as the projector rebuilds it:
// the node content, the rationale it was recorded with, and its created time.
// Every write of the node, its first and every change after, goes through
// payload, so the serializer checks each one and a replay writes the same
// bytes (adr/0357).
type hypothesisNode struct {
	HypothesisRecorded
	Rationale string    `json:"rationale,omitempty"`
	Created   time.Time `json:"created"`
}

func (n hypothesisNode) payload(created, modified time.Time) ([]byte, error) {
	h := n.HypothesisRecorded
	return stix.Marshal(stix.Object{
		Type:     stixTypeHypothesis,
		ID:       h.ID,
		Created:  created,
		Modified: modified,
		Properties: map[string]any{
			"statement":     h.Statement,
			"handle":        h.Handle,
			"status":        h.Status,
			"parent_ref":    h.ParentRef,
			"rooted_at_ref": h.RootedAtRef,
			"rationale":     n.Rationale,
			"labels":        h.Labels,
		},
	})
}

func insertHypothesis(ctx context.Context, tx *sql.Tx, evt Event, rec InterpretationRecorded, h HypothesisRecorded) error {
	nodeUUID, err := stixIDUUID(h.ID, stixTypeHypothesis)
	if err != nil {
		return fmt.Errorf("project hypothesis: %w", err)
	}
	at := stix.Truncate(evt.OccurredAt)
	payload, err := hypothesisPayload(evt, rec, h)
	if err != nil {
		return fmt.Errorf("marshal reckon-hypothesis: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stix_objects (id, tenant_id, type, spec_version, payload, created, modified)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (id) DO NOTHING
	`, nodeUUID, evt.TenantID, stixTypeHypothesis, stix.SpecVersion, payload, at); err != nil {
		return fmt.Errorf("insert stix reckon-hypothesis: %w", err)
	}

	var labels []byte
	if len(h.Labels) > 0 {
		if labels, err = json.Marshal(h.Labels); err != nil {
			return fmt.Errorf("marshal labels: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hypothesis_current (
			id, aggregate_id, tenant_id, statement, status, parent_ref,
			rooted_at_ref, labels, created_at, updated_at, last_event_sequence, handle
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING
	`, h.ID, evt.AggregateID, evt.TenantID, h.Statement, h.Status,
		nullString(h.ParentRef), nullString(h.RootedAtRef), labels,
		evt.OccurredAt, evt.SequenceNo, h.Handle); err != nil {
		return fmt.Errorf("insert hypothesis_current: %w", err)
	}
	return nil
}

// reviseHypothesisFields rewrites a hypothesis's statement and/or handle in
// both stores (acknowledge-with-restatement / revise). Empty means unchanged.
func reviseHypothesisFields(ctx context.Context, tx *sql.Tx, evt Event, ref, statement, handle string) error {
	nodeUUID, err := stixIDUUID(ref, stixTypeHypothesis)
	if err != nil {
		return fmt.Errorf("project hypothesis revise: %w", err)
	}
	if statement != "" || handle != "" {
		if err := rewriteNode(ctx, tx, nodeUUID, stixTypeHypothesis, evt.OccurredAt, func(node any) {
			n := node.(*hypothesisNode)
			if statement != "" {
				n.Statement = statement
			}
			if handle != "" {
				n.Handle = handle
			}
		}); err != nil {
			return err
		}
	}
	if statement != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE hypothesis_current SET statement = $2, last_event_sequence = $3, updated_at = $4 WHERE id = $1
		`, ref, statement, evt.SequenceNo, evt.OccurredAt); err != nil {
			return fmt.Errorf("update hypothesis_current statement: %w", err)
		}
	}
	if handle != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE hypothesis_current SET handle = $2, last_event_sequence = $3, updated_at = $4 WHERE id = $1
		`, ref, handle, evt.SequenceNo, evt.OccurredAt); err != nil {
			return fmt.Errorf("update hypothesis_current handle: %w", err)
		}
	}
	return nil
}

// predictionPayload builds the node's STIX JSON (01 §reckon-prediction).
func predictionPayload(evt Event, p PredictionRecorded) ([]byte, error) {
	at := stix.Truncate(evt.OccurredAt)
	return predictionNode{PredictionRecorded: p}.payload(at, at)
}

// predictionNode is a stored reckon-prediction as the projector rebuilds it,
// with the test results its outcome cites. As hypothesisNode, every write goes
// through payload (adr/0357).
type predictionNode struct {
	PredictionRecorded
	// The outcome's citations as stored: STIX ids in TestResultRefs, raw OCSF
	// event ids in TestResultEventIDs (adr/0357).
	TestResultRefs     []string  `json:"test_result_refs,omitempty"`
	TestResultEventIDs []string  `json:"test_result_event_ids,omitempty"`
	Created            time.Time `json:"created"`
}

// cite stores an outcome's citations as the command gave them, split by form:
// a STIX id is a ref, and a bare UUID is a raw OCSF event's id (adr/0357). The
// event log keeps the list whole, so a replay splits it the same way.
func (n *predictionNode) cite(refs []string) {
	n.TestResultRefs, n.TestResultEventIDs = nil, nil
	for _, r := range refs {
		if stix.IsIdentifier(r) {
			n.TestResultRefs = append(n.TestResultRefs, r)
		} else {
			n.TestResultEventIDs = append(n.TestResultEventIDs, r)
		}
	}
}

func (n predictionNode) payload(created, modified time.Time) ([]byte, error) {
	p := n.PredictionRecorded
	properties := map[string]any{
		"hypothesis_ref": p.HypothesisRef,
		"statement":      p.Statement,
		"title":          p.Title,
		"status":         p.Status,
		"test_query":     p.TestQuery,
		"discriminates":  p.Discriminates,
		"test_kind":      p.TestKind,
		// An empty list is left out, never stored as [] (adr/0357).
		"test_result_refs":      n.TestResultRefs,
		"test_result_event_ids": n.TestResultEventIDs,
	}
	if p.TitleDerived {
		properties["title_derived"] = true
	}
	return stix.Marshal(stix.Object{Type: stixTypePrediction, ID: p.ID, Created: created, Modified: modified, Properties: properties})
}

// rewriteNode rebuilds a stored reasoning node from its current payload with
// change applied, through the node's payload function, and writes it whole
// with modified at the event's time (adr/0357). A change the serializer
// refuses fails the event's transaction.
func rewriteNode(ctx context.Context, tx *sql.Tx, nodeUUID uuid.UUID, stixType string, at time.Time, change func(node any)) error {
	var stored []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM stix_objects WHERE id = $1 FOR UPDATE`, nodeUUID).Scan(&stored); err != nil {
		return fmt.Errorf("read stix %s: %w", stixType, err)
	}
	modified := stix.Truncate(at)
	var payload []byte
	var err error
	switch stixType {
	case stixTypeHypothesis:
		var n hypothesisNode
		if err := json.Unmarshal(stored, &n); err != nil {
			return fmt.Errorf("decode stix %s: %w", stixType, err)
		}
		change(&n)
		payload, err = n.payload(n.Created, modified)
	case stixTypePrediction:
		var n predictionNode
		if err := json.Unmarshal(stored, &n); err != nil {
			return fmt.Errorf("decode stix %s: %w", stixType, err)
		}
		change(&n)
		payload, err = n.payload(n.Created, modified)
	default:
		return fmt.Errorf("rewrite %s: not a reasoning node", stixType)
	}
	if err != nil {
		return fmt.Errorf("marshal %s: %w", stixType, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE stix_objects SET payload = $2, modified = $3 WHERE id = $1`, nodeUUID, payload, modified); err != nil {
		return fmt.Errorf("update stix %s: %w", stixType, err)
	}
	return nil
}

func insertPrediction(ctx context.Context, tx *sql.Tx, evt Event, _ InterpretationRecorded, p PredictionRecorded) error {
	nodeUUID, err := stixIDUUID(p.ID, stixTypePrediction)
	if err != nil {
		return fmt.Errorf("project prediction: %w", err)
	}
	at := stix.Truncate(evt.OccurredAt)
	payload, err := predictionPayload(evt, p)
	if err != nil {
		return fmt.Errorf("marshal reckon-prediction: %w", err)
	}
	testKind := p.TestKind
	if testKind == "" {
		testKind = TestKindTool
	}
	var discriminates []byte
	if len(p.Discriminates) > 0 {
		if discriminates, err = json.Marshal(p.Discriminates); err != nil {
			return fmt.Errorf("marshal discriminates: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stix_objects (id, tenant_id, type, spec_version, payload, created, modified)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (id) DO NOTHING
	`, nodeUUID, evt.TenantID, stixTypePrediction, stix.SpecVersion, payload, at); err != nil {
		return fmt.Errorf("insert stix reckon-prediction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO prediction_current (
			id, hypothesis_ref, aggregate_id, tenant_id, statement, status,
			created_at, updated_at, last_event_sequence, discriminates, test_kind,
			title, title_derived
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING
	`, p.ID, p.HypothesisRef, evt.AggregateID, evt.TenantID, p.Statement, p.Status,
		evt.OccurredAt, evt.SequenceNo, discriminates, testKind, p.Title, p.TitleDerived); err != nil {
		return fmt.Errorf("insert prediction_current: %w", err)
	}
	return nil
}

// transitionNode moves a node's status in both stores. testResultRefs is
// non-nil only for prediction outcomes; it lands in both the STIX payload and
// prediction_current.
func transitionNode(ctx context.Context, tx *sql.Tx, evt Event, stixType, ref, to string, testResultRefs []string) error {
	nodeUUID, err := stixIDUUID(ref, stixType)
	if err != nil {
		return fmt.Errorf("project %s transition: %w", stixType, err)
	}

	// The STIX payload: the status (+ test_result_refs for predictions),
	// rebuilt whole through the node's payload function, modified moved in
	// the payload and the column alike (adr/0357).
	if err := rewriteNode(ctx, tx, nodeUUID, stixType, evt.OccurredAt, func(node any) {
		switch n := node.(type) {
		case *hypothesisNode:
			n.Status = to
		case *predictionNode:
			n.Status = to
			if testResultRefs != nil {
				n.cite(testResultRefs)
			}
		}
	}); err != nil {
		return err
	}

	switch stixType {
	case stixTypeHypothesis:
		if _, err := tx.ExecContext(ctx, `
			UPDATE hypothesis_current
			SET status = $2, last_event_sequence = $3, updated_at = $4
			WHERE id = $1
		`, ref, to, evt.SequenceNo, evt.OccurredAt); err != nil {
			return fmt.Errorf("update hypothesis_current: %w", err)
		}
	case stixTypePrediction:
		var refs []byte
		if len(testResultRefs) > 0 {
			refs, err = json.Marshal(testResultRefs)
			if err != nil {
				return fmt.Errorf("marshal test_result_refs: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE prediction_current
			SET status = $2, test_result_refs = COALESCE($3, test_result_refs),
			    last_event_sequence = $4, updated_at = $5
			WHERE id = $1
		`, ref, to, refs, evt.SequenceNo, evt.OccurredAt); err != nil {
			return fmt.Errorf("update prediction_current: %w", err)
		}
	}
	return nil
}

// Reset clears the projection for replay. stix_objects is scoped to the
// reasoning types — the object store also holds non-event-sourced entities
// (capability-layer SCOs, from evidence.Store) that a replay must never destroy.
func (ReasoningNodeProjector) Reset(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM stix_objects WHERE type IN ($1, $2)`,
		stixTypeHypothesis, stixTypePrediction); err != nil {
		return fmt.Errorf("delete reasoning stix_objects: %w", err)
	}
	// Attestation notes are ours to rebuild; a note from any other producer
	// (none today) is not, so the delete is scoped by the reckon-attestation
	// property-extension (adr/0357).
	// #nosec G202 -- the joined fragment is a fixed JSON path from the stix package, never input.
	if _, err := tx.ExecContext(ctx, `DELETE FROM stix_objects WHERE type = $1 AND `+stix.HasExtensionSQL("payload", stix.ExtAttestation),
		stixTypeNote); err != nil {
		return fmt.Errorf("delete attestation notes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `TRUNCATE hypothesis_current, prediction_current`); err != nil {
		return fmt.Errorf("truncate reasoning projections: %w", err)
	}
	return nil
}

// --- read side -----------------------------------------------------------------

// HypothesisCurrent is one materialized hypothesis row.
type HypothesisCurrent struct {
	ID                string
	AggregateID       AggregateID
	Statement         string
	Handle            string
	Status            string
	ParentRef         string
	RootedAtRef       string
	Labels            []string
	LastEventSequence int64
}

// PredictionCurrent is one materialized prediction row. TestQuery is read from
// the canonical reckon-prediction STIX payload (the projector's other write) rather
// than a duplicated column — the declared test is immutable after creation.
type PredictionCurrent struct {
	ID                string
	HypothesisRef     string
	AggregateID       AggregateID
	Statement         string
	Title             string // the recorded short name (adr/0090)
	TitleDerived      bool   // the aggregate derived it from the statement
	Status            string
	TestQuery         *QuerySpec
	Discriminates     []string
	TestKind          string               // tool | manual (adr/0054)
	Attestation       *AttestationRecorded // the human observation the outcome cites, when it has one
	TestResultRefs    []string
	LastEventSequence int64
}

// ListHypotheses returns the hypotheses of one investigation, oldest first.
func (r *ReadModel) ListHypotheses(ctx context.Context, aggregateID uuid.UUID) ([]HypothesisCurrent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, aggregate_id, statement, handle, status, parent_ref, rooted_at_ref, labels, last_event_sequence
		FROM hypothesis_current
		WHERE aggregate_id = $1
		ORDER BY created_at, id
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query hypothesis_current: %w", err)
	}
	defer rows.Close()

	var out []HypothesisCurrent
	for rows.Next() {
		var h HypothesisCurrent
		var parent, rooted sql.NullString
		var labels []byte
		if err := rows.Scan(&h.ID, &h.AggregateID, &h.Statement, &h.Handle, &h.Status,
			&parent, &rooted, &labels, &h.LastEventSequence); err != nil {
			return nil, fmt.Errorf("scan hypothesis_current: %w", err)
		}
		h.ParentRef, h.RootedAtRef = parent.String, rooted.String
		if len(labels) > 0 {
			_ = json.Unmarshal(labels, &h.Labels)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListPredictions returns the predictions of one investigation, oldest first.
// The declared test query rides along from the canonical STIX payload, so the
// tracker can stage it ("Test this") without a second fetch.
func (r *ReadModel) ListPredictions(ctx context.Context, aggregateID uuid.UUID) ([]PredictionCurrent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.hypothesis_ref, p.aggregate_id, p.statement, p.status,
		       p.test_result_refs, p.discriminates, p.last_event_sequence,
		       s.payload -> 'test_query', p.test_kind, p.attestation, p.title, p.title_derived
		FROM prediction_current p
		LEFT JOIN stix_objects s ON s.id = split_part(p.id, '--', 2)::uuid
		WHERE p.aggregate_id = $1
		ORDER BY p.created_at, p.id
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query prediction_current: %w", err)
	}
	defer rows.Close()

	var out []PredictionCurrent
	for rows.Next() {
		var p PredictionCurrent
		var refs, discriminates, testQuery, attestation []byte
		if err := rows.Scan(&p.ID, &p.HypothesisRef, &p.AggregateID, &p.Statement,
			&p.Status, &refs, &discriminates, &p.LastEventSequence, &testQuery, &p.TestKind, &attestation,
			&p.Title, &p.TitleDerived); err != nil {
			return nil, fmt.Errorf("scan prediction_current: %w", err)
		}
		if len(refs) > 0 {
			_ = json.Unmarshal(refs, &p.TestResultRefs)
		}
		if len(discriminates) > 0 {
			_ = json.Unmarshal(discriminates, &p.Discriminates)
		}
		if len(testQuery) > 0 {
			_ = json.Unmarshal(testQuery, &p.TestQuery)
		}
		if len(attestation) > 0 {
			_ = json.Unmarshal(attestation, &p.Attestation)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListReasoningStixPayloads returns the canonical STIX payloads of the
// interpretation-layer nodes belonging to one investigation — the objects the
// export bundle's stix-bundle.json carries (07 §2.1). The reasoning nodes'
// _current tables key the node by its full STIX id (reckon-hypothesis--<uuid>);
// stix_objects keys by the bare uuid, so we join on the uuid extracted from the
// full id (split on the STIX "--" separator). Ordered by type+id for a stable
// bundle.
func (r *ReadModel) ListReasoningStixPayloads(ctx context.Context, aggregateID uuid.UUID) ([]json.RawMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT payload FROM stix_objects
		WHERE id IN (
			SELECT split_part(id, '--', 2)::uuid FROM hypothesis_current WHERE aggregate_id = $1
			UNION
			SELECT split_part(id, '--', 2)::uuid FROM prediction_current WHERE aggregate_id = $1
			UNION
			-- the attestation notes the predictions' outcomes cite (adr/0054)
			SELECT split_part(p.attestation ->> 'id', '--', 2)::uuid
			FROM prediction_current p WHERE p.aggregate_id = $1 AND p.attestation IS NOT NULL
		)
		ORDER BY type, id
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query reasoning stix_objects: %w", err)
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan stix payload: %w", err)
		}
		out = append(out, json.RawMessage(payload))
	}
	return out, rows.Err()
}

// ActionSummary is the lightweight view of a reckon-action for the export report
// (07 §2.1) — what it was, its tier, and where it ended.
type ActionSummary struct {
	ActionID   uuid.UUID
	ActionType string
	Tier       string
	Status     string
}

// ListActionSummaries returns every reckon-action of one investigation, oldest
// first, for the export report + audit view.
func (r *ReadModel) ListActionSummaries(ctx context.Context, aggregateID uuid.UUID) ([]ActionSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT action_id, action_type, tier, status
		FROM action_current
		WHERE aggregate_id = $1
		ORDER BY created_at, action_id
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query action_current: %w", err)
	}
	defer rows.Close()

	var out []ActionSummary
	for rows.Next() {
		var a ActionSummary
		if err := rows.Scan(&a.ActionID, &a.ActionType, &a.Tier, &a.Status); err != nil {
			return nil, fmt.Errorf("scan action summary: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
