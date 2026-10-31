// Package server holds the HTTP server: router, auth middleware, the
// projection-delta WebSocket (handshake-authenticated, dropped on token
// expiry), and the dependency-probe gate that runs before traffic is accepted.
// The actual lifecycle (Start / Stop / Health) satisfies supervisor.Component
// so the supervisor manages it alongside the bundled subprocesses.
//
// See reckon/design/05-component-architecture.md §3 for the runtime topology.
package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/apiversion"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/supervisor"
	"github.com/sd-strax/reckon/temporal"
)

// BackendConfig configures the in-process reckon HTTP backend.
//
// Dependencies that travel as connection strings (Pg DSN, Temporal host:port,
// Keycloak issuer URL) are passed by value — the Backend depends on
// reachable services, not on the specific components that brought them up.
// This makes it cheap to repoint Backend at managed deps later (paid self-
// hosted at scale) without changing the supervisor seam.
//
// The Handler (aggregate command dispatch) is constructed by the caller
// and injected. Tests pass in a fixture-backed handler; production wires
// the real one.
type BackendConfig struct {
	// HTTPPort is the port the backend's HTTP server listens on. Default 8080.
	HTTPPort int

	// PgDSN is a libpq connection string for the aggregate's database
	// (typically the result of supervisor.Postgres.DSN("reckon_main")).
	PgDSN string

	// TemporalHostPort is host:port of the Temporal frontend gRPC.
	TemporalHostPort string

	// KeycloakIssuer is the OIDC issuer URL the realm is expected to publish.
	// At Start, Backend (1) fetches .well-known/openid-configuration and
	// asserts the issuer field matches — proves Keycloak is up AND the realm
	// exists — and (2) constructs an authz.Verifier from this URL.
	KeycloakIssuer string

	// KeycloakClientID is the audience the Verifier requires on every token
	// (adr/0220). Required; Start fails without it.
	KeycloakClientID string

	// KeycloakLoginClientID is the public OIDC client the interactive human
	// login flow uses (PKCE authorization-code). Advertised unauthenticated at
	// /api/auth-config so the workbench discovers the issuer + client without
	// hardcoding Keycloak's port/realm (design/13 §7 step 1). Empty leaves
	// /api/auth-config a clean 503 — the extension then reports auth
	// unconfigured rather than guessing.
	KeycloakLoginClientID string

	// KeycloakAgentClientID is the public OIDC client the AI-delegate token
	// path uses (the realm client that stamps delegate_kind issuer-side —
	// implementation/jwt-claims.md). Advertised alongside the login client so
	// the workbench's second PKCE flow discovers it rather than hardcoding the
	// "-agent" naming convention (implementation/agent-sidecar.md §5).
	KeycloakAgentClientID string

	// Handler is the aggregate command dispatcher used by /api routes. Must
	// be non-nil for any /api route to function (routes guard internally
	// with a 503 if absent so the server still serves /healthz cleanly when
	// the engine is being torn down).
	Handler *aggregate.Handler

	// Evidence persists what capability invocations return and opens cited
	// refs. Nil skips persistence, and evidence routes answer 503.
	Evidence *evidence.Store

	// Middleware, when non-nil, wraps the entire router — it carries the
	// telemetry tracing/metrics layer (telemetry.Provider.HTTPMiddleware).
	// Injected as a function so server stays decoupled from the telemetry
	// package. Nil means no wrapping.
	Middleware func(http.Handler) http.Handler

	// MetricsHandler, when non-nil, is served at /metrics (Prometheus text
	// exposition). Nil disables the endpoint.
	MetricsHandler http.Handler

	// CapabilityResolver and CapabilityCatalog, when both non-nil, enable the
	// /api/capabilities route (the read-side capability layer, Phase B). Nil
	// leaves the route returning 503 so the backend still serves without a
	// capability config. Read through Backend.capabilitySurface — the
	// enablement endpoint hot-swaps them after a config change.
	CapabilityResolver *capability.Resolver
	CapabilityCatalog  *capability.Catalog

	// CapabilityConfigPath + CapabilityFixtureRoot mirror the runtime config
	// the resolver was built from; both non-empty enables the /api/enablement
	// surface (11 §5.1) — the file is the single source of truth, the endpoint
	// is sugar that edits it and rebuilds the resolver in place.
	CapabilityConfigPath  string
	CapabilityFixtureRoot string

	// Demo mirrors config.Demo.Enabled: this install is the bundled demo
	// sandbox, so the FIXTURE adapter class is permitted. A hot reload
	// (the in-package build behind Reload) must carry it so fixtures can't be re-wired
	// against a real install after a config edit.
	Demo bool

	// Rebuild re-reads the tenant config from disk and builds both sides of
	// the no-restart reload (11 §5.1) through the FULL plugin path: the
	// capability resolver + catalog and the write-side action resolver
	// (adr/0223). It is injected by runtime because only runtime holds the
	// adapter host; the server calls it without importing the plugin layer. Nil
	// falls back to the in-package build (fixture adapters only), which is why
	// enabling a plugin adapter without it still needs a restart.
	Rebuild func() (ReloadedSurface, error)

	// Gate2 and ActionCatalog, when both non-nil, enable the POST /api/actions
	// (request_action) route (the write-side authorization path, Phase C). Nil
	// leaves it a 503. The Temporal dispatch client is opened at Start
	// regardless — it also serves the export pipeline.
	Gate2         *action.Gate2
	ActionCatalog *action.ActionCatalog
	// ActionResolver, with ActionCatalog, backs GET /api/action-types — the
	// agent's write-side catalog with per-type dispatchability. Nil leaves the
	// route a clean 503.
	ActionResolver *action.ActionResolver
	// ActionDispatch is the Temporal worker's dispatch holder. A reload swaps
	// it in step with the /action-types readout. Nil when the action layer is
	// off.
	ActionDispatch *action.AtomicResolver

	// Knowledge, when non-nil, enables the SOP corpus routes (/api/sops,
	// /api/knowledge/recall_sops — Phase C.5). Nil leaves them unregistered.
	Knowledge *knowledge.Store

	// Comms, when non-nil, enables the comms-thread routes (Phase F, binding
	// §4): GET /api/investigations/{id}/comms + /api/comms acts. Threads are
	// opened by the action-result path, never by these routes.
	Comms *comms.Store

	// TenantNamespace is this install's identity namespace UUID (03 §7.1),
	// stamped onto export bundles + their archive path (D.5).
	TenantNamespace string

	// ExportIncludeSideStores is the tenant policy for whether export bundles
	// carry the Layer B side stores (07 §2.2). Sourced from config, NOT the
	// export requester — a compliance deployment's redaction cannot be overridden
	// per-request.
	ExportIncludeSideStores bool

	// ExportAutoOnConclude fires the post-conclusion export automatically when an
	// investigation concludes (07 §2.3). When false, export is on-demand only.
	ExportAutoOnConclude bool

	// AllowAIVerdict is the AI-verdict trust dial (01 §Verdict; config
	// trust.ai_verdict). Default false: an AI-delegated verdict is refused
	// here AND at the aggregate (the command carries the enabling config ref
	// only when this is true — structural default-deny).
	AllowAIVerdict bool

	// AllowAIReasoning is the autonomous-reasoning trust dial (01 §Interpretation
	// types; config trust.ai_reasoning). Default false: an AI delegate cannot
	// record an evidential outcome on an unacknowledged (PROPOSED) hypothesis —
	// a human must Acknowledge it first. When true, the server stamps the
	// enabling ref so the aggregate permits full AI-driven hypothesis analysis.
	AllowAIReasoning bool

	// HypothesisFraming is the effective competing-hypotheses dial
	// (config.Reasoning.HypothesisFraming: "leading_null" default |
	// "leading_only"), surfaced to the agent on the investigation detail view
	// (01 §x-hypothesis "Competing hypotheses").
	HypothesisFraming string

	// CaseNoteOnSeed is the effective case write-back posture
	// (config.Cases.NoteOnSeed: "confirm" default | "auto" | "off";
	// adr/0032-case-note.md), surfaced on create/detail so the client knows
	// whether to ask, act, or stay silent; "off" is enforced at the endpoint.
	CaseNoteOnSeed string
}

// Backend is the in-process HTTP server.
type Backend struct {
	cfg BackendConfig
	sup *supervisor.Supervisor

	// reloadMu serialises reloads (adr/0223). The enablement endpoint holds it
	// across its file write too, so two applies cannot lose each other's edit.
	reloadMu sync.Mutex

	mu      sync.Mutex
	srv     *http.Server
	started bool
	// serveErr is why the HTTP server stopped serving on its own; Health
	// reports it so the supervisor restarts the backend.
	serveErr error
	verifier *authz.Verifier

	// dispatchClient is the shared Temporal client: it starts ActionLifecycle/
	// ReversalSaga on approvals AND the PostConclusionPipeline for exports.
	// Opened at Start (Temporal is a probed dependency), independent of whether
	// the action layer is configured — the export surface must work without it.
	dispatchClient *temporal.Client

	// pipelineOverride is a test-only injection point for the export endpoint's
	// pipeline starter; nil in production (the live dispatchClient is used).
	pipelineOverride pipelineStarter

	// actionStarterOverride is the same test-only seam for the action
	// workflows (approval, resolution, stranded-action sweep).
	actionStarterOverride actionStarter

	// stopSweeps ends the action sweeps started at Start.
	stopSweeps context.CancelFunc

	// capOverride, when set (under mu), supersedes cfg.CapabilityResolver/
	// Catalog — the hot swap a reload installs.
	capOverride *capabilitySurface

	// actionOverride, when set (under mu), supersedes cfg.ActionResolver for the
	// /action-types readout — the write-side reload swap (11 §5.1).
	actionOverride *action.ActionResolver

	// identityOnce/identityRes memoize the tenant identity resolver used to
	// mint STIX ids for entity seeds (createInvestigation). Built lazily from
	// cfg.TenantNamespace; nil when the namespace is unset/invalid, which
	// leaves seed_input entity resolution a clean 400 rather than a panic.
	identityOnce sync.Once
	identityRes  *identity.Resolver
}

// identityResolver returns the memoized tenant identity resolver, or nil when
// cfg.TenantNamespace is empty or not a UUID.
func (b *Backend) identityResolver() *identity.Resolver {
	b.identityOnce.Do(func() {
		ns, err := uuid.Parse(b.cfg.TenantNamespace)
		if err != nil {
			return
		}
		b.identityRes = identity.NewResolver(ns)
	})
	return b.identityRes
}

// NewBackend constructs the Backend (not yet started). The supervisor
// reference lets /status roll up every registered component including
// Backend itself.
func NewBackend(cfg BackendConfig, sup *supervisor.Supervisor) *Backend {
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	return &Backend{cfg: cfg, sup: sup}
}

// Name returns "backend" — same identifier the supervisor knew it by when
// Backend lived under supervisor/. Keeps existing /status output stable.
func (b *Backend) Name() string { return "backend" }

// Start probes every dependency, constructs the authz.Verifier (which does
// OIDC discovery and so requires Keycloak to be reachable), wires the route
// table, and brings the HTTP listener up.
func (b *Backend) Start(ctx context.Context) error {
	if err := b.probePostgres(ctx); err != nil {
		return fmt.Errorf("postgres unreachable: %w", err)
	}
	if err := b.probeTemporal(ctx); err != nil {
		return fmt.Errorf("temporal unreachable: %w", err)
	}
	if err := b.probeKeycloak(ctx); err != nil {
		return fmt.Errorf("keycloak unreachable: %w", err)
	}

	verifier, err := authz.NewVerifier(ctx, authz.VerifierConfig{
		Issuer:   b.cfg.KeycloakIssuer,
		ClientID: b.cfg.KeycloakClientID,
	})
	if err != nil {
		return fmt.Errorf("init verifier: %w", err)
	}

	// Open the dispatch client unconditionally (Temporal is already probed
	// reachable above). It serves the action path (ActionLifecycle on approval)
	// AND the export path (PostConclusionPipeline) — tying it to the action
	// layer would silently disable auto-export on deployments that never
	// configure write actions (07 §2.3 defaults auto-export ON).
	dispatchClient, err := temporal.NewClientContext(ctx, temporal.ClientConfig{HostPort: b.cfg.TemporalHostPort})
	if err != nil {
		return fmt.Errorf("start temporal dispatch client: %w", err)
	}

	mux := b.buildRouter(verifier)

	addr := fmt.Sprintf("localhost:%d", b.cfg.HTTPPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	b.mu.Lock()
	b.verifier = verifier
	b.dispatchClient = dispatchClient
	b.srv = newHTTPServer(addr, mux)
	b.started = true
	b.serveErr = nil
	srv := b.srv
	b.mu.Unlock()

	go b.serve(srv, listener)

	// Action reconciliation: every pending action gets its durable expiry
	// timer, and every stranded action its workflow (adr/0216), at startup and
	// then on a tick. Idempotent per action; background — never blocks startup.
	if b.cfg.Handler != nil {
		// Deliberately not derived from Start's ctx, which is a startup
		// deadline: the sweeps run until Stop.
		sweepCtx, cancel := context.WithCancel(context.Background())
		b.mu.Lock()
		b.stopSweeps = cancel
		b.mu.Unlock()
		go b.runActionSweeps(sweepCtx)
	}

	return nil
}

// serve runs srv until it stops. A stop that is not Shutdown is recorded for
// Health, so the supervisor sees a backend that no longer serves.
func (b *Backend) serve(srv *http.Server, listener net.Listener) {
	err := srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return
	}
	log.Printf("backend http server: %v", err)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.srv == srv { // not a server a later Start replaced
		b.serveErr = fmt.Errorf("http server exited: %w", err)
	}
}

// Stop gracefully shuts the HTTP server down within ctx's deadline, and ends
// the action sweeps and the dispatch client.
func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	srv := b.srv
	dispatchClient := b.dispatchClient
	stopSweeps := b.stopSweeps
	b.srv = nil
	b.started = false
	b.verifier = nil
	b.dispatchClient = nil
	b.stopSweeps = nil
	b.mu.Unlock()

	if stopSweeps != nil {
		stopSweeps()
	}

	if dispatchClient != nil {
		dispatchClient.Close()
	}
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// getDispatchClient returns the shared Temporal client under lock (nil until
// Start has run).
func (b *Backend) getDispatchClient() *temporal.Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dispatchClient
}

// Health reports whether the HTTP server is up. The rich "are deps healthy"
// view is at /status (which calls into the supervisor's Health rollup).
func (b *Backend) Health(_ context.Context) HealthStatus {
	b.mu.Lock()
	started, serveErr := b.started, b.serveErr
	b.mu.Unlock()
	if !started {
		return HealthStatus{Ready: false, Message: "not started"}
	}
	if serveErr != nil {
		return HealthStatus{Ready: false, Message: serveErr.Error()}
	}
	return HealthStatus{Ready: true, Message: fmt.Sprintf("http :%d", b.cfg.HTTPPort)}
}

// HealthStatus mirrors supervisor.HealthStatus so server can expose its own
// Health method that satisfies supervisor.Component without a forward
// dependency.
type HealthStatus = supervisor.HealthStatus

// --- routing -----------------------------------------------------------------

func (b *Backend) buildRouter(verifier *authz.Verifier) http.Handler {
	mux := http.NewServeMux()

	// Public — used by the supervisor + reckon status command.
	mux.HandleFunc("/healthz", b.handleHealthz)
	mux.HandleFunc("/status", b.handleStatus)

	// Public — Prometheus scrape endpoint. Unauthenticated in the bundled/local
	// shape (scraped over loopback); a paid/SaaS deployment fronts it with
	// network policy or an auth proxy.
	if b.cfg.MetricsHandler != nil {
		mux.Handle("/metrics", b.cfg.MetricsHandler)
	}

	// API — auth required except where noted.
	api := http.NewServeMux()

	// GET /api/auth-config — PUBLIC (must be reachable before login): the OIDC
	// issuer + public login client the workbench needs to run its PKCE flow
	// (design/13 §7). Not auth-wrapped. Clean 503 when login isn't configured.
	api.HandleFunc("/auth-config", b.handleAuthConfig)

	// GET /api/me — who am I; any authenticated principal.
	api.Handle("/me", authz.RequireAuth(verifier)(http.HandlerFunc(b.handleMe)))

	// /investigations — viewer for read, analyst for write.
	api.Handle("/investigations", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.investigationsCollection),
	))
	api.Handle("/investigations/", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.investigationsItem),
	))

	// GET /api/capabilities — list_capabilities (§2.8): the verbs resolvable in
	// this tenant and their availability. Any authenticated reader.
	api.Handle("/capabilities", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.listCapabilities),
	))

	// GET /api/cases, GET /api/cases/{number} — the case queue and one case,
	// read-only and non-persisting (adr/0030-cases-pane.md, 14 §4.2). Any
	// authenticated reader.
	api.Handle("/cases", authz.RequireAuth(verifier)(http.HandlerFunc(b.casesRoute)))
	api.Handle("/cases/", authz.RequireAuth(verifier)(http.HandlerFunc(b.casesRoute)))

	// POST /api/capability/{verb} — verb invocation (03 §3.4, Phase E.1): the
	// agent loop's read-tool dispatch target. Analyst role.
	api.Handle("/capability/", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.capabilityInvokeRoute),
	))

	// /api/enablement — the operator's installed/enabled surface + the
	// human-confirmed apply path (11 §5.1). The tenant config FILE stays the
	// authority; these routes are sugar that reads/edits it. Analyst role;
	// the POST additionally refuses AI-delegated tokens inside the handler.
	api.Handle("/enablement", authz.RequireAuth(verifier)(http.HandlerFunc(b.enablementRoute)))
	api.Handle("/enablement/", authz.RequireAuth(verifier)(http.HandlerFunc(b.enablementRoute)))

	// GET /api/action-types — list_action_types (08 §3): the write-side catalog
	// (action_type, intent, tier, reversibility, D3FEND) with per-type
	// dispatchability, so the agent requests real action types instead of
	// guessing. Any authenticated reader.
	api.Handle("/action-types", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.listActionTypes),
	))

	// POST /api/actions — request_action (08 §2): propose a state-changing
	// action; runs Gate 2 and (on auto-approval) triggers dispatch. Analyst role.
	api.Handle("/actions", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.actionsCollection),
	))

	// POST /api/actions/{id}/approve|reject — the manual approval surface
	// (04 §5, Phase D): honors REQUIRE_TWO_PARTY, triggers dispatch on final
	// approval. Analyst role.
	api.Handle("/actions/", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.actionsItem),
	))

	// POST /api/interpretations — the agent loop's write into the reasoning
	// thread (05 §3.4): record one reasoning act + its transcript/tool-call side
	// store. Analyst role (the AI authors as a delegate, never a principal).
	api.Handle("/interpretations", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.interpretationsCollection),
	))

	// POST /api/interpretations/{id}/supersede — retraction/un-pin
	// (01 §Interpretation lifecycle and correction). Analyst role.
	api.Handle("/interpretations/", authz.RequireAuth(verifier)(
		http.HandlerFunc(b.interpretationsItem),
	))

	// GET /api/entities/{ref}/appearances — cross-investigation memory
	// (binding §6.1): every investigation citing the ref. Any reader.
	api.Handle("/entities/", authz.RequireAuth(verifier)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, "GET")
				return
			}
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listRefAppearances)
		},
	)))

	// GET /api/evidence/{ref} — citation-open (design/ui 02 §2.8): any cited
	// STIX object or raw OCSF telemetry record, read-only. Any reader.
	api.Handle("/evidence/", authz.RequireAuth(verifier)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, "GET")
				return
			}
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.getEvidence)
		},
	)))

	// Comms acts (Phase F, binding §4): inbound-reply ingestion + the
	// analyst's acknowledge/done/snooze on a thread.
	api.Handle("/comms/", authz.RequireAuth(verifier)(http.HandlerFunc(b.commsRoute)))

	// Knowledge service (Phase C.5): SOP corpus CRUD + keyword retrieval, plus
	// the K4 similar-investigation similarity recall over the case-summaries
	// corpus.
	if b.cfg.Knowledge != nil {
		api.Handle("/knowledge/recall_sops", authz.RequireAuth(verifier)(http.HandlerFunc(b.recallSOPs)))
		api.Handle("/knowledge/recall_similar_investigations", authz.RequireAuth(verifier)(http.HandlerFunc(b.recallSimilar)))
		api.Handle("/knowledge/prior_investigations", authz.RequireAuth(verifier)(http.HandlerFunc(b.priorInvestigations)))
		api.Handle("/knowledge/summary_narrative", authz.RequireAuth(verifier)(http.HandlerFunc(b.summaryNarrative)))
		api.Handle("/knowledge/case_summaries", authz.RequireAuth(verifier)(http.HandlerFunc(b.listCaseSummaries)))
		api.Handle("/knowledge/case_summaries/", authz.RequireAuth(verifier)(http.HandlerFunc(b.getCaseSummary)))
		api.Handle("/sops", authz.RequireAuth(verifier)(http.HandlerFunc(b.sopsCollection)))
		// Exact patterns beat the /sops/ prefix, so "import" never parses as an id.
		api.Handle("/sops/import", authz.RequireAuth(verifier)(http.HandlerFunc(b.importSOP)))
		api.Handle("/sops/import_markdown", authz.RequireAuth(verifier)(http.HandlerFunc(b.importMarkdownSOP)))
		api.Handle("/sops/", authz.RequireAuth(verifier)(http.HandlerFunc(b.sopsItem)))
		// `reckon demo seed` loads the embedded demo knowledge pack; virginity is
		// enforced in the handler so it can never contaminate a working install.
		api.Handle("/admin/demo/seed", authz.RequireAuth(verifier)(http.HandlerFunc(b.seedDemoKnowledge)))
	}

	mux.Handle("/api/", http.StripPrefix("/api", api))

	if b.cfg.Middleware != nil {
		return b.cfg.Middleware(mux)
	}
	return mux
}

// --- handlers ---------------------------------------------------------------

func (b *Backend) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprintln(w, "ok")
}

// APIVersion is the backend's HTTP API contract version (see internal/apiversion).
const APIVersion = apiversion.Current

// StatusResponse is the JSON shape served by /status and consumed by
// `reckon status` and the workbench version handshake (design/13 §2).
type StatusResponse struct {
	Overall    string                     `json:"overall"`
	Components map[string]ComponentStatus `json:"components"`
	// APIVersion is the contract version (see APIVersion). The workbench
	// asserts it is in its supported set and fails closed with a diagnostic on
	// mismatch rather than dispatching against an incompatible surface.
	APIVersion int `json:"api_version"`
}

// ComponentStatus is the per-component slice of StatusResponse.
type ComponentStatus struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

func (b *Backend) handleStatus(w http.ResponseWriter, r *http.Request) {
	rollup := b.sup.Health(r.Context())
	resp := StatusResponse{
		Overall:    "ok",
		Components: make(map[string]ComponentStatus, len(rollup)),
		APIVersion: APIVersion,
	}
	for name, h := range rollup {
		resp.Components[name] = ComponentStatus{Ready: h.Ready, Message: h.Message}
		if !h.Ready {
			resp.Overall = "degraded"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if resp.Overall != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// AuthConfigResponse is the public shape of /api/auth-config: everything the
// workbench needs to start an OIDC PKCE login, sourced from the backend so the
// extension never hardcodes (or drifts from) Keycloak's port/realm/client.
type AuthConfigResponse struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	// AgentClientID is the delegate-path client (stamps delegate_kind). The
	// workbench runs its silent second PKCE flow against it; omitted when the
	// deployment did not configure one.
	AgentClientID string `json:"agent_client_id,omitempty"`
}

// handleAuthConfig serves the login config unauthenticated (it is a
// prerequisite of authenticating). A 503 when login isn't configured keeps the
// contract honest: the extension reports "auth unconfigured" rather than
// guessing an issuer.
func (b *Backend) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	if b.cfg.KeycloakLoginClientID == "" || b.cfg.KeycloakIssuer == "" {
		writeJSONError(w, http.StatusServiceUnavailable, "interactive login not configured")
		return
	}
	writeJSON(w, http.StatusOK, AuthConfigResponse{
		Issuer:        b.cfg.KeycloakIssuer,
		ClientID:      b.cfg.KeycloakLoginClientID,
		AgentClientID: b.cfg.KeycloakAgentClientID,
	})
}

// MeResponse is the public shape of /api/me.
type MeResponse struct {
	Subject           string   `json:"subject"`
	PreferredUsername string   `json:"preferred_username"`
	TenantID          string   `json:"tenant_id"`
	Roles             []string `json:"roles"`
	DelegateKind      string   `json:"delegate_kind,omitempty"`
}

func (b *Backend) handleMe(w http.ResponseWriter, r *http.Request) {
	c, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	writeJSON(w, http.StatusOK, MeResponse{
		Subject:           c.Subject,
		PreferredUsername: c.PreferredUsername,
		TenantID:          c.TenantID,
		Roles:             c.Roles,
		DelegateKind:      c.DelegateKind,
	})
}

// /investigations handles both GET (list) and POST (create) on the
// collection. Method-level role requirements differ.
func (b *Backend) investigationsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigations)
	case http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.createInvestigation)
	default:
		methodNotAllowed(w, "GET, POST")
	}
}

// /investigations/{id} handles GET (load one); the /hypotheses sub-resource
// lists the investigation's reasoning nodes (D.2); the /actions sub-resource
// lists its x-actions (the pending-approval queue + audit list); the /thread
// sub-resource lists the chronological reasoning thread (13 §4); the /export
// sub-resource triggers the post-conclusion export bundle (D.5); the /lifecycle
// sub-resource drives the state machine (activate/pause/resume/conclude/reopen/
// archive, D.6); the /rename sub-resource changes the title.
func (b *Backend) investigationsItem(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasSuffix(trimmed, "/hypotheses") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigationHypotheses)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/actions") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigationActions)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/thread") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigationThread)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/pins") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigationPins)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/comms") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.listInvestigationComms)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/export") {
		switch r.Method {
		case http.MethodPost:
			b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.exportInvestigation)
		default:
			methodNotAllowed(w, "POST")
		}
		return
	}
	// The live markdown projection (binding §6 item 10): any-time, any reader —
	// distinct from POST /export, which builds the signed post-conclusion bundle.
	if strings.HasSuffix(trimmed, "/export.md") {
		switch r.Method {
		case http.MethodGet:
			b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.getInvestigationMarkdown)
		default:
			methodNotAllowed(w, "GET")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/lifecycle") {
		switch r.Method {
		case http.MethodPost:
			b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.investigationLifecycle)
		default:
			methodNotAllowed(w, "POST")
		}
		return
	}
	if strings.HasSuffix(trimmed, "/rename") {
		switch r.Method {
		case http.MethodPost:
			b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.renameInvestigation)
		default:
			methodNotAllowed(w, "POST")
		}
		return
	}
	// POST /api/investigations/{id}/case_note — request the "opened in reckon"
	// note on the seed case as a ticket.comment action (adr/0032). Analyst.
	if strings.HasSuffix(trimmed, "/case_note") {
		switch r.Method {
		case http.MethodPost:
			b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.requestCaseNote)
		default:
			methodNotAllowed(w, "POST")
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, b.getInvestigation)
	default:
		methodNotAllowed(w, "GET")
	}
}

// investigationSubresourceID parses the id out of `/investigations/{id}/<sub>`
// (the /api prefix is already stripped). The single parser for every
// investigation sub-resource route, so path handling cannot drift per-suffix.
func investigationSubresourceID(p, sub string) (uuid.UUID, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 3 || parts[0] != "investigations" || parts[2] != sub {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.UUID{}, false
	}
	return id, true
}

// requireRolesOrDeny is an inline equivalent to authz.RequireRole, used by
// handlers that vary their role requirement per-method. Behavior matches:
// 403 with required_roles surfaced, 500 if claims context missing.
func (b *Backend) requireRolesOrDeny(w http.ResponseWriter, r *http.Request, roles []string, next http.HandlerFunc) {
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth wiring error")
		return
	}
	if !claims.HasAnyRole(roles...) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":          "required role(s) missing",
			"required_roles": roles,
			"required_any":   true,
		})
		return
	}
	next(w, r)
}

// listCapabilities serves GET /api/capabilities. Returns 503 when the capability
// layer is not configured (no tenant capability YAML); otherwise the verb
// summaries with their availability. Read requires viewer/analyst/auditor.
func (b *Backend) listCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	resolver, catalog := b.capabilitySurface()
	if resolver == nil || catalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "capability layer not configured")
		return
	}
	b.requireRolesOrDeny(w, r, []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}, func(w http.ResponseWriter, _ *http.Request) {
		summaries := resolver.ListCapabilities(catalog)
		body := map[string]any{"capabilities": summaries}
		// The tenant's case queue (14 §4.2) rides the catalog response: it is
		// the SoR surface's one piece of standing config the picker needs.
		if q := resolver.PolicyString(capability.PolicyCaseQueue); q != "" {
			body["case_queue"] = q
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}

// capabilitySurface returns the live resolver + catalog: the hot-swapped pair
// when the enablement endpoint has rebuilt them, else the boot-time pair.
func (b *Backend) capabilitySurface() (*capability.Resolver, *capability.Catalog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.capOverride != nil {
		return b.capOverride.resolver, b.capOverride.catalog
	}
	return b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog
}

// capabilitySurface bundles the pair so the override is one pointer swap.
type capabilitySurface struct {
	resolver *capability.Resolver
	catalog  *capability.Catalog
}

// actionResolver returns the live write-side resolver for the /action-types
// availability readout: the hot-swapped one when a reload installed it, else
// the boot-time one. (The dispatch path itself lives in the Temporal worker,
// swapped in lockstep — this override only governs the availability display.)
func (b *Backend) actionResolver() *action.ActionResolver {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.actionOverride != nil {
		return b.actionOverride
	}
	return b.cfg.ActionResolver
}

// ReloadedSurface is what one reload builds from the tenant config: both
// sides, applied together or not at all.
type ReloadedSurface struct {
	CapabilityResolver *capability.Resolver
	CapabilityCatalog  *capability.Catalog
	// ActionResolver is nil when the action layer is off.
	ActionResolver *action.ActionResolver
}

// Reload rebuilds both sides from the tenant config on disk and swaps them in:
// the capability surface, the /action-types readout and the worker's dispatch
// holder. It is the one reload routine, called by SIGHUP and the enablement
// endpoint (adr/0223). A build failure on either side swaps nothing, so a
// broken edit degrades to "the change didn't take" and the process never runs
// half a file. In-flight requests keep the resolvers they already read (a
// resolver is immutable once built).
func (b *Backend) Reload() error {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	return b.reloadLocked()
}

// reloadLocked is Reload for a caller already holding reloadMu.
func (b *Backend) reloadLocked() error {
	rebuild := b.cfg.Rebuild
	if rebuild == nil {
		if b.cfg.CapabilityConfigPath == "" {
			return nil // nothing to reload from
		}
		rebuild = b.buildInPackage
	}
	s, err := rebuild()
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	b.mu.Lock()
	b.capOverride = &capabilitySurface{resolver: s.CapabilityResolver, catalog: s.CapabilityCatalog}
	if s.ActionResolver != nil {
		// Dispatch first, then the readout: /action-types never advertises a
		// binding the worker cannot yet dispatch through.
		if b.cfg.ActionDispatch != nil {
			b.cfg.ActionDispatch.Store(s.ActionResolver)
		}
		b.actionOverride = s.ActionResolver
	}
	b.mu.Unlock()
	return nil
}

// --- dependency probes -------------------------------------------------------

func (b *Backend) probePostgres(ctx context.Context) error {
	db, err := sql.Open("postgres", b.cfg.PgDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(pingCtx)
}

func (b *Backend) probeTemporal(ctx context.Context) error {
	c, err := temporal.Dial(ctx, b.cfg.TemporalHostPort, "")
	if err != nil {
		return err
	}
	defer c.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = c.CheckHealth(pingCtx, &client.CheckHealthRequest{})
	return err
}

func (b *Backend) probeKeycloak(ctx context.Context) error {
	url := strings.TrimRight(b.cfg.KeycloakIssuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d on %s", resp.StatusCode, url)
	}
	var disc struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return fmt.Errorf("parse OIDC discovery: %w", err)
	}
	if disc.Issuer != b.cfg.KeycloakIssuer {
		return fmt.Errorf("issuer mismatch: discovery says %q, expected %q",
			disc.Issuer, b.cfg.KeycloakIssuer)
	}
	return nil
}

// Server timeouts (adr/0219). ReadTimeout covers the whole request, so the
// largest body (a turn's transcript, maxInterpretationBodyBytes) has room.
// WriteTimeout runs from the end of the headers to the end of the response; it
// sits above the agent client's 60s so a slow capability call is ended by the
// client, not cut mid-response by the server. The WebSocket stream is not
// affected: net/http clears the connection's deadlines when it is hijacked.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 90 * time.Second
	idleTimeout       = 120 * time.Second
)

// newHTTPServer is the backend's listener configuration.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// writeJSON writes a JSON response with the given status. Every handler's JSON
// response goes through here (or writeJSONError) so the Content-Type is set
// uniformly — a bare Encode after WriteHeader gets content-sniffed to
// text/plain by net/http.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a stable error JSON shape for all non-2xx responses.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// writeInternalError answers a server-side failure with a 500 that names what
// failed but not why (adr/0220): the error text can carry table, constraint
// and connection details the client has no use for. The cause is logged with a
// short reference, and the same reference rides the response so a reported
// failure can be found in the log.
func writeInternalError(w http.ResponseWriter, what string, err error) {
	ref := newErrorRef()
	slog.Error("internal error", "what", what, "ref", ref, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": what + ": internal error (ref " + ref + ")",
		"ref":   ref,
	})
}

// newErrorRef returns a short random reference for one internal error.
func newErrorRef() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// methodNotAllowed writes the 405 + Allow-header pair every route uses for an
// unsupported method.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// writeCommandError maps an aggregate.Handler.Handle failure onto the HTTP
// outcome for every /api write path, so a domain rejection, a permission
// denial, a lost OCC race, and an infrastructure fault are never conflated.
// Order matters: the sentinels (which arrive wrapped in a *RejectedError) are
// matched before the generic rejection, and infrastructure failures — which
// Handle returns unwrapped — fall through to 500. The action label prefixes the
// message ("conclude", "request action", …). A nil err is a caller bug, treated
// as 500.
func writeCommandError(w http.ResponseWriter, action string, err error) {
	var rejected *aggregate.RejectedError
	switch {
	case errors.Is(err, aggregate.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, action+": investigation not found")
	case errors.Is(err, aggregate.ErrAIDenied):
		writeJSONError(w, http.StatusForbidden, action+": "+err.Error())
	case errors.Is(err, aggregate.ErrConcurrent):
		writeJSONError(w, http.StatusConflict, action+": "+err.Error())
	case errors.As(err, &rejected):
		writeJSONError(w, http.StatusUnprocessableEntity, action+": "+err.Error())
	default:
		writeInternalError(w, action, err)
	}
}
