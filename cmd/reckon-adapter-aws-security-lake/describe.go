package main

import (
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

const (
	opActivity = "cloudtrail.api_activity"
	opHistory  = "cloudtrail.resource_history"
	opContext  = "config.resource_context"

	verbActivity = "get_cloud_api_activity"
	verbHistory  = "get_cloud_resource_history"
	verbContext  = "get_cloud_resource_context"

	adapterName = "aws-security-lake"
)

// bindingPriority is the default priority of the bindings this adapter
// suggests: above the bundled fixture's (100), so when the adapter is enabled
// beside the cloud demo scenario the live source answers, as servicenow's
// bindings do.
const bindingPriority = 200

// describe is the reckon surface this adapter serves (11 §4.2). It runs
// before configure in the handshake, so it cannot know config_enabled: it
// always offers all three verbs, and an instance configured without AWS
// Config declines config.resource_context at invoke with FALLTHROUGH. The
// verb descriptors are the engine's own catalog entries, so they cannot
// drift from it.
func describe() adapterplugin.DescribeResult {
	catalog := capability.DefaultCatalog()
	var verbs []capability.CapabilityDescriptor
	for _, v := range []string{verbActivity, verbHistory, verbContext} {
		if d, ok := catalog.Descriptor(v); ok {
			verbs = append(verbs, d)
		}
	}
	return adapterplugin.DescribeResult{
		Verbs: verbs,
		Operations: []adapterplugin.OperationSchema{
			{Name: opActivity, Params: object(map[string]any{
				"principal": object(map[string]any{"name": strSchema("principal ARN or name, matched against actor.user.uid, actor.session.issuer and actor.user.name where the table carries them"), "credential": strSchema("access key id, matched against actor.user.credential_uid and actor.session.credential_uid where the table carries them")}),
				"resource":  resourceSchema(),
				"account":   object(map[string]any{"uid": strSchema("account id the call ran in (matched on the accountid column)"), "provider": strSchema("provider slug; a provider other than aws is declined (FALLTHROUGH)")}),
				"operation": strSchema("API operation, e.g. PutBucketPolicy"),
				"service":   strSchema("service, e.g. s3.amazonaws.com"),
				"outcome":   map[string]any{"type": "string", "enum": []any{"SUCCESS", "FAILURE", "ALL"}},
				"limit":     limitSchema(),
				"window":    windowSchema(),
			}, "window")},
			{Name: opHistory, Params: object(map[string]any{
				"resource": resourceSchema(),
				"limit":    limitSchema(),
				"window":   windowSchema(),
			}, "resource", "window")},
			{Name: opContext, Params: object(map[string]any{
				"resource": resourceSchema(),
			}, "resource")},
		},
		DefaultReadBindings: []adapterplugin.ReadBinding{
			{Verb: verbActivity, Adapter: adapterName, Operation: opActivity, Priority: bindingPriority,
				Params: map[string]any{
					"principal": map[string]any{ //nolint:gosec // G101: template references to the entity's name and access key id, not credentials
						"name":       "${entity.user.name?}",
						"credential": "${entity.user.credential_uid?}",
					},
					"resource": resourceBinding(),
					"account": map[string]any{
						"uid":      "${entity.cloud_account.uid?}",
						"provider": "${entity.cloud_account.provider? | lower}",
					},
					"operation": "${operation?}",
					"service":   "${service?}",
					"outcome":   `${outcome ?? "ALL"}`,
					"limit":     "${limit ?? 200}",
					"window":    windowBinding(),
				}},
			{Verb: verbHistory, Adapter: adapterName, Operation: opHistory, Priority: bindingPriority,
				Params: map[string]any{
					"resource": resourceBinding(),
					"limit":    "${limit ?? 200}",
					"window":   windowBinding(),
				}},
			{Verb: verbContext, Adapter: adapterName, Operation: opContext, Priority: bindingPriority,
				Params: map[string]any{"resource": resourceBinding()}},
		},
		ConfigSchema: configSchema(),
		SchemaSource: schemaSource(),
	}
}

// Profile identity (11 §4.4): the Security Lake CloudTrail management-events
// source at source version 2.0, the table suffix _2_0.
const (
	profileName    = "cloudtrail-mgmt-2.0"
	profileStore   = "aws-security-lake/cloud_trail_mgmt"
	profileVersion = "2.0"
)

// schemaSource declares the query kind (11 §4.4): the adapter names columns
// in Athena SQL over a table the lake owns, discovering the table's schema
// and reading the candidate fields it carries. Its one profile lists every
// field the adapter may read (candidateFields); the recording under
// testdata/profiles/cloudtrail-mgmt-2.0 is the table at that version.
func schemaSource() *adapterplugin.SchemaSource {
	return &adapterplugin.SchemaSource{
		Kind: adapterplugin.SchemaSourceQuery,
		Profiles: []adapterplugin.SchemaProfile{{
			Name:    profileName,
			Store:   profileStore,
			Version: profileVersion,
			Fields:  candidateFields(),
		}},
	}
}

// resourceBinding forwards every identity scope the entity may carry
// (examples/capability/exposed-bucket.yaml), so a scoped question never
// broadens.
func resourceBinding() map[string]any {
	return map[string]any{
		"uid":         "${entity.cloud_resource.uid?}",
		"name":        "${entity.cloud_resource.name?}",
		"provider":    "${entity.cloud_resource.provider? | lower}",
		"type":        "${entity.cloud_resource.type?}",
		"account_uid": "${entity.cloud_resource.account_uid?}",
		"region":      "${entity.cloud_resource.region?}",
		"zone":        "${entity.cloud_resource.zone?}",
		"group":       "${entity.cloud_resource.group?}",
		"namespace":   "${entity.cloud_resource.namespace?}",
	}
}

func windowBinding() map[string]any {
	return map[string]any{"from": "${window.from | iso8601}", "to": "${window.to | iso8601}"}
}

func configSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"region", "athena_workgroup", "output_location", "access_key_id", "secret_access_key"},
		"properties": map[string]any{
			"region":            strSchema("AWS region of the subscriber (Athena, Glue, Config)"),
			"glue_database":     strSchema("default amazon_security_lake_glue_db_<region>"),
			"cloudtrail_table":  strSchema("default amazon_security_lake_table_<region>_cloud_trail_mgmt_2_0"),
			"athena_workgroup":  strSchema("Athena workgroup the queries run in"),
			"output_location":   strSchema("s3://… for Athena results"),
			"account_uids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "restrict to these accounts; empty = every account in the lake"},
			"role_arn":          strSchema("optional: assume this role for the queries"),
			"access_key_id":     secretSchema("AWS access key id, given as a secret reference (keychain:// / env:// / vault://)"),
			"secret_access_key": secretSchema("AWS secret access key, given as a secret reference"),
			"session_token":     secretSchema("optional AWS session token, given as a secret reference"),
			"query_timeout":     strSchema("ISO 8601 duration, default PT60S"),
			"config_enabled":    map[string]any{"type": "boolean", "description": "answer get_cloud_resource_context from AWS Config (default true)"},
		},
	}
}

func resourceSchema() map[string]any {
	props := map[string]any{}
	for _, k := range []string{"uid", "name", "provider", "type", "account_uid", "region", "zone", "group", "namespace"} {
		props[k] = map[string]any{"type": "string"}
	}
	return object(props)
}

func windowSchema() map[string]any {
	return object(map[string]any{
		"from": map[string]any{"type": "string", "format": "date-time"},
		"to":   map[string]any{"type": "string", "format": "date-time"},
	}, "from", "to")
}

func limitSchema() map[string]any {
	return map[string]any{"type": []any{"integer", "string"}, "description": "max rows, 1..1000 (default 200)"}
}

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		s["required"] = req
	}
	return s
}

func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func secretSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "x-secret": true, "description": desc}
}
