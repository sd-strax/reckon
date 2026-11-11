package bundledadapters

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// TestSecurityLakeManifestMatchesExample pins the embedded manifest to the
// example the operator reads: they are the same file, byte for byte.
func TestSecurityLakeManifestMatchesExample(t *testing.T) {
	b, ok := Get("security-lake")
	if !ok {
		t.Fatal("security-lake is not bundled")
	}
	if b.Binary != "reckon-adapter-security-lake" {
		t.Errorf("Binary = %q", b.Binary)
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "adapters", "security-lake", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Manifest, example) {
		t.Error("internal/bundledadapters/manifests/security-lake.yaml differs from examples/adapters/security-lake/manifest.yaml")
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
