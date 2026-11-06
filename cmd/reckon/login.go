package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sd-strax/reckon/agent"
	"github.com/sd-strax/reckon/config"
	"github.com/sd-strax/reckon/internal/branding"
)

// localBackendURL is the running backend on this machine, the one every CLI
// client command talks to.
func localBackendURL(cfg config.Config) string {
	return fmt.Sprintf("http://localhost:%d", cfg.Backend.HTTPPort)
}

// backendLogin signs in through dev-auth against the local realm as clientID
// (the human client, or the agent client the investigate loop also needs). It
// is the one login path for every CLI command that calls the backend; an
// empty password is the dev-auth default, the same the eval harness uses, and
// the hint names the step a fresh install is missing.
func backendLogin(ctx context.Context, cfg config.Config, clientID, user, pass string) (*agent.Credential, error) {
	if pass == "" {
		pass = branding.CLI
	}
	issuer := fmt.Sprintf("http://localhost:%d/realms/%s", cfg.Keycloak.HTTPPort, cfg.Keycloak.Realm)
	cred, err := agent.NewCredential(ctx, issuer, clientID, user, pass)
	if err != nil {
		return nil, fmt.Errorf("login (%s): %w\n  hint: the shipped realm has no login user and ROPC is off — run `%s dev-auth` first", clientID, err, branding.CLI)
	}
	return cred, nil
}

// envOr reads an environment variable with a default.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
