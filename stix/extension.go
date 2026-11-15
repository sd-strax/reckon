package stix

import (
	"embed"
	"encoding/json"
	"io/fs"
	"slices"

	"github.com/sd-strax/reckon/identity"
)

// SpecVersion is the STIX version every reckon object is written in.
const SpecVersion = "2.1"

// SchemaBaseURL is where the schema files are published: the package
// directory in the public repository.
const SchemaBaseURL = "https://raw.githubusercontent.com/sd-strax/reckon/main/stix/"

// definitionTime is the created and modified time of the publisher identity
// and of every version 1.0.0 definition.
const definitionTime = "2026-11-13T00:00:00.000Z"

// ExtensionType is a value of an extension definition's extension_types.
type ExtensionType string

// The extension types reckon defines.
const (
	NewSDO            ExtensionType = "new-sdo"
	NewSCO            ExtensionType = "new-sco"
	PropertyExtension ExtensionType = "property-extension"
)

// The names of reckon's property-extensions. The custom types' definitions
// are named after the type, as in identity.TypeHost.
const (
	ExtProvenance   = "reckon-provenance"
	ExtObservation  = "reckon-observation"
	ExtAttestation  = "reckon-attestation"
	ExtAccount      = "reckon-account"
	ExtPerson       = "reckon-person"
	ExtFile         = "reckon-file"
	ExtProcess      = "reckon-process"
	ExtEmailMessage = "reckon-email-message"
	ExtAssertion    = "reckon-assertion"
)

//go:embed schemas
var schemaFS embed.FS

// Identity is a STIX identity object.
type Identity struct {
	Type          string `json:"type"`
	SpecVersion   string `json:"spec_version"`
	ID            string `json:"id"`
	Created       string `json:"created"`
	Modified      string `json:"modified"`
	Name          string `json:"name"`
	IdentityClass string `json:"identity_class"`
}

// Publisher returns the identity of the organization that publishes reckon.
// It is the created_by_ref of every definition, and the only place that
// organization is named.
func Publisher() Identity {
	return Identity{
		Type:          "identity",
		SpecVersion:   SpecVersion,
		ID:            "identity--07fe8513-7a0c-416a-86ba-6f708cc68755",
		Created:       definitionTime,
		Modified:      definitionTime,
		Name:          "Hypredge Inc.",
		IdentityClass: "organization",
	}
}

// Definition is one reckon extension definition. Its JSON form, from
// MarshalJSON, is the STIX extension-definition object.
type Definition struct {
	// ID is the definition's STIX id, extension-definition--<UUIDv4>.
	ID string
	// Name is the reckon type or property-extension the definition declares.
	Name        string
	Description string
	// Version is the definition's semver version, which names its schema file.
	Version        string
	ExtensionTypes []ExtensionType
}

// SchemaPath is the schema file's path inside the package directory.
func (d Definition) SchemaPath() string {
	return "schemas/" + d.Name + "/" + d.Version + ".json"
}

// SchemaURL is the schema file's URL in the public repository. It is the
// definition's schema property and the schema's $id.
func (d Definition) SchemaURL() string {
	return SchemaBaseURL + d.SchemaPath()
}

// Schema returns the definition's JSON Schema, as embedded in the binary.
func (d Definition) Schema() ([]byte, error) {
	return schemaFS.ReadFile(d.SchemaPath())
}

// MarshalJSON writes the definition as a STIX extension-definition object.
func (d Definition) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type           string          `json:"type"`
		SpecVersion    string          `json:"spec_version"`
		ID             string          `json:"id"`
		Created        string          `json:"created"`
		Modified       string          `json:"modified"`
		CreatedByRef   string          `json:"created_by_ref"`
		Name           string          `json:"name"`
		Description    string          `json:"description"`
		Schema         string          `json:"schema"`
		Version        string          `json:"version"`
		ExtensionTypes []ExtensionType `json:"extension_types"`
	}{
		Type:           "extension-definition",
		SpecVersion:    SpecVersion,
		ID:             d.ID,
		Created:        definitionTime,
		Modified:       definitionTime,
		CreatedByRef:   Publisher().ID,
		Name:           d.Name,
		Description:    d.Description,
		Schema:         d.SchemaURL(),
		Version:        d.Version,
		ExtensionTypes: d.ExtensionTypes,
	})
}

// definitions lists every definition. Each id was generated once at random
// and must never change: a breaking change gets a new definition instead.
var definitions = []Definition{
	define(identity.TypeHypothesis, "63d968b4-ad6e-4935-9a8a-73844fc84465", NewSDO,
		"A hypothesis is a theory of what happened, which predictions test."),
	define(identity.TypePrediction, "bdb6459a-6735-4c55-befc-fdd27bd7a924", NewSDO,
		"A prediction is an observable consequence of a hypothesis, with the test that checks it."),
	define(identity.TypeInterpretation, "5f961080-3aa4-4749-97d6-dbb73956c8d9", NewSDO,
		"An interpretation is one recorded reasoning act, with who made it and what it reasoned over."),
	define(identity.TypeAction, "a20b8c4e-9962-4cf1-9a4c-223dc6fc3cd9", NewSDO,
		"An action is a state-changing operation against the world, with its authorization and execution."),
	define(identity.TypeNullObservation, "747f2bff-9f97-49b5-b998-2ba31bb4aa17", NewSDO,
		"A null observation records that a source with complete coverage answered a question with nothing."),
	define(identity.TypeHost, "7aa9a12c-441c-41e1-ab22-8bb028fd44e1", NewSCO,
		"A host is a machine that telemetry names."),
	define(identity.TypeScheduledTask, "c591a59e-f9c6-46b6-a960-9c81cad781ac", NewSCO,
		"A scheduled task is a job set to run on a host."),
	define(identity.TypeGroup, "5af54b63-6939-40a3-a50d-b03a3314cd99", NewSCO,
		"A group is a directory group that accounts are members of."),
	define(identity.TypeCloudAccount, "00fede09-6d0b-476c-9629-b7f9719f16e0", NewSCO,
		"A cloud account is the subscription, project or account that cloud resources and principals live in."),
	define(identity.TypeCloudResource, "9caa0041-a0ac-4d58-a8b3-ca1aacba7d11", NewSCO,
		"A cloud resource is an object a cloud control plane manages, such as a bucket or a role."),
	define(ExtProvenance, "d9cb6ba5-7f21-41e1-88bc-3fb28093fb6e", PropertyExtension,
		"Provenance says how an evidence object came to be: from which tool, through which normalizer, and whether directly or by inference."),
	define(ExtObservation, "3f256422-b1e1-4eab-b517-8a5e2ef879fc", PropertyExtension,
		"Observation fields are what an observed-data keeps from its OCSF event that STIX has no property for."),
	define(ExtAttestation, "7e5f42fd-8a20-4f42-9fe1-8bb610acacd8", PropertyExtension,
		"An attestation on a note is a person's answer to a prediction's test: which prediction, who was asked, how and when."),
	define(ExtAccount, "995fe152-3be8-4989-925d-66f84ff8c5cc", PropertyExtension,
		"Account fields are what a source says about a user account beyond the STIX user-account properties."),
	define(ExtPerson, "172a680d-cc8c-47e6-9562-fed23e4fa9ff", PropertyExtension,
		"Person fields are what a directory says about a person identity beyond the STIX identity properties."),
	define(ExtFile, "dd95cf33-8636-464c-b0f0-0a235b98052c", PropertyExtension,
		"File fields hold a file's full path, which the STIX file object has no property for."),
	define(ExtProcess, "f6373d83-b6b9-49e4-b16c-e300d5187038", PropertyExtension,
		"Process fields hold a process's name, which the STIX process object has no property for."),
	define(ExtEmailMessage, "d406ebfb-83e0-48cf-adc4-299269d886fe", PropertyExtension,
		"Email message fields mark a message known only by a source's own id, and hold that id and the source."),
	define(ExtAssertion, "7fd9b083-aa06-4623-b280-873699a3a3c6", PropertyExtension,
		"An assertion on a relationship records the person who asserted it, when and why, and its withdrawal."),
}

// define builds a version 1.0.0 definition.
func define(name, uuid string, typ ExtensionType, description string) Definition {
	return Definition{
		ID:             "extension-definition--" + uuid,
		Name:           name,
		Description:    description,
		Version:        "1.0.0",
		ExtensionTypes: []ExtensionType{typ},
	}
}

// Definitions returns every reckon extension definition.
func Definitions() []Definition {
	out := make([]Definition, len(definitions))
	for i, d := range definitions {
		out[i] = d.clone()
	}
	return out
}

// Lookup returns the definition of a reckon custom type, such as
// identity.TypeHost, or of a property-extension, such as ExtProvenance.
func Lookup(name string) (Definition, bool) {
	for _, d := range definitions {
		if d.Name == name {
			return d.clone(), true
		}
	}
	return Definition{}, false
}

// SchemaFS returns the embedded schema files, rooted at the package
// directory, so each definition's file is at its SchemaPath.
func SchemaFS() fs.FS { return schemaFS }

func (d Definition) clone() Definition {
	d.ExtensionTypes = slices.Clone(d.ExtensionTypes)
	return d
}
