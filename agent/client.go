package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sd-strax/reckon/internal/agentapi"
)

// Most shapes the client sends and reads are the backend's own, from
// internal/agentapi (adr/0278). The seam is the HTTP API, and the agent
// imports no engine package (adr/0385).
//
// Four bodies are still sent as maps: RecallSOPs, RecallSimilar,
// PriorInvestigations and SubmitSummaryNarrative. Their request types are the
// knowledge service's and the server's, and were not moved. For three of them
// the type would also change the bytes the agent sends. The two recall types
// always send limit and include_retired, which the agent leaves out. The
// summary narrative type orders its keys differently and drops an empty
// generator_model, which the agent always sends. The prior-investigations
// type would send the same bytes.
//
// The recall and capability results the model reads are passed through as the
// backend sent them. The agent decodes them only to record what was consulted.

// APIError is a non-2xx backend response, surfaced with its body so the model
// (or analyst) sees the engine's explanation — a Gate 2 denial or a guarded
// transition is information, not noise.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("backend %d: %s", e.Status, e.Body) }

// Client is the loop's HTTP client to the reckon backend. It holds two tokens
// for the same analyst: the agent token (from the reckon-agent OIDC client,
// which stamps delegate_kind issuer-side) for everything the loop does, and the
// human token (from the reckon client) for the acts that are the analyst's own
// — approvals, lifecycle transitions. The backend derives the actor from the
// token, never from the body, so using the right token per call IS the
// authorization story.
type Client struct {
	base     string
	agentSrc TokenSource
	humanSrc TokenSource
	http     *http.Client
}

// NewClient builds a backend client. base is the backend root
// (e.g. http://localhost:8080). agentSrc/humanSrc mint (and refresh) the two
// tokens; pass StaticToken(...) for a fixed token that never refreshes.
func NewClient(base string, agentSrc, humanSrc TokenSource) *Client {
	return &Client{
		base:     base,
		agentSrc: agentSrc,
		humanSrc: humanSrc,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

// do executes one JSON request against the given token source; out (when
// non-nil) receives the decoded 2xx body. A 401 — the token was rejected
// earlier than its computed expiry (clock skew, revocation) — triggers one
// forced refresh and retry, so a long-idle session recovers transparently
// instead of failing the analyst's turn. raw responses are capped defensively.
func (c *Client) do(ctx context.Context, method, path string, src TokenSource, in, out any) error {
	var reqBody []byte
	if in != nil {
		var err error
		reqBody, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal %s %s: %w", method, path, err)
		}
	}

	send := func(forceRefresh bool) (*http.Response, error) {
		var (
			token string
			err   error
		)
		if forceRefresh {
			token, err = src.Refresh(ctx)
		} else {
			token, err = src.Token(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("acquire token for %s %s: %w", method, path, err)
		}
		var body io.Reader
		if reqBody != nil {
			body = bytes.NewReader(reqBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.http.Do(req)
	}

	resp, err := send(false)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		resp, err = send(true)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("read %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

// ListCapabilities fetches the verb catalog with availability (agent token).
func (c *Client) ListCapabilities(ctx context.Context) ([]agentapi.CapabilitySummary, error) {
	var out agentapi.CapabilitiesResponse
	if err := c.do(ctx, http.MethodGet, "/api/capabilities", c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out.Capabilities, nil
}

// InvokeCapability executes one read verb (agent token) and returns the raw
// envelope JSON — the loop feeds it to the model as the tool result verbatim.
func (c *Client) InvokeCapability(ctx context.Context, verb string, in agentapi.CapabilityInvokeBody) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/api/capability/"+verb, c.agentSrc, in, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActionTypes fetches the write-side action catalog with availability
// (agent token). The loop builds one action tool per catalog type from it,
// and refuses the calls of an unavailable one. A 503 (action layer off) surfaces as an *APIError the session reads as
// "no action tools"; any other error fails the session start.
func (c *Client) ListActionTypes(ctx context.Context) ([]agentapi.ActionSummary, error) {
	var out agentapi.ActionTypesResponse
	if err := c.do(ctx, http.MethodGet, "/api/action-types", c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out.ActionTypes, nil
}

// RecallSOPs runs SOP retrieval (agent token), returning the raw response.
func (c *Client) RecallSOPs(ctx context.Context, body map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/api/knowledge/recall_sops", c.agentSrc, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SOPSections reads a procedure's outline (agent token): every section's
// place without its text. A positive revision pins the read to the revision
// recall returned; the server refuses (409) when it is no longer current.
func (c *Client) SOPSections(ctx context.Context, sopID string, pin sectionPin) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/sops/"+url.PathEscape(sopID)+"/sections"+pin.query(), c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SOPSection reads one section of a procedure whole, with the outline
// around it (agent token); revision as for SOPSections.
func (c *Client) SOPSection(ctx context.Context, sopID string, n int, pin sectionPin) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/sops/"+url.PathEscape(sopID)+"/sections/"+strconv.Itoa(n)+pin.query(), c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// sectionPin is what a section index is read against: the revision and cut a
// recall hit (or an earlier read) reported, so the backend refuses the read
// (409) when either has moved rather than answering another section under
// the same number. The zero value is unpinned: an outline read.
type sectionPin struct {
	Revision int
	Cut      string
}

func (p sectionPin) query() string {
	q := url.Values{}
	if p.Revision > 0 {
		q.Set("revision", strconv.Itoa(p.Revision))
	}
	if p.Cut != "" {
		q.Set("cut", p.Cut)
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// RecallSimilar runs similar-investigation retrieval (agent token), returning
// the raw response.
func (c *Client) RecallSimilar(ctx context.Context, body map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/api/knowledge/recall_similar_investigations", c.agentSrc, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PriorInvestigations runs the exact by-entity lookup of concluded
// investigations (agent token, adr/0163), returning the raw response.
func (c *Client) PriorInvestigations(ctx context.Context, body map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/api/knowledge/prior_investigations", c.agentSrc, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SubmitSummaryNarrative posts the client-authored knowledge-summary narrative
// for a concluded investigation (agent token; design/06 §3.2 two-tier — the
// BYOK client writes the prose, the server holds no model key).
func (c *Client) SubmitSummaryNarrative(ctx context.Context, investigationRef, narrative, generatorModel string) error {
	body := map[string]any{
		"investigation_ref": investigationRef,
		"narrative":         narrative,
		"generator_model":   generatorModel,
	}
	return c.do(ctx, http.MethodPost, "/api/knowledge/summary_narrative", c.agentSrc, body, nil)
}

// RecordNarrative appends one revision of the investigation's narrative
// (agent token, adr/0103): the record attributes it to the model under the
// principal. Returns the revision recorded.
func (c *Client) RecordNarrative(ctx context.Context, investigationID, body, model string) (int, error) {
	var out agentapi.NarrativeView
	req := agentapi.NarrativeRequestBody{Body: body, Model: model}
	err := c.do(ctx, http.MethodPost, "/api/investigations/"+url.PathEscape(investigationID)+"/narrative", c.agentSrc, req, &out)
	return out.Revision, err
}

// RecordInterpretation appends one reasoning act (agent token).
func (c *Client) RecordInterpretation(ctx context.Context, body agentapi.RecordInterpretationBody) (agentapi.RecordInterpretationResponse, error) {
	var out agentapi.RecordInterpretationResponse
	err := c.do(ctx, http.MethodPost, "/api/interpretations", c.agentSrc, body, &out)
	return out, err
}

// RequestAction proposes a state-changing action (agent token; Gate 2 and the
// baseline DENY apply server-side).
func (c *Client) RequestAction(ctx context.Context, body agentapi.RequestActionBody) (agentapi.RequestActionResponse, error) {
	var out agentapi.RequestActionResponse
	err := c.do(ctx, http.MethodPost, "/api/actions", c.agentSrc, body, &out)
	return out, err
}

// GetInvestigation loads the investigation header (agent token — a read).
func (c *Client) GetInvestigation(ctx context.Context, id string) (agentapi.InvestigationView, error) {
	var out agentapi.InvestigationView
	err := c.do(ctx, http.MethodGet, "/api/investigations/"+id, c.agentSrc, nil, &out)
	return out, err
}

// ListHypotheses returns the investigation's reasoning nodes as raw JSON for
// prompt context.
func (c *Client) ListHypotheses(ctx context.Context, id string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/investigations/"+id+"/hypotheses", c.agentSrc, nil, &out)
	return out, err
}

// Thread returns the investigation's committed reasoning thread in sequence
// order (agent token — a read).
func (c *Client) Thread(ctx context.Context, id string) ([]agentapi.ThreadEntryView, error) {
	var out agentapi.ThreadResponse
	if err := c.do(ctx, http.MethodGet, "/api/investigations/"+id+"/thread", c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out.Thread, nil
}

// Transcript returns the committed turn transcript behind one interpretation
// (agent token — a read). The line-framed body is what the loop rehydrates.
func (c *Client) Transcript(ctx context.Context, interpretationID string) (string, error) {
	var out agentapi.TranscriptResponse
	err := c.do(ctx, http.MethodGet, "/api/interpretations/"+interpretationID+"/transcript", c.agentSrc, nil, &out)
	return out.Body, err
}

// ListActions returns the investigation's reckon-actions with their current status
// (agent token — a read). The durable source of the pending-approval queue.
func (c *Client) ListActions(ctx context.Context, id string) ([]agentapi.ActionView, error) {
	var out agentapi.ActionsResponse
	if err := c.do(ctx, http.MethodGet, "/api/investigations/"+id+"/actions", c.agentSrc, nil, &out); err != nil {
		return nil, err
	}
	return out.Actions, nil
}

// ApproveAction approves a pending action AS THE HUMAN (human token): approval
// is the analyst's act, never the delegate's — the backend rejects an agent
// token here regardless.
func (c *Client) ApproveAction(ctx context.Context, actionID, challengeResponse string) (json.RawMessage, error) {
	body := agentapi.ApproveActionBody{ChallengeResponse: challengeResponse}
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/actions/"+actionID+"/approve", c.humanSrc, body, &out)
	return out, err
}

// RejectAction rejects a pending action as the human.
func (c *Client) RejectAction(ctx context.Context, actionID, reason string) (json.RawMessage, error) {
	body := agentapi.RejectActionBody{Reason: reason}
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/actions/"+actionID+"/reject", c.humanSrc, body, &out)
	return out, err
}
