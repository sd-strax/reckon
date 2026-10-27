package aggregate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// SideStore persists the AI reasoning side stores (02 §6 Layer B): the raw
// transcript bytes and the per-turn tool-call log. These are bulky, content-
// addressed data that must NOT live in the event log — the InterpretationRecorded
// event references them by content hash, this store holds the bytes. Writes run
// inside the aggregate's Handle transaction (via WriteReasoningTx) so an
// interpretation event and its transcript/tool-call rows commit atomically: the
// audit trail can never reference a transcript that failed to persist.
//
// The store is content-addressed: ai_transcripts is keyed by (tenant_id, hash),
// so re-recording byte-identical content is idempotent (ON CONFLICT DO NOTHING).
type SideStore struct {
	db *sql.DB
}

// NewSideStore constructs a SideStore over the aggregate database (the same
// reckon_main DB the events + side-store tables share, migration 0004).
func NewSideStore(db *sql.DB) *SideStore { return &SideStore{db: db} }

// TranscriptContent is the raw transcript for one reasoning turn, carried on a
// RecordInterpretation command. Body is hashed at record time (HashContent);
// the resulting hash is what the interpretation event's transcript_ref stores.
type TranscriptContent struct {
	TranscriptID uuid.UUID `json:"transcript_id"`
	TurnID       string    `json:"turn_id,omitempty"`
	Body         []byte    `json:"body"`
}

// ToolCallContent is one tool dispatch the LLM made during the turn, carried on
// a RecordInterpretation command and logged to ai_tool_calls. Args is stored
// inline byte-exact (TEXT, migration 0010 — JSONB would re-serialize and break
// the content address; cast tool_args::jsonb to query it). ResultHash is
// CALLER-CLAIMED provenance: it names content the backend has not (yet) stored,
// so it is recorded verbatim but is not a verifiable content address until the
// tool-result store lands (v1 — results are served by /capability/<verb>
// responses today and not retained). Treat it as a client assertion, not proof.
type ToolCallContent struct {
	CallID     string          `json:"call_id"` // the model's tool_use id
	ToolName   string          `json:"tool_name"`
	Args       json.RawMessage `json:"args,omitempty"`
	ResultHash string          `json:"result_hash,omitempty"`
	// CapabilityCallID is the capability call the dispatch made (adr/0092),
	// empty for an intrinsic tool or a call made before calls were recorded.
	CapabilityCallID string `json:"capability_call_id,omitempty"`
}

// RetrievalContent is one retrieved procedure excerpt as the model saw it
// (06 §6.2; adr/0097), carried on a RecordInterpretation and written to
// knowledge_retrievals (the bytes, keyed by excerpt hash) and
// knowledge_retrieval_uses (this interpretation's use, with score and band).
type RetrievalContent struct {
	SOPID        uuid.UUID       `json:"sop_id"`
	RevisionHash string          `json:"revision_hash"`
	Excerpt      string          `json:"excerpt"`
	Location     json.RawMessage `json:"location,omitempty"`
	Score        float64         `json:"score,omitempty"`
	Band         string          `json:"band,omitempty"`
}

// ExcerptHash is the content address of a retrieved excerpt: SHA-256 of its
// bytes, the same function the agent and the event use.
func (r RetrievalContent) ExcerptHash() string { return HashContent([]byte(r.Excerpt)) }

// TranscriptRef is the reference the interpretation event carries into the
// transcript side store (02 §6): the content hash locates the bytes, the
// transcript/turn ids locate the position within the conversation.
type TranscriptRef struct {
	TranscriptID uuid.UUID `json:"transcript_id"`
	TurnID       string    `json:"turn_id,omitempty"`
	ContentHash  string    `json:"content_hash"`
}

// ToolCallRef is the reference the interpretation event carries for one tool
// call: the call id plus the content hash of its arguments.
type ToolCallRef struct {
	CallID      string `json:"call_id"`
	ContentHash string `json:"content_hash"`
}

// HashContent returns the SHA-256 hex of b — the content address used for both
// the interpretation event's refs and the side-store rows. Pure (no IO/time/
// rand), so applyCommand can compute the ref hash and the SideStore can
// recompute the identical hash from the identical bytes without any shared
// stored value to drift out of sync.
func HashContent(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// normalizeArgs maps empty/nil tool-call args to the canonical empty JSON
// object. Both the event's tool_call_refs hash (applyRecordInterpretation) and
// the stored ai_tool_calls row (WriteReasoningTx) go through this SAME helper —
// the content address on the event must be the hash of exactly the bytes the
// side store holds, or a verifier recomputing hashes gets a false tamper alarm.
func normalizeArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage("{}")
	}
	return args
}

// WriteReasoningTx persists the transcript + tool-call side stores for one
// RecordInterpretation inside tx, linking the tool-call rows to the
// interpretation event that was just appended (interpEventID). Content-addressed
// and idempotent: the transcript upsert is ON CONFLICT DO NOTHING, so a retry of
// the same content is a no-op.
func (s *SideStore) WriteReasoningTx(ctx context.Context, tx *sql.Tx, env Envelope, interpEventID uuid.UUID, c RecordInterpretation) error {
	var transcriptHash string
	if c.Transcript != nil {
		transcriptHash = HashContent(c.Transcript.Body)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ai_transcripts (tenant_id, hash, body, size_bytes, stored_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant_id, hash) DO NOTHING
		`, env.TenantID, transcriptHash, c.Transcript.Body, len(c.Transcript.Body), env.OccurredAt); err != nil {
			return fmt.Errorf("insert ai_transcripts: %w", err)
		}
	}

	for i := range c.ToolCalls {
		tc := c.ToolCalls[i]
		args := normalizeArgs(tc.Args)
		var capCall any
		if id, err := uuid.Parse(tc.CapabilityCallID); err == nil {
			capCall = id
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ai_tool_calls (
				id, tenant_id, investigation_id, event_id, tool_name, tool_args,
				result_hash, transcript_hash, occurred_at, capability_call_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, uuid.Must(uuid.NewV7()), env.TenantID, env.AggregateID, interpEventID,
			tc.ToolName, []byte(args), nullString(tc.ResultHash), nullString(transcriptHash),
			env.OccurredAt, capCall); err != nil {
			return fmt.Errorf("insert ai_tool_calls: %w", err)
		}
	}
	for _, r := range c.Retrievals {
		if r.Excerpt == "" || r.SOPID == uuid.Nil {
			continue
		}
		var loc any
		if len(r.Location) > 0 {
			loc = []byte(r.Location)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_retrievals (tenant_id, excerpt_hash, sop_id, revision_hash, excerpt, location, first_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant_id, excerpt_hash) DO NOTHING
		`, env.TenantID, r.ExcerptHash(), r.SOPID, r.RevisionHash, r.Excerpt, loc, env.OccurredAt); err != nil {
			return fmt.Errorf("insert knowledge_retrievals: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_retrieval_uses (tenant_id, investigation_id, interpretation_id, excerpt_hash, score, band, retrieved_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING
		`, env.TenantID, env.AggregateID, c.InterpretationID, r.ExcerptHash(), r.Score, nullString(r.Band), env.OccurredAt); err != nil {
			return fmt.Errorf("insert knowledge_retrieval_uses: %w", err)
		}
	}
	return nil
}

// nullString maps "" to SQL NULL.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
