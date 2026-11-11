package bundledadapters

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// TestAWSSecurityLakeManifestMatchesExample pins the embedded manifest to the
// example the operator reads: they are the same file, byte for byte.
func TestAWSSecurityLakeManifestMatchesExample(t *testing.T) {
	b, ok := Get("aws-security-lake")
	if !ok {
		t.Fatal("aws-security-lake is not bundled")
	}
	if b.Binary != "reckon-adapter-aws-security-lake" {
		t.Errorf("Binary = %q", b.Binary)
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "adapters", "aws-security-lake", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Manifest, example) {
		t.Error("internal/bundledadapters/manifests/aws-security-lake.yaml differs from examples/adapters/aws-security-lake/manifest.yaml")
	}
}

// TestBundledManifestsLoad installs every bundled manifest into a scratch
// directory and loads it as the host would.
func TestBundledManifestsLoad(t *testing.T) {
	for _, name := range Names() {
		b, _ := Get(name)
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), b.Manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		inst, err := adapterplugin.Load(dir)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if inst.Manifest.Name != name {
			t.Errorf("%s: manifest name = %q", name, inst.Manifest.Name)
		}
	}
}

// TestRegistryNamesAgree pins, for every bundled adapter, that its registry
// key is its name everywhere the name is a contract (adr/0331): the entry's
// Name, the binary reckon-adapter-<name>, and the embedded manifest's name
// and exec.
func TestRegistryNamesAgree(t *testing.T) {
	for key, b := range registry {
		if b.Name != key {
			t.Errorf("%s: Name = %q", key, b.Name)
		}
		if want := "reckon-adapter-" + key; b.Binary != want {
			t.Errorf("%s: Binary = %q, want %q", key, b.Binary, want)
		}
		var m adapterplugin.Manifest
		if err := yaml.Unmarshal(b.Manifest, &m); err != nil {
			t.Errorf("%s: manifest: %v", key, err)
			continue
		}
		if m.Name != key {
			t.Errorf("%s: manifest name = %q", key, m.Name)
		}
		if want := []string{"./" + b.Binary}; !reflect.DeepEqual(m.Exec, want) {
			t.Errorf("%s: manifest exec = %q, want %q", key, m.Exec, want)
		}
	}
}
