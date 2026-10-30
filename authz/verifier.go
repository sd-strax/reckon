package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifier validates Keycloak-issued JWTs against the configured issuer's
// JWKS and extracts reckon's Claims from them.
//
// Construction discovers the issuer's JWKS via the standard OIDC discovery
// URL (issuer + /.well-known/openid-configuration), so the supervisor's
// Keycloak must be reachable at construction time. Once constructed, the
// Verifier caches keys and refreshes them lazily on signature failures.
type Verifier struct {
	verifier *oidc.IDTokenVerifier
	clientID string
}

// VerifierConfig configures the Verifier.
type VerifierConfig struct {
	// Issuer is the full OIDC issuer URL. For the bundled deployment this
	// comes from supervisor.Keycloak.IssuerURL() — e.g.,
	// "http://localhost:8543/realms/reckon".
	Issuer string

	// ClientID is the audience every accepted token must carry in its aud
	// claim. Required: the audience check is never skipped (adr/0220). For
	// the bundled deployment this is "reckon".
	ClientID string
}

// NewVerifier discovers the issuer's JWKS and returns a Verifier ready to
// validate tokens.
func NewVerifier(ctx context.Context, cfg VerifierConfig) (*Verifier, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("authz: issuer URL is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("authz: client id (the required token audience) is required")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	oidcConfig := &oidc.Config{ClientID: cfg.ClientID}
	return &Verifier{
		verifier: provider.Verifier(oidcConfig),
		clientID: cfg.ClientID,
	}, nil
}

// Verify validates the raw JWT string and returns the extracted Claims.
// Returns an error if the signature is invalid, the token is expired, or
// any required claim is missing.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	if v == nil {
		return Claims{}, errors.New("authz: nil verifier")
	}
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify token: %w", err)
	}
	var raw rawClaims
	if err := idToken.Claims(&raw); err != nil {
		return Claims{}, fmt.Errorf("decode claims: %w", err)
	}
	c := raw.toClaims(v.clientID)
	if c.Subject == "" {
		return Claims{}, errors.New("authz: token missing `sub` claim")
	}
	return c, nil
}
