package server

import (
	"reflect"
	"testing"

	"github.com/sd-strax/reckon/capability"
)

// TestConfigSchemaView reads the fixture class's config descriptor into the
// form's fields, and a secret field's mark.
func TestConfigSchemaView(t *testing.T) {
	got := configSchemaView(capability.AdapterConfigSchema(capability.ClassFixture))
	if got == nil || !reflect.DeepEqual(got.Required, []string{"scenario"}) {
		t.Fatalf("required = %+v", got)
	}
	if p := got.Properties["scenario"]; p.Type != "string" || p.Description == "" || p.Secret {
		t.Errorf("scenario = %+v", p)
	}
	secret := configSchemaView(map[string]any{
		"properties": map[string]any{"token": map[string]any{"type": "string", "x-secret": true}},
		"required":   []string{"token"},
	})
	if !secret.Properties["token"].Secret || !reflect.DeepEqual(secret.Required, []string{"token"}) {
		t.Errorf("secret = %+v", secret)
	}
	if configSchemaView(nil) != nil {
		t.Error("no descriptor gave a view")
	}
}
