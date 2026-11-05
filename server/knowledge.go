package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/module"
)

// SOPBody is the create/update payload for a SOP.
type SOPBody struct {
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	Tags           []string `json:"tags,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
}

// SOPImportBody is the import payload (design/06 §2.4): SOPBody plus the
// original author and the source pointer. source.url keys the re-import
// lineage — importing the same URL revises the existing SOP.
type SOPImportBody struct {
	SOPBody
	Author string `json:"author,omitempty"` // the ORIGINAL document author, from the doc
	Source struct {
		System  string `json:"system,omitempty"`
		URL     string `json:"url"`
		Version string `json:"version,omitempty"`
	} `json:"source"`
}

// recallSOPs handles POST /api/knowledge/recall_sops (design/06 §4): keyword
// retrieval over the SOP corpus. Any authenticated reader — the agent loop
// consults it during reasoning.
func (b *Backend) recallSOPs(w http.ResponseWriter, r *http.Request) {
	var req knowledge.RecallRequest
	if !decodeBody(w, r, &req, maxBodyBytes) {
		return
	}
	res, err := b.cfg.Knowledge.RecallSOPs(r.Context(), module.SingleTenantUUID, req)
	if errors.Is(err, knowledge.ErrInvalidRequest) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, "recall_sops", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ReindexJob is the state of the knowledge reindex (GET /api/knowledge/reindex):
// the last run started on demand, or none. A run is one embedder call per
// stale entry, so it outlives an HTTP request's deadline; the POST starts it
// and answers 202, and the state is read here until it is done or failed.
type ReindexJob struct {
	State      string     `json:"state"` // idle | running | done | failed
	Entries    int        `json:"entries,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// reindexTimeout bounds one on-demand run; the startup reconciliation has
// the component's own deadline.
const reindexTimeout = time.Hour

// startReindexKnowledge handles POST /api/knowledge/reindex: starts bringing
// the corpus up to date with the substrate's section rule and embedding model
// (the reconciliation the backend also runs at startup), for an operator who
// changed configuration and wants the corpus current without a restart.
// Analyst role, like the import routes. A run already in progress is answered
// with its state rather than started twice.
func (b *Backend) startReindexKnowledge(w http.ResponseWriter, r *http.Request) {
	b.reindexMu.Lock()
	if b.reindex.State == "running" {
		job := b.reindex
		b.reindexMu.Unlock()
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	now := time.Now().UTC()
	b.reindex = ReindexJob{State: "running", StartedAt: &now}
	job := b.reindex
	b.reindexMu.Unlock()

	// The run outlives the request: its own context, its own deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), reindexTimeout)
	go func() {
		defer cancel()
		n, err := b.cfg.Knowledge.Reindex(ctx, module.SingleTenantUUID)
		done := time.Now().UTC()
		b.reindexMu.Lock()
		defer b.reindexMu.Unlock()
		b.reindex.FinishedAt = &done
		b.reindex.Entries = n
		if err != nil {
			b.reindex.State, b.reindex.Error = "failed", err.Error()
			return
		}
		b.reindex.State = "done"
	}()
	writeJSON(w, http.StatusAccepted, job)
}

// getReindexKnowledge handles GET /api/knowledge/reindex: the state of the
// last on-demand run, idle when none was started since the backend came up.
func (b *Backend) getReindexKnowledge(w http.ResponseWriter, _ *http.Request) {
	b.reindexMu.Lock()
	job := b.reindex
	b.reindexMu.Unlock()
	if job.State == "" {
		job.State = "idle"
	}
	writeJSON(w, http.StatusOK, job)
}

// revisionParam reads the optional ?revision= a section read pins itself to.
func revisionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("revision")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		writeJSONError(w, http.StatusBadRequest, "revision must be a positive integer")
		return 0, false
	}
	return n, true
}

// recallSimilar handles POST /api/knowledge/recall_similar_investigations
// (design/06 §4): similarity retrieval over the concluded-investigation
// summary corpus — "have we handled something like this before?" Any
// authenticated reader; the agent loop consults it during reasoning.
func (b *Backend) recallSimilar(w http.ResponseWriter, r *http.Request) {
	var req knowledge.SimilarRequest
	if !decodeBody(w, r, &req, maxBodyBytes) {
		return
	}
	res, err := b.cfg.Knowledge.RecallSimilar(r.Context(), module.SingleTenantUUID, req)
	if err != nil {
		writeInternalError(w, "recall_similar_investigations", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// priorInvestigations handles POST /api/knowledge/prior_investigations
// (adr/0163): the exact lookup "have we investigated this entity before?" —
// every concluded investigation whose summary records the entity. Any
// authenticated reader.
func (b *Backend) priorInvestigations(w http.ResponseWriter, r *http.Request) {
	var req knowledge.PriorRequest
	if !decodeBody(w, r, &req, maxBodyBytes) {
		return
	}
	if strings.TrimSpace(req.Entity) == "" {
		writeJSONError(w, http.StatusBadRequest, "prior_investigations: entity required")
		return
	}
	res, err := b.cfg.Knowledge.PriorByEntity(r.Context(), module.SingleTenantUUID, req)
	if err != nil {
		writeInternalError(w, "prior_investigations", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// listCaseSummaries handles GET /api/knowledge/case_summaries: the concluded
// investigations' summaries, most recent first — the library's past cases
// (adr/0129). Any authenticated reader.
func (b *Backend) listCaseSummaries(w http.ResponseWriter, r *http.Request) {
	list, err := b.cfg.Knowledge.ListSummaries(r.Context(), module.SingleTenantUUID, 100)
	if err != nil {
		writeInternalError(w, "case_summaries", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"case_summaries": list})
}

// getCaseSummary handles GET /api/knowledge/case_summaries/{investigation_ref}
// (design/ui 08 §8.6.1 "Similar past case"): one concluded investigation's
// summary read back by its source ref, for the inspector. The demo's seeded
// prior cases have no local aggregate, so the summary is the only record. Any
// authenticated reader; 404 when none exists.
func (b *Backend) getCaseSummary(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	v, err := b.cfg.Knowledge.SummaryByInvestigation(r.Context(), module.SingleTenantUUID, ref)
	if errors.Is(err, knowledge.ErrNoSummary) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, "case_summaries", err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// SummaryNarrativeBody is the client-side enrichment payload (design/06 §3.2
// two-tier): the narrative the analyst's own BYOK client wrote at conclusion.
type SummaryNarrativeBody struct {
	InvestigationRef string `json:"investigation_ref"`
	Narrative        string `json:"narrative"`
	GeneratorModel   string `json:"generator_model,omitempty"`
}

// summaryNarrative handles POST /api/knowledge/summary_narrative (analyst):
// the BYOK client enriches the concluded investigation's structured summary
// with an LLM narrative. The server never holds a model key — the two-tier
// split is the design (06 §3.2): server writes structure at conclusion,
// client writes prose. 404 (ErrNoSummary) means the post-conclusion pipeline
// hasn't written the baseline yet; clients retry briefly.
func (b *Backend) summaryNarrative(w http.ResponseWriter, r *http.Request) {
	var body SummaryNarrativeBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	id, err := b.cfg.Knowledge.EnrichSummary(r.Context(), module.SingleTenantUUID, body.InvestigationRef, body.Narrative, body.GeneratorModel)
	if errors.Is(err, knowledge.ErrNoSummary) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "summary_narrative: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary_id": id.String()})
}

// importSOP handles POST /api/sops/import (analyst): upsert institutional
// knowledge from an external source (design/06 §2.4). Attribution keeps three
// facts distinct: the document's original author (from the payload), the
// source pointer (the re-import lineage key), and the importer (the
// authenticated principal — recorded here, never client-supplied).
func (b *Backend) importSOP(w http.ResponseWriter, r *http.Request) {
	var body SOPImportBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	claims, _ := authz.FromContext(r.Context())
	importer := claims.PreferredUsername
	if importer == "" {
		importer = claims.Subject
	}
	id, created, err := b.cfg.Knowledge.Import(r.Context(), knowledge.SOP{
		TenantID: module.SingleTenantUUID,
		Title:    body.Title, Body: body.Body, Tags: body.Tags,
		Recommendation: body.Recommendation,
		AuthorID:       body.Author,
		ImportedBy:     importer,
		Source: &knowledge.SOPSource{
			System: body.Source.System, URL: body.Source.URL, Version: body.Source.Version,
		},
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "import sop: "+err.Error())
		return
	}
	status := http.StatusOK
	outcome := "revised"
	if created {
		status = http.StatusCreated
		outcome = "created"
	}
	writeJSON(w, status, map[string]string{"id": id.String(), "outcome": outcome})
}

// SOPMarkdownBody is the workbench import payload (design/06 §2.4): a raw
// markdown file's bytes plus its name. The server parses frontmatter + body
// with the shared parser — the workbench never reimplements it, and richer
// formats are converted to markdown at the edge (pandoc) before arriving here.
type SOPMarkdownBody struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// importMarkdownSOP handles POST /api/sops/import_markdown (analyst): parse one
// markdown document and upsert it. The importer is the authenticated principal
// (recorded server-side, never client-supplied); the source pointer defaults to
// file:<filename> when the frontmatter omits it — the re-import lineage key, so
// clients send the full path as the filename (a bare basename would collide
// across directories). Only the basename feeds the fallback title.
func (b *Backend) importMarkdownSOP(w http.ResponseWriter, r *http.Request) {
	var body SOPMarkdownBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	name := body.Filename
	if name == "" {
		name = "untitled.md"
	}
	doc, err := sopdoc.Parse([]byte(body.Content), sopdoc.TitleStem(name))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parse sop document: "+err.Error())
		return
	}
	if doc.SourceURL == "" {
		doc.SourceSystem = "file"
		doc.SourceURL = "file:" + name
	}
	claims, _ := authz.FromContext(r.Context())
	importer := claims.PreferredUsername
	if importer == "" {
		importer = claims.Subject
	}
	id, created, err := b.cfg.Knowledge.Import(r.Context(), knowledge.SOP{
		TenantID: module.SingleTenantUUID,
		Title:    doc.Title, Body: doc.Body, Tags: doc.Tags,
		Recommendation: doc.Recommendation,
		AuthorID:       doc.Author,
		ImportedBy:     importer,
		Source: &knowledge.SOPSource{
			System: doc.SourceSystem, URL: doc.SourceURL, Version: doc.SourceVersion,
		},
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "import sop: "+err.Error())
		return
	}
	status, outcome := http.StatusOK, "revised"
	if created {
		status, outcome = http.StatusCreated, "created"
	}
	writeJSON(w, status, map[string]string{"id": id.String(), "outcome": outcome, "title": doc.Title})
}

func (b *Backend) createSOP(w http.ResponseWriter, r *http.Request) {
	var body SOPBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	claims, _ := authz.FromContext(r.Context())
	sop := knowledge.SOP{
		TenantID: module.SingleTenantUUID,
		Title:    body.Title, Body: body.Body, Tags: body.Tags,
		Recommendation: body.Recommendation,
		// Lightweight governance: published on write. author_id is set only when
		// the principal is a UUID (Keycloak sub); otherwise left NULL.
		Status:   knowledge.StatusPublished,
		AuthorID: uuidStringOrEmpty(claims.Subject),
	}
	id, err := b.cfg.Knowledge.Create(r.Context(), sop)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "create sop: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (b *Backend) listSOPs(w http.ResponseWriter, r *http.Request) {
	includeRetired := r.URL.Query().Get("include_retired") == "true"
	sops, err := b.cfg.Knowledge.List(r.Context(), module.SingleTenantUUID, includeRetired)
	if err != nil {
		writeInternalError(w, "list sops", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sops": sops})
}

func (b *Backend) getSOP(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	sop, err := b.cfg.Knowledge.Get(r.Context(), module.SingleTenantUUID, id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "sop not found")
		return
	}
	writeJSON(w, http.StatusOK, sop)
}

// getSOPSections handles GET /api/sops/{id}/sections: the procedure's outline,
// every section's place without its text (design/06 §4.2).
func (b *Backend) getSOPSections(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	revision, ok := revisionParam(w, r)
	if !ok {
		return
	}
	out, err := b.cfg.Knowledge.Sections(r.Context(), module.SingleTenantUUID, id, revision)
	if errors.Is(err, knowledge.ErrRevisionChanged) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, knowledge.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "sop not found")
		return
	}
	if err != nil {
		writeInternalError(w, "sop sections", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getSOPSection handles GET /api/sops/{id}/sections/{n}: one section whole,
// with the outline around it — the read behind the agent's read_sop tool.
func (b *Backend) getSOPSection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		writeJSONError(w, http.StatusBadRequest, "section index in path must be a positive integer")
		return
	}
	revision, ok := revisionParam(w, r)
	if !ok {
		return
	}
	out, err := b.cfg.Knowledge.Section(r.Context(), module.SingleTenantUUID, id, n, revision)
	if errors.Is(err, knowledge.ErrRevisionChanged) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, knowledge.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "sop or section not found")
		return
	}
	if err != nil {
		writeInternalError(w, "sop section", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (b *Backend) updateSOP(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var body SOPBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	err := b.cfg.Knowledge.Update(r.Context(), module.SingleTenantUUID, id, body.Title, body.Body, body.Tags, body.Recommendation)
	if errors.Is(err, knowledge.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "sop not found or retired")
		return
	}
	if err != nil {
		writeInternalError(w, "update sop", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *Backend) retireSOP(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	err := b.cfg.Knowledge.Retire(r.Context(), module.SingleTenantUUID, id)
	if errors.Is(err, knowledge.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "sop not found or already retired")
		return
	}
	if err != nil {
		writeInternalError(w, "retire sop", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uuidStringOrEmpty returns s if it parses as a UUID, else "" (the author_id
// column is UUID; a non-UUID principal is recorded as NULL for v0).
func uuidStringOrEmpty(s string) string {
	if _, err := uuid.Parse(s); err == nil {
		return s
	}
	return ""
}
