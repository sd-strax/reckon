package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/internal/apiversion"
	"github.com/sd-strax/reckon/temporal"
)

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
