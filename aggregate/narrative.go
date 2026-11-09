package aggregate

// The investigation's narrative (design/06 §3.2 as amended, design/ui binding
// §6 gap 18, adr/0103): a written account of the record so far, drafted on
// demand by the model on the analyst's own key and edited by a person. It is
// an account of the record, not a reasoning act, so like a rename it is one
// domain event with no paired interpretation and never enters the chronicle.
// Every record is a new revision; the model's draft stays under a person's
// edit. Authorship is the envelope's actor, the same attribution every act
// carries: AI_DELEGATED with the person as principal for a draft, HUMAN for
// an edit.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// EventTypeNarrativeRecorded records one revision of the narrative.
const EventTypeNarrativeRecorded = "investigation.narrative_recorded"

// maxNarrativeRunes bounds one revision's body: a handoff account, not a
// transcript. The side completion asks for a few hundred words; a person
// pasting a report in is the case the bound is for.
const maxNarrativeRunes = 20_000

// NarrativeRecorded is the payload: the body, and for a model's draft the
// model that wrote it. Who recorded it is the event's actor.
type NarrativeRecorded struct {
	Body  string `json:"body"`
	Model string `json:"model,omitempty"`
}

// RecordNarrative appends a revision of the narrative. On the AI allowlist:
// a draft is the model's act under the analyst's principal, and the record
// says so. Model names the model for an AI actor and is refused on a human
// one, so an edit can never be dressed as a draft.
type RecordNarrative struct {
	Body  string `json:"body"`
	Model string `json:"model,omitempty"`
}

// Kind returns "RecordNarrative".
func (RecordNarrative) Kind() string { return "RecordNarrative" }

// Validate requires a non-blank body within the bound, and a model only on an
// AI-delegated actor.
func (c RecordNarrative) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	body := strings.TrimSpace(c.Body)
	if body == "" {
		return errors.New("RecordNarrative: Body is empty")
	}
	if n := utf8.RuneCountInString(body); n > maxNarrativeRunes {
		return fmt.Errorf("RecordNarrative: Body is %d runes; at most %d", n, maxNarrativeRunes)
	}
	if c.Model != "" && !env.Actor.IsAIDelegated() {
		return errors.New("RecordNarrative: Model is set on a human actor; only a delegate's draft names a model")
	}
	return nil
}

// narrativeEvent builds the one domain event. A concluded investigation may
// still be narrated: the handoff of a closed case is the account's use.
// Archived and discarded are refused by applyCommand's terminal guard.
func narrativeEvent(env Envelope, state aggregateState, c RecordNarrative) ([]Event, error) {
	payload, err := json.Marshal(NarrativeRecorded{Body: strings.TrimSpace(c.Body), Model: c.Model})
	if err != nil {
		return nil, err
	}
	return []Event{{
		AggregateID:   env.AggregateID,
		SequenceNo:    state.Seq + 1,
		TenantID:      env.TenantID,
		Type:          EventTypeNarrativeRecorded,
		Version:       schemaVersion,
		Payload:       payload,
		Actor:         env.Actor,
		OccurredAt:    env.OccurredAt,
		CorrelationID: env.CorrelationID,
	}}, nil
}

// NarrativeProjector keeps investigation_narratives: one row per revision,
// in the append transaction, so the history is a read and never disagrees
// with the log. The revision number is the count so far plus one.
type NarrativeProjector struct{}

// Name returns "investigation_narratives".
func (NarrativeProjector) Name() string { return "investigation_narratives" }

// Apply inserts the revision a narrative_recorded event carries.
func (NarrativeProjector) Apply(ctx context.Context, tx *sql.Tx, evt Event) error {
	if evt.Type != EventTypeNarrativeRecorded {
		return nil
	}
	var p NarrativeRecorded
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal NarrativeRecorded: %w", err)
	}
	kind := evt.Actor.Kind
	if kind == "" {
		kind = ActorHuman
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO investigation_narratives
		    (aggregate_id, revision, sequence_no, body, actor_kind, principal_id, principal_name, model, occurred_at)
		SELECT $1, COALESCE(MAX(revision), 0) + 1, $2, $3, $4, $5, $6, $7, $8
		FROM investigation_narratives WHERE aggregate_id = $1`,
		evt.AggregateID, evt.SequenceNo, p.Body, kind, evt.Actor.PrincipalID, evt.Actor.PrincipalName, p.Model, evt.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert investigation_narratives: %w", err)
	}
	return nil
}

// Reset truncates the table for a replay.
func (NarrativeProjector) Reset(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `TRUNCATE investigation_narratives`); err != nil {
		return fmt.Errorf("reset investigation_narratives: %w", err)
	}
	return nil
}

// NarrativeAuthor is who recorded a revision: the actor's kind and display
// name, and for a delegate's draft the model.
type NarrativeAuthor struct {
	Kind        string
	PrincipalID string
	Name        string
	Model       string
}

// NarrativeRevision is one row of the history.
type NarrativeRevision struct {
	Revision   int
	SequenceNo int64
	OccurredAt time.Time
	Author     NarrativeAuthor
	Body       string
}

// Narrative is the current revision with its history, newest first, and who
// drafted the first one.
type Narrative struct {
	Current   NarrativeRevision
	DraftedBy NarrativeAuthor
	Revisions []NarrativeRevision
}

// Narrative reads an investigation's narrative; ErrNoRecord when none was
// ever recorded. Every revision carries its body: the bound keeps the
// history small, and a reader may show any revision.
func (r *ReadModel) Narrative(ctx context.Context, aggID AggregateID) (Narrative, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT revision, sequence_no, occurred_at, actor_kind, principal_id, principal_name, model, body
		FROM investigation_narratives WHERE aggregate_id = $1 ORDER BY revision DESC`, aggID)
	if err != nil {
		return Narrative{}, fmt.Errorf("read narrative: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	var out Narrative
	for rows.Next() {
		var rev NarrativeRevision
		if err := rows.Scan(&rev.Revision, &rev.SequenceNo, &rev.OccurredAt, &rev.Author.Kind, &rev.Author.PrincipalID, &rev.Author.Name, &rev.Author.Model, &rev.Body); err != nil {
			return Narrative{}, fmt.Errorf("scan narrative: %w", err)
		}
		out.Revisions = append(out.Revisions, rev)
	}
	if err := rows.Err(); err != nil {
		return Narrative{}, err
	}
	if len(out.Revisions) == 0 {
		return Narrative{}, ErrNoRecord
	}
	out.Current = out.Revisions[0]
	out.DraftedBy = out.Revisions[len(out.Revisions)-1].Author
	return out, nil
}
