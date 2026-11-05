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
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
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
	Handler commandHandler

	// Reads is the aggregate's read side. Handlers take a non-nil Handler to
	// mean the aggregate is there, so the two are set together; NewBackend
	// refuses one without the other.
	Reads readModel

	// Evidence persists what capability invocations return and opens cited
	// refs. Nil skips persistence, and evidence routes answer 503.
	Evidence evidenceStore

	// Middleware, when non-nil, wraps the entire router — it carries the
	// telemetry tracing/metrics layer (telemetry.Provider.HTTPMiddleware).
	// Injected as a function so server stays decoupled from the telemetry
	// package. Nil means no wrapping.
	Middleware func(http.Handler) http.Handler

	// MetricsHandler, when non-nil, is served at /metrics (Prometheus text
	// exposition). Nil disables the endpoint.
	MetricsHandler http.Handler

	// CapabilityResolver and CapabilityCatalog, when both non-nil, enable the
	// /api/capabilities route (the read-side capability layer). Nil
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
	// (request_action) route (the write-side authorization path). Nil
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
	// /api/knowledge/recall_sops). Nil leaves them unregistered.
	Knowledge knowledgeStore

	// Comms, when non-nil, enables the comms-thread routes (binding §4): GET /api/investigations/{id}/comms + /api/comms acts. Threads are
	// opened by the action-result path, never by these routes.
	Comms commsStore

	// TenantNamespace is this install's identity namespace UUID (03 §7.1),
	// stamped onto export bundles + their archive path.
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
// Backend itself. Handler without Reads, or Reads without Handler, is a wiring
// mistake, so it stops the process here rather than on a request or in a sweep.
func NewBackend(cfg BackendConfig, sup *supervisor.Supervisor) *Backend {
	if (cfg.Handler == nil) != (cfg.Reads == nil) {
		panic("server: BackendConfig.Handler and Reads are set together")
	}
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

	// Knowledge reconciliation: the corpus is brought up to date with this
	// configuration's section rule and embedding model before any recall can
	// run (00-substrate §10). A current corpus costs one scan. An embedder
	// that cannot answer fails startup here, where an unreachable one would
	// otherwise fail the first recall.
	if b.cfg.Knowledge != nil {
		n, err := b.cfg.Knowledge.Reindex(ctx, module.SingleTenantUUID)
		if err != nil {
			return fmt.Errorf("reconcile knowledge index: %w", err)
		}
		if n > 0 {
			log.Printf("knowledge index reconciled: %d entries updated", n)
		}
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

// HealthStatus is supervisor.HealthStatus under this package's name, the return
// type of the Health method that satisfies supervisor.Component.
type HealthStatus = supervisor.HealthStatus

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
