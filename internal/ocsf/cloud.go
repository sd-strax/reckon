package ocsf

import "strings"

// ProviderSlug maps an OCSF cloud.provider to the slug a cloud principal's
// account_type and a cloud account's provider carry (03 §7.2). The provider is
// the principal's namespace, deliberately not "cloud" (the IdP sign-in
// account): an ARN and an IdP user id never share one. A provider the table
// does not know is kept, lowercased.
func ProviderSlug(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "aws", "amazon web services":
		return "aws"
	case "azure", "microsoft azure":
		return "azure"
	case "gcp", "google cloud", "google cloud platform":
		return "gcp"
	}
	return p
}

// SessionScopedActors names, per provider slug, the field of an actor that
// holds the durable principal behind a session-scoped actor id. The rule is
// "the provider's durable principal identifier". A row exists where a
// provider's actor id is session-scoped: an AWS assumed-role ARN names the
// session, and the session's issuer names the role. Another such provider is
// a row, not a branch.
var SessionScopedActors = map[string]string{
	"aws": "session.issuer",
}

// PrincipalContext is what a principal form needs to know about the finding
// beside the user's id.
type PrincipalContext struct {
	// AccountUID is the finding's own cloud.account.uid, "" for none.
	AccountUID string
	// ActorAccountUID is the actor's own user.account.uid, "" for none.
	// IsCloudPrincipal fills it from the actor.
	ActorAccountUID string
}

// PrincipalForms says, per provider slug, whether a user id has the form of
// that provider's principal id on a finding. Azure and GCP have no row:
// their link rests on the account type (adr/0359). A new provider adds a
// row.
var PrincipalForms = map[string]func(uid string, finding PrincipalContext) bool{
	"aws": awsPrincipalForm,
}

// awsPrincipalForm reports whether uid has the form of an AWS principal id:
//
//   - an ARN in an AWS partition: "arn:", then "aws" or "aws-" and lowercase
//     letters in one or more hyphenated parts (aws-cn, aws-us-gov, aws-iso-b),
//     then ":";
//   - an IAM unique id: AIDA (a user) or AROA (a role) followed by at least
//     16 capital letters or digits;
//   - an account's root principal: a 12-digit id equal to the finding's own
//     account or to the actor's own account. Any other account id is not a
//     principal.
func awsPrincipalForm(uid string, finding PrincipalContext) bool {
	if rest, ok := strings.CutPrefix(uid, "arn:"); ok {
		partition, _, ok := strings.Cut(rest, ":")
		return ok && awsPartition(partition)
	}
	if len(uid) == 12 && allDigits(uid) && (uid == finding.AccountUID || uid == finding.ActorAccountUID) {
		return true
	}
	if !strings.HasPrefix(uid, "AIDA") && !strings.HasPrefix(uid, "AROA") {
		return false
	}
	rest := uid[4:]
	if len(rest) < 16 {
		return false
	}
	for _, c := range rest {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// awsPartition reports whether p names an AWS partition: "aws", or "aws"
// followed by one or more "-" parts of lowercase letters.
func awsPartition(p string) bool {
	rest, ok := strings.CutPrefix(p, "aws")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	for _, part := range strings.Split(rest, "-")[1:] {
		if part == "" {
			return false
		}
		for _, c := range part {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return strings.HasPrefix(rest, "-")
}

// allDigits reports whether s is all decimal digits.
func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// AccountTypeKind is what an OCSF account.type_id says about where a user
// lives.
type AccountTypeKind struct {
	// Host is true for an account on a host or in a directory.
	Host bool
	// Provider is the slug of the provider whose principal the type names,
	// "" for none.
	Provider string
	// Caption is OCSF's caption for the type.
	Caption string
}

// AccountTypeKinds classifies every value of the OCSF 1.9.0 account.type_id
// enum (adr/0359). A test checks that no value of the enum is missing. Only
// a principal type names a provider. A container type (an AWS account, a GCP
// project, an Azure subscription), a SaaS type and the rest say nothing
// about the user.
var AccountTypeKinds = map[int]AccountTypeKind{
	0:  {Caption: "Unknown"},
	1:  {Host: true, Caption: "LDAP Account"},
	2:  {Host: true, Caption: "Windows Account"},
	3:  {Provider: "aws", Caption: "AWS IAM User"},
	4:  {Provider: "aws", Caption: "AWS IAM Role"},
	5:  {Provider: "gcp", Caption: "GCP Account"},
	6:  {Provider: "azure", Caption: "Azure AD Account"},
	7:  {Host: true, Caption: "Mac OS Account"},
	8:  {Caption: "Apple Account"},
	9:  {Host: true, Caption: "Linux Account"},
	10: {Caption: "AWS Account"},        // a container
	11: {Caption: "GCP Project"},        // a container
	12: {Caption: "OCI Compartment"},    // a container
	13: {Caption: "Azure Subscription"}, // a container
	14: {Caption: "Salesforce Account"},
	15: {Caption: "Google Workspace"},
	16: {Caption: "Servicenow Instance"},
	17: {Caption: "M365 Tenant"}, // a container
	18: {Caption: "Email Account"},
	19: {Host: true, Caption: "ActiveDirectory Account"},
	99: {Caption: "Other"},
}

// accountTypeCaptions is AccountTypeKinds as id → caption, the shape
// CaptionID reads.
func accountTypeCaptions() map[int]string {
	out := make(map[int]string, len(AccountTypeKinds))
	for id, k := range AccountTypeKinds {
		out[id] = k.Caption
	}
	return out
}

// AccountTypeID is the account.type_id an OCSF account object gives: the id
// when present, else the id OCSF gives the account.type caption, letter case
// aside (CaptionID). A caption OCSF does not have reads as Other, and no
// caption as Unknown.
func AccountTypeID(id any, caption string) int {
	if n, ok := WholeNumber(id); ok {
		return n
	}
	if strings.TrimSpace(caption) == "" {
		return Unknown
	}
	return CaptionID(accountTypeCaptions(), strings.TrimSpace(caption))
}

// IsCloudPrincipal reports whether the user of an evidence actor on a
// finding that names provider (a slug) is that provider's user (03 §4.12,
// adr/0359). finding is what the principal forms need from the finding.
// read reads a field of the actor by its path from the actor ("user.uid").
// The caller passes its own recorded reader, so the schema check sees which
// fields were read.
//
// The account type is account.type_id, else the account.type caption
// (AccountTypeID). A host or directory type says no, whatever else the actor
// carries. A principal type of the finding's own provider says yes. A
// principal type of another provider is no signal. Otherwise the id the
// principal would be keyed on (the provider's session row, else uid, else
// uid_alt) in the provider's principal form says yes. With no signal
// the answer is no: the actor is then read by the account rule (03 §7.2),
// as on a finding with no provider.
func IsCloudPrincipal(provider string, finding PrincipalContext, read func(path string) any) bool {
	var kind AccountTypeKind
	if typeID := read("user.account.type_id"); typeID != nil {
		kind = AccountTypeKinds[AccountTypeID(typeID, "")]
	} else {
		caption, _ := read("user.account.type").(string)
		kind = AccountTypeKinds[AccountTypeID(nil, caption)]
	}
	if kind.Host {
		return false
	}
	if kind.Provider != "" && kind.Provider == provider {
		return true
	}
	// The form is checked on the id the principal would be keyed on: the
	// session row's value when present, else uid, else uid_alt, as
	// durablePrincipalID in package capability picks it. An id that is not
	// the key proves nothing, and neither does a session value that does not
	// have the principal's form.
	finding.ActorAccountUID = scalarText(read("user.account.uid"))
	key := principalID(read)
	if path, ok := SessionScopedActors[provider]; ok {
		if issuer := scalarText(read(path)); issuer != "" {
			key = issuer
		}
	}
	form, ok := PrincipalForms[provider]
	return ok && form(key, finding)
}

// principalID is the user id a principal is keyed on, when no session row
// names one: uid, else uid_alt, trimmed. A whole number counts as an id, as
// it does for the key.
func principalID(read func(path string) any) string {
	for _, path := range []string{"user.uid", "user.uid_alt"} {
		if id := scalarText(read(path)); id != "" {
			return id
		}
	}
	return ""
}

// scalarText is a string trimmed, or a whole number in decimal (integerText),
// or "" for anything else.
func scalarText(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return integerText(v)
}
