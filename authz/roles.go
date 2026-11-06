// Package authz holds reckon's JWT validation, claim extraction, and the
// two-axis authorization evaluation skeleton.
//
// Gate 1 (RBAC) — covered by middleware in this package.
// Gate 2 (action authorization, CEL-based) — the engine lives in package action.
//
// See reckon/design/05-component-architecture.md §5 for the architectural
// commitments this package implements.
package authz

// Role names registered in the bundled Keycloak `reckon` realm (see
// supervisor/keycloak_realm.json). The constants exist so callers
// reference role strings via a typed name rather than re-typing strings.
//
// Roles live in the IdP and travel in JWTs; reckon does not cache or mirror
// them for authorization, which reads the live token on every request. The
// record keeps, display only, what a token asserted at the moment of an act
// (adr/0105). See reckon/CLAUDE.md "Architectural commitments".
const (
	RoleViewer         = "viewer"
	RoleAnalyst        = "analyst"
	RoleApprover       = "approver"
	RoleSeniorApprover = "senior_approver"
	RolePolicyAuthor   = "policy_author"
	RolePolicySigner   = "policy_signer"
	RoleSOPAuthor      = "sop_author"
	RoleSOPSigner      = "sop_signer"
	RoleTenantAdmin    = "tenant_admin"
	RoleAuditor        = "auditor"
)

// AllRoles is the canonical role set. The supervisor's bootstrapped realm
// registers exactly these.
var AllRoles = []string{
	RoleViewer,
	RoleAnalyst,
	RoleApprover,
	RoleSeniorApprover,
	RolePolicyAuthor,
	RolePolicySigner,
	RoleSOPAuthor,
	RoleSOPSigner,
	RoleTenantAdmin,
	RoleAuditor,
}

// ReadRoles may read an investigation and its record. AnalystRoles may act on
// one. Each route names one of these, so a change to who may read is one edit.
var (
	ReadRoles    = []string{RoleViewer, RoleAnalyst, RoleAuditor}
	AnalystRoles = []string{RoleAnalyst}
)
